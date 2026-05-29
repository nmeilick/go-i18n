package locale

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/language"
)

const (
	defaultLocale       = "en"
	defaultCalendar     = "gregory"
	maxAcceptLangBytes  = 4096
	maxAcceptLangRanges = 32
	maxTimeZoneNameLen  = 128
)

// CurrencyCode is an ISO 4217 currency code. The zero value means "unset".
type CurrencyCode string

// Currency normalizes a currency code to upper-case ISO 4217 form.
func Currency(code string) CurrencyCode {
	return CurrencyCode(strings.ToUpper(strings.TrimSpace(code)))
}

// String returns the normalized currency code.
func (c CurrencyCode) String() string {
	return string(c)
}

// Valid reports whether c is empty or a syntactically valid ISO 4217 code.
func (c CurrencyCode) Valid() bool {
	if c == "" {
		return true
	}
	s := string(c)
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// Parse parses a BCP-47 tag.
func Parse(tag string) (language.Tag, error) {
	t, err := language.Parse(strings.TrimSpace(tag))
	if err != nil {
		return language.Und, fmt.Errorf("parse locale %q: %w", tag, err)
	}
	return t, nil
}

// Normalize parses tag and returns its canonical string representation.
func Normalize(tag string) (string, error) {
	t, err := Parse(tag)
	if err != nil {
		return "", err
	}
	return t.String(), nil
}

// Candidate is one locale preference from a specific source.
type Candidate struct {
	Tag      string
	Source   string
	Weight   float64
	Trusted  bool
	Vary     []string
	Private  bool
	Explicit bool
}

// Preference creates a locale candidate from application code.
func Preference(tag string) Candidate {
	return Candidate{Tag: tag, Weight: 1, Explicit: true}
}

// RejectedCandidate records a malformed or unsupported candidate.
type RejectedCandidate struct {
	Tag    string
	Source string
	Reason string
}

// Resolved is the result of locale negotiation.
type Resolved struct {
	Tag           language.Tag
	Locale        string
	Source        string
	Confidence    language.Confidence
	Explicit      bool
	Trusted       bool
	Private       bool
	Vary          []string
	Requested     []string
	FallbackChain []string
	Rejected      []RejectedCandidate
}

// MatchMode controls how supported locales are selected.
type MatchMode int

const (
	// MatchBest uses x/text best matching. This is convenient for applications
	// where a close regional variant is preferable to the default locale.
	MatchBest MatchMode = iota
	// MatchStrict uses exact tag and parent fallback only.
	MatchStrict
)

// Negotiator resolves locale candidates against supported locales.
type Negotiator struct {
	defaultTag language.Tag
	supported  []language.Tag
	matcher    language.Matcher
	supportedM map[string]language.Tag
	aliases    map[string]language.Tag
	mode       MatchMode
	min        language.Confidence
}

// Option configures a Negotiator.
type Option func(*Negotiator) error

// Alias maps one requested locale to a supported or canonical locale.
func Alias(from, to string) Option {
	return func(n *Negotiator) error {
		f, err := Parse(from)
		if err != nil {
			return err
		}
		t, err := Parse(to)
		if err != nil {
			return err
		}
		n.aliases[f.String()] = t
		return nil
	}
}

// Mode sets the matching mode.
func Mode(mode MatchMode) Option {
	return func(n *Negotiator) error {
		n.mode = mode
		return nil
	}
}

// MinConfidence sets the minimum accepted best-match confidence.
func MinConfidence(conf language.Confidence) Option {
	return func(n *Negotiator) error {
		n.min = conf
		return nil
	}
}

// NewNegotiator creates a locale negotiator.
func NewNegotiator(defaultLocale string, supported []string, opts ...Option) (*Negotiator, error) {
	if defaultLocale == "" {
		defaultLocale = defaultLocaleConst()
	}
	def, err := Parse(defaultLocale)
	if err != nil {
		return nil, err
	}
	if len(supported) == 0 {
		supported = []string{def.String()}
	}
	n := &Negotiator{
		defaultTag: def,
		aliases:    make(map[string]language.Tag),
		mode:       MatchBest,
		min:        language.Low,
		supportedM: make(map[string]language.Tag),
	}
	for _, raw := range supported {
		t, err := Parse(raw)
		if err != nil {
			return nil, err
		}
		key := t.String()
		if _, ok := n.supportedM[key]; ok {
			continue
		}
		n.supported = append(n.supported, t)
		n.supportedM[key] = t
	}
	if _, ok := n.supportedM[def.String()]; !ok {
		n.supported = append([]language.Tag{def}, n.supported...)
		n.supportedM[def.String()] = def
	}
	for _, opt := range opts {
		if err := opt(n); err != nil {
			return nil, err
		}
	}
	n.matcher = language.NewMatcher(n.supported)
	return n, nil
}

func defaultLocaleConst() string { return defaultLocale }

// Supported returns a copy of supported locale tags.
func (n *Negotiator) Supported() []language.Tag {
	out := make([]language.Tag, len(n.supported))
	copy(out, n.supported)
	return out
}

// Default returns the default locale tag.
func (n *Negotiator) Default() language.Tag {
	return n.defaultTag
}

// Resolve resolves candidates in order, using each candidate's weight only for
// candidates generated from the same source such as Accept-Language.
func (n *Negotiator) Resolve(candidates ...Candidate) Resolved {
	res := Resolved{
		Tag:           n.defaultTag,
		Locale:        n.defaultTag.String(),
		Confidence:    language.No,
		FallbackChain: []string{n.defaultTag.String()},
	}
	for _, cand := range candidates {
		if cand.Weight == 0 {
			continue
		}
		raw := strings.TrimSpace(cand.Tag)
		if raw == "" {
			continue
		}
		res.Requested = append(res.Requested, raw)
		tag, err := Parse(raw)
		if err != nil {
			res.Rejected = append(res.Rejected, RejectedCandidate{Tag: raw, Source: cand.Source, Reason: "invalid"})
			continue
		}
		if alias, ok := n.aliases[tag.String()]; ok {
			tag = alias
		}
		matched, conf, chain, ok := n.match(tag)
		if !ok {
			res.Rejected = append(res.Rejected, RejectedCandidate{Tag: tag.String(), Source: cand.Source, Reason: "unsupported"})
			continue
		}
		return Resolved{
			Tag:           matched,
			Locale:        matched.String(),
			Source:        cand.Source,
			Confidence:    conf,
			Explicit:      cand.Explicit,
			Trusted:       cand.Trusted,
			Private:       cand.Private,
			Vary:          dedupeHeaderNames(cand.Vary),
			Requested:     append([]string(nil), res.Requested...),
			FallbackChain: chain,
			Rejected:      append([]RejectedCandidate(nil), res.Rejected...),
		}
	}
	return res
}

func (n *Negotiator) match(tag language.Tag) (language.Tag, language.Confidence, []string, bool) {
	if n.mode == MatchStrict {
		chain := []string{tag.String()}
		for cur := tag; cur != language.Und; cur = cur.Parent() {
			if supported, ok := n.supportedM[cur.String()]; ok {
				return supported, language.Exact, appendLocaleChain(chain, supported.String()), true
			}
			if cur.Parent() == cur {
				break
			}
			chain = appendLocaleChain(chain, cur.Parent().String())
		}
		chain = appendLocaleChain(chain, n.defaultTag.String())
		return n.defaultTag, language.No, chain, false
	}
	_, index, conf := n.matcher.Match(tag)
	if conf < n.min {
		return n.defaultTag, conf, appendLocaleChain([]string{tag.String()}, n.defaultTag.String()), false
	}
	matched := n.supported[index]
	return matched, conf, appendLocaleChain([]string{tag.String()}, matched.String()), true
}

func appendLocaleChain(chain []string, tags ...string) []string {
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if len(chain) == 0 || chain[len(chain)-1] != tag {
			chain = append(chain, tag)
		}
	}
	return chain
}

// AcceptLanguage parses an Accept-Language header into weighted candidates.
func AcceptLanguage(header string) []Candidate {
	if len(header) > maxAcceptLangBytes {
		header = header[:maxAcceptLangBytes]
	}
	tags, qs, err := language.ParseAcceptLanguage(header)
	if err != nil {
		return nil
	}
	limit := len(tags)
	if limit > maxAcceptLangRanges {
		limit = maxAcceptLangRanges
	}
	out := make([]Candidate, 0, limit)
	for i := 0; i < limit; i++ {
		if qs[i] <= 0 {
			continue
		}
		out = append(out, Candidate{
			Tag:      tags[i].String(),
			Source:   "accept-language",
			Weight:   float64(qs[i]),
			Vary:     []string{"Accept-Language"},
			Explicit: true,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	return out
}

func dedupeHeaderNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]string, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if _, ok := seen[key]; !ok {
			seen[key] = canonicalHeaderName(v)
		}
	}
	out := make([]string, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func canonicalHeaderName(s string) string {
	parts := strings.Split(strings.ToLower(s), "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "-")
}

// Profile is an immutable snapshot of localization preferences. It contains
// only formatting facts and must not be used as a user/account record.
type Profile struct {
	languages         []language.Tag
	timeZone          *time.Location
	currency          CurrencyCode
	numberingSystem   string
	calendar          string
	hourCycle         string
	measurementSystem string
	formattingRegion  string
	currentRegion     string
	marketRegion      string
	residenceRegion   string
	firstDay          string
	defaults          FormatDefaults
	explicitFields    map[Field]bool
	defaultedFields   map[Field]bool
}

// FormatDefaults carries optional formatting defaults.
type FormatDefaults struct {
	DateStyle          string
	TimeStyle          string
	CurrencyDisplay    CurrencyDisplayMode
	CurrencyAccounting bool
}

// ProfileOption configures a profile.
type ProfileOption func(*Profile) error

// NewProfile creates an immutable profile from string language preferences.
func NewProfile(languages []string, opts ...ProfileOption) (Profile, error) {
	tags := make([]language.Tag, 0, len(languages))
	for _, raw := range languages {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		tag, err := Parse(raw)
		if err != nil {
			return Profile{}, err
		}
		tags = append(tags, tag)
	}
	return NewProfileTags(tags, opts...)
}

// NewProfileTags creates an immutable profile from typed language preferences.
func NewProfileTags(languages []language.Tag, opts ...ProfileOption) (Profile, error) {
	if len(languages) == 0 {
		languages = []language.Tag{language.English}
	}
	p := Profile{
		languages: append([]language.Tag(nil), languages...),
		timeZone:  time.UTC,
		calendar:  defaultCalendar,
		defaults: FormatDefaults{
			DateStyle:       "medium",
			TimeStyle:       "medium",
			CurrencyDisplay: CurrencyDisplaySymbol,
		},
	}
	for _, opt := range opts {
		if err := opt(&p); err != nil {
			return Profile{}, err
		}
	}
	if !p.currency.Valid() {
		return Profile{}, fmt.Errorf("invalid currency code %q", p.currency)
	}
	p.explicitFields = cloneFieldSet(p.explicitFields)
	p.defaultedFields = cloneFieldSet(p.defaultedFields)
	return p, nil
}

// WithTimeZone sets the profile timezone.
func WithTimeZone(loc *time.Location) ProfileOption {
	return func(p *Profile) error {
		if loc == nil {
			return errors.New("timezone must not be nil")
		}
		p.timeZone = loc
		return nil
	}
}

// WithTimeZoneName loads and sets an IANA timezone name.
func WithTimeZoneName(name string) ProfileOption {
	return func(p *Profile) error {
		if err := validateTimeZoneName(name); err != nil {
			return err
		}
		loc, err := time.LoadLocation(name)
		if err != nil {
			return fmt.Errorf("load timezone %q: %w", name, err)
		}
		p.timeZone = loc
		return nil
	}
}

// WithCurrency sets the profile default currency.
func WithCurrency(code CurrencyCode) ProfileOption {
	return func(p *Profile) error {
		code = Currency(code.String())
		if !code.Valid() {
			return fmt.Errorf("invalid currency code %q", code)
		}
		p.currency = code
		return nil
	}
}

// WithNumberingSystem sets the preferred numbering system identifier.
func WithNumberingSystem(system string) ProfileOption {
	return func(p *Profile) error {
		system = strings.TrimSpace(system)
		if system != "" && !validTypeIdentifier(system) {
			return fmt.Errorf("invalid numbering system %q", system)
		}
		p.numberingSystem = system
		return nil
	}
}

// WithCalendar sets the preferred calendar identifier.
func WithCalendar(calendar string) ProfileOption {
	return func(p *Profile) error {
		calendar = strings.TrimSpace(calendar)
		if calendar == "" {
			calendar = defaultCalendar
		}
		if !validTypeIdentifier(calendar) {
			return fmt.Errorf("invalid calendar %q", calendar)
		}
		p.calendar = calendar
		return nil
	}
}

// WithHourCycle sets the preferred Unicode hour cycle identifier.
func WithHourCycle(hourCycle string) ProfileOption {
	return func(p *Profile) error {
		hourCycle = strings.ToLower(strings.TrimSpace(hourCycle))
		if hourCycle != "" && !validHourCycle(hourCycle) {
			return fmt.Errorf("invalid hour cycle %q", hourCycle)
		}
		p.hourCycle = hourCycle
		return nil
	}
}

// WithMeasurementSystem sets the preferred measurement system identifier.
func WithMeasurementSystem(system string) ProfileOption {
	return func(p *Profile) error {
		system = strings.ToLower(strings.TrimSpace(system))
		if system != "" && !validMeasurementSystem(system) {
			return fmt.Errorf("invalid measurement system %q", system)
		}
		p.measurementSystem = system
		return nil
	}
}

// WithFormattingRegion sets the territory used for formatting defaults.
func WithFormattingRegion(region string) ProfileOption {
	return func(p *Profile) error {
		p.formattingRegion = normalizeRegion(region)
		return nil
	}
}

// WithCurrentRegion sets the user's current territory signal.
func WithCurrentRegion(region string) ProfileOption {
	return func(p *Profile) error {
		p.currentRegion = normalizeRegion(region)
		return nil
	}
}

// WithMarketRegion sets a market/commerce territory signal.
func WithMarketRegion(region string) ProfileOption {
	return func(p *Profile) error {
		p.marketRegion = normalizeRegion(region)
		return nil
	}
}

// WithResidenceRegion sets a residence territory signal. It is a formatting
// preference only and is not legal residency truth.
func WithResidenceRegion(region string) ProfileOption {
	return func(p *Profile) error {
		p.residenceRegion = normalizeRegion(region)
		return nil
	}
}

// WithFirstDay sets the preferred first day of week.
func WithFirstDay(day string) ProfileOption {
	return func(p *Profile) error {
		day = strings.ToLower(strings.TrimSpace(day))
		if day != "" && !validWeekday(day) {
			return fmt.Errorf("invalid first day %q", day)
		}
		p.firstDay = day
		return nil
	}
}

// WithFormatDefaults sets formatting defaults.
func WithFormatDefaults(defaults FormatDefaults) ProfileOption {
	return func(p *Profile) error {
		p.defaults = defaults
		return nil
	}
}

// Languages returns a defensive copy of language preferences.
func (p Profile) Languages() []language.Tag {
	out := make([]language.Tag, len(p.languages))
	copy(out, p.languages)
	return out
}

// PrimaryLanguage returns the first language preference.
func (p Profile) PrimaryLanguage() language.Tag {
	if len(p.languages) == 0 {
		return language.English
	}
	return p.languages[0]
}

// TimeZone returns the profile timezone.
func (p Profile) TimeZone() *time.Location {
	if p.timeZone == nil {
		return time.UTC
	}
	return p.timeZone
}

// Currency returns the profile default currency.
func (p Profile) Currency() CurrencyCode { return p.currency }

// NumberingSystem returns the preferred numbering system.
func (p Profile) NumberingSystem() string { return p.numberingSystem }

// Calendar returns the preferred calendar.
func (p Profile) Calendar() string {
	if p.calendar == "" {
		return defaultCalendar
	}
	return p.calendar
}

// MeasurementSystem returns the preferred measurement system.
func (p Profile) MeasurementSystem() string { return p.measurementSystem }

// HourCycle returns the preferred hour cycle identifier.
func (p Profile) HourCycle() string { return p.hourCycle }

// FormattingRegion returns the region used for formatting defaults.
func (p Profile) FormattingRegion() string { return p.formattingRegion }

// CurrentRegion returns the user's current region signal.
func (p Profile) CurrentRegion() string { return p.currentRegion }

// MarketRegion returns the market region signal.
func (p Profile) MarketRegion() string { return p.marketRegion }

// ResidenceRegion returns the residence region signal.
func (p Profile) ResidenceRegion() string { return p.residenceRegion }

// FirstDay returns the preferred first day of week.
func (p Profile) FirstDay() string { return p.firstDay }

// Explicit reports whether a field came from an explicit/trusted observation.
func (p Profile) Explicit(field Field) bool { return p.explicitFields[field] }

// Defaulted reports whether a field was supplied by library defaults.
func (p Profile) Defaulted(field Field) bool { return p.defaultedFields[field] }

// Defaults returns formatting defaults.
func (p Profile) Defaults() FormatDefaults { return p.defaults }

// ProfileSnapshot is a serializable profile representation for jobs and APIs.
type ProfileSnapshot struct {
	Languages         []string       `json:"languages"`
	TimeZone          string         `json:"time_zone"`
	Currency          string         `json:"currency,omitempty"`
	NumberingSystem   string         `json:"numbering_system,omitempty"`
	Calendar          string         `json:"calendar,omitempty"`
	HourCycle         string         `json:"hour_cycle,omitempty"`
	MeasurementSystem string         `json:"measurement_system,omitempty"`
	FormattingRegion  string         `json:"formatting_region,omitempty"`
	CurrentRegion     string         `json:"current_region,omitempty"`
	MarketRegion      string         `json:"market_region,omitempty"`
	ResidenceRegion   string         `json:"residence_region,omitempty"`
	FirstDay          string         `json:"first_day,omitempty"`
	Defaults          FormatDefaults `json:"defaults"`
}

// Snapshot serializes the profile without identity or raw request data.
func (p Profile) Snapshot() ProfileSnapshot {
	langs := make([]string, len(p.languages))
	for i, tag := range p.languages {
		langs[i] = tag.String()
	}
	return ProfileSnapshot{
		Languages:         langs,
		TimeZone:          p.TimeZone().String(),
		Currency:          p.currency.String(),
		NumberingSystem:   p.numberingSystem,
		Calendar:          p.Calendar(),
		HourCycle:         p.hourCycle,
		MeasurementSystem: p.measurementSystem,
		FormattingRegion:  p.formattingRegion,
		CurrentRegion:     p.currentRegion,
		MarketRegion:      p.marketRegion,
		ResidenceRegion:   p.residenceRegion,
		FirstDay:          p.firstDay,
		Defaults:          p.defaults,
	}
}

// ProfileFromSnapshot reconstructs a Profile from a snapshot.
func ProfileFromSnapshot(s ProfileSnapshot) (Profile, error) {
	opts := []ProfileOption{}
	if s.TimeZone != "" {
		opts = append(opts, WithTimeZoneName(s.TimeZone))
	}
	if s.Currency != "" {
		opts = append(opts, WithCurrency(Currency(s.Currency)))
	}
	if s.NumberingSystem != "" {
		opts = append(opts, WithNumberingSystem(s.NumberingSystem))
	}
	if s.Calendar != "" {
		opts = append(opts, WithCalendar(s.Calendar))
	}
	if s.HourCycle != "" {
		opts = append(opts, WithHourCycle(s.HourCycle))
	}
	if s.MeasurementSystem != "" {
		opts = append(opts, WithMeasurementSystem(s.MeasurementSystem))
	}
	if s.FormattingRegion != "" {
		opts = append(opts, WithFormattingRegion(s.FormattingRegion))
	}
	if s.CurrentRegion != "" {
		opts = append(opts, WithCurrentRegion(s.CurrentRegion))
	}
	if s.MarketRegion != "" {
		opts = append(opts, WithMarketRegion(s.MarketRegion))
	}
	if s.ResidenceRegion != "" {
		opts = append(opts, WithResidenceRegion(s.ResidenceRegion))
	}
	if s.FirstDay != "" {
		opts = append(opts, WithFirstDay(s.FirstDay))
	}
	opts = append(opts, WithFormatDefaults(s.Defaults))
	return NewProfile(s.Languages, opts...)
}

// RedactedProfile is safe to log.
type RedactedProfile struct {
	Locale          string `json:"locale"`
	TimeZone        string `json:"time_zone"`
	CurrencyPresent bool   `json:"currency_present"`
}

// Redacted returns a log-safe profile summary.
func (p Profile) Redacted() RedactedProfile {
	return RedactedProfile{
		Locale:          p.PrimaryLanguage().String(),
		TimeZone:        p.TimeZone().String(),
		CurrencyPresent: p.currency != "",
	}
}

func normalizeRegion(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return ""
	}
	return strings.ToUpper(region)
}

func validateTimeZoneName(name string) error {
	raw := name
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("timezone is required")
	}
	if raw != name {
		return fmt.Errorf("invalid timezone %q", raw)
	}
	if len(name) > maxTimeZoneNameLen {
		return fmt.Errorf("timezone %q is too long", name)
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid timezone %q", name)
	}
	for _, r := range name {
		if r <= 0x20 || r == 0x7f {
			return fmt.Errorf("invalid timezone %q", name)
		}
	}
	return nil
}

func validTypeIdentifier(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validHourCycle(value string) bool {
	switch value {
	case "h11", "h12", "h23", "h24":
		return true
	default:
		return false
	}
}

func validMeasurementSystem(value string) bool {
	switch value {
	case "metric", "ussystem", "uksystem":
		return true
	default:
		return false
	}
}

func validWeekday(value string) bool {
	switch value {
	case "mon", "tue", "wed", "thu", "fri", "sat", "sun":
		return true
	default:
		return false
	}
}

func cloneFieldSet(in map[Field]bool) map[Field]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[Field]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
