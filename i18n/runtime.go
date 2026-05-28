package i18n

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"
	"time"

	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/observe"
	"golang.org/x/text/language"
)

// Runtime is the concurrency-safe holder for the current catalog snapshot.
type Runtime struct {
	current       atomic.Pointer[Catalog]
	sink          DiagnosticSink
	observer      observe.Observer
	observeAttrs  []observe.Attr
	observePolicy ObservePolicy
	formatter     locale.Formatter
	formatPolicy  locale.FormatPolicy
}

// RuntimeOption configures a Runtime.
type RuntimeOption func(*Runtime)

// WithDiagnostics configures the runtime diagnostic sink.
func WithDiagnostics(sink DiagnosticSink) RuntimeOption {
	return func(r *Runtime) {
		if sink != nil {
			r.sink = sink
		}
	}
}

// WithObserver configures the runtime observer for structured events.
func WithObserver(observer observe.Observer) RuntimeOption {
	return func(r *Runtime) {
		if observer != nil {
			r.observer = observer
		}
	}
}

// WithObservePolicy configures runtime event emission and bounds.
func WithObservePolicy(policy ObservePolicy) RuntimeOption {
	return func(r *Runtime) {
		r.observePolicy = policy
	}
}

// WithObserveAttrs adds runtime-level observability attributes.
func WithObserveAttrs(attrs ...observe.Attr) RuntimeOption {
	return func(r *Runtime) {
		r.observeAttrs = append(r.observeAttrs, attrs...)
	}
}

// WithFormatter configures the runtime value formatter.
func WithFormatter(formatter locale.Formatter) RuntimeOption {
	return func(r *Runtime) {
		if formatter != nil {
			r.formatter = formatter
		}
	}
}

// WithFormatPolicy configures formatter strictness and fallback behavior.
func WithFormatPolicy(policy locale.FormatPolicy) RuntimeOption {
	return func(r *Runtime) {
		r.formatPolicy = policy
	}
}

// SwapReport describes a runtime catalog swap.
type SwapReport struct {
	OldVersion string
	NewVersion string
	SwappedAt  time.Time
	Locales    []string
}

// NewRuntime creates a runtime.
func NewRuntime(cat *Catalog, opts ...RuntimeOption) *Runtime {
	if cat == nil {
		cat, _ = NewCatalog(nil)
	}
	r := &Runtime{
		sink:          NoopDiagnostics(),
		observePolicy: DefaultObservePolicy(),
		formatter:     locale.DefaultFormatter(),
		formatPolicy:  locale.DefaultFormatPolicy(),
	}
	for _, opt := range opts {
		opt(r)
	}
	r.current.Store(cat)
	return r
}

// Catalog returns the currently published catalog.
func (r *Runtime) Catalog() *Catalog {
	if r == nil {
		return nil
	}
	return r.current.Load()
}

// Swap atomically publishes cat.
func (r *Runtime) Swap(cat *Catalog) (SwapReport, error) {
	return r.SwapContext(context.Background(), cat)
}

// SwapContext atomically publishes cat and emits a catalog swap event with ctx.
func (r *Runtime) SwapContext(ctx context.Context, cat *Catalog) (SwapReport, error) {
	if r == nil {
		return SwapReport{}, fmt.Errorf("runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cat == nil || cat.snapshot == nil {
		return SwapReport{}, fmt.Errorf("catalog is nil")
	}
	old := r.current.Load()
	r.current.Store(cat)
	oldVersion := ""
	if old != nil {
		oldVersion = old.Version()
	}
	report := SwapReport{
		OldVersion: oldVersion,
		NewVersion: cat.Version(),
		SwappedAt:  time.Now().UTC(),
		Locales:    cat.Locales(),
	}
	r.observeCatalogSwap(ctx, report)
	return report, nil
}

// Localizer creates a localizer from locale preference strings.
func (r *Runtime) Localizer(preferences ...string) *Localizer {
	profile, err := locale.NewProfile(preferences)
	if err != nil {
		profile, _ = locale.NewProfile([]string{"en"})
	}
	return r.Translator(profile)
}

// Translator creates a profile-aware localizer.
func (r *Runtime) Translator(profile locale.Profile) *Localizer {
	cat := r.Catalog()
	if cat == nil {
		cat, _ = NewCatalog(nil)
	}
	snap := cat.snap()
	candidates := make([]locale.Candidate, 0, len(profile.Languages()))
	for _, tag := range profile.Languages() {
		candidates = append(candidates, locale.Preference(tag.String()))
	}
	resolved := snap.negotiator.Resolve(candidates...)
	return &Localizer{
		catalog:       snap,
		profile:       profile,
		resolved:      resolved,
		domain:        DefaultDomain,
		sink:          r.sink,
		observer:      r.observer,
		observeAttrs:  append([]observe.Attr(nil), r.observeAttrs...),
		observePolicy: r.observePolicy.normalized(),
		formatter:     r.formatter,
		formatPolicy:  r.formatPolicy,
	}
}

// Localizer translates messages using one immutable catalog snapshot and one
// immutable profile.
type Localizer struct {
	catalog       *catalogSnapshot
	profile       locale.Profile
	resolved      locale.Resolved
	domain        string
	sink          DiagnosticSink
	observer      observe.Observer
	observeAttrs  []observe.Attr
	observePolicy ObservePolicy
	ctx           context.Context
	formatter     locale.Formatter
	formatPolicy  locale.FormatPolicy
}

// Locale returns the resolved response locale string.
func (l *Localizer) Locale() string {
	if l == nil {
		return "en"
	}
	return l.resolved.Locale
}

// LocaleTag returns the resolved response locale tag.
func (l *Localizer) LocaleTag() language.Tag {
	if l == nil {
		return language.English
	}
	return l.resolved.Tag
}

// Profile returns the bound profile.
func (l *Localizer) Profile() locale.Profile {
	if l == nil {
		p, _ := locale.NewProfile([]string{"en"})
		return p
	}
	return l.profile
}

// WithDomain returns a copy bound to domain.
func (l *Localizer) WithDomain(domain string) *Localizer {
	if l == nil {
		return l
	}
	cp := *l
	cp.domain = cleanDomain(domain)
	return &cp
}

// WithContext returns a copy that emits observations with ctx.
func (l *Localizer) WithContext(ctx context.Context) *Localizer {
	if l == nil {
		return l
	}
	cp := *l
	if ctx == nil {
		ctx = context.Background()
	}
	cp.ctx = ctx
	return &cp
}

// WithAttrs returns a copy with additional observability attributes.
func (l *Localizer) WithAttrs(attrs ...observe.Attr) *Localizer {
	if l == nil {
		return l
	}
	cp := *l
	cp.observeAttrs = append(append([]observe.Attr(nil), l.observeAttrs...), attrs...)
	return &cp
}

// T translates a singular message.
func (l *Localizer) T(msgid string, vars ...Vars) string {
	return l.Lookup(Text(msgid), vars...).Text
}

// Tn translates a plural message.
func (l *Localizer) Tn(n int, singular, plural string, vars ...Vars) string {
	v := mergeVars(vars)
	if v == nil {
		v = Vars{}
	}
	if _, ok := v["n"]; !ok {
		v["n"] = Number(n)
	}
	return l.Lookup(Plural(n, singular, plural), v).Text
}

// Tc translates a singular message with gettext message context.
func (l *Localizer) Tc(messageContext, msgid string, vars ...Vars) string {
	return l.Lookup(Context(messageContext, msgid), vars...).Text
}

// Tnc translates a plural message with gettext message context.
func (l *Localizer) Tnc(messageContext string, n int, singular, plural string, vars ...Vars) string {
	v := mergeVars(vars)
	if v == nil {
		v = Vars{}
	}
	if _, ok := v["n"]; !ok {
		v["n"] = Number(n)
	}
	return l.Lookup(ContextPlural(messageContext, n, singular, plural), v).Text
}

// Lookup performs a checked lookup and interpolation.
func (l *Localizer) Lookup(msg Message, vars ...Vars) Result {
	return l.LookupContext(l.context(), msg, vars...)
}

// LookupContext performs a checked lookup and emits observations with ctx.
func (l *Localizer) LookupContext(ctx context.Context, msg Message, vars ...Vars) Result {
	if l == nil || l.catalog == nil {
		p, _ := locale.NewProfile([]string{"en"})
		ctx := locale.FormatContext{Profile: p, Locale: language.English, Policy: locale.DefaultFormatPolicy()}
		fallback, diagnostics := interpolate(msg.fallbackText(), mergeVars(vars), ctx, locale.DefaultFormatter())
		return Result{Text: fallback, Status: StatusMissing, Diagnostics: diagnostics}
	}
	if ctx == nil {
		ctx = l.context()
	}
	msg = msg.withDomain(l.domain)
	values := mergeVars(vars)
	text, status, messageLocale := l.lookupRaw(msg)
	formatCtx := locale.FormatContext{
		Profile:       l.profile,
		Locale:        l.resolved.Tag,
		MessageLocale: messageLocale,
		Policy:        l.formatPolicy,
	}
	if l.formatter != nil {
		formatCtx.DataVersion = l.formatter.DataVersion()
	}
	text, diagnostics := interpolate(text, values, formatCtx, l.formatter)
	if status != StatusMissing && formatCtx.Policy.Strict && hasStrictDiagnostics(diagnostics) {
		status = StatusError
	}
	if status == StatusMissing {
		diagnostics = append(diagnostics, Diagnostic{Code: "missing_translation"})
	}
	if status == StatusFallback {
		diagnostics = append(diagnostics, Diagnostic{Code: "fallback_translation"})
	}
	for _, d := range diagnostics {
		if l.sink != nil {
			l.sink.Report(d)
		}
	}
	result := Result{
		Text:            text,
		Status:          status,
		RequestedLocale: firstRequested(l.resolved),
		ResolvedLocale:  l.resolved.Locale,
		MessageLocale:   messageLocale,
		Domain:          msg.Domain,
		FallbackChain:   append([]string(nil), l.resolved.FallbackChain...),
		CatalogVersion:  l.catalog.version,
		Diagnostics:     diagnostics,
	}
	l.observeLookup(ctx, msg, result, values, formatCtx.DataVersion)
	return result
}

func (l *Localizer) context() context.Context {
	if l != nil && l.ctx != nil {
		return l.ctx
	}
	return context.Background()
}

func (l *Localizer) observeLookup(ctx context.Context, msg Message, result Result, vars Vars, dataVersion string) {
	observer := l.lookupObserver(ctx)
	if observer == nil {
		return
	}
	policy := l.observePolicy.normalized()
	if !policy.shouldEmitLookup(result) {
		return
	}
	event := observe.Event{
		Name:        "i18n.lookup",
		Severity:    lookupSeverity(result),
		Component:   "i18n",
		Operation:   "lookup",
		Outcome:     lookupOutcome(result.Status),
		Code:        lookupEventCode(result),
		Attrs:       l.lookupAttrs(ctx, msg, result, vars, dataVersion, policy),
		Diagnostics: observeDiagnostics(result.Diagnostics),
	}
	event, ok := policy.EventPolicy.Apply(event)
	if !ok {
		return
	}
	observe.SafeObserve(ctx, observer, event)
}

func (r *Runtime) observeCatalogSwap(ctx context.Context, report SwapReport) {
	observer := r.runtimeObserver(ctx)
	if observer == nil {
		return
	}
	policy := r.observePolicy.normalized()
	event := observe.Event{
		Name:      "i18n.catalog.swap",
		Severity:  observe.SeverityInfo,
		Component: "i18n.catalog",
		Operation: "swap",
		Outcome:   observe.OutcomeSuccess,
		Code:      "catalog_swapped",
		Attrs: []observe.Attr{
			observe.High("i18n.catalog.old_version", report.OldVersion),
			observe.High("i18n.catalog.new_version", report.NewVersion),
			observe.Int("i18n.catalog.locale_count", len(report.Locales)),
			observe.Bounded("i18n.catalog.locales", report.Locales),
		},
	}
	event.Attrs = append(event.Attrs, r.observeAttrs...)
	event.Attrs = append(event.Attrs, observe.AttrsFrom(ctx)...)
	event, ok := policy.EventPolicy.Apply(event)
	if !ok {
		return
	}
	observe.SafeObserve(ctx, observer, event)
}

func (r *Runtime) runtimeObserver(ctx context.Context) observe.Observer {
	ctxObserver, _ := observe.ObserverFrom(ctx)
	switch {
	case r.observer != nil && ctxObserver != nil:
		return observe.Multi(r.observer, ctxObserver)
	case r.observer != nil:
		return r.observer
	default:
		return ctxObserver
	}
}

func (l *Localizer) lookupObserver(ctx context.Context) observe.Observer {
	ctxObserver, _ := observe.ObserverFrom(ctx)
	switch {
	case l.observer != nil && ctxObserver != nil:
		return observe.Multi(l.observer, ctxObserver)
	case l.observer != nil:
		return l.observer
	default:
		return ctxObserver
	}
}

func (l *Localizer) lookupAttrs(ctx context.Context, msg Message, result Result, vars Vars, dataVersion string, policy ObservePolicy) []observe.Attr {
	attrs := []observe.Attr{
		observe.Low("i18n.status", string(result.Status)),
		observe.Bounded("i18n.domain", result.Domain),
		observe.Bounded("i18n.requested_locale", result.RequestedLocale),
		observe.Bounded("i18n.resolved_locale", result.ResolvedLocale),
		observe.Bounded("i18n.message_locale", result.MessageLocale),
		observe.High("i18n.fallback_chain", result.FallbackChain),
		observe.High("i18n.catalog_version", result.CatalogVersion),
		observe.High("i18n.formatter_data_version", dataVersion),
		observe.Bool("i18n.has_plural", msg.HasPlural),
	}
	if msg.HasPlural {
		attrs = append(attrs, observe.Int64("i18n.count", msg.Count))
	}
	switch policy.MessageIdentity {
	case MessageIdentityHash:
		attrs = append(attrs, observe.High("i18n.message_fingerprint", msg.fingerprint()))
	case MessageIdentityOmit:
	default:
		attrs = append(attrs,
			observe.High("i18n.message_fingerprint", msg.fingerprint()),
			observe.SourceText("i18n.message_id", msg.ID),
			observe.SourceText("i18n.message_context", msg.Context),
			observe.SourceText("i18n.plural_id", msg.PluralID),
		)
	}
	if codes := diagnosticCodes(result.Diagnostics); len(codes) > 0 {
		attrs = append(attrs, observe.Bounded("i18n.diagnostic_codes", codes))
	}
	if names, types := variableMetadata(vars); len(names) > 0 {
		attrs = append(attrs, observe.High("i18n.variable_names", names), observe.High("i18n.variable_types", types))
	}
	attrs = append(attrs, l.observeAttrs...)
	attrs = append(attrs, observe.AttrsFrom(ctx)...)
	return attrs
}

func lookupSeverity(result Result) observe.Severity {
	for _, d := range result.Diagnostics {
		if d.Severity == "error" {
			return observe.SeverityError
		}
	}
	if result.Status == StatusMissing || result.Status == StatusError {
		return observe.SeverityWarn
	}
	if result.Status == StatusFallback || len(result.Diagnostics) > 0 {
		return observe.SeverityInfo
	}
	return observe.SeverityDebug
}

func lookupOutcome(status Status) observe.Outcome {
	switch status {
	case StatusExact:
		return observe.OutcomeSuccess
	case StatusFallback:
		return observe.OutcomeFallback
	case StatusMissing:
		return observe.OutcomeMissing
	default:
		return observe.OutcomeError
	}
}

func lookupEventCode(result Result) string {
	switch result.Status {
	case StatusMissing:
		return "missing_translation"
	case StatusFallback:
		return "fallback_translation"
	}
	if len(result.Diagnostics) > 0 {
		return result.Diagnostics[0].Code
	}
	return "lookup"
}

func observeDiagnostics(ds []Diagnostic) []observe.Diagnostic {
	if len(ds) == 0 {
		return nil
	}
	out := make([]observe.Diagnostic, 0, len(ds))
	for _, d := range ds {
		out = append(out, observe.Diagnostic{
			Code:           d.Code,
			Severity:       d.Severity,
			Component:      d.Component,
			Kind:           d.Kind,
			Locale:         d.Locale,
			FallbackLocale: d.Fallback,
			SpecField:      d.SpecField,
			Source:         d.Source,
			Detail:         d.Message,
		})
	}
	return out
}

func diagnosticCodes(ds []Diagnostic) []string {
	if len(ds) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, d := range ds {
		if d.Code != "" && !seen[d.Code] {
			seen[d.Code] = true
			out = append(out, d.Code)
		}
	}
	sort.Strings(out)
	return out
}

func variableMetadata(vars Vars) ([]string, []string) {
	if len(vars) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	types := make([]string, len(names))
	for i, name := range names {
		value := vars[name]
		if value == nil {
			types[i] = "<nil>"
			continue
		}
		types[i] = reflect.TypeOf(value).String()
	}
	return names, types
}

func firstRequested(r locale.Resolved) string {
	if len(r.Requested) == 0 {
		return r.Locale
	}
	return r.Requested[0]
}

func (l *Localizer) lookupRaw(msg Message) (string, Status, string) {
	locales := []string{l.resolved.Locale}
	if l.catalog.defaultLocale != "" && l.catalog.defaultLocale != l.resolved.Locale {
		locales = append(locales, l.catalog.defaultLocale)
	}
	for i, loc := range locales {
		if text, ok := l.lookupInLocale(loc, msg); ok {
			status := StatusExact
			if i > 0 {
				status = StatusFallback
			}
			return text, status, loc
		}
	}
	return msg.fallbackText(), StatusMissing, ""
}

func (l *Localizer) lookupInLocale(loc string, msg Message) (string, bool) {
	lc, ok := l.catalog.locales[loc]
	if !ok {
		return "", false
	}
	domain := cleanDomain(msg.Domain)
	entries := lc.domains[domain]
	if entries == nil && domain != DefaultDomain {
		entries = lc.domains[DefaultDomain]
	}
	entry, ok := entries[keyFor(msg.Context, msg.ID)]
	if !ok {
		return "", false
	}
	if msg.HasPlural {
		index := lc.plural.Select(msg.Count)
		if index >= 0 && index < len(entry.translations) && entry.translations[index] != "" {
			return entry.translations[index], true
		}
		return msg.fallbackText(), false
	}
	if len(entry.translations) > 0 && entry.translations[0] != "" {
		return entry.translations[0], true
	}
	return "", false
}

func (m Message) fallbackText() string {
	if m.HasPlural && m.Count != 1 {
		if m.PluralID != "" {
			return m.PluralID
		}
	}
	return m.ID
}

type localizerKey struct{}

// WithLocalizer stores a Localizer in ctx.
func WithLocalizer(ctx context.Context, tr *Localizer) context.Context {
	return context.WithValue(ctx, localizerKey{}, tr)
}

// FromContext returns a Localizer from ctx.
func FromContext(ctx context.Context) (*Localizer, bool) {
	tr, ok := ctx.Value(localizerKey{}).(*Localizer)
	return tr, ok
}
