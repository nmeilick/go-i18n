package cldr

import (
	"strings"

	"github.com/nmeilick/go-i18n/internal/cldrdata"
)

type fromInternalProvider struct {
	p cldrdata.Provider
}

func (p fromInternalProvider) Metadata() Metadata {
	meta := p.p.Metadata()
	return Metadata{
		CLDRVersion:    meta.CLDRVersion,
		UnicodeVersion: meta.UnicodeVersion,
		Generator:      meta.Generator,
		LockID:         meta.LockID,
		FeatureSet:     append([]string(nil), meta.FeatureSet...),
		SourceIdentity: meta.SourceIdentity,
		TreeDigest:     meta.TreeDigest,
		License:        meta.License,
	}
}

func (p fromInternalProvider) Locale(tag string) (LocaleRecord, bool) {
	rec, ok := p.p.Locale(tag)
	if !ok {
		return LocaleRecord{}, false
	}
	return fromInternalLocale(rec), true
}

func (p fromInternalProvider) Parent(tag string) (string, bool) {
	return p.p.Parent(tag)
}

func (p fromInternalProvider) RegionDefaults(region string) (RegionDefaults, bool) {
	rec, ok := p.p.RegionDefaults(region)
	if !ok {
		return RegionDefaults{}, false
	}
	return RegionDefaults{
		Region:            rec.Region,
		Currency:          rec.Currency,
		MeasurementSystem: rec.MeasurementSystem,
		FirstDay:          rec.FirstDay,
		TimeZone:          rec.TimeZone,
	}, true
}

func (p fromInternalProvider) CurrencyFraction(code string) CurrencyFraction {
	rec := p.p.CurrencyFraction(code)
	return CurrencyFraction{Code: rec.Code, Digits: rec.Digits, CashDigits: rec.CashDigits, Rounding: rec.Rounding}
}

func (p fromInternalProvider) CurrencySymbol(locale, code string, display CurrencyDisplayMode) (string, bool) {
	return p.p.CurrencySymbol(locale, code, cldrdata.CurrencyDisplayMode(display))
}

func (p fromInternalProvider) ListPattern(locale, typ, width string) (ListPattern, bool) {
	rec, ok := p.p.ListPattern(locale, typ, width)
	return ListPattern{Two: rec.Two, Start: rec.Start, Middle: rec.Middle, End: rec.End}, ok
}

func (p fromInternalProvider) UnitPattern(locale, unit, width, category string) (string, bool) {
	return p.p.UnitPattern(locale, unit, width, category)
}

func (p fromInternalProvider) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	return p.p.CompactPattern(locale, width, magnitude, category)
}

func (p fromInternalProvider) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	return p.p.RelativeTimePattern(locale, field, width, direction, category)
}

func (p fromInternalProvider) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	return p.p.RelativeSpecial(locale, field, width, offset)
}

func (p fromInternalProvider) IntervalPattern(locale, skeleton, field string) (string, bool) {
	return p.p.IntervalPattern(locale, skeleton, field)
}

func (p fromInternalProvider) DisplayName(locale, kind, code string) (string, bool) {
	return p.p.DisplayName(locale, kind, code)
}

func (p fromInternalProvider) BCP47Types(key string) []BCP47TypeRecord {
	src := p.p.BCP47Types(key)
	out := make([]BCP47TypeRecord, len(src))
	for i, rec := range src {
		out[i] = BCP47TypeRecord{Key: rec.Key, Type: rec.Type, Alias: rec.Alias}
	}
	return out
}

func (p fromInternalProvider) AvailableLocales() []string {
	return p.p.AvailableLocales()
}

func (p fromInternalProvider) AvailableRegions() []string {
	return p.p.AvailableRegions()
}

func (p fromInternalProvider) AvailableCurrencyFractions() []CurrencyFraction {
	src := p.p.AvailableCurrencyFractions()
	out := make([]CurrencyFraction, len(src))
	for i, rec := range src {
		out[i] = CurrencyFraction{Code: rec.Code, Digits: rec.Digits, CashDigits: rec.CashDigits, Rounding: rec.Rounding}
	}
	return out
}

func (p fromInternalProvider) AvailableCurrencySymbols() []CurrencySymbolRecord {
	src := p.p.AvailableCurrencySymbols()
	out := make([]CurrencySymbolRecord, len(src))
	for i, rec := range src {
		out[i] = CurrencySymbolRecord{Locale: rec.Locale, Code: rec.Code, Symbol: rec.Symbol, Narrow: rec.Narrow}
	}
	return out
}

func (p fromInternalProvider) AvailableListPatterns() []ListPatternRecord {
	src := p.p.AvailableListPatterns()
	out := make([]ListPatternRecord, len(src))
	for i, rec := range src {
		out[i] = ListPatternRecord{Locale: rec.Locale, Type: rec.Type, Width: rec.Width, Pattern: ListPattern(rec.Pattern)}
	}
	return out
}

func (p fromInternalProvider) AvailableUnitPatterns() []UnitPatternRecord {
	src := p.p.AvailableUnitPatterns()
	out := make([]UnitPatternRecord, len(src))
	for i, rec := range src {
		out[i] = UnitPatternRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableCompactPatterns() []CompactPatternRecord {
	src := p.p.AvailableCompactPatterns()
	out := make([]CompactPatternRecord, len(src))
	for i, rec := range src {
		out[i] = CompactPatternRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableRelativeTimePatterns() []RelativePatternRecord {
	src := p.p.AvailableRelativeTimePatterns()
	out := make([]RelativePatternRecord, len(src))
	for i, rec := range src {
		out[i] = RelativePatternRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableRelativeSpecials() []RelativeSpecialRecord {
	src := p.p.AvailableRelativeSpecials()
	out := make([]RelativeSpecialRecord, len(src))
	for i, rec := range src {
		out[i] = RelativeSpecialRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableIntervalPatterns() []IntervalPatternRecord {
	src := p.p.AvailableIntervalPatterns()
	out := make([]IntervalPatternRecord, len(src))
	for i, rec := range src {
		out[i] = IntervalPatternRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableDisplayNames() []DisplayNameRecord {
	src := p.p.AvailableDisplayNames()
	out := make([]DisplayNameRecord, len(src))
	for i, rec := range src {
		out[i] = DisplayNameRecord(rec)
	}
	return out
}

func (p fromInternalProvider) AvailableBCP47Keys() []string {
	return p.p.AvailableBCP47Keys()
}

type toInternalProvider struct {
	p DataProvider
}

func (p toInternalProvider) Metadata() cldrdata.Metadata {
	meta := p.p.Metadata()
	return cldrdata.Metadata{
		CLDRVersion:    meta.CLDRVersion,
		UnicodeVersion: meta.UnicodeVersion,
		Generator:      meta.Generator,
		LockID:         meta.LockID,
		FeatureSet:     append([]string(nil), meta.FeatureSet...),
		SourceIdentity: meta.SourceIdentity,
		TreeDigest:     meta.TreeDigest,
		License:        meta.License,
	}
}

func (p toInternalProvider) Locale(tag string) (cldrdata.LocaleRecord, bool) {
	rec, ok := p.p.Locale(tag)
	if !ok {
		return cldrdata.LocaleRecord{}, false
	}
	return toInternalLocale(rec), true
}

func (p toInternalProvider) Parent(tag string) (string, bool) {
	return p.p.Parent(tag)
}

func (p toInternalProvider) RegionDefaults(region string) (cldrdata.RegionDefaults, bool) {
	rec, ok := p.p.RegionDefaults(region)
	if !ok {
		return cldrdata.RegionDefaults{}, false
	}
	return cldrdata.RegionDefaults{
		Region:            rec.Region,
		Currency:          rec.Currency,
		MeasurementSystem: rec.MeasurementSystem,
		FirstDay:          rec.FirstDay,
		TimeZone:          rec.TimeZone,
	}, true
}

func (p toInternalProvider) CurrencyFraction(code string) cldrdata.CurrencyFraction {
	rec := p.p.CurrencyFraction(code)
	return cldrdata.CurrencyFraction{Code: rec.Code, Digits: rec.Digits, CashDigits: rec.CashDigits, Rounding: rec.Rounding}
}

func (p toInternalProvider) CurrencySymbol(locale, code string, display cldrdata.CurrencyDisplayMode) (string, bool) {
	return p.p.CurrencySymbol(locale, code, CurrencyDisplayMode(display))
}

func (p toInternalProvider) ListPattern(locale, typ, width string) (cldrdata.ListPattern, bool) {
	rec, ok := p.p.ListPattern(locale, typ, width)
	return cldrdata.ListPattern{Two: rec.Two, Start: rec.Start, Middle: rec.Middle, End: rec.End}, ok
}

func (p toInternalProvider) UnitPattern(locale, unit, width, category string) (string, bool) {
	return p.p.UnitPattern(locale, unit, width, category)
}

func (p toInternalProvider) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	return p.p.CompactPattern(locale, width, magnitude, category)
}

func (p toInternalProvider) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	return p.p.RelativeTimePattern(locale, field, width, direction, category)
}

func (p toInternalProvider) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	return p.p.RelativeSpecial(locale, field, width, offset)
}

func (p toInternalProvider) IntervalPattern(locale, skeleton, field string) (string, bool) {
	return p.p.IntervalPattern(locale, skeleton, field)
}

func (p toInternalProvider) DisplayName(locale, kind, code string) (string, bool) {
	return p.p.DisplayName(locale, kind, code)
}

func (p toInternalProvider) BCP47Types(key string) []cldrdata.BCP47TypeRecord {
	src := p.p.BCP47Types(key)
	out := make([]cldrdata.BCP47TypeRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.BCP47TypeRecord{Key: rec.Key, Type: rec.Type, Alias: rec.Alias}
	}
	return out
}

func (p toInternalProvider) AvailableLocales() []string {
	return p.p.AvailableLocales()
}

func (p toInternalProvider) AvailableRegions() []string {
	return p.p.AvailableRegions()
}

func (p toInternalProvider) AvailableCurrencyFractions() []cldrdata.CurrencyFraction {
	src := p.p.AvailableCurrencyFractions()
	out := make([]cldrdata.CurrencyFraction, len(src))
	for i, rec := range src {
		out[i] = cldrdata.CurrencyFraction{Code: rec.Code, Digits: rec.Digits, CashDigits: rec.CashDigits, Rounding: rec.Rounding}
	}
	return out
}

func (p toInternalProvider) AvailableCurrencySymbols() []cldrdata.CurrencySymbolRecord {
	src := p.p.AvailableCurrencySymbols()
	out := make([]cldrdata.CurrencySymbolRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.CurrencySymbolRecord{Locale: rec.Locale, Code: rec.Code, Symbol: rec.Symbol, Narrow: rec.Narrow}
	}
	return out
}

func (p toInternalProvider) AvailableListPatterns() []cldrdata.ListPatternRecord {
	src := p.p.AvailableListPatterns()
	out := make([]cldrdata.ListPatternRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.ListPatternRecord{Locale: rec.Locale, Type: rec.Type, Width: rec.Width, Pattern: cldrdata.ListPattern(rec.Pattern)}
	}
	return out
}

func (p toInternalProvider) AvailableUnitPatterns() []cldrdata.UnitPatternRecord {
	src := p.p.AvailableUnitPatterns()
	out := make([]cldrdata.UnitPatternRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.UnitPatternRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableCompactPatterns() []cldrdata.CompactPatternRecord {
	src := p.p.AvailableCompactPatterns()
	out := make([]cldrdata.CompactPatternRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.CompactPatternRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableRelativeTimePatterns() []cldrdata.RelativePatternRecord {
	src := p.p.AvailableRelativeTimePatterns()
	out := make([]cldrdata.RelativePatternRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.RelativePatternRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableRelativeSpecials() []cldrdata.RelativeSpecialRecord {
	src := p.p.AvailableRelativeSpecials()
	out := make([]cldrdata.RelativeSpecialRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.RelativeSpecialRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableIntervalPatterns() []cldrdata.IntervalPatternRecord {
	src := p.p.AvailableIntervalPatterns()
	out := make([]cldrdata.IntervalPatternRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.IntervalPatternRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableDisplayNames() []cldrdata.DisplayNameRecord {
	src := p.p.AvailableDisplayNames()
	out := make([]cldrdata.DisplayNameRecord, len(src))
	for i, rec := range src {
		out[i] = cldrdata.DisplayNameRecord(rec)
	}
	return out
}

func (p toInternalProvider) AvailableBCP47Keys() []string {
	return p.p.AvailableBCP47Keys()
}

func fromInternalLocale(rec cldrdata.LocaleRecord) LocaleRecord {
	return LocaleRecord{
		Tag: rec.Tag, Parent: rec.Parent, NumberingSystem: rec.NumberingSystem,
		DateFormats: rec.DateFormats, TimeFormats: rec.TimeFormats, DateTimeFormats: rec.DateTimeFormats,
		MonthsWide: rec.MonthsWide, MonthsAbbr: rec.MonthsAbbr, WeekdaysWide: rec.WeekdaysWide,
		WeekdaysAbbr: rec.WeekdaysAbbr, DayPeriods: rec.DayPeriods, CurrencyPattern: rec.CurrencyPattern,
		Accounting: rec.Accounting,
	}
}

func toInternalLocale(rec LocaleRecord) cldrdata.LocaleRecord {
	return cldrdata.LocaleRecord{
		Tag: rec.Tag, Parent: rec.Parent, NumberingSystem: rec.NumberingSystem,
		DateFormats: rec.DateFormats, TimeFormats: rec.TimeFormats, DateTimeFormats: rec.DateTimeFormats,
		MonthsWide: rec.MonthsWide, MonthsAbbr: rec.MonthsAbbr, WeekdaysWide: rec.WeekdaysWide,
		WeekdaysAbbr: rec.WeekdaysAbbr, DayPeriods: rec.DayPeriods, CurrencyPattern: rec.CurrencyPattern,
		Accounting: rec.Accounting,
	}
}

// MergeLocaleRecords applies sparse child values over parent values.
func MergeLocaleRecords(parent, child LocaleRecord) LocaleRecord {
	out := parent
	out.Tag = child.Tag
	out.Parent = child.Parent
	if child.NumberingSystem != "" {
		out.NumberingSystem = child.NumberingSystem
	}
	merge4(&out.DateFormats, child.DateFormats)
	merge4(&out.TimeFormats, child.TimeFormats)
	merge4(&out.DateTimeFormats, child.DateTimeFormats)
	merge12(&out.MonthsWide, child.MonthsWide)
	merge12(&out.MonthsAbbr, child.MonthsAbbr)
	merge7(&out.WeekdaysWide, child.WeekdaysWide)
	merge7(&out.WeekdaysAbbr, child.WeekdaysAbbr)
	merge2(&out.DayPeriods, child.DayPeriods)
	if child.CurrencyPattern != "" {
		out.CurrencyPattern = child.CurrencyPattern
	}
	if child.Accounting != "" {
		out.Accounting = child.Accounting
	}
	return out
}

func merge2(dst *[2]string, src [2]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func CanonicalTag(tag string) string {
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

func merge4(dst *[4]string, src [4]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func merge12(dst *[12]string, src [12]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}

func merge7(dst *[7]string, src [7]string) {
	for i, v := range src {
		if v != "" {
			dst[i] = v
		}
	}
}
