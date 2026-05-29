package cldrgen

import (
	"sort"
	"strings"

	"github.com/nmeilick/go-i18n/locale/cldr"
	"github.com/nmeilick/go-i18n/locale/cldrpack"
)

type modelProvider struct {
	model model
}

func (p modelProvider) Metadata() cldr.Metadata {
	lock := p.model.lock
	return cldr.Metadata{
		CLDRVersion:    lock.CLDRVersion,
		UnicodeVersion: lock.UnicodeVersion,
		Generator:      lock.Generator,
		FeatureSet:     append([]string(nil), lock.FeatureSet...),
		SourceIdentity: lock.SourceIdentity,
		TreeDigest:     lock.TreeSHA256,
		License:        lock.License,
	}
}

func (p modelProvider) Locale(tag string) (cldr.LocaleRecord, bool) {
	tag = cldr.CanonicalTag(tag)
	return p.resolveLocale(tag, map[string]bool{})
}

func (p modelProvider) resolveLocale(tag string, seen map[string]bool) (cldr.LocaleRecord, bool) {
	if seen[tag] {
		return cldr.LocaleRecord{}, false
	}
	seen[tag] = true
	i := sort.Search(len(p.model.locales), func(i int) bool { return p.model.locales[i].Tag >= tag })
	if i >= len(p.model.locales) || p.model.locales[i].Tag != tag {
		return cldr.LocaleRecord{}, false
	}
	rec := modelLocale(p.model.locales[i])
	if rec.Parent == "" {
		return rec, true
	}
	parent, ok := p.resolveLocale(rec.Parent, seen)
	if !ok {
		return rec, true
	}
	return cldr.MergeLocaleRecords(parent, rec), true
}

func (p modelProvider) Parent(tag string) (string, bool) {
	tag = cldr.CanonicalTag(tag)
	i := sort.Search(len(p.model.locales), func(i int) bool { return p.model.locales[i].Tag >= tag })
	if i < len(p.model.locales) && p.model.locales[i].Tag == tag && p.model.locales[i].Parent != "" {
		return p.model.locales[i].Parent, true
	}
	return "", false
}

func (p modelProvider) RegionDefaults(region string) (cldr.RegionDefaults, bool) {
	region = strings.ToUpper(strings.TrimSpace(region))
	i := sort.Search(len(p.model.regions), func(i int) bool { return p.model.regions[i].Region >= region })
	if i < len(p.model.regions) && p.model.regions[i].Region == region {
		return modelRegion(p.model.regions[i]), true
	}
	i = sort.Search(len(p.model.regions), func(i int) bool { return p.model.regions[i].Region >= "001" })
	if i < len(p.model.regions) && p.model.regions[i].Region == "001" {
		return modelRegion(p.model.regions[i]), true
	}
	return cldr.RegionDefaults{}, false
}

func (p modelProvider) CurrencyFraction(code string) cldr.CurrencyFraction {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(len(p.model.fractions), func(i int) bool { return p.model.fractions[i].Code >= code })
	if i < len(p.model.fractions) && p.model.fractions[i].Code == code {
		return modelFraction(p.model.fractions[i])
	}
	return cldr.CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
}

func (p modelProvider) CurrencySymbol(locale, code string, display cldr.CurrencyDisplayMode) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.CurrencySymbol(locale, code, display)
}

func (p modelProvider) ListPattern(locale, typ, width string) (cldr.ListPattern, bool) {
	provider := generatedModelLookup(p)
	return provider.ListPattern(locale, typ, width)
}

func (p modelProvider) UnitPattern(locale, unit, width, category string) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.UnitPattern(locale, unit, width, category)
}

func (p modelProvider) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.CompactPattern(locale, width, magnitude, category)
}

func (p modelProvider) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.RelativeTimePattern(locale, field, width, direction, category)
}

func (p modelProvider) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.RelativeSpecial(locale, field, width, offset)
}

func (p modelProvider) IntervalPattern(locale, skeleton, field string) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.IntervalPattern(locale, skeleton, field)
}

func (p modelProvider) DisplayName(locale, kind, code string) (string, bool) {
	provider := generatedModelLookup(p)
	return provider.DisplayName(locale, kind, code)
}

func (p modelProvider) BCP47Types(key string) []cldr.BCP47TypeRecord {
	key = strings.TrimSpace(key)
	start := sort.Search(len(p.model.bcp47), func(i int) bool { return p.model.bcp47[i].Key >= key })
	out := []cldr.BCP47TypeRecord{}
	for i := start; i < len(p.model.bcp47) && p.model.bcp47[i].Key == key; i++ {
		out = append(out, modelBCP47(p.model.bcp47[i]))
	}
	return out
}

func (p modelProvider) AvailableLocales() []string {
	out := make([]string, len(p.model.locales))
	for i, rec := range p.model.locales {
		out[i] = rec.Tag
	}
	return out
}

func (p modelProvider) AvailableRegions() []string {
	out := make([]string, len(p.model.regions))
	for i, rec := range p.model.regions {
		out[i] = rec.Region
	}
	return out
}

func (p modelProvider) AvailableCurrencyFractions() []cldr.CurrencyFraction {
	out := make([]cldr.CurrencyFraction, len(p.model.fractions))
	for i, rec := range p.model.fractions {
		out[i] = modelFraction(rec)
	}
	return out
}

func (p modelProvider) AvailableCurrencySymbols() []cldr.CurrencySymbolRecord {
	out := make([]cldr.CurrencySymbolRecord, len(p.model.symbols))
	for i, rec := range p.model.symbols {
		out[i] = cldr.CurrencySymbolRecord{Locale: rec.Locale, Code: rec.Code, Symbol: rec.Symbol, Narrow: rec.Narrow}
	}
	return out
}

func (p modelProvider) AvailableListPatterns() []cldr.ListPatternRecord {
	out := make([]cldr.ListPatternRecord, len(p.model.listPatterns))
	for i, rec := range p.model.listPatterns {
		out[i] = cldr.ListPatternRecord{Locale: rec.Locale, Type: rec.Type, Width: rec.Width, Pattern: cldr.ListPattern{Two: rec.Pattern.Two, Start: rec.Pattern.Start, Middle: rec.Pattern.Middle, End: rec.Pattern.End}}
	}
	return out
}

func (p modelProvider) AvailableUnitPatterns() []cldr.UnitPatternRecord {
	out := make([]cldr.UnitPatternRecord, len(p.model.unitPatterns))
	for i, rec := range p.model.unitPatterns {
		out[i] = cldr.UnitPatternRecord(rec)
	}
	return out
}

func (p modelProvider) AvailableCompactPatterns() []cldr.CompactPatternRecord {
	out := make([]cldr.CompactPatternRecord, len(p.model.compactPatterns))
	for i, rec := range p.model.compactPatterns {
		out[i] = cldr.CompactPatternRecord{
			Locale:    rec.Locale,
			Width:     rec.Width,
			Magnitude: rec.Magnitude,
			Category:  rec.Category,
			Pattern:   rec.Pattern,
		}
	}
	return out
}

func (p modelProvider) AvailableRelativeTimePatterns() []cldr.RelativePatternRecord {
	out := make([]cldr.RelativePatternRecord, len(p.model.relativePatterns))
	for i, rec := range p.model.relativePatterns {
		out[i] = cldr.RelativePatternRecord(rec)
	}
	return out
}

func (p modelProvider) AvailableRelativeSpecials() []cldr.RelativeSpecialRecord {
	out := make([]cldr.RelativeSpecialRecord, len(p.model.relativeSpecials))
	for i, rec := range p.model.relativeSpecials {
		out[i] = cldr.RelativeSpecialRecord{
			Locale: rec.Locale,
			Field:  rec.Field,
			Width:  rec.Width,
			Offset: rec.Offset,
			Text:   rec.Text,
		}
	}
	return out
}

func (p modelProvider) AvailableIntervalPatterns() []cldr.IntervalPatternRecord {
	out := make([]cldr.IntervalPatternRecord, len(p.model.intervalPatterns))
	for i, rec := range p.model.intervalPatterns {
		out[i] = cldr.IntervalPatternRecord(rec)
	}
	return out
}

func (p modelProvider) AvailableDisplayNames() []cldr.DisplayNameRecord {
	out := make([]cldr.DisplayNameRecord, len(p.model.displayNames))
	for i, rec := range p.model.displayNames {
		out[i] = cldr.DisplayNameRecord(rec)
	}
	return out
}

func (p modelProvider) AvailableBCP47Keys() []string {
	keys := []string{}
	var last string
	for _, rec := range p.model.bcp47 {
		if rec.Key != last {
			keys = append(keys, rec.Key)
			last = rec.Key
		}
	}
	return keys
}

func modelLocale(rec localeRecord) cldr.LocaleRecord {
	return cldr.LocaleRecord{
		Tag: rec.Tag, Parent: rec.Parent, NumberingSystem: rec.NumberingSystem,
		DateFormats: rec.DateFormats, TimeFormats: rec.TimeFormats, DateTimeFormats: rec.DateTimeFormats,
		MonthsWide: rec.MonthsWide, MonthsAbbr: rec.MonthsAbbr, WeekdaysWide: rec.WeekdaysWide,
		WeekdaysAbbr: rec.WeekdaysAbbr, DayPeriods: rec.DayPeriods, CurrencyPattern: rec.CurrencyPattern,
		Accounting: rec.Accounting,
	}
}

func modelRegion(rec regionDefault) cldr.RegionDefaults {
	return cldr.RegionDefaults{
		Region: rec.Region, Currency: rec.Currency, MeasurementSystem: rec.MeasurementSystem,
		FirstDay: rec.FirstDay, TimeZone: rec.TimeZone,
	}
}

func modelFraction(rec currencyFraction) cldr.CurrencyFraction {
	return cldr.CurrencyFraction{Code: rec.Code, Digits: rec.Digits, CashDigits: rec.CashDigits, Rounding: rec.Rounding}
}

func modelBCP47(rec bcp47Type) cldr.BCP47TypeRecord {
	return cldr.BCP47TypeRecord{Key: rec.Key, Type: rec.Type, Alias: rec.Alias}
}

func providerVersions(meta cldr.Metadata) cldr.Versions {
	return cldr.Versions{
		CLDR: meta.CLDRVersion, Unicode: meta.UnicodeVersion, ProviderAPIMajor: cldr.ProviderAPIMajor,
		ProviderAPIMinor: cldr.ProviderAPIMinor, FeatureRegistryMajor: cldr.FeatureRegistryMajor,
		FeatureRegistryMinor: cldr.FeatureRegistryMinor, Generator: meta.Generator,
		SourceIdentity: meta.SourceIdentity, SourceDigest: meta.TreeDigest, License: meta.License,
	}
}

type generatedModelLookup struct{ model model }

func (p generatedModelLookup) parent(tag string) string {
	if provider := modelProvider(p); true {
		if parent, ok := provider.Parent(tag); ok {
			return parent
		}
	}
	return parentOf(tag)
}

func (p generatedModelLookup) CurrencySymbol(locale, code string, display cldr.CurrencyDisplayMode) (string, bool) {
	if display == cldr.CurrencyDisplayCode {
		code = strings.ToUpper(strings.TrimSpace(code))
		return code, code != ""
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	for cur := cldr.CanonicalTag(locale); cur != ""; {
		key := currencySymbolKey(currencySymbol{Locale: cur, Code: code})
		i := sort.Search(len(p.model.symbols), func(i int) bool { return currencySymbolKey(p.model.symbols[i]) >= key })
		if i < len(p.model.symbols) && currencySymbolKey(p.model.symbols[i]) == key {
			rec := p.model.symbols[i]
			if display == cldr.CurrencyDisplayNarrowSymbol && rec.Narrow != "" {
				return rec.Narrow, true
			}
			if rec.Symbol != "" {
				return rec.Symbol, true
			}
		}
		parent := p.parent(cur)
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	if locale != "en" {
		return p.CurrencySymbol("en", code, display)
	}
	return "", false
}

func (p generatedModelLookup) ListPattern(locale, typ, width string) (cldr.ListPattern, bool) {
	var zero cldr.ListPattern
	value, ok := p.lookup(locale, func(cur string) (any, bool) {
		key := listPatternKey(listPatternRecord{Locale: cur, Type: typ, Width: normalizeWidth(width)})
		i := sort.Search(len(p.model.listPatterns), func(i int) bool { return listPatternKey(p.model.listPatterns[i]) >= key })
		if i < len(p.model.listPatterns) && listPatternKey(p.model.listPatterns[i]) == key {
			rec := p.model.listPatterns[i].Pattern
			return cldr.ListPattern{Two: rec.Two, Start: rec.Start, Middle: rec.Middle, End: rec.End}, true
		}
		return nil, false
	})
	if !ok {
		return zero, false
	}
	return value.(cldr.ListPattern), true
}

func (p generatedModelLookup) UnitPattern(locale, unit, width, category string) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := unitPatternKey(unitPatternRecord{Locale: cur, Unit: unit, Width: normalizeWidth(width), Category: normalizeCategory(category)})
		i := sort.Search(len(p.model.unitPatterns), func(i int) bool { return unitPatternKey(p.model.unitPatterns[i]) >= key })
		if i < len(p.model.unitPatterns) && unitPatternKey(p.model.unitPatterns[i]) == key {
			return p.model.unitPatterns[i].Pattern, true
		}
		if category != "other" {
			return p.UnitPattern(cur, unit, width, "other")
		}
		return "", false
	})
}

func (p generatedModelLookup) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := compactPatternKey(compactPatternRecord{Locale: cur, Width: normalizeCompactWidth(width), Magnitude: magnitude, Category: normalizeCategory(category)})
		i := sort.Search(len(p.model.compactPatterns), func(i int) bool { return compactPatternKey(p.model.compactPatterns[i]) >= key })
		if i < len(p.model.compactPatterns) && compactPatternKey(p.model.compactPatterns[i]) == key {
			return p.model.compactPatterns[i].Pattern, true
		}
		if category != "other" {
			return p.CompactPattern(cur, width, magnitude, "other")
		}
		return "", false
	})
}

func (p generatedModelLookup) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := relativePatternKey(relativePatternRecord{Locale: cur, Field: field, Width: normalizeWidth(width), Direction: direction, Category: normalizeCategory(category)})
		i := sort.Search(len(p.model.relativePatterns), func(i int) bool { return relativePatternKey(p.model.relativePatterns[i]) >= key })
		if i < len(p.model.relativePatterns) && relativePatternKey(p.model.relativePatterns[i]) == key {
			return p.model.relativePatterns[i].Pattern, true
		}
		if category != "other" {
			return p.RelativeTimePattern(cur, field, width, direction, "other")
		}
		return "", false
	})
}

func (p generatedModelLookup) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := relativeSpecialKey(relativeSpecialRecord{Locale: cur, Field: field, Width: normalizeWidth(width), Offset: offset})
		i := sort.Search(len(p.model.relativeSpecials), func(i int) bool { return relativeSpecialKey(p.model.relativeSpecials[i]) >= key })
		if i < len(p.model.relativeSpecials) && relativeSpecialKey(p.model.relativeSpecials[i]) == key {
			return p.model.relativeSpecials[i].Text, true
		}
		return "", false
	})
}

func (p generatedModelLookup) IntervalPattern(locale, skeleton, field string) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := intervalPatternKey(intervalPatternRecord{Locale: cur, Skeleton: skeleton, Field: field})
		i := sort.Search(len(p.model.intervalPatterns), func(i int) bool { return intervalPatternKey(p.model.intervalPatterns[i]) >= key })
		if i < len(p.model.intervalPatterns) && intervalPatternKey(p.model.intervalPatterns[i]) == key {
			return p.model.intervalPatterns[i].Pattern, true
		}
		return "", false
	})
}

func (p generatedModelLookup) DisplayName(locale, kind, code string) (string, bool) {
	return p.lookupString(locale, func(cur string) (string, bool) {
		key := displayNameKey(displayNameRecord{Locale: cur, Kind: kind, Code: code})
		i := sort.Search(len(p.model.displayNames), func(i int) bool { return displayNameKey(p.model.displayNames[i]) >= key })
		if i < len(p.model.displayNames) && displayNameKey(p.model.displayNames[i]) == key {
			return p.model.displayNames[i].Name, true
		}
		return "", false
	})
}

func (p generatedModelLookup) lookupString(locale string, fn func(string) (string, bool)) (string, bool) {
	value, ok := p.lookup(locale, func(cur string) (any, bool) { return fn(cur) })
	if !ok {
		return "", false
	}
	return value.(string), true
}

func (p generatedModelLookup) lookup(locale string, fn func(string) (any, bool)) (any, bool) {
	for cur := cldr.CanonicalTag(locale); cur != ""; {
		if value, ok := fn(cur); ok {
			return value, true
		}
		parent := p.parent(cur)
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	if locale != "en" {
		return fn("en")
	}
	return nil, false
}

func modelFeatures() []cldr.FeatureID {
	return []cldr.FeatureID{
		cldr.FeatureCoreIdentity,
		cldr.FeatureProfileDefaults,
		cldr.FeatureNumbersSymbols,
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersPercent,
		cldr.FeatureCurrenciesFractions,
		cldr.FeatureCurrenciesSymbols,
		cldr.FeatureCurrenciesNarrowSymbols,
		cldr.FeatureDatesGregorianPatterns,
		cldr.FeatureDatesGregorianNames,
		cldr.FeatureDatesGregorianDayPeriods,
		cldr.FeatureDatesGregorianIntervals,
		cldr.FeatureBCP47Extensions,
		cldr.FeatureListsPatterns,
		cldr.FeaturePluralsCardinal,
		cldr.FeatureNumbersCompactDecimal,
		cldr.FeatureUnitsDurationCore,
		cldr.FeatureDatesRelativeTime,
		cldr.FeatureDisplayNamesLanguages,
		cldr.FeatureDisplayNamesTerritories,
		cldr.FeatureDisplayNamesScripts,
		cldr.FeatureDisplayNamesCalendars,
	}
}

func modelCoverage(provider cldr.DataProvider) []cldr.Coverage {
	locales := provider.AvailableLocales()
	return []cldr.Coverage{
		{Feature: cldr.FeatureCoreIdentity, Scope: cldr.ScopeGlobal, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureProfileDefaults, Scope: cldr.ScopeTerritory, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureNumbersSymbols, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureNumbersDecimal, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureNumbersPercent, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureCurrenciesFractions, Scope: cldr.ScopeCurrency, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureCurrenciesSymbols, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureCurrenciesNarrowSymbols, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureDatesGregorianPatterns, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDatesGregorianNames, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDatesGregorianDayPeriods, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDatesGregorianIntervals, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureBCP47Extensions, Scope: cldr.ScopeGlobal, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureListsPatterns, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeaturePluralsCardinal, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureNumbersCompactDecimal, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureUnitsDurationCore, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDatesRelativeTime, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDisplayNamesLanguages, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureDisplayNamesTerritories, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureDisplayNamesScripts, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
		{Feature: cldr.FeatureDisplayNamesCalendars, Scope: cldr.ScopeFeaturePrivate, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
	}
}

func codecName(codec cldrpack.Codec) string {
	if codec == cldrpack.CodecZstd {
		return "zstd"
	}
	return "raw"
}
