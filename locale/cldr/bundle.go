package cldr

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/nmeilick/go-i18n/internal/cldrdata"
)

// ErrorCode identifies structured CLDR bundle and service failures.
type ErrorCode string

const (
	ErrInvalidBundle         ErrorCode = "invalid_bundle"
	ErrIncompatibleVersion   ErrorCode = "incompatible_version"
	ErrCoverageConflict      ErrorCode = "coverage_conflict"
	ErrClosedService         ErrorCode = "closed_service"
	ErrUnsupportedCapability ErrorCode = "unsupported_capability"
)

// Error is a structured CLDR error.
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" && e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Bundle is a closeable CLDR data source with semantic coverage metadata.
type Bundle interface {
	Info() Info
	Coverage() []Coverage
	Data() DataProvider
	Close() error
}

type staticBundle struct {
	info     Info
	coverage []Coverage
	data     DataProvider
	close    func() error
	once     sync.Once
	err      error
}

// NewBundle wraps a public DataProvider with bundle metadata.
func NewBundle(info Info, coverage []Coverage, data DataProvider, close func() error) (Bundle, error) {
	if data == nil {
		return nil, &Error{Code: ErrInvalidBundle, Message: "cldr bundle requires a data provider"}
	}
	info = normalizeInfo(info, data)
	coverage = normalizeCoverage(coverage)
	return &staticBundle{
		info:     info,
		coverage: cloneCoverage(coverage),
		data:     data,
		close:    close,
	}, nil
}

func (b *staticBundle) Info() Info {
	return cloneInfo(b.info)
}

func (b *staticBundle) Coverage() []Coverage {
	return cloneCoverage(b.coverage)
}

func (b *staticBundle) Data() DataProvider {
	return b.data
}

func (b *staticBundle) Close() error {
	b.once.Do(func() {
		if b.close != nil {
			b.err = b.close()
		}
	})
	return b.err
}

// BuiltinLean returns the generated lean all-locale CLDR bundle.
func BuiltinLean() Bundle {
	provider := fromInternalProvider{p: cldrdata.Default()}
	b, err := NewBundle(builtinInfo(provider), builtinCoverage(provider), provider, nil)
	if err != nil {
		panic(err)
	}
	return b
}

func builtinInfo(provider DataProvider) Info {
	meta := provider.Metadata()
	return Info{
		ID:   "builtin-lean",
		Name: "Built-in lean CLDR bundle",
		Mode: "native-go",
		Versions: Versions{
			CLDR:                 meta.CLDRVersion,
			Unicode:              meta.UnicodeVersion,
			SchemaMajor:          1,
			ProviderAPIMajor:     ProviderAPIMajor,
			ProviderAPIMinor:     ProviderAPIMinor,
			FeatureRegistryMajor: FeatureRegistryMajor,
			FeatureRegistryMinor: FeatureRegistryMinor,
			Generator:            meta.Generator,
			SourceIdentity:       meta.SourceIdentity,
			SourceDigest:         meta.TreeDigest,
			License:              meta.License,
		},
		Features: leanFeatures(),
		Locales:  provider.AvailableLocales(),
		Digest:   meta.TreeDigest,
	}
}

func leanFeatures() []FeatureID {
	return []FeatureID{
		FeatureCoreIdentity,
		FeatureProfileDefaults,
		FeatureNumbersSymbols,
		FeatureNumbersDecimal,
		FeatureNumbersPercent,
		FeatureCurrenciesFractions,
		FeatureCurrenciesSymbols,
		FeatureDatesGregorianPatterns,
		FeatureDatesGregorianNames,
		FeatureBCP47Extensions,
	}
}

func builtinCoverage(provider DataProvider) []Coverage {
	locales := provider.AvailableLocales()
	return []Coverage{
		{Feature: FeatureCoreIdentity, Scope: ScopeGlobal, Role: RoleAuthoritative, Status: StatusImplemented, All: true},
		{Feature: FeatureProfileDefaults, Scope: ScopeTerritory, Role: RoleAuthoritative, Status: StatusImplemented, All: true},
		{Feature: FeatureNumbersSymbols, Scope: ScopeLocale, Role: RoleAuthoritative, Status: StatusImplemented, Keys: locales},
		{Feature: FeatureNumbersDecimal, Scope: ScopeLocale, Role: RoleAuthoritative, Status: StatusImplemented, Keys: locales},
		{Feature: FeatureNumbersPercent, Scope: ScopeLocale, Role: RoleAuthoritative, Status: StatusImplemented, Keys: locales},
		{Feature: FeatureCurrenciesFractions, Scope: ScopeCurrency, Role: RoleAuthoritative, Status: StatusImplemented, All: true},
		{Feature: FeatureCurrenciesSymbols, Scope: ScopeCurrency, Role: RoleAuthoritative, Status: StatusDataAvailable, All: true},
		{Feature: FeatureDatesGregorianPatterns, Scope: ScopeLocale, Role: RoleAuthoritative, Status: StatusImplemented, Keys: locales},
		{Feature: FeatureDatesGregorianNames, Scope: ScopeLocale, Role: RoleAuthoritative, Status: StatusImplemented, Keys: locales},
		{Feature: FeatureBCP47Extensions, Scope: ScopeGlobal, Role: RoleAuthoritative, Status: StatusImplemented, All: true},
	}
}

// ComposeOption configures bundle composition.
type ComposeOption func(*composeConfig)

type composeConfig struct {
	replaceAuthoritative bool
	allowVersionMismatch bool
}

// WithAuthoritativeReplacement lets later bundles replace earlier
// authoritative coverage. Without this option, authoritative overlap is an
// error.
func WithAuthoritativeReplacement() ComposeOption {
	return func(c *composeConfig) { c.replaceAuthoritative = true }
}

// WithVersionMismatchAllowed disables default version compatibility checks.
func WithVersionMismatchAllowed() ComposeOption {
	return func(c *composeConfig) { c.allowVersionMismatch = true }
}

// Compose validates and combines bundles. Later bundles are consulted first
// for lookups when explicit authoritative replacement is enabled.
func Compose(bundles []Bundle, opts ...ComposeOption) (Bundle, error) {
	cfg := composeConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(bundles) == 0 {
		return BuiltinLean(), nil
	}
	for i, b := range bundles {
		if b == nil || b.Data() == nil {
			return nil, &Error{Code: ErrInvalidBundle, Message: fmt.Sprintf("bundle %d is nil or has no data provider", i)}
		}
	}
	if !cfg.allowVersionMismatch {
		if err := checkVersions(bundles); err != nil {
			return nil, err
		}
	}
	coverage, err := composeCoverage(bundles, cfg.replaceAuthoritative)
	if err != nil {
		return nil, err
	}
	info := composeInfo(bundles, coverage)
	return NewBundle(info, coverage, compositeProvider{bundles: append([]Bundle(nil), bundles...)}, func() error {
		return closeBundles(bundles)
	})
}

func checkVersions(bundles []Bundle) error {
	base := bundles[0].Info().Versions
	for _, b := range bundles[1:] {
		v := b.Info().Versions
		if v.CLDR != base.CLDR || v.Unicode != base.Unicode || v.TZDB != base.TZDB ||
			v.SchemaMajor != base.SchemaMajor || v.ProviderAPIMajor != base.ProviderAPIMajor ||
			v.FeatureRegistryMajor != base.FeatureRegistryMajor {
			return &Error{Code: ErrIncompatibleVersion, Message: "CLDR bundles have incompatible versions"}
		}
	}
	return nil
}

func composeCoverage(bundles []Bundle, replace bool) ([]Coverage, error) {
	out := []Coverage{}
	authoritative := []Coverage{}
	for _, b := range bundles {
		for _, rec := range b.Coverage() {
			rec = normalizeOneCoverage(rec)
			if rec.Role != RoleAuthoritative {
				out = append(out, rec)
				continue
			}
			conflictAt := -1
			for i, existing := range authoritative {
				if overlapsCoverage(existing, rec) {
					conflictAt = i
					break
				}
			}
			if conflictAt >= 0 {
				if !replace {
					return nil, &Error{
						Code:    ErrCoverageConflict,
						Message: fmt.Sprintf("authoritative coverage overlap for %s/%s", rec.Feature, rec.Scope),
					}
				}
				authoritative[conflictAt] = rec
				continue
			}
			authoritative = append(authoritative, rec)
		}
	}
	out = append(out, authoritative...)
	return normalizeCoverage(out), nil
}

func overlapsCoverage(a, b Coverage) bool {
	if a.Feature != b.Feature || a.Scope != b.Scope {
		return false
	}
	if a.All || b.All {
		return true
	}
	set := make(map[string]struct{}, len(a.Keys))
	for _, key := range a.Keys {
		set[key] = struct{}{}
	}
	for _, key := range b.Keys {
		if _, ok := set[key]; ok {
			return true
		}
	}
	return false
}

func composeInfo(bundles []Bundle, coverage []Coverage) Info {
	info := bundles[0].Info()
	info.ID = "composed"
	info.Name = "Composed CLDR bundle"
	info.Mode = "composed"
	features := map[FeatureID]bool{}
	locales := map[string]bool{}
	for _, b := range bundles {
		bi := b.Info()
		for _, feature := range bi.Features {
			features[feature] = true
		}
		for _, loc := range bi.Locales {
			locales[loc] = true
		}
	}
	info.Features = sortedFeatureIDs(features)
	info.Locales = sortedStrings(locales)
	return info
}

func closeBundles(bundles []Bundle) error {
	var errs []error
	for i := len(bundles) - 1; i >= 0; i-- {
		if err := bundles[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type compositeProvider struct {
	bundles []Bundle
}

func (p compositeProvider) providers() []DataProvider {
	out := make([]DataProvider, 0, len(p.bundles))
	for i := len(p.bundles) - 1; i >= 0; i-- {
		out = append(out, p.bundles[i].Data())
	}
	return out
}

func (p compositeProvider) Metadata() Metadata {
	if len(p.bundles) == 0 {
		return Metadata{}
	}
	return p.bundles[0].Data().Metadata()
}

func (p compositeProvider) Locale(tag string) (LocaleRecord, bool) {
	for _, provider := range p.providers() {
		if rec, ok := provider.Locale(tag); ok {
			return rec, true
		}
	}
	return LocaleRecord{}, false
}

func (p compositeProvider) Parent(tag string) (string, bool) {
	for _, provider := range p.providers() {
		if parent, ok := provider.Parent(tag); ok {
			return parent, true
		}
	}
	return "", false
}

func (p compositeProvider) RegionDefaults(region string) (RegionDefaults, bool) {
	for _, provider := range p.providers() {
		if rec, ok := provider.RegionDefaults(region); ok {
			return rec, true
		}
	}
	return RegionDefaults{}, false
}

func (p compositeProvider) CurrencyFraction(code string) CurrencyFraction {
	for _, provider := range p.providers() {
		rec := provider.CurrencyFraction(code)
		if strings.EqualFold(rec.Code, strings.TrimSpace(code)) {
			return rec
		}
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	return CurrencyFraction{Code: code, Digits: 2, CashDigits: 2}
}

func (p compositeProvider) CurrencySymbol(locale, code string) (string, bool) {
	for _, provider := range p.providers() {
		if sym, ok := provider.CurrencySymbol(locale, code); ok {
			return sym, true
		}
	}
	return "", false
}

func (p compositeProvider) BCP47Types(key string) []BCP47TypeRecord {
	seen := map[string]bool{}
	out := []BCP47TypeRecord{}
	for _, provider := range p.providers() {
		for _, rec := range provider.BCP47Types(key) {
			id := rec.Key + "\x00" + rec.Type + "\x00" + rec.Alias
			if !seen[id] {
				seen[id] = true
				out = append(out, rec)
			}
		}
	}
	return out
}

func (p compositeProvider) AvailableLocales() []string {
	set := map[string]bool{}
	for _, provider := range p.providers() {
		for _, loc := range provider.AvailableLocales() {
			set[loc] = true
		}
	}
	return sortedStrings(set)
}

func (p compositeProvider) AvailableRegions() []string {
	set := map[string]bool{}
	for _, provider := range p.providers() {
		for _, region := range provider.AvailableRegions() {
			set[region] = true
		}
	}
	return sortedStrings(set)
}

func (p compositeProvider) AvailableCurrencyFractions() []CurrencyFraction {
	seen := map[string]bool{}
	out := []CurrencyFraction{}
	for _, provider := range p.providers() {
		for _, rec := range provider.AvailableCurrencyFractions() {
			if !seen[rec.Code] {
				seen[rec.Code] = true
				out = append(out, rec)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func (p compositeProvider) AvailableCurrencySymbols() []CurrencySymbolRecord {
	seen := map[string]bool{}
	out := []CurrencySymbolRecord{}
	for _, provider := range p.providers() {
		for _, rec := range provider.AvailableCurrencySymbols() {
			if !seen[rec.Code] {
				seen[rec.Code] = true
				out = append(out, rec)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func (p compositeProvider) AvailableBCP47Keys() []string {
	set := map[string]bool{}
	for _, provider := range p.providers() {
		for _, key := range provider.AvailableBCP47Keys() {
			set[key] = true
		}
	}
	return sortedStrings(set)
}

func normalizeInfo(info Info, data DataProvider) Info {
	meta := data.Metadata()
	if info.Versions.CLDR == "" {
		info.Versions.CLDR = meta.CLDRVersion
	}
	if info.Versions.Unicode == "" {
		info.Versions.Unicode = meta.UnicodeVersion
	}
	if info.Versions.TZDB == "" {
		info.Versions.TZDB = meta.TZDBVersion
	}
	if info.Versions.ProviderAPIMajor == 0 {
		info.Versions.ProviderAPIMajor = ProviderAPIMajor
	}
	if info.Versions.FeatureRegistryMajor == 0 {
		info.Versions.FeatureRegistryMajor = FeatureRegistryMajor
	}
	if info.Versions.Generator == "" {
		info.Versions.Generator = meta.Generator
	}
	if info.Versions.SourceIdentity == "" {
		info.Versions.SourceIdentity = meta.SourceIdentity
	}
	if info.Versions.SourceDigest == "" {
		info.Versions.SourceDigest = meta.TreeDigest
	}
	if info.Versions.License == "" {
		info.Versions.License = meta.License
	}
	if len(info.Locales) == 0 {
		info.Locales = data.AvailableLocales()
	}
	info.Locales = sortedStringSlice(info.Locales)
	info.Features = sortedFeatureSlice(info.Features)
	return info
}

func normalizeCoverage(in []Coverage) []Coverage {
	out := make([]Coverage, len(in))
	for i, rec := range in {
		out[i] = normalizeOneCoverage(rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return string(a.Feature)+"\x00"+string(a.Scope)+"\x00"+string(a.Role) <
			string(b.Feature)+"\x00"+string(b.Scope)+"\x00"+string(b.Role)
	})
	return out
}

func normalizeOneCoverage(rec Coverage) Coverage {
	if rec.Role == "" {
		rec.Role = RoleAuthoritative
	}
	if rec.Status == "" {
		rec.Status = StatusDataAvailable
	}
	rec.Keys = sortedStringSlice(rec.Keys)
	return rec
}

func cloneInfo(info Info) Info {
	info.Features = append([]FeatureID(nil), info.Features...)
	info.Locales = append([]string(nil), info.Locales...)
	return info
}

func cloneCoverage(in []Coverage) []Coverage {
	out := make([]Coverage, len(in))
	for i, rec := range in {
		out[i] = rec
		out[i].Keys = append([]string(nil), rec.Keys...)
	}
	return out
}

func sortedFeatureIDs(set map[FeatureID]bool) []FeatureID {
	out := make([]FeatureID, 0, len(set))
	for feature := range set {
		out = append(out, feature)
	}
	return sortedFeatureSlice(out)
}

func sortedFeatureSlice(in []FeatureID) []FeatureID {
	out := append([]FeatureID(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedStrings(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sortedStringSlice(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
