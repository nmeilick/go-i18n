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

func (p modelProvider) CurrencySymbol(_ string, code string) (string, bool) {
	code = strings.ToUpper(strings.TrimSpace(code))
	i := sort.Search(len(p.model.symbols), func(i int) bool { return p.model.symbols[i].Code >= code })
	if i < len(p.model.symbols) && p.model.symbols[i].Code == code {
		return p.model.symbols[i].Symbol, true
	}
	return "", false
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
		out[i] = cldr.CurrencySymbolRecord{Code: rec.Code, Symbol: rec.Symbol}
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
		WeekdaysAbbr: rec.WeekdaysAbbr, CurrencyPattern: rec.CurrencyPattern, Accounting: rec.Accounting,
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

func modelFeatures() []cldr.FeatureID {
	return []cldr.FeatureID{
		cldr.FeatureCoreIdentity,
		cldr.FeatureProfileDefaults,
		cldr.FeatureNumbersSymbols,
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersPercent,
		cldr.FeatureCurrenciesFractions,
		cldr.FeatureCurrenciesSymbols,
		cldr.FeatureDatesGregorianPatterns,
		cldr.FeatureDatesGregorianNames,
		cldr.FeatureBCP47Extensions,
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
		{Feature: cldr.FeatureCurrenciesSymbols, Scope: cldr.ScopeCurrency, Role: cldr.RoleAuthoritative, Status: cldr.StatusDataAvailable, All: true},
		{Feature: cldr.FeatureDatesGregorianPatterns, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureDatesGregorianNames, Scope: cldr.ScopeLocale, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, Keys: locales},
		{Feature: cldr.FeatureBCP47Extensions, Scope: cldr.ScopeGlobal, Role: cldr.RoleAuthoritative, Status: cldr.StatusImplemented, All: true},
	}
}

func codecName(codec cldrpack.Codec) string {
	if codec == cldrpack.CodecZstd {
		return "zstd"
	}
	return "raw"
}
