package cldrgen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nmeilick/go-i18n/internal/atomicfile"
	"github.com/nmeilick/go-i18n/locale/cldr"
	"github.com/nmeilick/go-i18n/locale/cldrpack"
)

const (
	GeneratorVersion = "cldrgen-1"
	DefaultSourceDir = "assets/cldr-json-48.2.0"
	DefaultOutput    = "internal/cldrdata/data_gen.go"
	DefaultLockPath  = "cldr.lock.json"
)

var featureSet = []string{
	"locale-parents",
	"profile-defaults",
	"currency-fractions",
	"default-numbering-systems",
	"global-currency-symbols",
	"gregorian-date-time",
	"month-weekday-names",
	"bcp47-extension-metadata",
}

// Options control generated-data operations.
type Options struct {
	SourceDir       string
	OutputPath      string
	LockPath        string
	SizeBudgetBytes int64
}

// Normalize applies default paths.
func (o Options) Normalize() Options {
	if o.SourceDir == "" {
		o.SourceDir = DefaultSourceDir
	}
	if o.OutputPath == "" {
		o.OutputPath = DefaultOutput
	}
	if o.LockPath == "" {
		o.LockPath = DefaultLockPath
	}
	if o.SizeBudgetBytes == 0 {
		o.SizeBudgetBytes = 2 << 20
	}
	return o
}

// SourceLock is the tracked source-of-truth for generated CLDR data.
type SourceLock struct {
	Version        int          `json:"version"`
	CLDRVersion    string       `json:"cldr_version"`
	UnicodeVersion string       `json:"unicode_version"`
	SourceIdentity string       `json:"source_identity"`
	TreeSHA256     string       `json:"tree_sha256"`
	Generator      string       `json:"generator"`
	FeatureSet     []string     `json:"feature_set"`
	OutputPaths    []string     `json:"output_paths"`
	Bundles        []BundleLock `json:"bundles,omitempty"`
	License        string       `json:"license"`
	GeneratedAt    string       `json:"generated_at,omitempty"`
}

// BundleLock records deterministic custom-bundle planning decisions.
type BundleLock struct {
	Name               string           `json:"name"`
	Features           []cldr.FeatureID `json:"features"`
	Locales            []string         `json:"locales"`
	Languages          []string         `json:"languages,omitempty"`
	OutputMode         string           `json:"output_mode"`
	OutputPath         string           `json:"output_path"`
	Codec              string           `json:"codec,omitempty"`
	AutoThresholdBytes int64            `json:"auto_threshold_bytes,omitempty"`
}

// Result describes one generation/check/update operation.
type Result struct {
	Lock         SourceLock `json:"lock"`
	OutputPath   string     `json:"output_path"`
	OutputBytes  int        `json:"output_bytes"`
	Changed      []string   `json:"changed,omitempty"`
	Added        []string   `json:"added,omitempty"`
	Removed      []string   `json:"removed,omitempty"`
	SizeBudget   int64      `json:"size_budget_bytes"`
	SizeBudgetOK bool       `json:"size_budget_ok"`
	Diagnostics  []string   `json:"diagnostics,omitempty"`
}

// PackResult describes pack generation from the same normalized model used for
// generated Go output.
type PackResult struct {
	Result
	OutputMode string             `json:"output_mode"`
	Codec      string             `json:"codec"`
	Selection  cldr.SelectionPlan `json:"selection"`
}

// Generate builds generated source and lock bytes from local CLDR JSON assets.
func Generate(ctx context.Context, opts Options) (Result, []byte, []byte, error) {
	opts = opts.Normalize()
	if err := ctx.Err(); err != nil {
		return Result{}, nil, nil, err
	}
	lock, err := BuildLock(ctx, opts)
	if err != nil {
		return Result{}, nil, nil, err
	}
	model, err := loadModel(ctx, opts.SourceDir)
	if err != nil {
		return Result{}, nil, nil, err
	}
	model.lock = lock
	source, err := render(model)
	if err != nil {
		return Result{}, nil, nil, err
	}
	lock.GeneratedAt = ""
	lockBytes, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return Result{}, nil, nil, err
	}
	lockBytes = append(lockBytes, '\n')
	result := Result{
		Lock: lock, OutputPath: opts.OutputPath, OutputBytes: len(source),
		SizeBudget: opts.SizeBudgetBytes, SizeBudgetOK: int64(len(source)) <= opts.SizeBudgetBytes,
	}
	if !result.SizeBudgetOK {
		return result, source, lockBytes, fmt.Errorf("generated data size %d exceeds budget %d", len(source), opts.SizeBudgetBytes)
	}
	return result, source, lockBytes, nil
}

// GeneratePack builds a .cldrpack from the same normalized CLDR model used by
// Generate. Runtime code should load the resulting bytes through
// locale/cldrpack rather than depending on generator internals.
func GeneratePack(ctx context.Context, opts Options, selection cldr.Selection, codec cldrpack.Codec) (PackResult, []byte, error) {
	opts = opts.Normalize()
	if err := ctx.Err(); err != nil {
		return PackResult{}, nil, err
	}
	lock, err := BuildLock(ctx, opts)
	if err != nil {
		return PackResult{}, nil, err
	}
	model, err := loadModel(ctx, opts.SourceDir)
	if err != nil {
		return PackResult{}, nil, err
	}
	model.lock = lock
	provider := modelProvider{model: model}
	info := cldr.Info{
		ID:       "generated-cldrpack",
		Name:     "Generated CLDR pack",
		Mode:     "external-pack",
		Versions: providerVersions(provider.Metadata()),
		Features: modelFeatures(),
		Locales:  provider.AvailableLocales(),
		Digest:   lock.TreeSHA256,
	}
	bundle, err := cldr.NewBundle(info, modelCoverage(provider), provider, nil)
	if err != nil {
		return PackResult{}, nil, err
	}
	selected, plan, err := cldr.SelectBundle(bundle, selection)
	if err != nil {
		return PackResult{}, nil, err
	}
	pack, err := cldrpack.Build(selected, cldrpack.WithCodec(codec))
	if err != nil {
		return PackResult{}, nil, err
	}
	result := PackResult{
		Result: Result{
			Lock: lock, OutputPath: opts.OutputPath, OutputBytes: len(pack),
			SizeBudget: opts.SizeBudgetBytes, SizeBudgetOK: opts.SizeBudgetBytes <= 0 || int64(len(pack)) <= opts.SizeBudgetBytes,
		},
		OutputMode: "external-pack",
		Codec:      codecName(codec),
		Selection:  plan,
	}
	if !result.SizeBudgetOK {
		return result, pack, fmt.Errorf("generated pack size %d exceeds budget %d", len(pack), opts.SizeBudgetBytes)
	}
	return result, pack, nil
}

// BuildLock computes deterministic metadata from local assets.
func BuildLock(ctx context.Context, opts Options) (SourceLock, error) {
	opts = opts.Normalize()
	version, unicode, err := readVersions(opts.SourceDir)
	if err != nil {
		return SourceLock{}, err
	}
	digest, err := treeDigest(ctx, opts.SourceDir)
	if err != nil {
		return SourceLock{}, err
	}
	return SourceLock{
		Version: 1, CLDRVersion: version, UnicodeVersion: unicode,
		SourceIdentity: sourceIdentity(opts.SourceDir), TreeSHA256: digest,
		Generator: GeneratorVersion, FeatureSet: append([]string(nil), featureSet...),
		OutputPaths: []string{sourceIdentity(opts.OutputPath)}, License: "Unicode-3.0",
	}, nil
}

func sourceIdentity(path string) string {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return filepath.ToSlash(clean)
	}
	if root, ok := findRepoRoot(); ok {
		if rel, err := filepath.Rel(root, clean); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Base(clean))
}

func findRepoRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", false
		}
		dir = next
	}
}

// ReadLock reads a source lock.
func ReadLock(path string) (SourceLock, error) {
	// #nosec G304 -- the lock path is a maintainer-selected local project file.
	data, err := os.ReadFile(path)
	if err != nil {
		return SourceLock{}, err
	}
	var lock SourceLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return SourceLock{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return lock, nil
}

type model struct {
	lock      SourceLock
	locales   []localeRecord
	regions   []regionDefault
	fractions []currencyFraction
	symbols   []currencySymbol
	bcp47     []bcp47Type
}

type localeRecord struct {
	Tag, Parent, NumberingSystem              string
	DateFormats, TimeFormats, DateTimeFormats [4]string
	MonthsWide, MonthsAbbr                    [12]string
	WeekdaysWide, WeekdaysAbbr                [7]string
	CurrencyPattern, Accounting               string
}

type regionDefault struct {
	Region, Currency, MeasurementSystem, FirstDay, TimeZone string
}

type currencyFraction struct {
	Code                         string
	Digits, CashDigits, Rounding int
}

type currencySymbol struct {
	Code, Symbol string
}

type bcp47Type struct {
	Key, Type, Alias string
}

func loadModel(ctx context.Context, source string) (model, error) {
	if err := ctx.Err(); err != nil {
		return model{}, err
	}
	localeSet, err := localeDirs(filepath.Join(source, "cldr-dates-full", "main"))
	if err != nil {
		return model{}, err
	}
	parentOverrides := loadParentLocales(source)
	m := model{}
	for _, tag := range localeSet {
		parent := parentOf(tag)
		if override := parentOverrides[tag]; override != "" {
			parent = override
		}
		rec := localeRecord{Tag: tag, Parent: parent, NumberingSystem: "latn"}
		loadNumbers(source, tag, &rec)
		loadDates(source, tag, &rec)
		m.locales = append(m.locales, rec)
	}
	m.locales = sparseLocales(m.locales)
	m.regions = loadRegionDefaults(source)
	m.fractions = loadCurrencyFractions(source)
	m.symbols = globalCurrencySymbols()
	m.bcp47 = loadBCP47(source)
	sort.Slice(m.locales, func(i, j int) bool { return m.locales[i].Tag < m.locales[j].Tag })
	sort.Slice(m.regions, func(i, j int) bool { return m.regions[i].Region < m.regions[j].Region })
	sort.Slice(m.fractions, func(i, j int) bool { return m.fractions[i].Code < m.fractions[j].Code })
	sort.Slice(m.symbols, func(i, j int) bool { return m.symbols[i].Code < m.symbols[j].Code })
	sort.Slice(m.bcp47, func(i, j int) bool {
		return m.bcp47[i].Key+"\x00"+m.bcp47[i].Type < m.bcp47[j].Key+"\x00"+m.bcp47[j].Type
	})
	return m, nil
}

func sparseLocales(records []localeRecord) []localeRecord {
	full := make(map[string]localeRecord, len(records))
	for _, rec := range records {
		full[rec.Tag] = rec
	}
	out := make([]localeRecord, 0, len(records))
	for _, rec := range records {
		parent, ok := full[rec.Parent]
		if ok {
			rec = deltaLocale(parent, rec)
		}
		out = append(out, rec)
	}
	return out
}

func deltaLocale(parent, child localeRecord) localeRecord {
	out := child
	if out.NumberingSystem == parent.NumberingSystem {
		out.NumberingSystem = ""
	}
	out.DateFormats = delta4(parent.DateFormats, out.DateFormats)
	out.TimeFormats = delta4(parent.TimeFormats, out.TimeFormats)
	out.DateTimeFormats = delta4(parent.DateTimeFormats, out.DateTimeFormats)
	out.MonthsWide = delta12(parent.MonthsWide, out.MonthsWide)
	out.MonthsAbbr = delta12(parent.MonthsAbbr, out.MonthsAbbr)
	out.WeekdaysWide = delta7(parent.WeekdaysWide, out.WeekdaysWide)
	out.WeekdaysAbbr = delta7(parent.WeekdaysAbbr, out.WeekdaysAbbr)
	if out.CurrencyPattern == parent.CurrencyPattern {
		out.CurrencyPattern = ""
	}
	if out.Accounting == parent.Accounting {
		out.Accounting = ""
	}
	return out
}

func delta4(parent, child [4]string) [4]string {
	for i := range child {
		if child[i] == parent[i] {
			child[i] = ""
		}
	}
	return child
}

func delta7(parent, child [7]string) [7]string {
	for i := range child {
		if child[i] == parent[i] {
			child[i] = ""
		}
	}
	return child
}

func delta12(parent, child [12]string) [12]string {
	for i := range child {
		if child[i] == parent[i] {
			child[i] = ""
		}
	}
	return child
}

func localeDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func parentOf(tag string) string {
	parts := strings.Split(tag, "-")
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], "-")
	}
	if tag != "en" {
		return "en"
	}
	return ""
}

func loadParentLocales(source string) map[string]string {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", "parentLocales.json"), &doc) {
		return nil
	}
	root := nestedMap(doc, "supplemental", "parentLocales", "parentLocale")
	out := map[string]string{}
	for tag, raw := range root {
		out[tag] = stringValue(raw, "")
	}
	return out
}

func loadNumbers(source, tag string, rec *localeRecord) {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-numbers-full", "main", tag, "numbers.json"), &doc) {
		return
	}
	nums := nestedMap(doc, "main", tag, "numbers")
	rec.NumberingSystem = stringValue(nums["defaultNumberingSystem"], "latn")
	curFmt := nestedMap(nums, "currencyFormats-numberSystem-latn")
	rec.CurrencyPattern = stringValue(curFmt["standard"], rec.CurrencyPattern)
	rec.Accounting = stringValue(curFmt["accounting"], rec.Accounting)
}

func globalCurrencySymbols() []currencySymbol {
	return []currencySymbol{
		{Code: "CHF", Symbol: "CHF"},
		{Code: "EUR", Symbol: "€"},
		{Code: "GBP", Symbol: "£"},
		{Code: "JPY", Symbol: "¥"},
		{Code: "USD", Symbol: "$"},
	}
}

func loadDates(source, tag string, rec *localeRecord) {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-dates-full", "main", tag, "ca-gregorian.json"), &doc) {
		return
	}
	greg := nestedMap(doc, "main", tag, "dates", "calendars", "gregorian")
	fillWidths(&rec.DateFormats, nestedMap(greg, "dateFormats"))
	fillWidths(&rec.TimeFormats, nestedMap(greg, "timeFormats"))
	fillWidths(&rec.DateTimeFormats, nestedMap(greg, "dateTimeFormats"))
	fillMonths(&rec.MonthsWide, nestedMap(greg, "months", "format", "wide"))
	fillMonths(&rec.MonthsAbbr, nestedMap(greg, "months", "format", "abbreviated"))
	fillWeekdays(&rec.WeekdaysWide, nestedMap(greg, "days", "format", "wide"))
	fillWeekdays(&rec.WeekdaysAbbr, nestedMap(greg, "days", "format", "abbreviated"))
}

func loadRegionDefaults(source string) []regionDefault {
	currencies := regionCurrencies(source)
	measurements := supplementalStringMap(source, "measurementData.json", "measurementData", "measurementSystem")
	firstDays := supplementalStringMap(source, "weekData.json", "weekData", "firstDay")
	timezones := primaryZones(source)
	regions := map[string]bool{"001": true}
	for k := range currencies {
		regions[k] = true
	}
	for k := range measurements {
		regions[k] = true
	}
	for k := range firstDays {
		regions[k] = true
	}
	for k := range timezones {
		regions[k] = true
	}
	out := make([]regionDefault, 0, len(regions))
	for region := range regions {
		out = append(out, regionDefault{
			Region: region, Currency: firstNonEmpty(currencies[region], currencies["001"], "USD"),
			MeasurementSystem: firstNonEmpty(measurements[region], measurements["001"], "metric"),
			FirstDay:          firstNonEmpty(firstDays[region], firstDays["001"], "mon"),
			TimeZone:          firstNonEmpty(timezones[region], timezones["001"], "UTC"),
		})
	}
	return out
}

func loadCurrencyFractions(source string) []currencyFraction {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", "currencyData.json"), &doc) {
		return nil
	}
	fractions := nestedMap(doc, "supplemental", "currencyData", "fractions")
	out := make([]currencyFraction, 0, len(fractions))
	for code, raw := range fractions {
		info, _ := raw.(map[string]any)
		digits := atoi(stringValue(info["_digits"], "2"), 2)
		cashDigits := atoi(stringValue(info["_cashDigits"], ""), digits)
		out = append(out, currencyFraction{Code: code, Digits: digits, CashDigits: cashDigits, Rounding: atoi(stringValue(info["_cashRounding"], stringValue(info["_rounding"], "0")), 0)})
	}
	return out
}

func loadBCP47(source string) []bcp47Type {
	files := map[string]string{"calendar": "ca", "number": "nu", "measure": "ms", "currency": "cu", "timezone": "tz", "collation": "co", "segmentation": "ss"}
	out := []bcp47Type{}
	for file, key := range files {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-bcp47", "bcp47", file+".json"), &doc) {
			continue
		}
		keyword := nestedMap(doc, "keyword", "u", key)
		for typ, raw := range keyword {
			if strings.HasPrefix(typ, "_") {
				continue
			}
			info, _ := raw.(map[string]any)
			out = append(out, bcp47Type{Key: key, Type: typ, Alias: stringValue(info["_alias"], "")})
		}
	}
	return out
}

func render(m model) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Code generated by lingo cldrgen; DO NOT EDIT.\n\npackage cldrdata\n\n")
	writeMetadata(&b, m.lock)
	writeLocales(&b, m.locales)
	writeRegions(&b, m.regions)
	writeFractions(&b, m.fractions)
	writeSymbols(&b, m.symbols)
	writeBCP47(&b, m.bcp47)
	return format.Source(b.Bytes())
}

func writeMetadata(b *bytes.Buffer, lock SourceLock) {
	fmt.Fprintf(b, "var metadata = Metadata{CLDRVersion:%s, UnicodeVersion:%s, Generator:%s, LockID:%s, SourceIdentity:%s, TreeDigest:%s, License:%s, FeatureSet: []string{",
		q(lock.CLDRVersion), q(lock.UnicodeVersion), q(lock.Generator), q(lock.TreeSHA256[:12]), q(lock.SourceIdentity), q(lock.TreeSHA256), q(lock.License))
	for _, f := range lock.FeatureSet {
		fmt.Fprintf(b, "%s,", q(f))
	}
	b.WriteString("}}\n\n")
}

func writeLocales(b *bytes.Buffer, records []localeRecord) {
	b.WriteString("var localeRecords = []LocaleRecord{\n")
	for _, r := range records {
		b.WriteString("{")
		writeField(b, "Tag", q(r.Tag))
		writeField(b, "Parent", q(r.Parent))
		if r.NumberingSystem != "" {
			writeField(b, "NumberingSystem", q(r.NumberingSystem))
		}
		if has4(r.DateFormats) {
			writeField(b, "DateFormats", arr4(r.DateFormats))
		}
		if has4(r.TimeFormats) {
			writeField(b, "TimeFormats", arr4(r.TimeFormats))
		}
		if has4(r.DateTimeFormats) {
			writeField(b, "DateTimeFormats", arr4(r.DateTimeFormats))
		}
		if has12(r.MonthsWide) {
			writeField(b, "MonthsWide", arr12(r.MonthsWide))
		}
		if has12(r.MonthsAbbr) {
			writeField(b, "MonthsAbbr", arr12(r.MonthsAbbr))
		}
		if has7(r.WeekdaysWide) {
			writeField(b, "WeekdaysWide", arr7(r.WeekdaysWide))
		}
		if has7(r.WeekdaysAbbr) {
			writeField(b, "WeekdaysAbbr", arr7(r.WeekdaysAbbr))
		}
		if r.CurrencyPattern != "" {
			writeField(b, "CurrencyPattern", q(r.CurrencyPattern))
		}
		if r.Accounting != "" {
			writeField(b, "Accounting", q(r.Accounting))
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\n")
}

func writeField(b *bytes.Buffer, name, value string) {
	fmt.Fprintf(b, "%s:%s,", name, value)
}

func writeRegions(b *bytes.Buffer, records []regionDefault) {
	b.WriteString("var regionDefaults = []RegionDefaults{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Region:%s, Currency:%s, MeasurementSystem:%s, FirstDay:%s, TimeZone:%s},\n", q(r.Region), q(r.Currency), q(r.MeasurementSystem), q(r.FirstDay), q(r.TimeZone))
	}
	b.WriteString("}\n\n")
}

func writeFractions(b *bytes.Buffer, records []currencyFraction) {
	b.WriteString("var currencyFractions = []CurrencyFraction{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Code:%s, Digits:%d, CashDigits:%d, Rounding:%d},\n", q(r.Code), r.Digits, r.CashDigits, r.Rounding)
	}
	b.WriteString("}\n\n")
}

func writeSymbols(b *bytes.Buffer, records []currencySymbol) {
	b.WriteString("var currencySymbols = []CurrencySymbolRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Code:%s, Symbol:%s},\n", q(r.Code), q(r.Symbol))
	}
	b.WriteString("}\n\n")
}

func writeBCP47(b *bytes.Buffer, records []bcp47Type) {
	b.WriteString("var bcp47Types = []BCP47TypeRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Key:%s, Type:%s, Alias:%s},\n", q(r.Key), q(r.Type), q(r.Alias))
	}
	b.WriteString("}\n")
}

func arr4(a [4]string) string {
	return "[4]string{" + q(a[0]) + "," + q(a[1]) + "," + q(a[2]) + "," + q(a[3]) + "}"
}
func arr7(a [7]string) string {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = q(v)
	}
	return "[7]string{" + strings.Join(parts, ",") + "}"
}
func arr12(a [12]string) string {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = q(v)
	}
	return "[12]string{" + strings.Join(parts, ",") + "}"
}

func has4(a [4]string) bool {
	for _, v := range a {
		if v != "" {
			return true
		}
	}
	return false
}

func has7(a [7]string) bool {
	for _, v := range a {
		if v != "" {
			return true
		}
	}
	return false
}

func has12(a [12]string) bool {
	for _, v := range a {
		if v != "" {
			return true
		}
	}
	return false
}

func q(s string) string { return strconv.Quote(s) }

func readVersions(source string) (string, string, error) {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", "currencyData.json"), &doc) {
		return "", "", fmt.Errorf("read CLDR version metadata")
	}
	v := nestedMap(doc, "supplemental", "version")
	return strings.TrimPrefix(stringValue(v["_cldrVersion"], ""), "release-"), stringValue(v["_unicodeVersion"], ""), nil
}

func treeDigest(ctx context.Context, root string) (string, error) {
	h := sha256.New()
	paths := []string{}
	for _, sub := range []string{"cldr-core", "cldr-bcp47", "cldr-numbers-full", "cldr-dates-full"} {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".json") {
				return nil
			}
			paths = append(paths, path)
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(root, path)
		// #nosec G304 -- source digest reads files discovered under the configured CLDR source root.
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		sum := sha256.Sum256(data)
		h.Write(sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readJSON(path string, out any) bool {
	// #nosec G304 -- generator JSON paths are built from the configured local CLDR source root.
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

func nestedMap(root map[string]any, path ...string) map[string]any {
	cur := root
	for _, part := range path {
		next, _ := cur[part].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

func stringValue(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func fillWidths(out *[4]string, m map[string]any) {
	out[0] = stringValue(m["short"], "")
	out[1] = stringValue(m["medium"], "")
	out[2] = stringValue(m["long"], "")
	out[3] = stringValue(m["full"], "")
}

func fillMonths(out *[12]string, m map[string]any) {
	for i := 1; i <= 12; i++ {
		out[i-1] = stringValue(m[strconv.Itoa(i)], "")
	}
}

func fillWeekdays(out *[7]string, m map[string]any) {
	for i, key := range []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"} {
		out[i] = stringValue(m[key], "")
	}
}

func supplementalStringMap(source, file string, path ...string) map[string]string {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", file), &doc) {
		return nil
	}
	root := nestedMap(doc, append([]string{"supplemental"}, path...)...)
	out := map[string]string{}
	for k, v := range root {
		out[k] = stringValue(v, "")
	}
	return out
}

func regionCurrencies(source string) map[string]string {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", "currencyData.json"), &doc) {
		return nil
	}
	regions := nestedMap(doc, "supplemental", "currencyData", "region")
	out := map[string]string{"001": "USD"}
	for region, raw := range regions {
		arr, _ := raw.([]any)
		for _, item := range arr {
			m, _ := item.(map[string]any)
			for code, details := range m {
				info, _ := details.(map[string]any)
				if stringValue(info["_to"], "") == "" && stringValue(info["_tender"], "true") != "false" {
					out[region] = code
					break
				}
			}
			if out[region] != "" {
				break
			}
		}
	}
	return out
}

func primaryZones(source string) map[string]string {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-core", "supplemental", "primaryZones.json"), &doc) {
		return nil
	}
	zones := nestedMap(doc, "supplemental", "primaryZones")
	out := map[string]string{"001": "UTC"}
	for region, raw := range zones {
		switch v := raw.(type) {
		case string:
			out[region] = v
		case map[string]any:
			for _, rawZone := range v {
				out[region] = stringValue(rawZone, "")
				break
			}
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ChangedFiles compares generated bytes with tracked files.
func ChangedFiles(opts Options, source, lock []byte) ([]string, error) {
	opts = opts.Normalize()
	changed := []string{}
	for path, data := range map[string][]byte{opts.OutputPath: source, opts.LockPath: lock} {
		same, err := atomicfile.FileContentEqual(path, data)
		if err != nil {
			if os.IsNotExist(err) {
				changed = append(changed, filepath.ToSlash(path))
				continue
			}
			return nil, fmt.Errorf("compare generated file %s: %w", path, err)
		}
		if !same {
			changed = append(changed, filepath.ToSlash(path))
		}
	}
	sort.Strings(changed)
	return changed, nil
}

// WriteAtomically writes generated data after validating every file.
func WriteAtomically(opts Options, source, lock []byte, force bool) error {
	opts = opts.Normalize()
	for _, path := range []string{opts.OutputPath, opts.LockPath} {
		// #nosec G304 -- overwrite guard reads configured generated output paths.
		if data, err := os.ReadFile(path); err == nil && !force && !bytes.Contains(data, []byte("Code generated")) && strings.HasSuffix(path, ".go") {
			return fmt.Errorf("refuse to overwrite non-generated file %s without force", path)
		}
	}
	writes := map[string][]byte{opts.OutputPath: source, opts.LockPath: lock}
	return atomicfile.WriteFiles(writes, 0o644)
}

// SourceSummary returns a compact, auditable source summary.
func SourceSummary(ctx context.Context, opts Options) (SourceLock, error) {
	lock, err := BuildLock(ctx, opts.Normalize())
	if err != nil {
		return SourceLock{}, err
	}
	lock.GeneratedAt = time.Time{}.Format(time.RFC3339)
	return lock, nil
}
