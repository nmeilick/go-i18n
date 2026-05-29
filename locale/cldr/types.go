package cldr

import "strings"

// ProviderAPIMajor is the semantic provider API major version implemented by
// this package. Bundles with a different major version are rejected by default.
const ProviderAPIMajor = 1

// ProviderAPIMinor is additive and may increase without breaking readers.
const ProviderAPIMinor = 0

// FeatureRegistryMajor is the stable public feature-ID registry major version.
const FeatureRegistryMajor = 1

// FeatureRegistryMinor is additive and may increase without breaking readers.
const FeatureRegistryMinor = 0

// FeatureID is a stable semantic CLDR capability identifier.
type FeatureID string

const (
	FeatureCoreIdentity             FeatureID = "core.identity"
	FeatureProfileDefaults          FeatureID = "profile.defaults"
	FeatureNumbersSymbols           FeatureID = "numbers.symbols"
	FeatureNumbersDecimal           FeatureID = "numbers.decimal"
	FeatureNumbersPercent           FeatureID = "numbers.percent"
	FeatureCurrenciesFractions      FeatureID = "currencies.fractions"
	FeatureCurrenciesSymbols        FeatureID = "currencies.symbols.standard"
	FeatureCurrenciesNarrowSymbols  FeatureID = "currencies.symbols.narrow"
	FeatureDatesGregorianPatterns   FeatureID = "dates.gregorian.patterns"
	FeatureDatesGregorianNames      FeatureID = "dates.gregorian.names"
	FeatureDatesGregorianDayPeriods FeatureID = "dates.gregorian.dayperiods"
	FeatureDatesGregorianIntervals  FeatureID = "dates.gregorian.intervals"
	FeatureBCP47Extensions          FeatureID = "bcp47.extensions"
	FeatureListsPatterns            FeatureID = "lists.patterns"
	FeatureUnitsDurationCore        FeatureID = "units.duration_core"
	FeaturePluralsCardinal          FeatureID = "plurals.cardinal"
	FeaturePluralsOrdinal           FeatureID = "plurals.ordinal"
	FeatureNumbersCompactDecimal    FeatureID = "numbers.compact.decimal"
	FeatureDatesRelativeTime        FeatureID = "dates.relative_time"
	FeatureTimeZonesNames           FeatureID = "timezones.names"
	FeatureDisplayNamesLanguages    FeatureID = "displaynames.languages"
	FeatureDisplayNamesTerritories  FeatureID = "displaynames.territories"
	FeatureDisplayNamesScripts      FeatureID = "displaynames.scripts"
	FeatureDisplayNamesCalendars    FeatureID = "displaynames.calendars"
)

// CapabilityStatus describes behavior, not merely data presence.
type CapabilityStatus string

const (
	StatusUnsupported   CapabilityStatus = "unsupported"
	StatusDataAvailable CapabilityStatus = "data-available"
	StatusExperimental  CapabilityStatus = "experimental"
	StatusImplemented   CapabilityStatus = "implemented"
	StatusConformant    CapabilityStatus = "conformant"
)

// CoverageRole distinguishes data ownership from dependency closure.
type CoverageRole string

const (
	RoleAuthoritative CoverageRole = "authoritative"
	RoleDependency    CoverageRole = "dependency-only"
)

// ScopeKind describes the key space for a coverage record.
type ScopeKind string

const (
	ScopeGlobal          ScopeKind = "global"
	ScopeLocale          ScopeKind = "locale"
	ScopeTerritory       ScopeKind = "territory"
	ScopeCurrency        ScopeKind = "currency"
	ScopeUnit            ScopeKind = "unit"
	ScopeCalendar        ScopeKind = "calendar"
	ScopeTimeZone        ScopeKind = "timezone"
	ScopeMetaZone        ScopeKind = "metazone"
	ScopeNumberingSystem ScopeKind = "numbering_system"
	ScopeCollation       ScopeKind = "collation"
	ScopeFeaturePrivate  ScopeKind = "feature_private"
)

// Versions carries compatibility-relevant CLDR bundle versions.
type Versions struct {
	CLDR                 string `json:"cldr,omitempty"`
	Unicode              string `json:"unicode,omitempty"`
	TZDB                 string `json:"tzdb,omitempty"`
	SchemaMajor          uint16 `json:"schema_major,omitempty"`
	SchemaMinor          uint16 `json:"schema_minor,omitempty"`
	ProviderAPIMajor     uint16 `json:"provider_api_major,omitempty"`
	ProviderAPIMinor     uint16 `json:"provider_api_minor,omitempty"`
	FeatureRegistryMajor uint16 `json:"feature_registry_major,omitempty"`
	FeatureRegistryMinor uint16 `json:"feature_registry_minor,omitempty"`
	Generator            string `json:"generator,omitempty"`
	SourceIdentity       string `json:"source_identity,omitempty"`
	SourceDigest         string `json:"source_digest,omitempty"`
	License              string `json:"license,omitempty"`
}

// Info describes a bundle without exposing storage internals.
type Info struct {
	ID       string      `json:"id,omitempty"`
	Name     string      `json:"name,omitempty"`
	Mode     string      `json:"mode,omitempty"`
	Versions Versions    `json:"versions"`
	Features []FeatureID `json:"features,omitempty"`
	Locales  []string    `json:"locales,omitempty"`
	Digest   string      `json:"digest,omitempty"`
}

// Coverage describes where a bundle is authoritative or dependency-only.
type Coverage struct {
	Feature FeatureID        `json:"feature"`
	Scope   ScopeKind        `json:"scope"`
	Role    CoverageRole     `json:"role"`
	Status  CapabilityStatus `json:"status"`
	All     bool             `json:"all,omitempty"`
	Keys    []string         `json:"keys,omitempty"`
}

// Contains reports whether key is included in this coverage record.
func (c Coverage) Contains(key string) bool {
	if c.All {
		return true
	}
	key = strings.TrimSpace(key)
	for _, candidate := range c.Keys {
		if candidate == key {
			return true
		}
	}
	return false
}

// Diagnostic is a stable, redaction-safe CLDR service diagnostic.
type Diagnostic struct {
	Code      string `json:"code"`
	Severity  string `json:"severity,omitempty"`
	Component string `json:"component,omitempty"`
	Feature   string `json:"feature,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Metadata describes the CLDR data source behind a DataProvider.
type Metadata struct {
	CLDRVersion    string
	UnicodeVersion string
	TZDBVersion    string
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

// CurrencySymbolRecord stores a locale-specific currency display symbol.
type CurrencySymbolRecord struct {
	Locale string
	Code   string
	Symbol string
	Narrow string
}

// LocaleRecord stores effective locale-specific formatting data.
type LocaleRecord struct {
	Tag             string
	Parent          string
	NumberingSystem string
	DateFormats     [4]string
	TimeFormats     [4]string
	DateTimeFormats [4]string
	MonthsWide      [12]string
	MonthsAbbr      [12]string
	WeekdaysWide    [7]string
	WeekdaysAbbr    [7]string
	DayPeriods      [2]string
	CurrencyPattern string
	Accounting      string
}

// ListPattern stores one CLDR list pattern set.
type ListPattern struct {
	Two    string
	Start  string
	Middle string
	End    string
}

// ListPatternRecord stores one localized list pattern set.
type ListPatternRecord struct {
	Locale  string
	Type    string
	Width   string
	Pattern ListPattern
}

// UnitPatternRecord stores one localized unit pattern for one plural category.
type UnitPatternRecord struct {
	Locale   string
	Unit     string
	Width    string
	Category string
	Pattern  string
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

// DataProvider is the public read-only CLDR data boundary. Implementations
// must be safe for concurrent use.
type DataProvider interface {
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
