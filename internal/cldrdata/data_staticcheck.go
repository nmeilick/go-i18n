//go:build staticcheck

package cldrdata

// These stubs let staticcheck type-check packages without parsing the large
// generated CLDR literal tables. Normal builds do not set the staticcheck tag.
var metadata = Metadata{
	CLDRVersion:    "staticcheck",
	UnicodeVersion: "staticcheck",
	Generator:      "staticcheck",
	LockID:         "staticcheck",
	FeatureSet:     []string{},
}

var localeRecords []LocaleRecord
var regionDefaults []RegionDefaults
var currencyFractions []CurrencyFraction
var currencySymbols []CurrencySymbolRecord
var listPatterns []ListPatternRecord
var unitPatterns []UnitPatternRecord
var compactPatterns []CompactPatternRecord
var relativePatterns []RelativePatternRecord
var relativeSpecials []RelativeSpecialRecord
var intervalPatterns []IntervalPatternRecord
var displayNames []DisplayNameRecord
var bcp47Types []BCP47TypeRecord
