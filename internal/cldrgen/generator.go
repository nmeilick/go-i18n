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
	GeneratorVersion  = "cldrgen-1"
	DefaultSourceDir  = "assets/cldr-json-48.2.0"
	DefaultOutput     = "internal/cldrdata/data_gen.go"
	DefaultLockPath   = "cldr.lock.json"
	DefaultSizeBudget = 24 << 20
)

var featureSet = []string{
	"locale-parents",
	"profile-defaults",
	"currency-fractions",
	"default-numbering-systems",
	"per-locale-currency-symbols",
	"per-locale-currency-narrow-symbols",
	"gregorian-date-time",
	"month-weekday-names",
	"day-period-names",
	"list-patterns",
	"cardinal-plural-rules",
	"compact-decimal-patterns",
	"duration-core-unit-patterns",
	"relative-time-patterns",
	"date-time-interval-patterns",
	"display-names-languages",
	"display-names-territories",
	"display-names-scripts",
	"display-names-calendars",
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
		o.SizeBudgetBytes = DefaultSizeBudget
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
	SizeReport   []SizeRow  `json:"size_report,omitempty"`
	Changed      []string   `json:"changed,omitempty"`
	Added        []string   `json:"added,omitempty"`
	Removed      []string   `json:"removed,omitempty"`
	SizeBudget   int64      `json:"size_budget_bytes"`
	SizeBudgetOK bool       `json:"size_budget_ok"`
	Diagnostics  []string   `json:"diagnostics,omitempty"`
}

// SizeRow reports generated model size by domain.
type SizeRow struct {
	Domain        string `json:"domain"`
	Rows          int    `json:"rows,omitempty"`
	RawRows       int    `json:"raw_rows,omitempty"`
	DeltaRows     int    `json:"delta_rows,omitempty"`
	SourceBytes   int    `json:"source_bytes,omitempty"`
	EncodedBytes  int    `json:"encoded_bytes,omitempty"`
	UniqueStrings int    `json:"unique_strings,omitempty"`
	StringBytes   int    `json:"string_bytes,omitempty"`
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
		SizeReport: modelSizeReport(model, len(source), len(lockBytes), packFootprint{}),
		SizeBudget: opts.SizeBudgetBytes, SizeBudgetOK: int64(len(source)) <= opts.SizeBudgetBytes,
	}
	if !result.SizeBudgetOK {
		return result, source, lockBytes, fmt.Errorf("generated data size %d exceeds budget %d", len(source), opts.SizeBudgetBytes)
	}
	return result, source, lockBytes, nil
}

// MeasureFootprint builds the generated source in memory and measures optional
// pack encodings. It is intentionally heavier than Generate so normal data
// checks do not pay zstd pack-build cost.
func MeasureFootprint(ctx context.Context, opts Options) (Result, error) {
	opts = opts.Normalize()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	lock, err := BuildLock(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	model, err := loadModel(ctx, opts.SourceDir)
	if err != nil {
		return Result{}, err
	}
	model.lock = lock
	source, err := render(model)
	if err != nil {
		return Result{}, err
	}
	lock.GeneratedAt = ""
	lockBytes, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return Result{}, err
	}
	lockBytes = append(lockBytes, '\n')
	packs, err := modelPackFootprint(model)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Lock: lock, OutputPath: opts.OutputPath, OutputBytes: len(source),
		SizeReport: modelSizeReport(model, len(source), len(lockBytes), packs),
		SizeBudget: opts.SizeBudgetBytes, SizeBudgetOK: int64(len(source)) <= opts.SizeBudgetBytes,
	}
	if !result.SizeBudgetOK {
		return result, fmt.Errorf("generated data size %d exceeds budget %d", len(source), opts.SizeBudgetBytes)
	}
	return result, nil
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
	bundle, err := newModelBundle(provider, "generated-cldrpack", "Generated CLDR pack", "external-pack")
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
			SizeReport: modelSizeReport(model, 0, 0, packFootprintForCodec(codec, len(pack))),
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

func packFootprintForCodec(codec cldrpack.Codec, bytes int) packFootprint {
	if codec == cldrpack.CodecZstd {
		return packFootprint{ZstdBytes: bytes}
	}
	return packFootprint{RawBytes: bytes}
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
	lock             SourceLock
	rawRows          map[string]int
	locales          []localeRecord
	regions          []regionDefault
	fractions        []currencyFraction
	symbols          []currencySymbol
	listPatterns     []listPatternRecord
	unitPatterns     []unitPatternRecord
	compactPatterns  []compactPatternRecord
	relativePatterns []relativePatternRecord
	relativeSpecials []relativeSpecialRecord
	intervalPatterns []intervalPatternRecord
	displayNames     []displayNameRecord
	bcp47            []bcp47Type
}

type localeRecord struct {
	Tag, Parent, NumberingSystem              string
	DateFormats, TimeFormats, DateTimeFormats [4]string
	MonthsWide, MonthsAbbr                    [12]string
	WeekdaysWide, WeekdaysAbbr                [7]string
	DayPeriods                                [2]string
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
	Locale, Code, Symbol, Narrow string
}

type listPatternRecord struct {
	Locale, Type, Width string
	Pattern             listPattern
}

type listPattern struct {
	Two, Start, Middle, End string
}

type unitPatternRecord struct {
	Locale, Unit, Width, Category, Pattern string
}

type compactPatternRecord struct {
	Locale, Width, Category, Pattern string
	Magnitude                        int64
}

type relativePatternRecord struct {
	Locale, Field, Width, Direction, Category, Pattern string
}

type relativeSpecialRecord struct {
	Locale, Field, Width, Text string
	Offset                     int
}

type intervalPatternRecord struct {
	Locale, Skeleton, Field, Pattern string
}

type displayNameRecord struct {
	Locale, Kind, Code, Name string
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
	rawLocales := len(m.locales)
	m.locales = sparseLocales(m.locales)
	m.regions = loadRegionDefaults(source)
	m.fractions = loadCurrencyFractions(source)
	rawSymbols := loadCurrencySymbols(source)
	m.symbols = sparseCurrencySymbols(rawSymbols, parentOverrides)
	rawListPatterns := loadListPatterns(source)
	m.listPatterns = sparseListPatterns(rawListPatterns, parentOverrides)
	rawUnitPatterns := loadUnitPatterns(source)
	m.unitPatterns = sparseUnitPatterns(rawUnitPatterns, parentOverrides)
	rawCompactPatterns := loadCompactPatterns(source)
	m.compactPatterns = sparseCompactPatterns(rawCompactPatterns, parentOverrides)
	rawRelativePatterns, rawRelativeSpecials := loadRelativeTime(source)
	m.relativePatterns, m.relativeSpecials = rawRelativePatterns, rawRelativeSpecials
	m.relativePatterns = sparseRelativePatterns(m.relativePatterns, parentOverrides)
	m.relativeSpecials = sparseRelativeSpecials(m.relativeSpecials, parentOverrides)
	rawIntervalPatterns := loadIntervalPatterns(source)
	m.intervalPatterns = sparseIntervalPatterns(rawIntervalPatterns, parentOverrides)
	rawDisplayNames := loadDisplayNames(source)
	m.displayNames = sparseDisplayNames(rawDisplayNames, parentOverrides)
	m.bcp47 = loadBCP47(source)
	m.rawRows = map[string]int{
		"locales":            rawLocales,
		"regions":            len(m.regions),
		"currency_fractions": len(m.fractions),
		"currency_symbols":   len(rawSymbols),
		"list_patterns":      len(rawListPatterns),
		"unit_patterns":      len(rawUnitPatterns),
		"compact_patterns":   len(rawCompactPatterns),
		"relative_patterns":  len(rawRelativePatterns),
		"relative_specials":  len(rawRelativeSpecials),
		"interval_patterns":  len(rawIntervalPatterns),
		"display_names":      len(rawDisplayNames),
		"bcp47":              len(m.bcp47),
	}
	sort.Slice(m.locales, func(i, j int) bool { return m.locales[i].Tag < m.locales[j].Tag })
	sort.Slice(m.regions, func(i, j int) bool { return m.regions[i].Region < m.regions[j].Region })
	sort.Slice(m.fractions, func(i, j int) bool { return m.fractions[i].Code < m.fractions[j].Code })
	sort.Slice(m.symbols, func(i, j int) bool { return currencySymbolKey(m.symbols[i]) < currencySymbolKey(m.symbols[j]) })
	sort.Slice(m.listPatterns, func(i, j int) bool { return listPatternKey(m.listPatterns[i]) < listPatternKey(m.listPatterns[j]) })
	sort.Slice(m.unitPatterns, func(i, j int) bool { return unitPatternKey(m.unitPatterns[i]) < unitPatternKey(m.unitPatterns[j]) })
	sort.Slice(m.compactPatterns, func(i, j int) bool {
		return compactPatternKey(m.compactPatterns[i]) < compactPatternKey(m.compactPatterns[j])
	})
	sort.Slice(m.relativePatterns, func(i, j int) bool {
		return relativePatternKey(m.relativePatterns[i]) < relativePatternKey(m.relativePatterns[j])
	})
	sort.Slice(m.relativeSpecials, func(i, j int) bool {
		return relativeSpecialKey(m.relativeSpecials[i]) < relativeSpecialKey(m.relativeSpecials[j])
	})
	sort.Slice(m.intervalPatterns, func(i, j int) bool {
		return intervalPatternKey(m.intervalPatterns[i]) < intervalPatternKey(m.intervalPatterns[j])
	})
	sort.Slice(m.displayNames, func(i, j int) bool { return displayNameKey(m.displayNames[i]) < displayNameKey(m.displayNames[j]) })
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
	out.DayPeriods = delta2(parent.DayPeriods, out.DayPeriods)
	if out.CurrencyPattern == parent.CurrencyPattern {
		out.CurrencyPattern = ""
	}
	if out.Accounting == parent.Accounting {
		out.Accounting = ""
	}
	return out
}

func delta2(parent, child [2]string) [2]string {
	for i := range child {
		if child[i] == parent[i] {
			child[i] = ""
		}
	}
	return child
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
	fillDayPeriods(&rec.DayPeriods, nestedMap(greg, "dayPeriods", "format", "abbreviated"))
}

func loadCurrencySymbols(source string) []currencySymbol {
	locales, err := localeDirs(filepath.Join(source, "cldr-numbers-full", "main"))
	if err != nil {
		return nil
	}
	out := []currencySymbol{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-numbers-full", "main", tag, "currencies.json"), &doc) {
			continue
		}
		currencies := nestedMap(doc, "main", tag, "numbers", "currencies")
		for code, raw := range currencies {
			info, _ := raw.(map[string]any)
			symbol := stringValue(info["symbol"], "")
			narrow := stringValue(info["symbol-alt-narrow"], "")
			if symbol != "" || narrow != "" {
				out = append(out, currencySymbol{Locale: tag, Code: strings.ToUpper(code), Symbol: symbol, Narrow: narrow})
			}
		}
	}
	return out
}

func loadListPatterns(source string) []listPatternRecord {
	locales, err := localeDirs(filepath.Join(source, "cldr-misc-full", "main"))
	if err != nil {
		return nil
	}
	out := []listPatternRecord{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-misc-full", "main", tag, "listPatterns.json"), &doc) {
			continue
		}
		patterns := nestedMap(doc, "main", tag, "listPatterns")
		for _, typ := range []string{"standard", "or", "unit"} {
			for _, width := range []string{"long", "short", "narrow"} {
				key := "listPattern-type-" + typ
				if width != "long" {
					key += "-" + width
				}
				m := nestedMap(patterns, key)
				rec := listPatternRecord{
					Locale: tag,
					Type:   typ,
					Width:  width,
					Pattern: listPattern{
						Two:    stringValue(m["2"], ""),
						Start:  stringValue(m["start"], ""),
						Middle: stringValue(m["middle"], ""),
						End:    stringValue(m["end"], ""),
					},
				}
				if rec.Pattern.Two != "" || rec.Pattern.Start != "" || rec.Pattern.Middle != "" || rec.Pattern.End != "" {
					out = append(out, rec)
				}
			}
		}
	}
	return out
}

func loadUnitPatterns(source string) []unitPatternRecord {
	locales, err := localeDirs(filepath.Join(source, "cldr-units-full", "main"))
	if err != nil {
		return nil
	}
	units := []string{"duration-year", "duration-month", "duration-week", "duration-day", "duration-hour", "duration-minute", "duration-second"}
	out := []unitPatternRecord{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-units-full", "main", tag, "units.json"), &doc) {
			continue
		}
		root := nestedMap(doc, "main", tag, "units")
		for _, width := range []string{"long", "short", "narrow"} {
			widthMap := nestedMap(root, width)
			for _, unit := range units {
				info := nestedMap(widthMap, unit)
				for _, category := range pluralCategories() {
					pattern := stringValue(info["unitPattern-count-"+category], "")
					if pattern != "" {
						out = append(out, unitPatternRecord{Locale: tag, Unit: unit, Width: width, Category: category, Pattern: pattern})
					}
				}
			}
		}
	}
	return out
}

func loadCompactPatterns(source string) []compactPatternRecord {
	locales, err := localeDirs(filepath.Join(source, "cldr-numbers-full", "main"))
	if err != nil {
		return nil
	}
	out := []compactPatternRecord{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-numbers-full", "main", tag, "numbers.json"), &doc) {
			continue
		}
		nums := nestedMap(doc, "main", tag, "numbers")
		for _, width := range []string{"short", "long"} {
			formatMap := nestedMap(nums, "decimalFormats-numberSystem-latn", width, "decimalFormat")
			for key, raw := range formatMap {
				parts := strings.Split(key, "-count-")
				if len(parts) != 2 {
					continue
				}
				magnitude, err := strconv.ParseInt(parts[0], 10, 64)
				if err != nil {
					continue
				}
				pattern := stringValue(raw, "")
				if pattern != "" {
					out = append(out, compactPatternRecord{Locale: tag, Width: width, Magnitude: magnitude, Category: parts[1], Pattern: pattern})
				}
			}
		}
	}
	return out
}

func loadRelativeTime(source string) ([]relativePatternRecord, []relativeSpecialRecord) {
	locales, err := localeDirs(filepath.Join(source, "cldr-dates-full", "main"))
	if err != nil {
		return nil, nil
	}
	fields := []string{"second", "minute", "hour", "day", "week", "month", "year"}
	patterns := []relativePatternRecord{}
	specials := []relativeSpecialRecord{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-dates-full", "main", tag, "dateFields.json"), &doc) {
			continue
		}
		root := nestedMap(doc, "main", tag, "dates", "fields")
		for _, field := range fields {
			for _, width := range []string{"long", "short", "narrow"} {
				key := field
				if width != "long" {
					key += "-" + width
				}
				info := nestedMap(root, key)
				for _, offset := range []int{-2, -1, 0, 1, 2} {
					text := stringValue(info["relative-type-"+strconv.Itoa(offset)], "")
					if text != "" {
						specials = append(specials, relativeSpecialRecord{Locale: tag, Field: field, Width: width, Offset: offset, Text: text})
					}
				}
				for _, direction := range []string{"future", "past"} {
					dirMap := nestedMap(info, "relativeTime-type-"+direction)
					for _, category := range pluralCategories() {
						pattern := stringValue(dirMap["relativeTimePattern-count-"+category], "")
						if pattern != "" {
							patterns = append(patterns, relativePatternRecord{Locale: tag, Field: field, Width: width, Direction: direction, Category: category, Pattern: pattern})
						}
					}
				}
			}
		}
	}
	return patterns, specials
}

func loadIntervalPatterns(source string) []intervalPatternRecord {
	locales, err := localeDirs(filepath.Join(source, "cldr-dates-full", "main"))
	if err != nil {
		return nil
	}
	out := []intervalPatternRecord{}
	for _, tag := range locales {
		var doc map[string]any
		if !readJSON(filepath.Join(source, "cldr-dates-full", "main", tag, "ca-gregorian.json"), &doc) {
			continue
		}
		intervals := nestedMap(doc, "main", tag, "dates", "calendars", "gregorian", "dateTimeFormats", "intervalFormats")
		for skeleton, raw := range intervals {
			if skeleton == "intervalFormatFallback" {
				continue
			}
			fields, _ := raw.(map[string]any)
			for field, patternRaw := range fields {
				pattern := stringValue(patternRaw, "")
				if pattern != "" {
					out = append(out, intervalPatternRecord{Locale: tag, Skeleton: skeleton, Field: field, Pattern: pattern})
				}
			}
		}
	}
	return out
}

func loadDisplayNames(source string) []displayNameRecord {
	locales, err := localeDirs(filepath.Join(source, "cldr-localenames-full", "main"))
	if err != nil {
		return nil
	}
	out := []displayNameRecord{}
	for _, tag := range locales {
		out = append(out, loadDisplayNameFile(source, tag, "languages.json", "language", "languages")...)
		out = append(out, loadDisplayNameFile(source, tag, "territories.json", "territory", "territories")...)
		out = append(out, loadDisplayNameFile(source, tag, "scripts.json", "script", "scripts")...)
		var doc map[string]any
		if readJSON(filepath.Join(source, "cldr-localenames-full", "main", tag, "localeDisplayNames.json"), &doc) {
			cals := nestedMap(doc, "main", tag, "localeDisplayNames", "types", "calendar")
			for code, raw := range cals {
				if displayNameKeyAllowed(code) {
					out = append(out, displayNameRecord{Locale: tag, Kind: "calendar", Code: code, Name: stringValue(raw, "")})
				}
			}
		}
	}
	return out
}

func loadDisplayNameFile(source, tag, file, kind, key string) []displayNameRecord {
	var doc map[string]any
	if !readJSON(filepath.Join(source, "cldr-localenames-full", "main", tag, file), &doc) {
		return nil
	}
	values := nestedMap(doc, "main", tag, "localeDisplayNames", key)
	out := []displayNameRecord{}
	for code, raw := range values {
		if displayNameKeyAllowed(code) {
			out = append(out, displayNameRecord{Locale: tag, Kind: kind, Code: code, Name: stringValue(raw, "")})
		}
	}
	return out
}

func displayNameKeyAllowed(code string) bool {
	return !strings.Contains(code, "-alt-") && !strings.Contains(code, "-menu-") && !strings.HasSuffix(code, "-core")
}

func pluralCategories() []string {
	return []string{"zero", "one", "two", "few", "many", "other"}
}

func parentFor(tag string, overrides map[string]string) string {
	if p := overrides[tag]; p != "" {
		return p
	}
	return parentOf(tag)
}

func sparseCurrencySymbols(records []currencySymbol, overrides map[string]string) []currencySymbol {
	full := map[string]currencySymbol{}
	for _, r := range records {
		full[currencySymbolKey(r)] = r
	}
	out := make([]currencySymbol, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[currencySymbolKey(parent)]; ok && p.Symbol == r.Symbol && p.Narrow == r.Narrow {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseListPatterns(records []listPatternRecord, overrides map[string]string) []listPatternRecord {
	full := map[string]listPatternRecord{}
	for _, r := range records {
		full[listPatternKey(r)] = r
	}
	out := make([]listPatternRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[listPatternKey(parent)]; ok && p.Pattern == r.Pattern {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseUnitPatterns(records []unitPatternRecord, overrides map[string]string) []unitPatternRecord {
	full := map[string]unitPatternRecord{}
	for _, r := range records {
		full[unitPatternKey(r)] = r
	}
	out := make([]unitPatternRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[unitPatternKey(parent)]; ok && p.Pattern == r.Pattern {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseCompactPatterns(records []compactPatternRecord, overrides map[string]string) []compactPatternRecord {
	full := map[string]compactPatternRecord{}
	for _, r := range records {
		full[compactPatternKey(r)] = r
	}
	out := make([]compactPatternRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[compactPatternKey(parent)]; ok && p.Pattern == r.Pattern {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseRelativePatterns(records []relativePatternRecord, overrides map[string]string) []relativePatternRecord {
	full := map[string]relativePatternRecord{}
	for _, r := range records {
		full[relativePatternKey(r)] = r
	}
	out := make([]relativePatternRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[relativePatternKey(parent)]; ok && p.Pattern == r.Pattern {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseRelativeSpecials(records []relativeSpecialRecord, overrides map[string]string) []relativeSpecialRecord {
	full := map[string]relativeSpecialRecord{}
	for _, r := range records {
		full[relativeSpecialKey(r)] = r
	}
	out := make([]relativeSpecialRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[relativeSpecialKey(parent)]; ok && p.Text == r.Text {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseIntervalPatterns(records []intervalPatternRecord, overrides map[string]string) []intervalPatternRecord {
	full := map[string]intervalPatternRecord{}
	for _, r := range records {
		full[intervalPatternKey(r)] = r
	}
	out := make([]intervalPatternRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[intervalPatternKey(parent)]; ok && p.Pattern == r.Pattern {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sparseDisplayNames(records []displayNameRecord, overrides map[string]string) []displayNameRecord {
	full := map[string]displayNameRecord{}
	for _, r := range records {
		full[displayNameKey(r)] = r
	}
	out := make([]displayNameRecord, 0, len(records))
	for _, r := range records {
		parent := r
		parent.Locale = parentFor(r.Locale, overrides)
		if p, ok := full[displayNameKey(parent)]; ok && p.Name == r.Name {
			continue
		}
		out = append(out, r)
	}
	return out
}

func currencySymbolKey(r currencySymbol) string {
	return r.Locale + "\x00" + r.Code
}

func listPatternKey(r listPatternRecord) string {
	return r.Locale + "\x00" + r.Type + "\x00" + r.Width
}

func unitPatternKey(r unitPatternRecord) string {
	return r.Locale + "\x00" + r.Unit + "\x00" + r.Width + "\x00" + r.Category
}

func compactPatternKey(r compactPatternRecord) string {
	return r.Locale + "\x00" + r.Width + "\x00" + sortableInt(r.Magnitude) + "\x00" + r.Category
}

func relativePatternKey(r relativePatternRecord) string {
	return r.Locale + "\x00" + r.Field + "\x00" + r.Width + "\x00" + r.Direction + "\x00" + r.Category
}

func relativeSpecialKey(r relativeSpecialRecord) string {
	return r.Locale + "\x00" + r.Field + "\x00" + r.Width + "\x00" + sortableInt(int64(r.Offset))
}

func intervalPatternKey(r intervalPatternRecord) string {
	return r.Locale + "\x00" + r.Skeleton + "\x00" + r.Field
}

func displayNameKey(r displayNameRecord) string {
	return r.Locale + "\x00" + r.Kind + "\x00" + r.Code
}

func sortableInt(n int64) string {
	if n < 0 {
		return "-" + sortableInt(-n)
	}
	s := strconv.FormatInt(n, 10)
	return strings.Repeat("0", 20-len(s)) + s
}

func normalizeWidth(width string) string {
	switch strings.ToLower(strings.TrimSpace(width)) {
	case "short":
		return "short"
	case "narrow":
		return "narrow"
	default:
		return "long"
	}
}

func normalizeCompactWidth(width string) string {
	switch strings.ToLower(strings.TrimSpace(width)) {
	case "long":
		return "long"
	default:
		return "short"
	}
}

func normalizeCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "zero", "one", "two", "few", "many":
		return strings.ToLower(strings.TrimSpace(category))
	default:
		return "other"
	}
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
	b.WriteString("// Code generated by lingo cldrgen; DO NOT EDIT.\n\n//go:build !staticcheck\n\npackage cldrdata\n\n")
	writeMetadata(&b, m.lock)
	writeLocales(&b, m.locales)
	writeRegions(&b, m.regions)
	writeFractions(&b, m.fractions)
	writeSymbols(&b, m.symbols)
	writeListPatterns(&b, m.listPatterns)
	writeUnitPatterns(&b, m.unitPatterns)
	writeCompactPatterns(&b, m.compactPatterns)
	writeRelativePatterns(&b, m.relativePatterns)
	writeRelativeSpecials(&b, m.relativeSpecials)
	writeIntervalPatterns(&b, m.intervalPatterns)
	writeDisplayNames(&b, m.displayNames)
	writeBCP47(&b, m.bcp47)
	return format.Source(b.Bytes())
}

type packFootprint struct {
	RawBytes  int
	ZstdBytes int
}

type modelDomainReport struct {
	domain  string
	rows    int
	rowSize int
	write   func(*bytes.Buffer)
	strings func() []string
}

const (
	encodedStringRefBytes = 8

	encodedLocaleRowBytes          = 57 * encodedStringRefBytes
	encodedRegionRowBytes          = 5 * encodedStringRefBytes
	encodedFractionRowBytes        = encodedStringRefBytes + 8
	encodedSymbolRowBytes          = 4 * encodedStringRefBytes
	encodedBCP47RowBytes           = 3 * encodedStringRefBytes
	encodedListPatternRowBytes     = 7 * encodedStringRefBytes
	encodedUnitPatternRowBytes     = 5 * encodedStringRefBytes
	encodedCompactPatternRowBytes  = 5 * encodedStringRefBytes
	encodedRelativePatternRowBytes = 6 * encodedStringRefBytes
	encodedRelativeSpecialRowBytes = 5 * encodedStringRefBytes
	encodedIntervalPatternRowBytes = 4 * encodedStringRefBytes
	encodedDisplayNameRowBytes     = 4 * encodedStringRefBytes
)

func modelPackFootprint(m model) (packFootprint, error) {
	provider := modelProvider{model: m}
	bundle, err := newModelBundle(provider, "generated-cldrpack", "Generated CLDR pack", "external-pack")
	if err != nil {
		return packFootprint{}, err
	}
	raw, err := cldrpack.Build(bundle, cldrpack.WithCodec(cldrpack.CodecRaw))
	if err != nil {
		return packFootprint{}, err
	}
	zstd, err := cldrpack.Build(bundle, cldrpack.WithCodec(cldrpack.CodecZstd))
	if err != nil {
		return packFootprint{}, err
	}
	return packFootprint{RawBytes: len(raw), ZstdBytes: len(zstd)}, nil
}

func newModelBundle(provider cldr.DataProvider, id, name, mode string) (cldr.Bundle, error) {
	info := cldr.Info{
		ID:       id,
		Name:     name,
		Mode:     mode,
		Versions: providerVersions(provider.Metadata()),
		Features: modelFeatures(),
		Locales:  provider.AvailableLocales(),
		Digest:   provider.Metadata().TreeDigest,
	}
	return cldr.NewBundle(info, modelCoverage(provider), provider, nil)
}

func modelSizeReport(m model, sourceBytes, lockBytes int, packs packFootprint) []SizeRow {
	domains := []modelDomainReport{
		{
			domain: "locales", rows: len(m.locales), rowSize: encodedLocaleRowBytes,
			write: func(b *bytes.Buffer) { writeLocales(b, m.locales) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.locales {
					values = append(values, r.Tag, r.Parent, r.NumberingSystem, r.CurrencyPattern, r.Accounting)
					values = append(values, r.DateFormats[:]...)
					values = append(values, r.TimeFormats[:]...)
					values = append(values, r.DateTimeFormats[:]...)
					values = append(values, r.MonthsWide[:]...)
					values = append(values, r.MonthsAbbr[:]...)
					values = append(values, r.WeekdaysWide[:]...)
					values = append(values, r.WeekdaysAbbr[:]...)
					values = append(values, r.DayPeriods[:]...)
				}
				return values
			},
		},
		{
			domain: "regions", rows: len(m.regions), rowSize: encodedRegionRowBytes,
			write: func(b *bytes.Buffer) { writeRegions(b, m.regions) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.regions {
					values = append(values, r.Region, r.Currency, r.MeasurementSystem, r.FirstDay, r.TimeZone)
				}
				return values
			},
		},
		{
			domain: "currency_fractions", rows: len(m.fractions), rowSize: encodedFractionRowBytes,
			write: func(b *bytes.Buffer) { writeFractions(b, m.fractions) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.fractions {
					values = append(values, r.Code)
				}
				return values
			},
		},
		{
			domain: "currency_symbols", rows: len(m.symbols), rowSize: encodedSymbolRowBytes,
			write: func(b *bytes.Buffer) { writeSymbols(b, m.symbols) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.symbols {
					values = append(values, r.Locale, r.Code, r.Symbol, r.Narrow)
				}
				return values
			},
		},
		{
			domain: "list_patterns", rows: len(m.listPatterns), rowSize: encodedListPatternRowBytes,
			write: func(b *bytes.Buffer) { writeListPatterns(b, m.listPatterns) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.listPatterns {
					values = append(values, r.Locale, r.Type, r.Width, r.Pattern.Two, r.Pattern.Start, r.Pattern.Middle, r.Pattern.End)
				}
				return values
			},
		},
		{
			domain: "unit_patterns", rows: len(m.unitPatterns), rowSize: encodedUnitPatternRowBytes,
			write: func(b *bytes.Buffer) { writeUnitPatterns(b, m.unitPatterns) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.unitPatterns {
					values = append(values, r.Locale, r.Unit, r.Width, r.Category, r.Pattern)
				}
				return values
			},
		},
		{
			domain: "compact_patterns", rows: len(m.compactPatterns), rowSize: encodedCompactPatternRowBytes,
			write: func(b *bytes.Buffer) { writeCompactPatterns(b, m.compactPatterns) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.compactPatterns {
					values = append(values, r.Locale, r.Width, r.Category, r.Pattern)
				}
				return values
			},
		},
		{
			domain: "relative_patterns", rows: len(m.relativePatterns), rowSize: encodedRelativePatternRowBytes,
			write: func(b *bytes.Buffer) { writeRelativePatterns(b, m.relativePatterns) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.relativePatterns {
					values = append(values, r.Locale, r.Field, r.Width, r.Direction, r.Category, r.Pattern)
				}
				return values
			},
		},
		{
			domain: "relative_specials", rows: len(m.relativeSpecials), rowSize: encodedRelativeSpecialRowBytes,
			write: func(b *bytes.Buffer) { writeRelativeSpecials(b, m.relativeSpecials) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.relativeSpecials {
					values = append(values, r.Locale, r.Field, r.Width, r.Text)
				}
				return values
			},
		},
		{
			domain: "interval_patterns", rows: len(m.intervalPatterns), rowSize: encodedIntervalPatternRowBytes,
			write: func(b *bytes.Buffer) { writeIntervalPatterns(b, m.intervalPatterns) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.intervalPatterns {
					values = append(values, r.Locale, r.Skeleton, r.Field, r.Pattern)
				}
				return values
			},
		},
		{
			domain: "display_names", rows: len(m.displayNames), rowSize: encodedDisplayNameRowBytes,
			write: func(b *bytes.Buffer) { writeDisplayNames(b, m.displayNames) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.displayNames {
					values = append(values, r.Locale, r.Kind, r.Code, r.Name)
				}
				return values
			},
		},
		{
			domain: "bcp47", rows: len(m.bcp47), rowSize: encodedBCP47RowBytes,
			write: func(b *bytes.Buffer) { writeBCP47(b, m.bcp47) },
			strings: func() []string {
				values := []string{}
				for _, r := range m.bcp47 {
					values = append(values, r.Key, r.Type, r.Alias)
				}
				return values
			},
		},
	}
	rows := []SizeRow{}
	if sourceBytes > 0 {
		rows = append(rows, SizeRow{Domain: "generated_go", SourceBytes: sourceBytes})
	}
	if lockBytes > 0 {
		rows = append(rows, SizeRow{Domain: "source_lock", SourceBytes: lockBytes})
	}
	if packs.RawBytes > 0 {
		rows = append(rows, SizeRow{Domain: "cldrpack_raw", EncodedBytes: packs.RawBytes})
	}
	if packs.ZstdBytes > 0 {
		rows = append(rows, SizeRow{Domain: "cldrpack_zstd", EncodedBytes: packs.ZstdBytes})
	}
	for _, domain := range domains {
		unique, stringBytes := stringStats(domain.strings())
		rows = append(rows, SizeRow{
			Domain:        domain.domain,
			Rows:          domain.rows,
			RawRows:       m.rawRows[domain.domain],
			DeltaRows:     domain.rows,
			SourceBytes:   renderedSectionBytes(domain.write),
			EncodedBytes:  domain.rows * domain.rowSize,
			UniqueStrings: unique,
			StringBytes:   stringBytes,
		})
	}
	return rows
}

func renderedSectionBytes(write func(*bytes.Buffer)) int {
	var b bytes.Buffer
	write(&b)
	if src, err := format.Source(b.Bytes()); err == nil {
		return len(src)
	}
	return b.Len()
}

func stringStats(values []string) (int, int) {
	seen := map[string]bool{}
	bytes := 0
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		bytes += len(value)
	}
	return len(seen), bytes
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
		if has2(r.DayPeriods) {
			writeField(b, "DayPeriods", arr2(r.DayPeriods))
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
		fmt.Fprintf(b, "{Locale:%s, Code:%s, Symbol:%s, Narrow:%s},\n", q(r.Locale), q(r.Code), q(r.Symbol), q(r.Narrow))
	}
	b.WriteString("}\n\n")
}

func writeListPatterns(b *bytes.Buffer, records []listPatternRecord) {
	b.WriteString("var listPatterns = []ListPatternRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Type:%s, Width:%s, Pattern:ListPattern{Two:%s, Start:%s, Middle:%s, End:%s}},\n",
			q(r.Locale), q(r.Type), q(r.Width), q(r.Pattern.Two), q(r.Pattern.Start), q(r.Pattern.Middle), q(r.Pattern.End))
	}
	b.WriteString("}\n\n")
}

func writeUnitPatterns(b *bytes.Buffer, records []unitPatternRecord) {
	b.WriteString("var unitPatterns = []UnitPatternRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Unit:%s, Width:%s, Category:%s, Pattern:%s},\n", q(r.Locale), q(r.Unit), q(r.Width), q(r.Category), q(r.Pattern))
	}
	b.WriteString("}\n\n")
}

func writeCompactPatterns(b *bytes.Buffer, records []compactPatternRecord) {
	b.WriteString("var compactPatterns = []CompactPatternRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Width:%s, Magnitude:%d, Category:%s, Pattern:%s},\n", q(r.Locale), q(r.Width), r.Magnitude, q(r.Category), q(r.Pattern))
	}
	b.WriteString("}\n\n")
}

func writeRelativePatterns(b *bytes.Buffer, records []relativePatternRecord) {
	b.WriteString("var relativePatterns = []RelativePatternRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Field:%s, Width:%s, Direction:%s, Category:%s, Pattern:%s},\n",
			q(r.Locale), q(r.Field), q(r.Width), q(r.Direction), q(r.Category), q(r.Pattern))
	}
	b.WriteString("}\n\n")
}

func writeRelativeSpecials(b *bytes.Buffer, records []relativeSpecialRecord) {
	b.WriteString("var relativeSpecials = []RelativeSpecialRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Field:%s, Width:%s, Offset:%d, Text:%s},\n", q(r.Locale), q(r.Field), q(r.Width), r.Offset, q(r.Text))
	}
	b.WriteString("}\n\n")
}

func writeIntervalPatterns(b *bytes.Buffer, records []intervalPatternRecord) {
	b.WriteString("var intervalPatterns = []IntervalPatternRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Skeleton:%s, Field:%s, Pattern:%s},\n", q(r.Locale), q(r.Skeleton), q(r.Field), q(r.Pattern))
	}
	b.WriteString("}\n\n")
}

func writeDisplayNames(b *bytes.Buffer, records []displayNameRecord) {
	b.WriteString("var displayNames = []DisplayNameRecord{\n")
	for _, r := range records {
		fmt.Fprintf(b, "{Locale:%s, Kind:%s, Code:%s, Name:%s},\n", q(r.Locale), q(r.Kind), q(r.Code), q(r.Name))
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
func arr2(a [2]string) string {
	return "[2]string{" + q(a[0]) + "," + q(a[1]) + "}"
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

func has2(a [2]string) bool {
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
	for _, sub := range []string{"cldr-core", "cldr-bcp47", "cldr-numbers-full", "cldr-dates-full", "cldr-misc-full", "cldr-units-full", "cldr-localenames-full"} {
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

func fillDayPeriods(out *[2]string, m map[string]any) {
	out[0] = stringValue(m["am"], "")
	out[1] = stringValue(m["pm"], "")
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
