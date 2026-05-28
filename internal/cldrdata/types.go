package cldrdata

import (
	"sort"
	"strings"
)

// Metadata describes the generated Unicode data bundle. All paths are
// repository-relative source identities, never local absolute paths.
type Metadata struct {
	CLDRVersion    string
	UnicodeVersion string
	Generator      string
	LockID         string
	FeatureSet     []string
	SourceIdentity string
	TreeDigest     string
	License        string
}

// RegionDefaults contains CLDR supplemental defaults for a territory. Values
// are formatting defaults only and are not legal, tax, residency, or financial
// truth.
type RegionDefaults struct {
	Region            string
	Currency          string
	MeasurementSystem string
	FirstDay          string
	TimeZone          string
}

// CurrencyFraction carries ISO 4217 display precision metadata.
type CurrencyFraction struct {
	Code       string
	Digits     int
	CashDigits int
	Rounding   int
}

// DateWidth indexes CLDR short/medium/long/full width arrays.
type DateWidth int

const (
	WidthShort DateWidth = iota
	WidthMedium
	WidthLong
	WidthFull
	widthCount
)

// WidthIndex maps a CLDR width string to its compact index.
func WidthIndex(width string) (DateWidth, bool) {
	switch strings.ToLower(strings.TrimSpace(width)) {
	case "short":
		return WidthShort, true
	case "", "medium":
		return WidthMedium, true
	case "long":
		return WidthLong, true
	case "full":
		return WidthFull, true
	default:
		return WidthMedium, false
	}
}

// ListPattern stores one CLDR list pattern set.
type ListPattern struct {
	Two    string
	Start  string
	Middle string
	End    string
}

// LocaleRecord stores locale-specific formatting data. Generated records are
// sparse parent deltas; Provider.Locale returns an inherited effective record.
type LocaleRecord struct {
	Tag             string
	Parent          string
	NumberingSystem string
	DateFormats     [widthCount]string
	TimeFormats     [widthCount]string
	DateTimeFormats [widthCount]string
	MonthsWide      [12]string
	MonthsAbbr      [12]string
	WeekdaysWide    [7]string
	WeekdaysAbbr    [7]string
	CurrencyPattern string
	Accounting      string
}

// CurrencySymbolRecord stores a tiny global currency symbol.
type CurrencySymbolRecord struct {
	Code   string
	Symbol string
}

// BCP47TypeRecord stores known BCP-47 extension type values.
type BCP47TypeRecord struct {
	Key   string
	Type  string
	Alias string
}

// Provider is the read-only data boundary consumed by locale resolution and
// formatting. Implementations must be safe for concurrent use.
type Provider interface {
	Metadata() Metadata
	Locale(tag string) (LocaleRecord, bool)
	Parent(tag string) (string, bool)
	RegionDefaults(region string) (RegionDefaults, bool)
	CurrencyFraction(code string) CurrencyFraction
	CurrencySymbol(locale, code string) (string, bool)
	BCP47Types(key string) []BCP47TypeRecord
	AvailableLocales() []string
	AvailableRegions() []string
	AvailableCurrencyFractions() []CurrencyFraction
	AvailableCurrencySymbols() []CurrencySymbolRecord
	AvailableBCP47Keys() []string
}

type bundle struct{}

// Default returns the built-in generated CLDR data provider.
func Default() Provider { return bundle{} }

func (bundle) Metadata() Metadata { return metadata }

func (bundle) AvailableLocales() []string {
	out := make([]string, len(localeRecords))
	for i, rec := range localeRecords {
		out[i] = rec.Tag
	}
	return out
}

func (bundle) AvailableRegions() []string {
	out := make([]string, len(regionDefaults))
	for i, rec := range regionDefaults {
		out[i] = rec.Region
	}
	return out
}

func (bundle) AvailableCurrencyFractions() []CurrencyFraction {
	return append([]CurrencyFraction(nil), currencyFractions...)
}

func (bundle) AvailableCurrencySymbols() []CurrencySymbolRecord {
	return append([]CurrencySymbolRecord(nil), currencySymbols...)
}

func (bundle) AvailableBCP47Keys() []string {
	keys := []string{}
	var last string
	for _, rec := range bcp47Types {
		if rec.Key != last {
			keys = append(keys, rec.Key)
			last = rec.Key
		}
	}
	return keys
}

func (bundle) Locale(tag string) (LocaleRecord, bool) {
	tag = canonicalTag(tag)
	return resolveLocale(tag, map[string]bool{})
}

func resolveLocale(tag string, seen map[string]bool) (LocaleRecord, bool) {
	if seen[tag] {
		return LocaleRecord{}, false
	}
	seen[tag] = true
	i := sort.Search(len(localeRecords), func(i int) bool { return localeRecords[i].Tag >= tag })
	if i < len(localeRecords) && localeRecords[i].Tag == tag {
		rec := localeRecords[i]
		if rec.Parent == "" {
			return rec, true
		}
		parent, ok := resolveLocale(rec.Parent, seen)
		if !ok {
			return rec, true
		}
		return mergeLocale(parent, rec), true
	}
	return LocaleRecord{}, false
}

func (bundle) Parent(tag string) (string, bool) {
	rec, ok := bundle{}.Locale(tag)
	if !ok || rec.Parent == "" {
		return "", false
	}
	return rec.Parent, true
}

func (bundle) RegionDefaults(region string) (RegionDefaults, bool) {
	region = strings.ToUpper(strings.TrimSpace(region))
	i := sort.Search(len(regionDefaults), func(i int) bool { return regionDefaults[i].Region >= region })
	if i < len(regionDefaults) && regionDefaults[i].Region == region {
		return regionDefaults[i], true
	}
	i = sort.Search(len(regionDefaults), func(i int) bool { return regionDefaults[i].Region >= "001" })
	if i < len(regionDefaults) && regionDefaults[i].Region == "001" {
		return regionDefaults[i], true
	}
	return RegionDefaults{}, false
}

func (bundle) CurrencyFraction(code string) CurrencyFraction {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(len(currencyFractions), func(i int) bool { return currencyFractions[i].Code >= code })
	if i < len(currencyFractions) && currencyFractions[i].Code == code {
		return currencyFractions[i]
	}
	return CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
}

func (bundle) CurrencySymbol(_ string, code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(len(currencySymbols), func(i int) bool { return currencySymbols[i].Code >= code })
	if i < len(currencySymbols) && currencySymbols[i].Code == code {
		return currencySymbols[i].Symbol, true
	}
	return "", false
}

func (bundle) BCP47Types(key string) []BCP47TypeRecord {
	key = strings.TrimSpace(key)
	start := sort.Search(len(bcp47Types), func(i int) bool { return bcp47Types[i].Key >= key })
	out := []BCP47TypeRecord{}
	for i := start; i < len(bcp47Types) && bcp47Types[i].Key == key; i++ {
		out = append(out, bcp47Types[i])
	}
	return out
}

func mergeLocale(parent, child LocaleRecord) LocaleRecord {
	out := parent
	out.Tag = child.Tag
	out.Parent = child.Parent
	if child.NumberingSystem != "" {
		out.NumberingSystem = child.NumberingSystem
	}
	mergeArray4(&out.DateFormats, child.DateFormats)
	mergeArray4(&out.TimeFormats, child.TimeFormats)
	mergeArray4(&out.DateTimeFormats, child.DateTimeFormats)
	mergeArray12(&out.MonthsWide, child.MonthsWide)
	mergeArray12(&out.MonthsAbbr, child.MonthsAbbr)
	mergeArray7(&out.WeekdaysWide, child.WeekdaysWide)
	mergeArray7(&out.WeekdaysAbbr, child.WeekdaysAbbr)
	if child.CurrencyPattern != "" {
		out.CurrencyPattern = child.CurrencyPattern
	}
	if child.Accounting != "" {
		out.Accounting = child.Accounting
	}
	return out
}

func mergeArray4(dst *[widthCount]string, src [widthCount]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func mergeArray12(dst *[12]string, src [12]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func mergeArray7(dst *[7]string, src [7]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func canonicalTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "en"
	}
	parts := strings.FieldsFunc(tag, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(part)
			continue
		}
		if len(part) == 2 || len(part) == 3 && strings.ToUpper(part) == part {
			parts[i] = strings.ToUpper(part)
			continue
		}
		if len(part) == 4 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			continue
		}
		parts[i] = strings.ToLower(part)
	}
	return strings.Join(parts, "-")
}
