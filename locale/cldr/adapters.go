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

func (p fromInternalProvider) CurrencySymbol(locale, code string) (string, bool) {
	return p.p.CurrencySymbol(locale, code)
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
		out[i] = CurrencySymbolRecord{Code: rec.Code, Symbol: rec.Symbol}
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

func (p toInternalProvider) CurrencySymbol(locale, code string) (string, bool) {
	return p.p.CurrencySymbol(locale, code)
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
		out[i] = cldrdata.CurrencySymbolRecord{Code: rec.Code, Symbol: rec.Symbol}
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
		WeekdaysAbbr: rec.WeekdaysAbbr, CurrencyPattern: rec.CurrencyPattern, Accounting: rec.Accounting,
	}
}

func toInternalLocale(rec LocaleRecord) cldrdata.LocaleRecord {
	return cldrdata.LocaleRecord{
		Tag: rec.Tag, Parent: rec.Parent, NumberingSystem: rec.NumberingSystem,
		DateFormats: rec.DateFormats, TimeFormats: rec.TimeFormats, DateTimeFormats: rec.DateTimeFormats,
		MonthsWide: rec.MonthsWide, MonthsAbbr: rec.MonthsAbbr, WeekdaysWide: rec.WeekdaysWide,
		WeekdaysAbbr: rec.WeekdaysAbbr, CurrencyPattern: rec.CurrencyPattern, Accounting: rec.Accounting,
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
	if child.CurrencyPattern != "" {
		out.CurrencyPattern = child.CurrencyPattern
	}
	if child.Accounting != "" {
		out.Accounting = child.Accounting
	}
	return out
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
