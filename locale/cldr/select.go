package cldr

import (
	"fmt"
	"sort"
	"strings"
)

// Selection describes a concrete or user-authored bundle subset. The keyword
// "all" is accepted in Locales, Languages, and Features.
type Selection struct {
	Locales   []string
	Languages []string
	Features  []FeatureID
}

// SelectionPlan is the deterministic expansion of a Selection.
type SelectionPlan struct {
	RequestedLocales   []string    `json:"requested_locales,omitempty"`
	RequestedLanguages []string    `json:"requested_languages,omitempty"`
	RequestedFeatures  []FeatureID `json:"requested_features,omitempty"`
	Locales            []string    `json:"locales"`
	Features           []FeatureID `json:"features"`
	Closure            []string    `json:"closure,omitempty"`
}

// PlanSelection expands all selectors and computes locale parent closure.
func PlanSelection(bundle Bundle, selection Selection) (SelectionPlan, error) {
	if bundle == nil {
		bundle = Builtin()
	}
	data := bundle.Data()
	if data == nil {
		return SelectionPlan{}, &Error{Code: ErrInvalidBundle, Message: "selection requires bundle data"}
	}
	features, err := expandFeatures(bundle.Info().Features, selection.Features)
	if err != nil {
		return SelectionPlan{}, err
	}
	locales, closure := expandLocales(data, selection.Locales, selection.Languages)
	return SelectionPlan{
		RequestedLocales:   sortedStringSlice(selection.Locales),
		RequestedLanguages: sortedStringSlice(selection.Languages),
		RequestedFeatures:  sortedFeatureSlice(selection.Features),
		Locales:            locales,
		Features:           features,
		Closure:            closure,
	}, nil
}

// SelectBundle returns a bundle view limited to selected features and locales.
func SelectBundle(bundle Bundle, selection Selection) (Bundle, SelectionPlan, error) {
	if bundle == nil {
		bundle = Builtin()
	}
	plan, err := PlanSelection(bundle, selection)
	if err != nil {
		return nil, SelectionPlan{}, err
	}
	featureSet := map[FeatureID]bool{}
	for _, feature := range plan.Features {
		featureSet[feature] = true
	}
	localeSet := map[string]bool{}
	for _, tag := range plan.Locales {
		localeSet[tag] = true
	}
	info := bundle.Info()
	info.ID = info.ID + "-selected"
	info.Mode = "selected"
	info.Features = plan.Features
	info.Locales = plan.Locales
	coverage := filterCoverage(bundle.Coverage(), featureSet, localeSet)
	selected := selectedProvider{base: bundle.Data(), features: featureSet, locales: localeSet, localeList: plan.Locales}
	out, err := NewBundle(info, coverage, selected, bundle.Close)
	if err != nil {
		return nil, SelectionPlan{}, err
	}
	return out, plan, nil
}

func expandFeatures(available []FeatureID, requested []FeatureID) ([]FeatureID, error) {
	all := len(requested) == 0
	for _, feature := range requested {
		if strings.EqualFold(string(feature), "all") {
			all = true
			break
		}
	}
	availableSet := map[FeatureID]bool{}
	for _, feature := range available {
		availableSet[feature] = true
	}
	if all {
		return sortedFeatureSlice(available), nil
	}
	expanded := map[FeatureID]bool{}
	for _, feature := range requested {
		feature = FeatureID(strings.TrimSpace(string(feature)))
		if feature == "" {
			continue
		}
		if !availableSet[feature] {
			return nil, &Error{Code: ErrUnsupportedCapability, Message: fmt.Sprintf("feature %s is not available in bundle", feature)}
		}
		expanded[feature] = true
	}
	return sortedFeatureIDs(expanded), nil
}

func expandLocales(data DataProvider, requestedLocales, requestedLanguages []string) ([]string, []string) {
	available := data.AvailableLocales()
	sort.Strings(available)
	all := len(requestedLocales) == 0 && len(requestedLanguages) == 0
	for _, value := range append(append([]string(nil), requestedLocales...), requestedLanguages...) {
		if strings.EqualFold(strings.TrimSpace(value), "all") {
			all = true
			break
		}
	}
	selected := map[string]bool{}
	if all {
		for _, tag := range available {
			selected[tag] = true
		}
		return sortedStrings(selected), nil
	}
	availableSet := map[string]bool{}
	for _, tag := range available {
		availableSet[tag] = true
	}
	for _, tag := range requestedLocales {
		tag = CanonicalTag(tag)
		if availableSet[tag] {
			selected[tag] = true
		}
	}
	languages := map[string]bool{}
	for _, lang := range requestedLanguages {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang != "" && lang != "all" {
			languages[lang] = true
		}
	}
	for _, tag := range available {
		base := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
		if languages[base] {
			selected[tag] = true
		}
	}
	closure := map[string]bool{}
	queue := sortedStrings(selected)
	for len(queue) > 0 {
		tag := queue[0]
		queue = queue[1:]
		for cur := tag; cur != ""; {
			parent, ok := data.Parent(cur)
			if !ok || parent == cur {
				break
			}
			if availableSet[parent] && !selected[parent] {
				selected[parent] = true
				closure[parent] = true
				queue = append(queue, parent)
			}
			cur = parent
		}
	}
	return sortedStrings(selected), sortedStrings(closure)
}

func filterCoverage(in []Coverage, features map[FeatureID]bool, locales map[string]bool) []Coverage {
	out := []Coverage{}
	for _, rec := range in {
		if !features[rec.Feature] {
			continue
		}
		if rec.Scope == ScopeLocale && !rec.All {
			keys := []string{}
			for _, key := range rec.Keys {
				if locales[key] {
					keys = append(keys, key)
				}
			}
			rec.Keys = keys
		}
		if rec.Scope == ScopeLocale && rec.All {
			rec.All = false
			rec.Keys = sortedStrings(locales)
		}
		if rec.Scope == ScopeLocale && !rec.All && len(rec.Keys) == 0 {
			continue
		}
		out = append(out, rec)
	}
	return out
}

type selectedProvider struct {
	base       DataProvider
	features   map[FeatureID]bool
	locales    map[string]bool
	localeList []string
}

func (p selectedProvider) Metadata() Metadata { return p.base.Metadata() }

func (p selectedProvider) Locale(tag string) (LocaleRecord, bool) {
	tag = CanonicalTag(tag)
	if !p.locales[tag] {
		return LocaleRecord{}, false
	}
	rec, ok := p.base.Locale(tag)
	if !ok {
		return LocaleRecord{}, false
	}
	if !p.features[FeatureDatesGregorianPatterns] {
		rec.DateFormats = [4]string{}
		rec.TimeFormats = [4]string{}
		rec.DateTimeFormats = [4]string{}
	}
	if !p.features[FeatureDatesGregorianNames] {
		rec.MonthsWide = [12]string{}
		rec.MonthsAbbr = [12]string{}
		rec.WeekdaysWide = [7]string{}
		rec.WeekdaysAbbr = [7]string{}
	}
	if !p.features[FeatureDatesGregorianDayPeriods] {
		rec.DayPeriods = [2]string{}
	}
	if !p.features[FeatureCurrenciesFractions] && !p.features[FeatureCurrenciesSymbols] {
		rec.CurrencyPattern = ""
		rec.Accounting = ""
	}
	if !p.features[FeatureNumbersSymbols] {
		rec.NumberingSystem = ""
	}
	return rec, true
}

func (p selectedProvider) Parent(tag string) (string, bool) {
	tag = CanonicalTag(tag)
	if !p.locales[tag] {
		return "", false
	}
	return p.base.Parent(tag)
}

func (p selectedProvider) RegionDefaults(region string) (RegionDefaults, bool) {
	if !p.features[FeatureProfileDefaults] {
		return RegionDefaults{}, false
	}
	return p.base.RegionDefaults(region)
}

func (p selectedProvider) CurrencyFraction(code string) CurrencyFraction {
	if !p.features[FeatureCurrenciesFractions] {
		code = strings.ToUpper(strings.TrimSpace(code))
		return CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
	}
	return p.base.CurrencyFraction(code)
}

func (p selectedProvider) CurrencySymbol(locale, code string, display CurrencyDisplayMode) (string, bool) {
	if display == CurrencyDisplayNarrowSymbol && !p.features[FeatureCurrenciesNarrowSymbols] {
		return "", false
	}
	if display != CurrencyDisplayNarrowSymbol && !p.features[FeatureCurrenciesSymbols] {
		return "", false
	}
	return p.base.CurrencySymbol(locale, code, display)
}

func (p selectedProvider) ListPattern(locale, typ, width string) (ListPattern, bool) {
	if !p.features[FeatureListsPatterns] || !p.locales[CanonicalTag(locale)] {
		return ListPattern{}, false
	}
	return p.base.ListPattern(locale, typ, width)
}

func (p selectedProvider) UnitPattern(locale, unit, width, category string) (string, bool) {
	if !p.features[FeatureUnitsDurationCore] || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.UnitPattern(locale, unit, width, category)
}

func (p selectedProvider) CompactPattern(locale, width string, magnitude int64, category string) (string, bool) {
	if !p.features[FeatureNumbersCompactDecimal] || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.CompactPattern(locale, width, magnitude, category)
}

func (p selectedProvider) RelativeTimePattern(locale, field, width, direction, category string) (string, bool) {
	if !p.features[FeatureDatesRelativeTime] || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.RelativeTimePattern(locale, field, width, direction, category)
}

func (p selectedProvider) RelativeSpecial(locale, field, width string, offset int) (string, bool) {
	if !p.features[FeatureDatesRelativeTime] || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.RelativeSpecial(locale, field, width, offset)
}

func (p selectedProvider) IntervalPattern(locale, skeleton, field string) (string, bool) {
	if !p.features[FeatureDatesGregorianIntervals] || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.IntervalPattern(locale, skeleton, field)
}

func (p selectedProvider) DisplayName(locale, kind, code string) (string, bool) {
	if !p.displayNameFeatureSelected(kind) || !p.locales[CanonicalTag(locale)] {
		return "", false
	}
	return p.base.DisplayName(locale, kind, code)
}

func (p selectedProvider) displayNameFeatureSelected(kind string) bool {
	switch kind {
	case "language":
		return p.features[FeatureDisplayNamesLanguages]
	case "territory":
		return p.features[FeatureDisplayNamesTerritories]
	case "script":
		return p.features[FeatureDisplayNamesScripts]
	case "calendar":
		return p.features[FeatureDisplayNamesCalendars]
	default:
		return false
	}
}

func (p selectedProvider) BCP47Types(key string) []BCP47TypeRecord {
	if !p.features[FeatureBCP47Extensions] {
		return nil
	}
	return p.base.BCP47Types(key)
}

func (p selectedProvider) AvailableLocales() []string {
	return append([]string(nil), p.localeList...)
}

func (p selectedProvider) AvailableRegions() []string {
	if !p.features[FeatureProfileDefaults] {
		return nil
	}
	return p.base.AvailableRegions()
}

func (p selectedProvider) AvailableCurrencyFractions() []CurrencyFraction {
	if !p.features[FeatureCurrenciesFractions] {
		return nil
	}
	return p.base.AvailableCurrencyFractions()
}

func (p selectedProvider) AvailableCurrencySymbols() []CurrencySymbolRecord {
	if !p.features[FeatureCurrenciesSymbols] && !p.features[FeatureCurrenciesNarrowSymbols] {
		return nil
	}
	return p.base.AvailableCurrencySymbols()
}

func (p selectedProvider) AvailableListPatterns() []ListPatternRecord {
	if !p.features[FeatureListsPatterns] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableListPatterns(), p.locales, func(r ListPatternRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableUnitPatterns() []UnitPatternRecord {
	if !p.features[FeatureUnitsDurationCore] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableUnitPatterns(), p.locales, func(r UnitPatternRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableCompactPatterns() []CompactPatternRecord {
	if !p.features[FeatureNumbersCompactDecimal] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableCompactPatterns(), p.locales, func(r CompactPatternRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableRelativeTimePatterns() []RelativePatternRecord {
	if !p.features[FeatureDatesRelativeTime] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableRelativeTimePatterns(), p.locales, func(r RelativePatternRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableRelativeSpecials() []RelativeSpecialRecord {
	if !p.features[FeatureDatesRelativeTime] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableRelativeSpecials(), p.locales, func(r RelativeSpecialRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableIntervalPatterns() []IntervalPatternRecord {
	if !p.features[FeatureDatesGregorianIntervals] {
		return nil
	}
	return filterLocaleRecords(p.base.AvailableIntervalPatterns(), p.locales, func(r IntervalPatternRecord) string { return r.Locale })
}

func (p selectedProvider) AvailableDisplayNames() []DisplayNameRecord {
	all := p.base.AvailableDisplayNames()
	out := make([]DisplayNameRecord, 0, len(all))
	for _, rec := range all {
		if p.locales[CanonicalTag(rec.Locale)] && p.displayNameFeatureSelected(rec.Kind) {
			out = append(out, rec)
		}
	}
	return out
}

func (p selectedProvider) AvailableBCP47Keys() []string {
	if !p.features[FeatureBCP47Extensions] {
		return nil
	}
	return p.base.AvailableBCP47Keys()
}

func filterLocaleRecords[T any](records []T, locales map[string]bool, localeOf func(T) string) []T {
	out := make([]T, 0, len(records))
	for _, rec := range records {
		if locales[CanonicalTag(localeOf(rec))] {
			out = append(out, rec)
		}
	}
	return out
}
