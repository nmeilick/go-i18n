package locale

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nmeilick/go-i18n/internal/cldrdata"
	"github.com/nmeilick/go-i18n/observe"
	"golang.org/x/text/language"
)

// Field identifies one resolved profile field.
type Field string

const (
	FieldLanguage          Field = "language"
	FieldTimeZone          Field = "timezone"
	FieldFormattingRegion  Field = "formatting_region"
	FieldCurrentRegion     Field = "current_region"
	FieldMarketRegion      Field = "market_region"
	FieldResidenceRegion   Field = "residence_region"
	FieldDisplayCurrency   Field = "display_currency"
	FieldCalendar          Field = "calendar"
	FieldNumberingSystem   Field = "numbering_system"
	FieldHourCycle         Field = "hour_cycle"
	FieldMeasurementSystem Field = "measurement_system"
	FieldFirstDay          Field = "first_day"
)

// RegionUse distinguishes the different meanings of a territory signal.
type RegionUse string

const (
	RegionFormatting RegionUse = "formatting"
	RegionCurrent    RegionUse = "current"
	RegionMarket     RegionUse = "market"
	RegionResidence  RegionUse = "residence"
)

// CurrencyUse distinguishes profile display currency from domain money data.
type CurrencyUse string

const (
	CurrencyDisplay CurrencyUse = "display"
)

// ObservationKind describes normalized observation value shape.
type ObservationKind string

const (
	ObservationLanguage        ObservationKind = "language"
	ObservationRegion          ObservationKind = "region"
	ObservationTimeZone        ObservationKind = "timezone"
	ObservationCurrency        ObservationKind = "currency"
	ObservationCalendar        ObservationKind = "calendar"
	ObservationNumberingSystem ObservationKind = "numbering_system"
	ObservationHourCycle       ObservationKind = "hour_cycle"
	ObservationMeasurement     ObservationKind = "measurement_system"
	ObservationFirstDay        ObservationKind = "first_day"
)

// SourceClass groups app-owned signal origins without carrying raw transport
// details or identifiers.
type SourceClass string

const (
	SourceExplicitUser   SourceClass = "explicit_user"
	SourceExplicitApp    SourceClass = "explicit_app"
	SourceAccountDefault SourceClass = "account_default"
	SourceJobDefault     SourceClass = "job_default"
	SourceClientLocale   SourceClass = "client_locale"
	SourceClientTimeZone SourceClass = "client_timezone"
	SourceEnvironment    SourceClass = "environment"
	SourceGeoIP          SourceClass = "geoip"
	SourceAppPolicy      SourceClass = "app_policy"
	SourceLibraryDefault SourceClass = "library_default"
)

// Trust describes how directly the observation expresses user/application
// preference.
type Trust int

const (
	TrustLow Trust = iota + 1
	TrustMedium
	TrustHigh
	TrustExplicit
)

// Sensitivity describes how cache/privacy-sensitive an observation source is.
type Sensitivity string

const (
	SensitivityPublic      Sensitivity = "public"
	SensitivityRequest     Sensitivity = "request"
	SensitivityUserPrivate Sensitivity = "user_private"
)

// Confidence is a stable public confidence bucket.
type Confidence string

const (
	ConfidenceNone   Confidence = "none"
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// Observation is one normalized, transport-neutral signal. It must not contain
// raw headers, cookies, IPs, account IDs, arbitrary env values, or cache rules.
type Observation struct {
	Kind          ObservationKind `json:"kind"`
	Value         string          `json:"value"`
	SourceClass   SourceClass     `json:"source_class"`
	Trust         Trust           `json:"trust"`
	Sensitivity   Sensitivity     `json:"sensitivity"`
	Age           time.Duration   `json:"age,omitempty"`
	AllowedFields []Field         `json:"allowed_fields,omitempty"`
	Weight        int             `json:"weight,omitempty"`
	Explicit      bool            `json:"explicit,omitempty"`
	RegionUse     RegionUse       `json:"region_use,omitempty"`
	CurrencyUse   CurrencyUse     `json:"currency_use,omitempty"`
}

// ObservationOption configures a normalized observation.
type ObservationOption func(*Observation)

// FromSource sets source metadata.
func FromSource(source SourceClass, trust Trust, sensitivity Sensitivity) ObservationOption {
	return func(o *Observation) {
		o.SourceClass = source
		o.Trust = trust
		o.Sensitivity = sensitivity
	}
}

// WithAllowedFields restricts which fields an observation may influence.
func WithAllowedFields(fields ...Field) ObservationOption {
	return func(o *Observation) {
		o.AllowedFields = append([]Field(nil), fields...)
	}
}

// WithAge sets observation age for scoring. Raw timestamps stay outside locale.
func WithAge(age time.Duration) ObservationOption {
	return func(o *Observation) {
		if age > 0 {
			o.Age = age
		}
	}
}

// WithWeight sets an input weight such as an Accept-Language q value scaled to
// 0..1000.
func WithWeight(weight int) ObservationOption {
	return func(o *Observation) { o.Weight = weight }
}

// Explicit marks the observation as a direct user/app setting.
func Explicit() ObservationOption {
	return func(o *Observation) {
		o.Explicit = true
		if o.Trust < TrustExplicit {
			o.Trust = TrustExplicit
		}
	}
}

// LanguageObservation creates a normalized language-tag observation.
func LanguageObservation(tag string, opts ...ObservationOption) (Observation, error) {
	parsed, err := Parse(tag)
	if err != nil {
		return Observation{}, err
	}
	o := Observation{Kind: ObservationLanguage, Value: parsed.String(), SourceClass: SourceClientLocale, Trust: TrustMedium, Sensitivity: SensitivityRequest, Weight: 1000}
	applyObservationOptions(&o, opts)
	return o, nil
}

// AcceptLanguageObservations parses an Accept-Language value and stores only
// normalized tag candidates plus source metadata.
func AcceptLanguageObservations(header string, opts ...ObservationOption) []Observation {
	cands := AcceptLanguage(header)
	out := make([]Observation, 0, len(cands))
	for _, cand := range cands {
		o, err := LanguageObservation(cand.Tag, append([]ObservationOption{
			FromSource(SourceClientLocale, TrustMedium, SensitivityRequest),
			WithWeight(int(cand.Weight * 1000)),
		}, opts...)...)
		if err == nil {
			out = append(out, o)
		}
	}
	return out
}

// TimeZoneObservation creates a normalized IANA timezone observation.
func TimeZoneObservation(name string, opts ...ObservationOption) (Observation, error) {
	if err := validateTimeZoneName(name); err != nil {
		return Observation{}, err
	}
	name = strings.TrimSpace(name)
	if _, err := time.LoadLocation(name); err != nil {
		return Observation{}, fmt.Errorf("load timezone %q: %w", name, err)
	}
	o := Observation{Kind: ObservationTimeZone, Value: name, SourceClass: SourceClientTimeZone, Trust: TrustMedium, Sensitivity: SensitivityRequest, Weight: 1000}
	applyObservationOptions(&o, opts)
	return o, nil
}

// RegionObservation creates a normalized territory observation.
func RegionObservation(region string, use RegionUse, opts ...ObservationOption) (Observation, error) {
	region = normalizeRegion(region)
	if len(region) != 2 && len(region) != 3 {
		return Observation{}, fmt.Errorf("invalid region %q", region)
	}
	o := Observation{Kind: ObservationRegion, Value: region, RegionUse: use, SourceClass: SourceGeoIP, Trust: TrustLow, Sensitivity: SensitivityUserPrivate, Weight: 1000}
	applyObservationOptions(&o, opts)
	return o, nil
}

// CurrencyObservation creates a display-currency observation.
func CurrencyObservation(code CurrencyCode, use CurrencyUse, opts ...ObservationOption) (Observation, error) {
	code = Currency(code.String())
	if code == "" || !code.Valid() {
		return Observation{}, fmt.Errorf("invalid currency %q", code)
	}
	if use == "" {
		use = CurrencyDisplay
	}
	o := Observation{Kind: ObservationCurrency, Value: code.String(), CurrencyUse: use, SourceClass: SourceExplicitUser, Trust: TrustExplicit, Sensitivity: SensitivityUserPrivate, Weight: 1000, Explicit: true}
	applyObservationOptions(&o, opts)
	return o, nil
}

// EnvironmentLocaleObservation creates a normalized locale observation from an
// application-supplied environment locale value. The raw environment variable
// name and value are not retained.
func EnvironmentLocaleObservation(value string, opts ...ObservationOption) (Observation, error) {
	return LanguageObservation(value, append([]ObservationOption{FromSource(SourceEnvironment, TrustMedium, SensitivityRequest)}, opts...)...)
}

func applyObservationOptions(o *Observation, opts []ObservationOption) {
	for _, opt := range opts {
		opt(o)
	}
	if o.Weight == 0 {
		o.Weight = 1000
	}
	if o.Sensitivity == "" {
		o.Sensitivity = SensitivityRequest
	}
}

// DefaultsProvider supplies CLDR-backed field defaults through a narrow public
// boundary.
type DefaultsProvider interface {
	DefaultsForRegion(region string) (RegionDefaults, bool)
	LocaleDefaults(tag language.Tag) LocaleDefaults
	CanonicalTimeZoneForRegion(region string) (string, bool)
	CurrencyDigits(code CurrencyCode) int
	KnownExtension(key, typ string) bool
	DataVersion() string
}

// RegionDefaults are formatting defaults for a territory.
type RegionDefaults struct {
	Region            string
	DisplayCurrency   CurrencyCode
	MeasurementSystem string
	FirstDay          string
	TimeZone          string
}

// LocaleDefaults are formatting defaults for a locale.
type LocaleDefaults struct {
	NumberingSystem string
	Calendar        string
}

type generatedDefaults struct {
	data cldrdata.Provider
}

// GeneratedDefaults returns the built-in CLDR-backed defaults provider.
func GeneratedDefaults() DefaultsProvider {
	return generatedDefaults{data: cldrdata.Default()}
}

func (g generatedDefaults) DefaultsForRegion(region string) (RegionDefaults, bool) {
	rec, ok := g.data.RegionDefaults(region)
	if !ok {
		return RegionDefaults{}, false
	}
	return RegionDefaults{
		Region:            rec.Region,
		DisplayCurrency:   Currency(rec.Currency),
		MeasurementSystem: rec.MeasurementSystem,
		FirstDay:          rec.FirstDay,
		TimeZone:          rec.TimeZone,
	}, true
}

func (g generatedDefaults) LocaleDefaults(tag language.Tag) LocaleDefaults {
	for cur := tag.String(); cur != ""; {
		if rec, ok := g.data.Locale(cur); ok {
			return LocaleDefaults{NumberingSystem: rec.NumberingSystem, Calendar: defaultCalendar}
		}
		parent, ok := g.data.Parent(cur)
		if !ok || parent == cur {
			break
		}
		cur = parent
	}
	return LocaleDefaults{NumberingSystem: "latn", Calendar: defaultCalendar}
}

func (g generatedDefaults) CanonicalTimeZoneForRegion(region string) (string, bool) {
	rec, ok := g.DefaultsForRegion(region)
	return rec.TimeZone, ok && rec.TimeZone != ""
}

func (g generatedDefaults) CurrencyDigits(code CurrencyCode) int {
	return g.data.CurrencyFraction(code.String()).Digits
}

func (g generatedDefaults) KnownExtension(key, typ string) bool {
	for _, rec := range g.data.BCP47Types(key) {
		if rec.Type == strings.ToLower(typ) || rec.Alias == typ {
			return true
		}
	}
	return false
}

func (g generatedDefaults) DataVersion() string {
	return g.data.Metadata().CLDRVersion
}

// FieldPolicy controls candidate acceptance for one field.
type FieldPolicy struct {
	Enabled               bool
	ApplyUntrusted        bool
	MinimumConfidence     Confidence
	AllowLanguageInfer    bool
	AllowRegionInfer      bool
	AllowTimeZoneInfer    bool
	AllowCurrencyInfer    bool
	AllowUnicodeExtension bool
}

// ResolverPolicy configures evidence scoring and field defaults.
type ResolverPolicy struct {
	Fields                map[Field]FieldPolicy
	SourceWeights         map[SourceClass]int
	TrustWeights          map[Trust]int
	MaxCandidatesPerField int
}

// HelpfulResolverPolicy is the default app-friendly policy.
func HelpfulResolverPolicy() ResolverPolicy {
	fields := map[Field]FieldPolicy{}
	for _, field := range allFields() {
		fields[field] = FieldPolicy{
			Enabled: true, ApplyUntrusted: true, MinimumConfidence: ConfidenceLow,
			AllowLanguageInfer: true, AllowRegionInfer: true, AllowTimeZoneInfer: true,
			AllowCurrencyInfer: true, AllowUnicodeExtension: true,
		}
	}
	return ResolverPolicy{
		Fields: fields,
		SourceWeights: map[SourceClass]int{
			SourceExplicitUser: 1000, SourceExplicitApp: 980, SourceAccountDefault: 850, SourceJobDefault: 800,
			SourceAppPolicy: 750, SourceEnvironment: 650, SourceClientLocale: 600, SourceClientTimeZone: 600,
			SourceGeoIP: 450, SourceLibraryDefault: 100,
		},
		TrustWeights:          map[Trust]int{TrustExplicit: 500, TrustHigh: 250, TrustMedium: 100, TrustLow: 0},
		MaxCandidatesPerField: 64,
	}
}

// StrictResolverPolicy disables implicit inference and untrusted extensions.
func StrictResolverPolicy() ResolverPolicy {
	p := HelpfulResolverPolicy()
	for field, fp := range p.Fields {
		fp.ApplyUntrusted = false
		fp.AllowLanguageInfer = false
		fp.AllowRegionInfer = false
		fp.AllowTimeZoneInfer = false
		fp.AllowCurrencyInfer = false
		fp.AllowUnicodeExtension = field == FieldLanguage
		fp.MinimumConfidence = ConfidenceMedium
		p.Fields[field] = fp
	}
	return p
}

// Resolver combines normalized observations into an immutable profile.
type Resolver struct {
	defaults      DefaultsProvider
	policy        ResolverPolicy
	observer      observe.Observer
	observePolicy observe.Policy
	observeAttrs  []observe.Attr
}

// ResolverOption configures a Resolver.
type ResolverOption func(*Resolver)

// WithDefaultsProvider overrides CLDR-backed defaults.
func WithDefaultsProvider(provider DefaultsProvider) ResolverOption {
	return func(r *Resolver) {
		if provider != nil {
			r.defaults = provider
		}
	}
}

// WithResolverPolicy overrides scoring policy.
func WithResolverPolicy(policy ResolverPolicy) ResolverOption {
	return func(r *Resolver) { r.policy = policy }
}

// WithObserver configures structured resolver events.
func WithObserver(observer observe.Observer) ResolverOption {
	return func(r *Resolver) {
		if observer != nil {
			r.observer = observer
		}
	}
}

// WithObservePolicy configures resolver event bounds and filtering.
func WithObservePolicy(policy observe.Policy) ResolverOption {
	return func(r *Resolver) { r.observePolicy = policy }
}

// WithObserveAttrs adds resolver-level observability attributes.
func WithObserveAttrs(attrs ...observe.Attr) ResolverOption {
	return func(r *Resolver) {
		r.observeAttrs = append(r.observeAttrs, attrs...)
	}
}

// NewResolver creates a profile resolver.
func NewResolver(opts ...ResolverOption) *Resolver {
	r := &Resolver{defaults: GeneratedDefaults(), policy: HelpfulResolverPolicy(), observePolicy: observe.DefaultPolicy()}
	for _, opt := range opts {
		opt(r)
	}
	if r.defaults == nil {
		r.defaults = GeneratedDefaults()
	}
	return r
}

// CandidateResult is a redaction-safe candidate score.
type CandidateResult struct {
	Field       Field       `json:"field"`
	Value       string      `json:"value"`
	Score       int         `json:"score"`
	Confidence  Confidence  `json:"confidence"`
	Selected    bool        `json:"selected"`
	SourceClass SourceClass `json:"source_class"`
	Trust       Trust       `json:"trust"`
	Reason      string      `json:"reason"`
	Sensitivity Sensitivity `json:"sensitivity,omitempty"`
}

// ResolveDiagnostic is a redaction-safe resolver diagnostic.
type ResolveDiagnostic struct {
	Code        string      `json:"code"`
	Field       Field       `json:"field,omitempty"`
	Value       string      `json:"value,omitempty"`
	SourceClass SourceClass `json:"source_class,omitempty"`
	Confidence  Confidence  `json:"confidence,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Severity    string      `json:"severity,omitempty"`
}

// ResolveResult exposes provenance for production debugging without raw input.
type ResolveResult struct {
	Profile     Profile             `json:"-"`
	Snapshot    ProfileSnapshot     `json:"profile"`
	Candidates  []CandidateResult   `json:"candidates,omitempty"`
	Diagnostics []ResolveDiagnostic `json:"diagnostics,omitempty"`
	Sensitivity Sensitivity         `json:"sensitivity,omitempty"`
	DataVersion string              `json:"data_version,omitempty"`
}

// Resolve returns the effective profile and discards detailed provenance.
func (r *Resolver) Resolve(observations ...Observation) (Profile, error) {
	res, err := r.ResolveDetailed(observations...)
	return res.Profile, err
}

// ResolveDetailed returns the effective profile and deterministic provenance.
func (r *Resolver) ResolveDetailed(observations ...Observation) (ResolveResult, error) {
	return r.ResolveDetailedContext(context.Background(), observations...)
}

// ResolveDetailedContext returns the effective profile and emits resolver events
// with ctx when an observer is configured.
func (r *Resolver) ResolveDetailedContext(ctx context.Context, observations ...Observation) (ResolveResult, error) {
	if r == nil {
		r = NewResolver()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	policy := normalizePolicy(r.policy)
	cands := r.expandCandidates(observations, policy)
	selected := selectCandidates(cands, policy)
	profile, diagnostics, err := r.profileFromSelected(selected, policy)
	if err != nil {
		return ResolveResult{}, err
	}
	results := make([]CandidateResult, len(cands))
	for i, cand := range cands {
		cr := cand.result()
		if chosen, ok := selected[cand.field]; ok && chosen.value == cand.value {
			cr.Selected = true
		}
		results[i] = cr
	}
	sort.SliceStable(results, func(i, j int) bool {
		return compareCandidateResult(results[i], results[j])
	})
	result := ResolveResult{
		Profile:     profile,
		Snapshot:    profile.Snapshot(),
		Candidates:  results,
		Diagnostics: diagnostics,
		Sensitivity: maxSensitivity(observations),
		DataVersion: r.defaults.DataVersion(),
	}
	r.observeResolve(ctx, result, observations)
	return result, nil
}

type resolverCandidate struct {
	field       Field
	value       string
	score       int
	source      SourceClass
	trust       Trust
	sensitivity Sensitivity
	reason      string
	explicit    bool
}

func (c resolverCandidate) result() CandidateResult {
	return CandidateResult{
		Field: c.field, Value: c.value, Score: c.score, Confidence: confidenceForScore(c.score),
		SourceClass: c.source, Trust: c.trust, Reason: c.reason, Sensitivity: c.sensitivity,
	}
}

func (r *Resolver) expandCandidates(observations []Observation, policy ResolverPolicy) []resolverCandidate {
	out := []resolverCandidate{}
	for _, obs := range observations {
		if strings.TrimSpace(obs.Value) == "" {
			continue
		}
		score := scoreObservation(obs, policy)
		switch obs.Kind {
		case ObservationLanguage:
			out = appendCandidate(out, obs, FieldLanguage, obs.Value, score, "observed_language")
			tag, err := Parse(obs.Value)
			if err == nil {
				if region, conf := tag.Region(); conf >= language.Exact && region.String() != "" {
					regionScore := score - 220
					out = appendCandidate(out, obs, FieldFormattingRegion, region.String(), regionScore, "language_region")
					r.addRegionDefaults(&out, obs, region.String(), regionScore-1200, "language_region_default")
				}
				r.addUnicodeExtensions(&out, obs, tag, score-40, policy)
			}
		case ObservationRegion:
			field := fieldForRegionUse(obs.RegionUse)
			out = appendCandidate(out, obs, field, obs.Value, score, "observed_region")
			out = appendCandidate(out, obs, FieldFormattingRegion, obs.Value, score-60, "region_formatting_default")
			r.addRegionDefaults(&out, obs, obs.Value, score-120, "region_default")
		case ObservationTimeZone:
			out = appendCandidate(out, obs, FieldTimeZone, obs.Value, score, "observed_timezone")
			if region := regionForTimeZone(obs.Value); region != "" {
				out = appendCandidate(out, obs, FieldCurrentRegion, region, score-180, "timezone_region")
				out = appendCandidate(out, obs, FieldFormattingRegion, region, score-240, "timezone_region")
				r.addRegionDefaults(&out, obs, region, score-300, "timezone_region_default")
			}
		case ObservationCurrency:
			if obs.CurrencyUse == "" || obs.CurrencyUse == CurrencyDisplay {
				out = appendCandidate(out, obs, FieldDisplayCurrency, obs.Value, score, "observed_display_currency")
			}
		case ObservationCalendar:
			out = appendCandidate(out, obs, FieldCalendar, obs.Value, score, "observed_calendar")
		case ObservationNumberingSystem:
			out = appendCandidate(out, obs, FieldNumberingSystem, obs.Value, score, "observed_numbering_system")
		case ObservationHourCycle:
			out = appendCandidate(out, obs, FieldHourCycle, obs.Value, score, "observed_hour_cycle")
		case ObservationMeasurement:
			out = appendCandidate(out, obs, FieldMeasurementSystem, obs.Value, score, "observed_measurement_system")
		case ObservationFirstDay:
			out = appendCandidate(out, obs, FieldFirstDay, strings.ToLower(obs.Value), score, "observed_first_day")
		}
	}
	out = dedupeResolverCandidates(out)
	out = filterResolverCandidates(out, r.defaults)
	out = limitResolverCandidates(out, policy.MaxCandidatesPerField)
	return out
}

func (r *Resolver) addRegionDefaults(out *[]resolverCandidate, obs Observation, region string, score int, reason string) {
	if defs, ok := r.defaults.DefaultsForRegion(region); ok {
		if defs.DisplayCurrency != "" {
			*out = appendCandidate(*out, obs, FieldDisplayCurrency, defs.DisplayCurrency.String(), score, reason+"_currency")
		}
		if defs.MeasurementSystem != "" {
			*out = appendCandidate(*out, obs, FieldMeasurementSystem, defs.MeasurementSystem, score, reason+"_measurement")
		}
		if defs.FirstDay != "" {
			*out = appendCandidate(*out, obs, FieldFirstDay, defs.FirstDay, score, reason+"_week")
		}
		if defs.TimeZone != "" {
			*out = appendCandidate(*out, obs, FieldTimeZone, defs.TimeZone, score-50, reason+"_timezone")
		}
	}
}

func (r *Resolver) addUnicodeExtensions(out *[]resolverCandidate, obs Observation, tag language.Tag, score int, policy ResolverPolicy) {
	extensions := map[Field]string{
		FieldCalendar:          tag.TypeForKey("ca"),
		FieldNumberingSystem:   tag.TypeForKey("nu"),
		FieldHourCycle:         tag.TypeForKey("hc"),
		FieldFirstDay:          tag.TypeForKey("fw"),
		FieldMeasurementSystem: tag.TypeForKey("ms"),
		FieldTimeZone:          tag.TypeForKey("tz"),
		FieldDisplayCurrency:   strings.ToUpper(tag.TypeForKey("cu")),
	}
	for field, value := range extensions {
		if value == "" || !policy.Fields[field].AllowUnicodeExtension {
			continue
		}
		if field == FieldTimeZone {
			if value == "usnyc" {
				value = "America/New_York"
			}
		}
		*out = appendCandidate(*out, obs, field, value, score, "unicode_extension")
	}
	if rg := tag.TypeForKey("rg"); len(rg) >= 2 && policy.Fields[FieldFormattingRegion].AllowUnicodeExtension {
		*out = appendCandidate(*out, obs, FieldFormattingRegion, strings.ToUpper(rg[:2]), score-40, "unicode_region_override")
	}
}

func appendCandidate(out []resolverCandidate, obs Observation, field Field, value string, score int, reason string) []resolverCandidate {
	value = strings.TrimSpace(value)
	if value == "" || !observationAllows(obs, field) {
		return out
	}
	return append(out, resolverCandidate{
		field: field, value: value, score: score, source: normalizeSourceClass(obs.SourceClass), trust: obs.Trust,
		sensitivity: obs.Sensitivity, reason: reason, explicit: obs.Explicit,
	})
}

func observationAllows(obs Observation, field Field) bool {
	if len(obs.AllowedFields) == 0 {
		return true
	}
	for _, allowed := range obs.AllowedFields {
		if allowed == field {
			return true
		}
	}
	return false
}

func scoreObservation(obs Observation, policy ResolverPolicy) int {
	score := policy.SourceWeights[normalizeSourceClass(obs.SourceClass)] + policy.TrustWeights[obs.Trust]
	if obs.Weight > 0 && obs.Weight < 1000 {
		score = score * obs.Weight / 1000
	}
	if obs.Explicit {
		score += 500
	}
	if obs.Age > 0 {
		days := int(obs.Age.Hours() / 24)
		if days > 365 {
			days = 365
		}
		score -= days
	}
	if score < 0 {
		return 0
	}
	return score
}

func selectCandidates(cands []resolverCandidate, policy ResolverPolicy) map[Field]resolverCandidate {
	selected := map[Field]resolverCandidate{}
	for _, cand := range cands {
		fp := policy.Fields[cand.field]
		if !fp.Enabled {
			continue
		}
		if !candidateInferenceAllowed(cand, fp) {
			continue
		}
		if !fp.ApplyUntrusted && cand.trust < TrustHigh {
			continue
		}
		if confidenceForScore(cand.score).less(fp.MinimumConfidence) {
			continue
		}
		if cur, ok := selected[cand.field]; !ok || betterCandidate(cand, cur) {
			selected[cand.field] = cand
		}
	}
	return selected
}

func betterCandidate(a, b resolverCandidate) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.trust != b.trust {
		return a.trust > b.trust
	}
	if sourcePriority(a.source) != sourcePriority(b.source) {
		return sourcePriority(a.source) > sourcePriority(b.source)
	}
	if a.explicit != b.explicit {
		return a.explicit
	}
	return a.value < b.value
}

func (r *Resolver) profileFromSelected(selected map[Field]resolverCandidate, policy ResolverPolicy) (Profile, []ResolveDiagnostic, error) {
	langs := []string{"en"}
	if cand, ok := selected[FieldLanguage]; ok {
		langs = []string{cand.value}
	}
	opts := []ProfileOption{}
	defaulted := map[Field]bool{}
	explicit := map[Field]bool{}
	diagnostics := []ResolveDiagnostic{}
	addExplicit := func(field Field) {
		if selected[field].explicit || selected[field].trust >= TrustHigh {
			explicit[field] = true
		}
	}
	if cand, ok := selected[FieldTimeZone]; ok {
		opts = append(opts, WithTimeZoneName(cand.value))
		addExplicit(FieldTimeZone)
	}
	if cand, ok := selected[FieldDisplayCurrency]; ok {
		opts = append(opts, WithCurrency(Currency(cand.value)))
		addExplicit(FieldDisplayCurrency)
	}
	if cand, ok := selected[FieldNumberingSystem]; ok {
		opts = append(opts, WithNumberingSystem(cand.value))
		addExplicit(FieldNumberingSystem)
	}
	if cand, ok := selected[FieldCalendar]; ok {
		opts = append(opts, WithCalendar(cand.value))
		addExplicit(FieldCalendar)
	}
	if cand, ok := selected[FieldHourCycle]; ok {
		opts = append(opts, WithHourCycle(cand.value))
		addExplicit(FieldHourCycle)
	}
	if cand, ok := selected[FieldMeasurementSystem]; ok {
		opts = append(opts, WithMeasurementSystem(cand.value))
		addExplicit(FieldMeasurementSystem)
	}
	for field, opt := range map[Field]func(string) ProfileOption{
		FieldFormattingRegion: WithFormattingRegion,
		FieldCurrentRegion:    WithCurrentRegion,
		FieldMarketRegion:     WithMarketRegion,
		FieldResidenceRegion:  WithResidenceRegion,
	} {
		if cand, ok := selected[field]; ok {
			opts = append(opts, opt(cand.value))
			addExplicit(field)
		}
	}
	if cand, ok := selected[FieldFirstDay]; ok {
		opts = append(opts, WithFirstDay(cand.value))
		addExplicit(FieldFirstDay)
	}
	p, err := NewProfile(langs, opts...)
	if err != nil {
		return Profile{}, nil, err
	}
	if p.NumberingSystem() == "" {
		defs := r.defaults.LocaleDefaults(p.PrimaryLanguage())
		if fieldAllowsDefault(policy, FieldNumberingSystem, false) {
			p.numberingSystem = defs.NumberingSystem
			defaulted[FieldNumberingSystem] = true
		}
		if fieldAllowsDefault(policy, FieldCalendar, false) {
			p.calendar = defs.Calendar
			defaulted[FieldCalendar] = true
		}
	}
	if p.FormattingRegion() == "" {
		if region, conf := p.PrimaryLanguage().Region(); conf >= language.Exact && fieldAllowsDefault(policy, FieldFormattingRegion, true) {
			p.formattingRegion = region.String()
			defaulted[FieldFormattingRegion] = true
		}
	}
	if p.FormattingRegion() != "" {
		if defs, ok := r.defaults.DefaultsForRegion(p.FormattingRegion()); ok {
			if p.Currency() == "" && fieldAllowsDefault(policy, FieldDisplayCurrency, true) {
				p.currency = defs.DisplayCurrency
				defaulted[FieldDisplayCurrency] = true
			}
			if p.MeasurementSystem() == "" && fieldAllowsDefault(policy, FieldMeasurementSystem, true) {
				p.measurementSystem = defs.MeasurementSystem
				defaulted[FieldMeasurementSystem] = true
			}
			if p.FirstDay() == "" && fieldAllowsDefault(policy, FieldFirstDay, true) {
				p.firstDay = defs.FirstDay
				defaulted[FieldFirstDay] = true
			}
		}
	}
	p.explicitFields = explicit
	p.defaultedFields = defaulted
	for field, cand := range selected {
		diagnostics = append(diagnostics, ResolveDiagnostic{
			Code: "selected_candidate", Field: field, Value: cand.value, SourceClass: cand.source,
			Confidence: confidenceForScore(cand.score), Reason: cand.reason, Severity: "info",
		})
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Field != diagnostics[j].Field {
			return diagnostics[i].Field < diagnostics[j].Field
		}
		return diagnostics[i].Value < diagnostics[j].Value
	})
	return p, diagnostics, nil
}

func normalizePolicy(p ResolverPolicy) ResolverPolicy {
	def := HelpfulResolverPolicy()
	if p.Fields == nil {
		p.Fields = cloneFieldPolicies(def.Fields)
	} else {
		p.Fields = cloneFieldPolicies(p.Fields)
	}
	if p.SourceWeights == nil {
		p.SourceWeights = cloneSourceWeights(def.SourceWeights)
	} else {
		p.SourceWeights = cloneSourceWeights(p.SourceWeights)
	}
	if p.TrustWeights == nil {
		p.TrustWeights = cloneTrustWeights(def.TrustWeights)
	} else {
		p.TrustWeights = cloneTrustWeights(p.TrustWeights)
	}
	if p.MaxCandidatesPerField <= 0 {
		p.MaxCandidatesPerField = def.MaxCandidatesPerField
	}
	for _, field := range allFields() {
		fp, ok := p.Fields[field]
		if !ok {
			p.Fields[field] = def.Fields[field]
			continue
		}
		if fp.MinimumConfidence == "" {
			fp.MinimumConfidence = def.Fields[field].MinimumConfidence
			p.Fields[field] = fp
		}
	}
	return p
}

func allFields() []Field {
	return []Field{FieldLanguage, FieldTimeZone, FieldFormattingRegion, FieldCurrentRegion, FieldMarketRegion, FieldResidenceRegion, FieldDisplayCurrency, FieldCalendar, FieldNumberingSystem, FieldHourCycle, FieldMeasurementSystem, FieldFirstDay}
}

func fieldForRegionUse(use RegionUse) Field {
	switch use {
	case RegionCurrent:
		return FieldCurrentRegion
	case RegionMarket:
		return FieldMarketRegion
	case RegionResidence:
		return FieldResidenceRegion
	default:
		return FieldFormattingRegion
	}
}

func confidenceForScore(score int) Confidence {
	switch {
	case score >= 900:
		return ConfidenceHigh
	case score >= 600:
		return ConfidenceMedium
	case score > 0:
		return ConfidenceLow
	default:
		return ConfidenceNone
	}
}

func (c Confidence) less(other Confidence) bool {
	return confidenceRank(c) < confidenceRank(other)
}

func confidenceRank(c Confidence) int {
	switch c {
	case ConfidenceHigh:
		return 3
	case ConfidenceMedium:
		return 2
	case ConfidenceLow:
		return 1
	default:
		return 0
	}
}

func sourcePriority(source SourceClass) int {
	switch normalizeSourceClass(source) {
	case SourceExplicitUser:
		return 100
	case SourceExplicitApp:
		return 95
	case SourceAccountDefault:
		return 80
	case SourceJobDefault:
		return 75
	case SourceAppPolicy:
		return 70
	case SourceEnvironment:
		return 60
	case SourceClientLocale, SourceClientTimeZone:
		return 50
	case SourceGeoIP:
		return 40
	default:
		return 10
	}
}

func normalizeSourceClass(source SourceClass) SourceClass {
	switch source {
	case SourceExplicitUser, SourceExplicitApp, SourceAccountDefault, SourceJobDefault, SourceClientLocale,
		SourceClientTimeZone, SourceEnvironment, SourceGeoIP, SourceAppPolicy, SourceLibraryDefault:
		return source
	default:
		return SourceLibraryDefault
	}
}

func dedupeResolverCandidates(in []resolverCandidate) []resolverCandidate {
	best := map[string]resolverCandidate{}
	for _, cand := range in {
		key := string(cand.field) + "\x00" + cand.value + "\x00" + string(cand.source)
		if cur, ok := best[key]; !ok || betterCandidate(cand, cur) {
			best[key] = cand
		}
	}
	out := make([]resolverCandidate, 0, len(best))
	for _, cand := range best {
		out = append(out, cand)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		if a.field != b.field {
			return a.field < b.field
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if a.trust != b.trust {
			return a.trust > b.trust
		}
		if sourcePriority(a.source) != sourcePriority(b.source) {
			return sourcePriority(a.source) > sourcePriority(b.source)
		}
		return a.value < b.value
	})
	return out
}

func filterResolverCandidates(in []resolverCandidate, defaults DefaultsProvider) []resolverCandidate {
	out := in[:0]
	for _, cand := range in {
		if validResolverCandidate(cand, defaults) {
			out = append(out, cand)
		}
	}
	return out
}

func validResolverCandidate(cand resolverCandidate, defaults DefaultsProvider) bool {
	switch cand.field {
	case FieldTimeZone:
		if err := validateTimeZoneName(cand.value); err != nil {
			return false
		}
		_, err := time.LoadLocation(cand.value)
		return err == nil
	case FieldDisplayCurrency:
		return Currency(cand.value).Valid() && Currency(cand.value) != ""
	case FieldFormattingRegion, FieldCurrentRegion, FieldMarketRegion, FieldResidenceRegion:
		region := normalizeRegion(cand.value)
		return len(region) == 2 || len(region) == 3
	case FieldCalendar:
		return validTypeIdentifier(cand.value) && (defaults == nil || defaults.KnownExtension("ca", cand.value))
	case FieldNumberingSystem:
		return validTypeIdentifier(cand.value) && (defaults == nil || defaults.KnownExtension("nu", cand.value))
	case FieldHourCycle:
		return validHourCycle(strings.ToLower(cand.value))
	case FieldMeasurementSystem:
		return validMeasurementSystem(strings.ToLower(cand.value))
	case FieldFirstDay:
		return validWeekday(strings.ToLower(cand.value))
	default:
		return true
	}
}

func limitResolverCandidates(in []resolverCandidate, maxPerField int) []resolverCandidate {
	if maxPerField <= 0 {
		return in
	}
	counts := map[Field]int{}
	out := in[:0]
	for _, cand := range in {
		if counts[cand.field] >= maxPerField {
			continue
		}
		counts[cand.field]++
		out = append(out, cand)
	}
	return out
}

func candidateInferenceAllowed(cand resolverCandidate, fp FieldPolicy) bool {
	if strings.HasPrefix(cand.reason, "observed_") || cand.reason == "unicode_extension" || cand.reason == "unicode_region_override" {
		return true
	}
	switch cand.field {
	case FieldLanguage:
		return fp.AllowLanguageInfer
	case FieldTimeZone:
		return fp.AllowTimeZoneInfer
	case FieldDisplayCurrency:
		return fp.AllowCurrencyInfer
	case FieldFormattingRegion, FieldCurrentRegion, FieldMarketRegion, FieldResidenceRegion,
		FieldMeasurementSystem, FieldFirstDay:
		return fp.AllowRegionInfer
	default:
		return true
	}
}

func fieldAllowsDefault(policy ResolverPolicy, field Field, regionDerived bool) bool {
	fp := policy.Fields[field]
	if !fp.Enabled {
		return false
	}
	if regionDerived {
		switch field {
		case FieldDisplayCurrency:
			return fp.AllowCurrencyInfer
		case FieldTimeZone:
			return fp.AllowTimeZoneInfer
		default:
			return fp.AllowRegionInfer
		}
	}
	return true
}

func compareCandidateResult(a, b CandidateResult) bool {
	if a.Field != b.Field {
		return a.Field < b.Field
	}
	if a.Selected != b.Selected {
		return a.Selected
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Trust != b.Trust {
		return a.Trust > b.Trust
	}
	return a.Value < b.Value
}

func cloneFieldPolicies(in map[Field]FieldPolicy) map[Field]FieldPolicy {
	out := make(map[Field]FieldPolicy, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSourceWeights(in map[SourceClass]int) map[SourceClass]int {
	out := make(map[SourceClass]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTrustWeights(in map[Trust]int) map[Trust]int {
	out := make(map[Trust]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func maxSensitivity(observations []Observation) Sensitivity {
	out := SensitivityPublic
	for _, obs := range observations {
		if obs.Sensitivity == SensitivityUserPrivate {
			return SensitivityUserPrivate
		}
		if obs.Sensitivity == SensitivityRequest {
			out = SensitivityRequest
		}
	}
	return out
}

func regionForTimeZone(tz string) string {
	switch tz {
	case "Asia/Tokyo":
		return "JP"
	case "Europe/Berlin":
		return "DE"
	case "Europe/Zurich":
		return "CH"
	case "Europe/London":
		return "GB"
	case "America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles":
		return "US"
	default:
		return ""
	}
}
