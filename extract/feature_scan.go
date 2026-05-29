package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nmeilick/go-i18n/locale/cldr"
)

const defaultI18NModule = "github.com/nmeilick/go-i18n"

// FeatureScanMode controls package discovery for CLDR feature auto-detection.
type FeatureScanMode string

const (
	FeatureScanAuto      FeatureScanMode = "auto"
	FeatureScanModule    FeatureScanMode = "module"
	FeatureScanImporters FeatureScanMode = "importers"
)

// FeatureRule maps an application wrapper function to one or more CLDR
// features.
type FeatureRule struct {
	Target   string           `json:"target"`
	Features []cldr.FeatureID `json:"features"`
}

// FeatureScanOptions configures CLDR feature auto-detection.
type FeatureScanOptions struct {
	Mode           FeatureScanMode
	ModuleDir      string
	Entrypoints    []string
	ScanRoots      []string
	OutputPaths    []string
	FeatureExtra   []cldr.FeatureID
	FeatureExclude []cldr.FeatureID
	Rules          []FeatureRule
	I18NModulePath string
}

// FeatureUsage is one source location that caused a feature to be included.
type FeatureUsage struct {
	Feature cldr.FeatureID `json:"feature"`
	Package string         `json:"package"`
	Path    string         `json:"path"`
	Line    int            `json:"line"`
	Symbol  string         `json:"symbol"`
	Reason  string         `json:"reason"`
}

// FeatureScanPackage is a scanned module-local package.
type FeatureScanPackage struct {
	ImportPath string `json:"import_path"`
	Dir        string `json:"dir"`
	Reason     string `json:"reason"`
}

// FeatureScanWarning is a deterministic, redaction-safe scanner warning.
type FeatureScanWarning struct {
	Code    string `json:"code"`
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// FeatureScanReport explains an auto-detected CLDR feature set.
type FeatureScanReport struct {
	Mode       FeatureScanMode      `json:"mode"`
	ModuleDir  string               `json:"module_dir,omitempty"`
	ModulePath string               `json:"module_path,omitempty"`
	Features   []cldr.FeatureID     `json:"features"`
	Packages   []FeatureScanPackage `json:"packages,omitempty"`
	Usages     []FeatureUsage       `json:"usages,omitempty"`
	Warnings   []FeatureScanWarning `json:"warnings,omitempty"`
}

// ScanCLDRFeatures detects CLDR features used by module-local source code.
func ScanCLDRFeatures(ctx context.Context, opts FeatureScanOptions) (FeatureScanReport, error) {
	opts = normalizeFeatureScanOptions(opts)
	module, err := moduleInfo(ctx, opts.ModuleDir)
	if err != nil {
		return FeatureScanReport{}, err
	}
	pkgs, err := loadPackages(ctx, module.Dir, opts.ScanRoots)
	if err != nil {
		return FeatureScanReport{}, err
	}
	local := localPackages(pkgs, module)
	targets, reasons, mode, err := selectScanPackages(ctx, module, pkgs, local, opts)
	if err != nil {
		return FeatureScanReport{}, err
	}
	outputDirs := outputDirs(opts.OutputPaths, module.Dir)
	report := FeatureScanReport{Mode: mode, ModuleDir: module.Dir, ModulePath: module.Path}
	features := map[cldr.FeatureID]bool{}
	for _, feature := range opts.FeatureExtra {
		if feature != "" {
			features[feature] = true
			report.Usages = append(report.Usages, FeatureUsage{Feature: feature, Reason: "feature_extra"})
		}
	}
	for _, importPath := range sortedKeys(targets) {
		pkg := pkgs[importPath]
		if outputDirs[pkg.Dir] {
			continue
		}
		report.Packages = append(report.Packages, FeatureScanPackage{
			ImportPath: importPath,
			Dir:        pkg.Dir,
			Reason:     reasons[importPath],
		})
		usages, warnings, err := scanFeaturePackage(pkg, opts)
		if err != nil {
			return FeatureScanReport{}, err
		}
		report.Warnings = append(report.Warnings, warnings...)
		for _, usage := range usages {
			features[usage.Feature] = true
			report.Usages = append(report.Usages, usage)
		}
	}
	for _, excluded := range opts.FeatureExclude {
		if features[excluded] {
			report.Warnings = append(report.Warnings, FeatureScanWarning{
				Code:    "feature_excluded",
				Message: fmt.Sprintf("detected feature %s was excluded by configuration", excluded),
			})
			delete(features, excluded)
		}
	}
	report.Features = sortedFeatureSet(features)
	sortFeatureUsages(report.Usages)
	sortFeatureWarnings(report.Warnings)
	return report, nil
}

type moduleContext struct {
	Dir  string
	Path string
}

type goPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	Imports    []string
	Module     *struct {
		Path string
		Dir  string
		Main bool
	}
	Error *struct {
		Err string
	}
}

func normalizeFeatureScanOptions(opts FeatureScanOptions) FeatureScanOptions {
	if opts.Mode == "" {
		opts.Mode = FeatureScanAuto
	}
	if opts.ModuleDir == "" {
		opts.ModuleDir = "."
	}
	if opts.I18NModulePath == "" {
		opts.I18NModulePath = defaultI18NModule
	}
	return opts
}

func moduleInfo(ctx context.Context, dir string) (moduleContext, error) {
	gomod, err := runGo(ctx, dir, "env", "GOMOD")
	if err != nil {
		return moduleContext{}, err
	}
	gomod = strings.TrimSpace(gomod)
	if gomod == "" || gomod == os.DevNull {
		return moduleContext{}, fmt.Errorf("feature scan requires a Go module; go env GOMOD returned %q", gomod)
	}
	modPath, err := runGo(ctx, filepath.Dir(gomod), "list", "-m", "-f", "{{.Path}}")
	if err != nil {
		return moduleContext{}, err
	}
	return moduleContext{Dir: filepath.Dir(gomod), Path: strings.TrimSpace(modPath)}, nil
}

func loadPackages(ctx context.Context, moduleDir string, roots []string) (map[string]goPackage, error) {
	patterns := []string{"./..."}
	if len(roots) > 0 {
		patterns = normalizePackagePatterns(roots, moduleDir)
	}
	args := append([]string{"list", "-e", "-json"}, patterns...)
	out, err := runGo(ctx, moduleDir, args...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(out))
	pkgs := map[string]goPackage{}
	for {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse go list output: %w", err)
		}
		if pkg.ImportPath != "" && pkg.Dir != "" && pkg.Error == nil {
			pkgs[pkg.ImportPath] = pkg
		}
	}
	return pkgs, nil
}

func normalizePackagePatterns(roots []string, moduleDir string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if filepath.IsAbs(root) {
			if rel, err := filepath.Rel(moduleDir, root); err == nil && !strings.HasPrefix(rel, "..") {
				root = "./" + filepath.ToSlash(rel)
			}
		}
		out = append(out, root)
	}
	if len(out) == 0 {
		return []string{"./..."}
	}
	return out
}

func selectScanPackages(ctx context.Context, module moduleContext, pkgs map[string]goPackage, local map[string]bool, opts FeatureScanOptions) (map[string]bool, map[string]string, FeatureScanMode, error) {
	if len(opts.ScanRoots) > 0 {
		targets := map[string]bool{}
		reasons := map[string]string{}
		for importPath := range pkgs {
			if local[importPath] {
				targets[importPath] = true
				reasons[importPath] = "scan_root"
			}
		}
		return targets, reasons, opts.Mode, nil
	}
	if len(opts.Entrypoints) > 0 {
		seeds, err := listEntrypoints(ctx, module, opts.Entrypoints)
		if err != nil {
			return nil, nil, opts.Mode, err
		}
		targets := packageClosure(seeds, pkgs, local)
		return targets, mapReasons(targets, "entrypoint"), opts.Mode, nil
	}
	switch opts.Mode {
	case FeatureScanModule:
		targets := map[string]bool{}
		reasons := map[string]string{}
		for importPath := range local {
			targets[importPath] = true
			reasons[importPath] = "module"
		}
		return targets, reasons, opts.Mode, nil
	case FeatureScanImporters:
		targets := importerTargets(pkgs, local, opts.I18NModulePath)
		return targets, mapReasons(targets, "importer"), opts.Mode, nil
	case FeatureScanAuto, "":
		mains := map[string]bool{}
		for importPath, pkg := range pkgs {
			if local[importPath] && pkg.Name == "main" {
				mains[importPath] = true
			}
		}
		if len(mains) > 0 {
			targets := packageClosure(mains, pkgs, local)
			return targets, mapReasons(targets, "entrypoint"), FeatureScanAuto, nil
		}
		targets := importerTargets(pkgs, local, opts.I18NModulePath)
		return targets, mapReasons(targets, "importer_fallback"), FeatureScanAuto, nil
	default:
		return nil, nil, opts.Mode, fmt.Errorf("unknown feature scan mode %q", opts.Mode)
	}
}

func listEntrypoints(ctx context.Context, module moduleContext, patterns []string) (map[string]bool, error) {
	args := append([]string{"list", "-e", "-json"}, normalizePackagePatterns(patterns, module.Dir)...)
	out, err := runGo(ctx, module.Dir, args...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(out))
	seeds := map[string]bool{}
	for {
		var pkg goPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parse entrypoint go list output: %w", err)
		}
		if pkg.ImportPath != "" && isLocalPackage(pkg, module) {
			seeds[pkg.ImportPath] = true
		}
	}
	return seeds, nil
}

func localPackages(pkgs map[string]goPackage, module moduleContext) map[string]bool {
	local := map[string]bool{}
	for importPath, pkg := range pkgs {
		if isLocalPackage(pkg, module) {
			local[importPath] = true
		}
	}
	return local
}

func isLocalPackage(pkg goPackage, module moduleContext) bool {
	if pkg.Module != nil && pkg.Module.Main {
		return true
	}
	if pkg.Module != nil && filepath.Clean(pkg.Module.Dir) == filepath.Clean(module.Dir) {
		return true
	}
	return pkg.ImportPath == module.Path || strings.HasPrefix(pkg.ImportPath, module.Path+"/")
}

func packageClosure(seeds map[string]bool, pkgs map[string]goPackage, local map[string]bool) map[string]bool {
	out := map[string]bool{}
	var visit func(string)
	visit = func(importPath string) {
		if out[importPath] || !local[importPath] {
			return
		}
		out[importPath] = true
		for _, dep := range pkgs[importPath].Imports {
			if local[dep] {
				visit(dep)
			}
		}
	}
	for importPath := range seeds {
		visit(importPath)
	}
	return out
}

func importerTargets(pkgs map[string]goPackage, local map[string]bool, modulePath string) map[string]bool {
	out := map[string]bool{}
	memo := map[string]bool{}
	var depends func(string, map[string]bool) bool
	depends = func(importPath string, seen map[string]bool) bool {
		if v, ok := memo[importPath]; ok {
			return v
		}
		if seen[importPath] {
			return false
		}
		seen[importPath] = true
		for _, dep := range pkgs[importPath].Imports {
			if dep == modulePath || strings.HasPrefix(dep, modulePath+"/") {
				memo[importPath] = true
				return true
			}
			if local[dep] && depends(dep, seen) {
				memo[importPath] = true
				return true
			}
		}
		memo[importPath] = false
		return false
	}
	for importPath := range local {
		if depends(importPath, map[string]bool{}) {
			out[importPath] = true
		}
	}
	return out
}

func mapReasons(targets map[string]bool, reason string) map[string]string {
	out := map[string]string{}
	for key := range targets {
		out[key] = reason
	}
	return out
}

func outputDirs(paths []string, moduleDir string) map[string]bool {
	out := map[string]bool{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(moduleDir, path)
		}
		out[filepath.Clean(filepath.Dir(path))] = true
	}
	return out
}

func scanFeaturePackage(pkg goPackage, opts FeatureScanOptions) ([]FeatureUsage, []FeatureScanWarning, error) {
	var usages []FeatureUsage
	var warnings []FeatureScanWarning
	for _, name := range pkg.GoFiles {
		path := filepath.Join(pkg.Dir, name)
		fileUsages, fileWarnings, err := scanFeatureFile(pkg, path, opts)
		if err != nil {
			return nil, nil, err
		}
		usages = append(usages, fileUsages...)
		warnings = append(warnings, fileWarnings...)
	}
	return usages, warnings, nil
}

func scanFeatureFile(pkg goPackage, path string, opts FeatureScanOptions) ([]FeatureUsage, []FeatureScanWarning, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, nil, err
	}
	imports := importAliases(file)
	var usages []FeatureUsage
	var warnings []FeatureScanWarning
	addUsage := func(pos token.Pos, symbol, reason string, features []cldr.FeatureID) {
		position := fset.Position(pos)
		for _, feature := range features {
			usages = append(usages, FeatureUsage{
				Feature: feature,
				Package: pkg.ImportPath,
				Path:    filepath.ToSlash(path),
				Line:    position.Line,
				Symbol:  symbol,
				Reason:  reason,
			})
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if symbol, features, ok := formatterMethodCall(n.Fun, imports); ok {
				addUsage(n.Pos(), symbol, "known_formatter_method", features)
			}
			symbols := callSymbols(n.Fun, imports)
			for _, symbol := range symbols {
				if features, ok := builtinFeatureSymbols(symbol); ok {
					addUsage(n.Pos(), symbol, "known_api", features)
				}
				for _, rule := range opts.Rules {
					if ruleMatches(rule.Target, symbol) {
						addUsage(n.Pos(), symbol, "wrapper_rule", rule.Features)
					}
				}
			}
		case *ast.CompositeLit:
			symbols := typeSymbols(n.Type, imports)
			for _, symbol := range symbols {
				if features, ok := builtinFeatureSymbols(symbol); ok {
					addUsage(n.Pos(), symbol, "known_spec", features)
				}
			}
		}
		return true
	})
	return dedupeFeatureUsages(usages), warnings, nil
}

func formatterMethodCall(expr ast.Expr, imports map[string]string) (string, []cldr.FeatureID, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	features, ok := formatterMethodFeatures(sel.Sel.Name)
	if !ok {
		return "", nil, false
	}
	switch receiver := sel.X.(type) {
	case *ast.CallExpr:
		for _, symbol := range callSymbols(receiver.Fun, imports) {
			if isFormatterConstructorSymbol(symbol) {
				return symbol + "." + sel.Sel.Name, features, true
			}
		}
	case *ast.CompositeLit:
		for _, symbol := range typeSymbols(receiver.Type, imports) {
			if isFormatterTypeSymbol(symbol) {
				return symbol + "." + sel.Sel.Name, features, true
			}
		}
	}
	return "", nil, false
}

func formatterMethodFeatures(method string) ([]cldr.FeatureID, bool) {
	switch method {
	case "FormatNumber":
		return []cldr.FeatureID{cldr.FeatureNumbersDecimal, cldr.FeatureNumbersSymbols}, true
	case "FormatCurrency":
		return []cldr.FeatureID{
			cldr.FeatureNumbersDecimal,
			cldr.FeatureNumbersSymbols,
			cldr.FeatureCurrenciesFractions,
			cldr.FeatureCurrenciesSymbols,
			cldr.FeatureCurrenciesNarrowSymbols,
		}, true
	case "FormatDateTime":
		return []cldr.FeatureID{
			cldr.FeatureDatesGregorianPatterns,
			cldr.FeatureDatesGregorianNames,
			cldr.FeatureDatesGregorianDayPeriods,
		}, true
	case "FormatDateTimeInterval":
		return []cldr.FeatureID{
			cldr.FeatureDatesGregorianPatterns,
			cldr.FeatureDatesGregorianNames,
			cldr.FeatureDatesGregorianDayPeriods,
			cldr.FeatureDatesGregorianIntervals,
		}, true
	case "FormatList":
		return []cldr.FeatureID{cldr.FeatureListsPatterns}, true
	case "FormatUnit":
		return []cldr.FeatureID{cldr.FeatureNumbersDecimal, cldr.FeatureNumbersSymbols, cldr.FeaturePluralsCardinal, cldr.FeatureUnitsDurationCore}, true
	case "FormatDuration":
		return []cldr.FeatureID{
			cldr.FeatureNumbersDecimal,
			cldr.FeatureNumbersSymbols,
			cldr.FeatureListsPatterns,
			cldr.FeaturePluralsCardinal,
			cldr.FeatureUnitsDurationCore,
		}, true
	case "FormatPeriod":
		return []cldr.FeatureID{
			cldr.FeatureNumbersDecimal,
			cldr.FeatureNumbersSymbols,
			cldr.FeatureListsPatterns,
			cldr.FeaturePluralsCardinal,
			cldr.FeatureUnitsDurationCore,
		}, true
	case "FormatRelative", "FormatRelativeTime":
		return []cldr.FeatureID{
			cldr.FeatureNumbersDecimal,
			cldr.FeatureNumbersSymbols,
			cldr.FeaturePluralsCardinal,
			cldr.FeatureDatesRelativeTime,
		}, true
	}
	return nil, false
}

func isFormatterConstructorSymbol(symbol string) bool {
	short := shortImportSymbol(symbol)
	return short == "locale.DefaultFormatter" || short == "locale.NewCLDRFormatter"
}

func isFormatterTypeSymbol(symbol string) bool {
	return shortImportSymbol(symbol) == "locale.CLDRFormatter"
}

func shortImportSymbol(symbol string) string {
	if slash := strings.LastIndex(symbol, "/"); slash >= 0 {
		return symbol[slash+1:]
	}
	return symbol
}

func importAliases(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil {
			out[imp.Name.Name] = path
			continue
		}
		out[filepath.Base(path)] = path
	}
	return out
}

func callSymbols(expr ast.Expr, imports map[string]string) []string {
	switch x := expr.(type) {
	case *ast.Ident:
		if path := imports["."]; path != "" {
			return []string{path + "." + x.Name, x.Name}
		}
		return []string{x.Name}
	case *ast.SelectorExpr:
		if ident, ok := x.X.(*ast.Ident); ok {
			if path := imports[ident.Name]; path != "" {
				return []string{path + "." + x.Sel.Name, ident.Name + "." + x.Sel.Name, x.Sel.Name}
			}
			return []string{ident.Name + "." + x.Sel.Name, x.Sel.Name}
		}
		return []string{x.Sel.Name}
	default:
		return nil
	}
}

func typeSymbols(expr ast.Expr, imports map[string]string) []string {
	switch x := expr.(type) {
	case *ast.Ident:
		if path := imports["."]; path != "" {
			return []string{path + "." + x.Name, x.Name}
		}
		return []string{x.Name}
	case *ast.SelectorExpr:
		if ident, ok := x.X.(*ast.Ident); ok {
			if path := imports[ident.Name]; path != "" {
				return []string{path + "." + x.Sel.Name, ident.Name + "." + x.Sel.Name, x.Sel.Name}
			}
			return []string{ident.Name + "." + x.Sel.Name, x.Sel.Name}
		}
		return []string{x.Sel.Name}
	default:
		return nil
	}
}

func builtinFeatureSymbols(symbol string) ([]cldr.FeatureID, bool) {
	if features, ok := builtinFeatureMap()[symbol]; ok {
		return features, true
	}
	if slash := strings.LastIndex(symbol, "/"); slash >= 0 {
		short := symbol[slash+1:]
		switch {
		case strings.HasPrefix(short, "i18n."), strings.HasPrefix(short, "locale."):
			if features, ok := builtinFeatureMap()[short]; ok {
				return features, true
			}
		}
	}
	return nil, false
}

func builtinFeatureMap() map[string][]cldr.FeatureID {
	number := []cldr.FeatureID{cldr.FeatureNumbersDecimal, cldr.FeatureNumbersSymbols}
	percent := []cldr.FeatureID{cldr.FeatureNumbersPercent, cldr.FeatureNumbersSymbols}
	currency := []cldr.FeatureID{
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersSymbols,
		cldr.FeatureCurrenciesFractions,
		cldr.FeatureCurrenciesSymbols,
		cldr.FeatureCurrenciesNarrowSymbols,
	}
	datetime := []cldr.FeatureID{
		cldr.FeatureDatesGregorianPatterns,
		cldr.FeatureDatesGregorianNames,
		cldr.FeatureDatesGregorianDayPeriods,
	}
	compact := []cldr.FeatureID{
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersSymbols,
		cldr.FeaturePluralsCardinal,
		cldr.FeatureNumbersCompactDecimal,
	}
	list := []cldr.FeatureID{cldr.FeatureListsPatterns}
	duration := []cldr.FeatureID{
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersSymbols,
		cldr.FeatureListsPatterns,
		cldr.FeaturePluralsCardinal,
		cldr.FeatureUnitsDurationCore,
	}
	relative := []cldr.FeatureID{
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersSymbols,
		cldr.FeaturePluralsCardinal,
		cldr.FeatureDatesRelativeTime,
	}
	interval := append(append([]cldr.FeatureID{}, datetime...), cldr.FeatureDatesGregorianIntervals)
	displayNames := []cldr.FeatureID{
		cldr.FeatureDisplayNamesLanguages,
		cldr.FeatureDisplayNamesTerritories,
		cldr.FeatureDisplayNamesScripts,
		cldr.FeatureDisplayNamesCalendars,
	}
	profile := []cldr.FeatureID{cldr.FeatureProfileDefaults, cldr.FeatureBCP47Extensions}
	return map[string][]cldr.FeatureID{
		defaultI18NModule + "/i18n.Number":                 number,
		defaultI18NModule + "/i18n.Percent":                percent,
		defaultI18NModule + "/i18n.Compact":                compact,
		defaultI18NModule + "/i18n.Currency":               currency,
		defaultI18NModule + "/i18n.Date":                   datetime,
		defaultI18NModule + "/i18n.Time":                   datetime,
		defaultI18NModule + "/i18n.DateTime":               datetime,
		defaultI18NModule + "/i18n.DateInterval":           interval,
		defaultI18NModule + "/i18n.TimeInterval":           interval,
		defaultI18NModule + "/i18n.DateTimeInterval":       interval,
		defaultI18NModule + "/i18n.Duration":               duration,
		defaultI18NModule + "/i18n.Period":                 duration,
		defaultI18NModule + "/i18n.Relative":               relative,
		defaultI18NModule + "/i18n.RelativeTime":           relative,
		defaultI18NModule + "/i18n.List":                   list,
		defaultI18NModule + "/i18n.Unit":                   duration,
		defaultI18NModule + "/locale.NumberSpec":           number,
		defaultI18NModule + "/locale.CurrencySpec":         currency,
		defaultI18NModule + "/locale.DateTimeSpec":         datetime,
		defaultI18NModule + "/locale.DateTimeIntervalSpec": interval,
		defaultI18NModule + "/locale.ListSpec":             list,
		defaultI18NModule + "/locale.UnitSpec":             duration,
		defaultI18NModule + "/locale.DurationSpec":         duration,
		defaultI18NModule + "/locale.PeriodSpec":           duration,
		defaultI18NModule + "/locale.RelativeSpec":         relative,
		defaultI18NModule + "/locale.RelativeTimeSpec":     relative,
		defaultI18NModule + "/locale.NewResolver":          profile,
		defaultI18NModule + "/locale.GeneratedDefaults":    profile,
		defaultI18NModule + "/locale.DisplayNames":         displayNames,
		defaultI18NModule + "/locale.NewDisplayNames":      displayNames,
		"i18n.Number":                 number,
		"i18n.Percent":                percent,
		"i18n.Compact":                compact,
		"i18n.Currency":               currency,
		"i18n.Date":                   datetime,
		"i18n.Time":                   datetime,
		"i18n.DateTime":               datetime,
		"i18n.DateInterval":           interval,
		"i18n.TimeInterval":           interval,
		"i18n.DateTimeInterval":       interval,
		"i18n.Duration":               duration,
		"i18n.Period":                 duration,
		"i18n.Relative":               relative,
		"i18n.RelativeTime":           relative,
		"i18n.List":                   list,
		"i18n.Unit":                   duration,
		"locale.NumberSpec":           number,
		"locale.CurrencySpec":         currency,
		"locale.DateTimeSpec":         datetime,
		"locale.DateTimeIntervalSpec": interval,
		"locale.ListSpec":             list,
		"locale.UnitSpec":             duration,
		"locale.DurationSpec":         duration,
		"locale.PeriodSpec":           duration,
		"locale.RelativeSpec":         relative,
		"locale.RelativeTimeSpec":     relative,
		"locale.NewResolver":          profile,
		"locale.GeneratedDefaults":    profile,
		"locale.DisplayNames":         displayNames,
		"locale.NewDisplayNames":      displayNames,
	}
}

func ruleMatches(target, symbol string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	return symbol == target || strings.HasSuffix(symbol, "."+target) || strings.HasSuffix(symbol, "/"+target)
}

func dedupeFeatureUsages(in []FeatureUsage) []FeatureUsage {
	seen := map[string]bool{}
	out := []FeatureUsage{}
	for _, usage := range in {
		key := string(usage.Feature) + "\x00" + usage.Path + "\x00" + strconv.Itoa(usage.Line) + "\x00" + usage.Reason
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, usage)
	}
	sortFeatureUsages(out)
	return out
}

func sortFeatureUsages(usages []FeatureUsage) {
	sort.Slice(usages, func(i, j int) bool {
		a, b := usages[i], usages[j]
		return string(a.Feature)+"\x00"+a.Path+"\x00"+strconv.Itoa(a.Line)+"\x00"+a.Symbol <
			string(b.Feature)+"\x00"+b.Path+"\x00"+strconv.Itoa(b.Line)+"\x00"+b.Symbol
	})
}

func sortFeatureWarnings(warnings []FeatureScanWarning) {
	sort.Slice(warnings, func(i, j int) bool {
		a, b := warnings[i], warnings[j]
		return a.Code+"\x00"+a.Path+"\x00"+strconv.Itoa(a.Line) < b.Code+"\x00"+b.Path+"\x00"+strconv.Itoa(b.Line)
	})
}

func sortedFeatureSet(features map[cldr.FeatureID]bool) []cldr.FeatureID {
	out := make([]cldr.FeatureID, 0, len(features))
	for feature := range features {
		out = append(out, feature)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func runGo(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
