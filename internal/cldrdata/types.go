package cldrdata

import (
	"sort"
	"strconv"
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

// CurrencyDisplayMode selects how a currency code is rendered.
type CurrencyDisplayMode string

const (
	CurrencyDisplaySymbol       CurrencyDisplayMode = "symbol"
	CurrencyDisplayNarrowSymbol CurrencyDisplayMode = "narrow-symbol"
	CurrencyDisplayCode         CurrencyDisplayMode = "code"
)

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
	DayPeriods      [2]string
	CurrencyPattern string
	Accounting      string
}

// CurrencySymbolRecord stores locale-specific currency display symbols.
type CurrencySymbolRecord struct {
	Locale string
	Code   string
	Symbol string
	Narrow string
}

// UnitPatternRecord stores one localized unit pattern for one plural category.
type UnitPatternRecord struct {
	Locale   string
	Unit     string
	Width    string
	Category string
	Pattern  string
}

// ListPatternRecord stores one localized list pattern set.
type ListPatternRecord struct {
	Locale  string
	Type    string
	Width   string
	Pattern ListPattern
}

// CompactPatternRecord stores one compact-decimal pattern.
type CompactPatternRecord struct {
	Locale    string
	Width     string
	Magnitude int64
	Category  string
	Pattern   string
}

// RelativePatternRecord stores one relative-time pattern.
type RelativePatternRecord struct {
	Locale    string
	Field     string
	Width     string
	Direction string
	Category  string
	Pattern   string
}

// RelativeSpecialRecord stores one named relative-time value such as today.
type RelativeSpecialRecord struct {
	Locale string
	Field  string
	Width  string
	Offset int
	Text   string
}

// IntervalPatternRecord stores one Gregorian interval pattern.
type IntervalPatternRecord struct {
	Locale   string
	Skeleton string
	Field    string
	Pattern  string
}

// DisplayNameRecord stores one localized code display name.
type DisplayNameRecord struct {
	Locale string
	Kind   string
	Code   string
	Name   string
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
	CurrencySymbol(locale, code string, display CurrencyDisplayMode) (string, bool)
	ListPattern(locale, typ, width string) (ListPattern, bool)
	UnitPattern(locale, unit, width, category string) (string, bool)
	CompactPattern(locale, width string, magnitude int64, category string) (string, bool)
	RelativeTimePattern(locale, field, width, direction, category string) (string, bool)
	RelativeSpecial(locale, field, width string, offset int) (string, bool)
	IntervalPattern(locale, skeleton, field string) (string, bool)
	DisplayName(locale, kind, code string) (string, bool)
	BCP47Types(key string) []BCP47TypeRecord
	AvailableLocales() []string
	AvailableRegions() []string
	AvailableCurrencyFractions() []CurrencyFraction
	AvailableCurrencySymbols() []CurrencySymbolRecord
	AvailableListPatterns() []ListPatternRecord
	AvailableUnitPatterns() []UnitPatternRecord
	AvailableCompactPatterns() []CompactPatternRecord
	AvailableRelativeTimePatterns() []RelativePatternRecord
	AvailableRelativeSpecials() []RelativeSpecialRecord
	AvailableIntervalPatterns() []IntervalPatternRecord
	AvailableDisplayNames() []DisplayNameRecord
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

func (bundle) AvailableListPatterns() []ListPatternRecord {
	return append([]ListPatternRecord(nil), listPatterns...)
}

func (bundle) AvailableUnitPatterns() []UnitPatternRecord {
	return append([]UnitPatternRecord(nil), unitPatterns...)
}

func (bundle) AvailableCompactPatterns() []CompactPatternRecord {
	return append([]CompactPatternRecord(nil), compactPatterns...)
}

func (bundle) AvailableRelativeTimePatterns() []RelativePatternRecord {
	return append([]RelativePatternRecord(nil), relativePatterns...)
}

func (bundle) AvailableRelativeSpecials() []RelativeSpecialRecord {
	return append([]RelativeSpecialRecord(nil), relativeSpecials...)
}

func (bundle) AvailableIntervalPatterns() []IntervalPatternRecord {
	return append([]IntervalPatternRecord(nil), intervalPatterns...)
}

func (bundle) AvailableDisplayNames() []DisplayNameRecord {
	return append([]DisplayNameRecord(nil), displayNames...)
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

func (bundle) CurrencySymbol(locale, code string, display CurrencyDisplayMode) (string, bool) {
	if display == CurrencyDisplayCode {
		code = strings.ToUpper(strings.TrimSpace(code))
		return code, code != ""
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	for cur := canonicalTag(locale); cur != ""; {
		if sym, ok := currencySymbolExact(cur, code, display); ok {
			return sym, true
		}
		parent, ok := bundle{}.Parent(cur)
		if !ok || parent == cur {
			parent = parentTag(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return currencySymbolExact("en", code, display)
}

func currencySymbolExact(locale, code string, display CurrencyDisplayMode) (string, bool) {
	locale = canonicalTag(locale)
	code = strings.ToUpper(strings.TrimSpace(code))
	key := locale + "\x00" + code
	i := sort.Search(len(currencySymbols), func(i int) bool {
		return currencySymbols[i].Locale+"\x00"+currencySymbols[i].Code >= key
	})
	if i < len(currencySymbols) && currencySymbols[i].Locale == locale && currencySymbols[i].Code == code {
		if display == CurrencyDisplayNarrowSymbol && currencySymbols[i].Narrow != "" {
			return currencySymbols[i].Narrow, true
		}
		if currencySymbols[i].Symbol != "" {
			return currencySymbols[i].Symbol, true
		}
	}
	return "", false
}

func (bundle) ListPattern(locale, typ, width string) (ListPattern, bool) {
	for cur := canonicalTag(locale); cur != ""; {
		if pattern, ok := listPatternExact(cur, typ, width); ok {
			return pattern, true
		}
		parent, ok := bundle{}.Parent(cur)
		if !ok || parent == cur {
			parent = parentTag(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return listPatternExact("en", typ, width)
}

func listPatternExact(locale, typ, width string) (ListPattern, bool) {
	locale = canonicalTag(locale)
	typ = strings.TrimSpace(typ)
	width = normalizeWidth(width)
	key := locale + "\x00" + typ + "\x00" + width
	i := sort.Search(len(listPatterns), func(i int) bool {
		return listPatterns[i].Locale+"\x00"+listPatterns[i].Type+"\x00"+listPatterns[i].Width >= key
	})
	if i < len(listPatterns) && listPatterns[i].Locale == locale && listPatterns[i].Type == typ && listPatterns[i].Width == width {
		return listPatterns[i].Pattern, true
	}
	return ListPattern{}, false
}

func (bundle) UnitPattern(locale, unit, width, category string) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return unitPatternExact(cur, unit, width, category)
	})
}

func unitPatternExact(locale, unit, width, category string) (string, bool) {
	locale = canonicalTag(locale)
	unit = strings.TrimSpace(unit)
	width = normalizeWidth(width)
	category = normalizeCategory(category)
	key := locale + "\x00" + unit + "\x00" + width + "\x00" + category
	i := sort.Search(len(unitPatterns), func(i int) bool {
		r := unitPatterns[i]
		return r.Locale+"\x00"+r.Unit+"\x00"+r.Width+"\x00"+r.Category >= key
	})
	if i < len(unitPatterns) {
		r := unitPatterns[i]
		if r.Locale == locale && r.Unit == unit && r.Width == width && r.Category == category {
			return r.Pattern, true
		}
	}
	if category != "other" {
		return unitPatternExact(locale, unit, width, "other")
	}
	return "", false
}

func (bundle) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return compactPatternExact(cur, width, magnitude, category)
	})
}

func compactPatternExact(locale, width string, magnitude int64, category string) (string, bool) {
	locale = canonicalTag(locale)
	width = normalizeCompactWidth(width)
	category = normalizeCategory(category)
	key := locale + "\x00" + width + "\x00" + intKey(magnitude) + "\x00" + category
	i := sort.Search(len(compactPatterns), func(i int) bool {
		r := compactPatterns[i]
		return r.Locale+"\x00"+r.Width+"\x00"+intKey(r.Magnitude)+"\x00"+r.Category >= key
	})
	if i < len(compactPatterns) {
		r := compactPatterns[i]
		if r.Locale == locale && r.Width == width && r.Magnitude == magnitude && r.Category == category {
			return r.Pattern, true
		}
	}
	if category != "other" {
		return compactPatternExact(locale, width, magnitude, "other")
	}
	return "", false
}

func (bundle) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return relativePatternExact(cur, field, width, direction, category)
	})
}

func relativePatternExact(locale, field, width, direction, category string) (string, bool) {
	locale = canonicalTag(locale)
	field = strings.TrimSpace(field)
	width = normalizeWidth(width)
	direction = strings.TrimSpace(direction)
	category = normalizeCategory(category)
	key := locale + "\x00" + field + "\x00" + width + "\x00" + direction + "\x00" + category
	i := sort.Search(len(relativePatterns), func(i int) bool {
		r := relativePatterns[i]
		return r.Locale+"\x00"+r.Field+"\x00"+r.Width+"\x00"+r.Direction+"\x00"+r.Category >= key
	})
	if i < len(relativePatterns) {
		r := relativePatterns[i]
		if r.Locale == locale && r.Field == field && r.Width == width && r.Direction == direction && r.Category == category {
			return r.Pattern, true
		}
	}
	if category != "other" {
		return relativePatternExact(locale, field, width, direction, "other")
	}
	return "", false
}

func (bundle) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return relativeSpecialExact(cur, field, width, offset)
	})
}

func relativeSpecialExact(locale, field, width string, offset int) (string, bool) {
	locale = canonicalTag(locale)
	field = strings.TrimSpace(field)
	width = normalizeWidth(width)
	key := locale + "\x00" + field + "\x00" + width + "\x00" + intKey(int64(offset))
	i := sort.Search(len(relativeSpecials), func(i int) bool {
		r := relativeSpecials[i]
		return r.Locale+"\x00"+r.Field+"\x00"+r.Width+"\x00"+intKey(int64(r.Offset)) >= key
	})
	if i < len(relativeSpecials) {
		r := relativeSpecials[i]
		if r.Locale == locale && r.Field == field && r.Width == width && r.Offset == offset {
			return r.Text, true
		}
	}
	return "", false
}

func (bundle) IntervalPattern(locale, skeleton, field string) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return intervalPatternExact(cur, skeleton, field)
	})
}

func intervalPatternExact(locale, skeleton, field string) (string, bool) {
	locale = canonicalTag(locale)
	skeleton = strings.TrimSpace(skeleton)
	field = strings.TrimSpace(field)
	key := locale + "\x00" + skeleton + "\x00" + field
	i := sort.Search(len(intervalPatterns), func(i int) bool {
		r := intervalPatterns[i]
		return r.Locale+"\x00"+r.Skeleton+"\x00"+r.Field >= key
	})
	if i < len(intervalPatterns) {
		r := intervalPatterns[i]
		if r.Locale == locale && r.Skeleton == skeleton && r.Field == field {
			return r.Pattern, true
		}
	}
	return "", false
}

func (bundle) DisplayName(locale, kind, code string) (string, bool) {
	return lookupStringByLocale(locale, func(cur string) (string, bool) {
		return displayNameExact(cur, kind, code)
	})
}

func displayNameExact(locale, kind, code string) (string, bool) {
	locale = canonicalTag(locale)
	kind = strings.TrimSpace(kind)
	code = strings.TrimSpace(code)
	key := locale + "\x00" + kind + "\x00" + code
	i := sort.Search(len(displayNames), func(i int) bool {
		r := displayNames[i]
		return r.Locale+"\x00"+r.Kind+"\x00"+r.Code >= key
	})
	if i < len(displayNames) {
		r := displayNames[i]
		if r.Locale == locale && r.Kind == kind && r.Code == code {
			return r.Name, true
		}
	}
	return "", false
}

func lookupStringByLocale(locale string, lookup func(string) (string, bool)) (string, bool) {
	for cur := canonicalTag(locale); cur != ""; {
		if value, ok := lookup(cur); ok && value != "" {
			return value, true
		}
		parent, ok := bundle{}.Parent(cur)
		if !ok || parent == cur {
			parent = parentTag(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return lookup("en")
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
	mergeArray2(&out.DayPeriods, child.DayPeriods)
	if child.CurrencyPattern != "" {
		out.CurrencyPattern = child.CurrencyPattern
	}
	if child.Accounting != "" {
		out.Accounting = child.Accounting
	}
	return out
}

func mergeArray2(dst *[2]string, src [2]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func parentTag(raw string) string {
	if i := strings.LastIndex(raw, "-"); i > 0 {
		return raw[:i]
	}
	return ""
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

func intKey(n int64) string {
	if n < 0 {
		return "-" + intKey(-n)
	}
	s := strconv.FormatInt(n, 10)
	return strings.Repeat("0", 20-len(s)) + s
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
