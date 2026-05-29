package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nmeilick/go-i18n/extract"
	"github.com/nmeilick/go-i18n/gettext"
	"github.com/nmeilick/go-i18n/internal/atomicfile"
	"github.com/nmeilick/go-i18n/internal/cldrgen"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Version int `toml:"version" json:"version"`
	Extract struct {
		Roots    []string `toml:"roots" json:"roots"`
		Keywords []string `toml:"keywords" json:"keywords,omitempty"`
	} `toml:"extract" json:"extract"`
	Catalogs struct {
		Template  string   `toml:"template" json:"template"`
		LocaleDir string   `toml:"locale_dir" json:"locale_dir"`
		Locales   []string `toml:"locales" json:"locales,omitempty"`
	} `toml:"catalogs" json:"catalogs"`
	Data struct {
		SourceDir       string             `toml:"source_dir" json:"source_dir"`
		LockFile        string             `toml:"lock_file" json:"lock_file"`
		OutputFile      string             `toml:"output_file" json:"output_file"`
		SizeBudgetBytes int64              `toml:"size_budget_bytes" json:"size_budget_bytes"`
		Bundles         []DataBundleConfig `toml:"bundles,omitempty" json:"bundles,omitempty"`
	} `toml:"data" json:"data"`
	Paths struct {
		AllowOutsideProject bool `toml:"allow_outside_project,omitempty" json:"allow_outside_project,omitempty"`
	} `toml:"paths,omitempty" json:"paths,omitempty"`
	baseDir string
}

// DataBundleConfig records deterministic custom CLDR bundle selections in
// project configuration.
type DataBundleConfig struct {
	Name               string   `toml:"name" json:"name"`
	Mode               string   `toml:"mode" json:"mode"`
	Output             string   `toml:"output" json:"output"`
	Package            string   `toml:"package,omitempty" json:"package,omitempty"`
	Codec              string   `toml:"codec,omitempty" json:"codec,omitempty"`
	Locales            []string `toml:"locales,omitempty" json:"locales,omitempty"`
	Languages          []string `toml:"languages,omitempty" json:"languages,omitempty"`
	Features           []string `toml:"features,omitempty" json:"features,omitempty"`
	ScanMode           string   `toml:"scan_mode,omitempty" json:"scan_mode,omitempty"`
	Entrypoints        []string `toml:"entrypoints,omitempty" json:"entrypoints,omitempty"`
	ScanRoots          []string `toml:"scan_roots,omitempty" json:"scan_roots,omitempty"`
	FeatureExtra       []string `toml:"feature_extra,omitempty" json:"feature_extra,omitempty"`
	FeatureExclude     []string `toml:"feature_exclude,omitempty" json:"feature_exclude,omitempty"`
	FeatureRules       []string `toml:"feature_rules,omitempty" json:"feature_rules,omitempty"`
	AutoThresholdBytes int64    `toml:"auto_threshold_bytes,omitempty" json:"auto_threshold_bytes,omitempty"`
}

func DefaultConfig() Config {
	var cfg Config
	cfg.Version = 1
	cfg.Extract.Roots = []string{"."}
	cfg.Catalogs.Template = "locales/messages.pot"
	cfg.Catalogs.LocaleDir = "locales"
	cfg.Data.SourceDir = cldrgen.DefaultSourceDir
	cfg.Data.LockFile = cldrgen.DefaultLockPath
	cfg.Data.OutputFile = cldrgen.DefaultOutput
	cfg.Data.SizeBudgetBytes = cldrgen.DefaultSizeBudget
	return cfg
}

func LoadConfig(path string) (Config, error) {
	// #nosec G304 -- workflow config path is selected by the local operator or project.
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg = WithBaseDir(cfg, filepath.Dir(path))
	return cfg, ValidateConfig(cfg)
}

// WithBaseDir returns cfg with relative workflow paths resolved from dir when
// the config is executed. It is primarily used by config-file loaders.
func WithBaseDir(cfg Config, dir string) Config {
	if dir == "" {
		return cfg
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		dir = abs
	}
	cfg.baseDir = filepath.Clean(dir)
	return cfg
}

// ConfigBaseDir reports the base directory used to resolve config-relative paths.
func ConfigBaseDir(cfg Config) string {
	return cfg.baseDir
}

func WriteDefaultConfig(path string) error {
	cfg := DefaultConfig()
	cfg.Catalogs.Locales = []string{"de"}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, data, 0o644)
}

func ValidateConfig(cfg Config) error {
	if cfg.Version <= 0 {
		return errors.New("config version must be positive")
	}
	if cfg.Catalogs.Template == "" {
		return errors.New("catalogs.template is required")
	}
	for _, spec := range cfg.Extract.Keywords {
		if _, err := extract.ParseKeyword(spec); err != nil {
			return err
		}
	}
	for _, bundle := range cfg.Data.Bundles {
		if strings.TrimSpace(bundle.Name) == "" {
			return errors.New("data.bundles.name is required")
		}
		switch bundle.Mode {
		case "", "native-go", "embedded-pack", "external-pack", "auto":
		default:
			return fmt.Errorf("data.bundles[%s].mode must be native-go, embedded-pack, external-pack, or auto", bundle.Name)
		}
		switch bundle.Codec {
		case "", "raw", "zstd":
		default:
			return fmt.Errorf("data.bundles[%s].codec must be raw or zstd", bundle.Name)
		}
		switch bundle.ScanMode {
		case "", "auto", "module", "importers":
		default:
			return fmt.Errorf("data.bundles[%s].scan_mode must be auto, module, or importers", bundle.Name)
		}
		for _, rule := range bundle.FeatureRules {
			target, features, ok := strings.Cut(rule, "=")
			if !ok || strings.TrimSpace(target) == "" || strings.TrimSpace(features) == "" {
				return fmt.Errorf("data.bundles[%s].feature_rules entries must use target=feature[,feature]", bundle.Name)
			}
		}
	}
	return nil
}

type Workflow struct {
	Config Config
}

func New(cfg Config) (*Workflow, error) {
	var err error
	cfg, err = normalizeConfigPaths(cfg)
	if err != nil {
		return nil, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	return &Workflow{Config: cfg}, nil
}

type Report struct {
	Messages       int               `json:"messages"`
	Warnings       int               `json:"warnings"`
	WarningDetails []extract.Warning `json:"warning_details,omitempty"`
	Changed        []string          `json:"changed"`
	Stale          []string          `json:"stale"`
}

// DataReport describes generated CLDR data state.
type DataReport struct {
	Version         int               `json:"version"`
	SourceDir       string            `json:"source_dir"`
	LockFile        string            `json:"lock_file"`
	OutputFile      string            `json:"output_file"`
	CLDRVersion     string            `json:"cldr_version"`
	UnicodeVersion  string            `json:"unicode_version"`
	TreeSHA256      string            `json:"tree_sha256"`
	Generator       string            `json:"generator"`
	FeatureSet      []string          `json:"feature_set"`
	Changed         []string          `json:"changed,omitempty"`
	OutputBytes     int               `json:"output_bytes,omitempty"`
	SizeReport      []cldrgen.SizeRow `json:"size_report,omitempty"`
	SizeBudgetBytes int64             `json:"size_budget_bytes,omitempty"`
	SizeBudgetOK    bool              `json:"size_budget_ok"`
	DryRun          bool              `json:"dry_run,omitempty"`
}

// ErrDataStale reports generated data that differs from local CLDR assets.
var ErrDataStale = errors.New("generated CLDR data is stale")

func (w *Workflow) keywords() ([]extract.Keyword, error) {
	if len(w.Config.Extract.Keywords) == 0 {
		return extract.DefaultKeywords(), nil
	}
	out := make([]extract.Keyword, 0, len(w.Config.Extract.Keywords))
	for _, spec := range w.Config.Extract.Keywords {
		kw, err := extract.ParseKeyword(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, kw)
	}
	return out, nil
}

func (w *Workflow) Extract(ctx context.Context) (*gettext.Document, Report, error) {
	keywords, err := w.keywords()
	if err != nil {
		return nil, Report{}, err
	}
	doc, report, err := extract.Extract(ctx, extract.Options{Roots: w.Config.Extract.Roots, Keywords: keywords})
	normalizeExtractionPaths(doc, &report, w.Config.baseDir)
	return doc, Report{Messages: report.Messages, Warnings: len(report.Warnings), WarningDetails: report.Warnings}, err
}

func (w *Workflow) Update(ctx context.Context, dryRun bool) (Report, error) {
	doc, report, err := w.Extract(ctx)
	if err != nil {
		return report, err
	}
	outputs, err := w.renderOutputs(doc)
	if err != nil {
		return report, err
	}
	if dryRun {
		for _, path := range sortedOutputPaths(outputs) {
			data := outputs[path]
			differs, err := fileDiffers(path, data)
			if err != nil {
				return report, err
			}
			if differs {
				report.Changed = append(report.Changed, path)
			}
		}
		return report, nil
	}
	writes := map[string][]byte{}
	for _, path := range sortedOutputPaths(outputs) {
		data := outputs[path]
		differs, err := fileDiffers(path, data)
		if err != nil {
			return report, err
		}
		if differs {
			report.Changed = append(report.Changed, path)
			writes[path] = data
		}
	}
	if err := atomicfile.WriteFiles(writes, 0o644); err != nil {
		return report, err
	}
	return report, nil
}

func (w *Workflow) Check(ctx context.Context, strict bool) (Report, error) {
	report, err := w.Update(ctx, true)
	if err != nil {
		return report, err
	}
	if err := w.validateCatalogs(strict); err != nil {
		return report, err
	}
	if strict && report.Warnings > 0 {
		return report, fmt.Errorf("strict check failed with %d warnings", report.Warnings)
	}
	if len(report.Changed) > 0 {
		return report, fmt.Errorf("catalogs are stale; run lingo update")
	}
	return report, nil
}

// Stale reports active locale entries that are no longer present in extraction.
func (w *Workflow) Stale(ctx context.Context) (Report, error) {
	doc, report, err := w.Extract(ctx)
	if err != nil {
		return report, err
	}
	active := map[string]bool{}
	for _, entry := range doc.Entries {
		active[entry.Domain+"\x00"+entry.Context+"\x00"+entry.ID] = true
	}
	for _, path := range localePaths(w.Config) {
		// #nosec G304 -- catalog paths come from the project workflow configuration.
		data, err := os.ReadFile(path)
		if err != nil {
			return report, fmt.Errorf("read %s: %w", path, err)
		}
		localeDoc, err := gettext.ParsePO(bytes.NewReader(data))
		if err != nil {
			return report, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, entry := range localeDoc.Entries {
			if entry.ID == "" || entry.Obsolete {
				continue
			}
			key := entry.Domain + "\x00" + entry.Context + "\x00" + entry.ID
			if !active[key] {
				report.Stale = append(report.Stale, path+":"+entry.ID)
			}
		}
	}
	return report, nil
}

// Format rewrites configured catalogs deterministically. With dryRun it reports
// files that would change without mutating them.
func (w *Workflow) Format(ctx context.Context, dryRun bool) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	report := Report{}
	for _, path := range append([]string{w.Config.Catalogs.Template}, localePaths(w.Config)...) {
		// #nosec G304 -- format reads project-configured catalog files.
		data, err := os.ReadFile(path)
		if err != nil {
			return report, fmt.Errorf("read %s: %w", path, err)
		}
		doc, err := gettext.ParsePO(bytes.NewReader(data))
		if err != nil {
			return report, fmt.Errorf("parse %s: %w", path, err)
		}
		var out bytes.Buffer
		if err := gettext.WritePO(&out, doc); err != nil {
			return report, err
		}
		if !bytes.Equal(data, out.Bytes()) {
			report.Changed = append(report.Changed, path)
			if !dryRun {
				if err := atomicfile.WriteFile(path, out.Bytes(), 0o644); err != nil {
					return report, err
				}
			}
		}
	}
	return report, nil
}

// DataSources returns the current generated-data source summary.
func (w *Workflow) DataSources(ctx context.Context) (DataReport, error) {
	opts := w.dataOptions()
	lock, err := cldrgen.SourceSummary(ctx, opts)
	if err != nil {
		return DataReport{}, err
	}
	return dataReport(opts, cldrgen.Result{Lock: lock, SizeBudget: opts.SizeBudgetBytes, SizeBudgetOK: true}, false), nil
}

// DataCheck regenerates into memory and verifies tracked generated files are current.
func (w *Workflow) DataCheck(ctx context.Context) (DataReport, error) {
	opts := w.dataOptions()
	result, source, lock, err := cldrgen.Generate(ctx, opts)
	if err != nil {
		return dataReport(opts, result, false), err
	}
	changed, err := cldrgen.ChangedFiles(opts, source, lock)
	if err != nil {
		return dataReport(opts, result, false), err
	}
	result.Changed = changed
	report := dataReport(opts, result, false)
	if len(changed) > 0 {
		return report, fmt.Errorf("%w; run lingo data update", ErrDataStale)
	}
	return report, nil
}

// DataDiff reports generated-data drift without mutating files.
func (w *Workflow) DataDiff(ctx context.Context) (DataReport, error) {
	opts := w.dataOptions()
	result, source, lock, err := cldrgen.Generate(ctx, opts)
	if err != nil {
		return dataReport(opts, result, false), err
	}
	changed, err := cldrgen.ChangedFiles(opts, source, lock)
	if err != nil {
		return dataReport(opts, result, false), err
	}
	result.Changed = changed
	return dataReport(opts, result, false), nil
}

// DataFootprint reports generated source and optional compact pack sizes.
func (w *Workflow) DataFootprint(ctx context.Context) (DataReport, error) {
	opts := w.dataOptions()
	result, err := cldrgen.MeasureFootprint(ctx, opts)
	if err != nil {
		return dataReport(opts, result, false), err
	}
	return dataReport(opts, result, false), nil
}

// DataUpdate regenerates generated CLDR data. With dryRun it writes only to a
// temporary directory inside cldrgen and returns ErrDataStale when changes are needed.
func (w *Workflow) DataUpdate(ctx context.Context, dryRun, force bool) (DataReport, error) {
	opts := w.dataOptions()
	result, source, lock, err := cldrgen.Generate(ctx, opts)
	if err != nil {
		return dataReport(opts, result, dryRun), err
	}
	changed, err := cldrgen.ChangedFiles(opts, source, lock)
	if err != nil {
		return dataReport(opts, result, dryRun), err
	}
	result.Changed = changed
	report := dataReport(opts, result, dryRun)
	if dryRun {
		if len(changed) > 0 {
			return report, ErrDataStale
		}
		return report, nil
	}
	if len(changed) == 0 {
		return report, nil
	}
	if err := cldrgen.WriteAtomically(opts, source, lock, force); err != nil {
		return report, err
	}
	return report, nil
}

func (w *Workflow) dataOptions() cldrgen.Options {
	return cldrgen.Options{
		SourceDir:       w.Config.Data.SourceDir,
		OutputPath:      w.Config.Data.OutputFile,
		LockPath:        w.Config.Data.LockFile,
		SizeBudgetBytes: w.Config.Data.SizeBudgetBytes,
	}.Normalize()
}

func normalizeConfigPaths(cfg Config) (Config, error) {
	if cfg.baseDir == "" {
		return cfg, nil
	}
	base := cfg.baseDir
	var err error
	resolve := func(path string) string {
		if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Clean(filepath.Join(base, path))
	}
	for i, root := range cfg.Extract.Roots {
		cfg.Extract.Roots[i] = resolve(root)
	}
	cfg.Catalogs.Template = resolve(cfg.Catalogs.Template)
	cfg.Catalogs.LocaleDir = resolve(cfg.Catalogs.LocaleDir)
	cfg.Data.SourceDir = resolve(cfg.Data.SourceDir)
	cfg.Data.LockFile = resolve(cfg.Data.LockFile)
	cfg.Data.OutputFile = resolve(cfg.Data.OutputFile)
	for i := range cfg.Data.Bundles {
		if cfg.Data.Bundles[i].Output != "" {
			cfg.Data.Bundles[i].Output = resolve(cfg.Data.Bundles[i].Output)
		}
		for j, root := range cfg.Data.Bundles[i].ScanRoots {
			cfg.Data.Bundles[i].ScanRoots[j] = resolve(root)
		}
	}
	if !cfg.Paths.AllowOutsideProject {
		for _, path := range append([]string{cfg.Catalogs.Template, cfg.Catalogs.LocaleDir, cfg.Data.LockFile, cfg.Data.OutputFile}, bundleOutputPaths(cfg)...) {
			if err = requireInsideBase(base, path); err != nil {
				return Config{}, err
			}
		}
	}
	return cfg, nil
}

func bundleOutputPaths(cfg Config) []string {
	out := make([]string, 0, len(cfg.Data.Bundles))
	for _, bundle := range cfg.Data.Bundles {
		if bundle.Output != "" {
			out = append(out, bundle.Output)
		}
	}
	return out
}

func requireInsideBase(base, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return fmt.Errorf("path %s cannot be compared with project root %s: %w", path, base, err)
	}
	if rel == "." || rel == "" {
		return nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %s escapes project root %s; set paths.allow_outside_project=true to allow it", path, base)
	}
	return nil
}

func dataReport(opts cldrgen.Options, result cldrgen.Result, dryRun bool) DataReport {
	lock := result.Lock
	return DataReport{
		Version: 1, SourceDir: opts.SourceDir, LockFile: opts.LockPath, OutputFile: opts.OutputPath,
		CLDRVersion: lock.CLDRVersion, UnicodeVersion: lock.UnicodeVersion, TreeSHA256: lock.TreeSHA256,
		Generator: lock.Generator, FeatureSet: append([]string(nil), lock.FeatureSet...),
		Changed: append([]string(nil), result.Changed...), OutputBytes: result.OutputBytes,
		SizeReport:      append([]cldrgen.SizeRow(nil), result.SizeReport...),
		SizeBudgetBytes: result.SizeBudget, SizeBudgetOK: result.SizeBudgetOK, DryRun: dryRun,
	}
}

func (w *Workflow) validateCatalogs(strict bool) error {
	template, err := readCatalog(w.Config.Catalogs.Template)
	if err != nil {
		return err
	}
	if err := validateCatalogDoc(w.Config.Catalogs.Template, template, strict); err != nil {
		return err
	}
	for _, loc := range w.Config.Catalogs.Locales {
		path := filepath.Join(w.Config.Catalogs.LocaleDir, loc+".po")
		doc, err := readCatalog(path)
		if err != nil {
			return err
		}
		if err := validateCatalogDoc(path, doc, strict); err != nil {
			return err
		}
		if strict {
			if err := validateLocaleCoverage(path, loc, doc); err != nil {
				return err
			}
		}
	}
	return nil
}

func readCatalog(path string) (*gettext.Document, error) {
	// #nosec G304 -- validation reads project-configured catalog files.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	doc, err := gettext.ParsePO(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return doc, nil
}

func validateCatalogDoc(path string, doc *gettext.Document, strict bool) error {
	report := gettext.Validate(doc)
	if len(report.Errors) > 0 {
		return fmt.Errorf("validate %s: %w", path, report.Errors[0])
	}
	if strict && len(report.Warnings) > 0 {
		return fmt.Errorf("strict validate %s: %w", path, report.Warnings[0])
	}
	return nil
}

func validateLocaleCoverage(path, loc string, doc *gettext.Document) error {
	header := doc.Header()
	if strings.TrimSpace(header["Language"]) == "" {
		return fmt.Errorf("strict validate %s: missing Language header", path)
	}
	if strings.TrimSpace(header["Plural-Forms"]) == "" {
		return fmt.Errorf("strict validate %s: missing Plural-Forms header", path)
	}
	for _, entry := range doc.Entries {
		if entry.ID == "" || entry.Obsolete {
			continue
		}
		for i, text := range entry.Strings {
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("strict validate %s: locale %s entry %q has untranslated msgstr[%d]", path, loc, entry.ID, i)
			}
		}
		if len(entry.Strings) == 0 {
			return fmt.Errorf("strict validate %s: locale %s entry %q has no translation", path, loc, entry.ID)
		}
	}
	return nil
}

func localePaths(cfg Config) []string {
	out := make([]string, 0, len(cfg.Catalogs.Locales))
	for _, loc := range cfg.Catalogs.Locales {
		out = append(out, filepath.Join(cfg.Catalogs.LocaleDir, loc+".po"))
	}
	return out
}

func (w *Workflow) renderOutputs(doc *gettext.Document) (map[string][]byte, error) {
	outputs := map[string][]byte{}
	var buf bytes.Buffer
	if err := gettext.WritePOT(&buf, doc); err != nil {
		return nil, err
	}
	outputs[w.Config.Catalogs.Template] = append([]byte(nil), buf.Bytes()...)
	for _, loc := range w.Config.Catalogs.Locales {
		path := filepath.Join(w.Config.Catalogs.LocaleDir, loc+".po")
		existing := &gettext.Document{}
		// #nosec G304 -- merge reads project-configured existing locale files.
		if data, err := os.ReadFile(path); err == nil {
			parsed, err := gettext.ParsePO(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			existing = parsed
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		merged, _, err := gettext.MergeLocale(doc, existing, loc)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		if err := gettext.WritePO(&out, merged); err != nil {
			return nil, err
		}
		outputs[path] = append([]byte(nil), out.Bytes()...)
	}
	return outputs, nil
}

func sortedOutputPaths(outputs map[string][]byte) []string {
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func fileDiffers(path string, data []byte) (bool, error) {
	same, err := atomicfile.FileContentEqual(path, data)
	if err == nil {
		return !same, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, fmt.Errorf("compare %s: %w", path, err)
}

func normalizeExtractionPaths(doc *gettext.Document, report *extract.Report, base string) {
	if strings.TrimSpace(base) == "" {
		return
	}
	if doc != nil {
		for i := range doc.Entries {
			for j := range doc.Entries[i].References {
				doc.Entries[i].References[j].File = displayPath(base, doc.Entries[i].References[j].File)
			}
		}
	}
	if report != nil {
		for i := range report.Warnings {
			report.Warnings[i].Path = displayPath(base, report.Warnings[i].Path)
		}
	}
}

func displayPath(base, path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	if rel, err := filepath.Rel(base, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
