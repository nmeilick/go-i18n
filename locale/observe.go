package locale

import (
	"context"
	"sort"

	"github.com/nmeilick/go-i18n/observe"
)

func finishFormat(ctx FormatContext, kind string, text string, diagnostics []FormatDiagnostic) (string, []FormatDiagnostic) {
	observeFormat(ctx, kind, diagnostics)
	return text, diagnostics
}

func finishBoundFormatted(ctx FormatContext, kind, text string) (string, []FormatDiagnostic) {
	out, diagnostics := boundFormatted(ctx, text, kind)
	return finishFormat(ctx, kind, out, diagnostics)
}

func finishFormatFallback(ctx FormatContext, kind, code, detail string) (string, []FormatDiagnostic) {
	out, diagnostics := formatFallback(ctx, kind, code, detail)
	return finishFormat(ctx, kind, out, diagnostics)
}

func withoutFormatObserver(ctx FormatContext) FormatContext {
	ctx.Observer = nil
	ctx.ObserveContext = nil
	ctx.ObserveAttrs = nil
	return ctx
}

func observeFormat(ctx FormatContext, kind string, diagnostics []FormatDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	observer := formatObserver(ctx)
	if observer == nil {
		return
	}
	event := observe.Event{
		Name:      "locale.format",
		Severity:  formatSeverity(diagnostics),
		Component: "locale.formatter",
		Operation: observe.Operation("format_" + kind),
		Outcome:   formatOutcome(diagnostics),
		Code:      diagnostics[0].Code,
		Attrs: []observe.Attr{
			observe.Low("locale.format.kind", kind),
			observe.Bounded("locale.format.locale", cldrLookupTag(effectiveFormatTag(ctx).String())),
			observe.Bounded("locale.message_locale", ctx.MessageLocale),
			observe.High("locale.format.data_version", ctx.DataVersion),
			observe.Bounded("locale.format.diagnostic_codes", formatDiagnosticCodes(diagnostics)),
		},
		Diagnostics: observeFormatDiagnostics(diagnostics),
	}
	event.Attrs = append(event.Attrs, ctx.ObserveAttrs...)
	event.Attrs = append(event.Attrs, observe.AttrsFrom(observeContext(ctx.ObserveContext))...)
	event, ok := ctx.ObservePolicy.Apply(event)
	if !ok {
		return
	}
	observe.SafeObserve(observeContext(ctx.ObserveContext), observer, event)
}

func formatObserver(ctx FormatContext) observe.Observer {
	ctxObserver, _ := observe.ObserverFrom(ctx.ObserveContext)
	switch {
	case ctx.Observer != nil && ctxObserver != nil:
		return observe.Multi(ctx.Observer, ctxObserver)
	case ctx.Observer != nil:
		return ctx.Observer
	default:
		return ctxObserver
	}
}

func observeContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func formatSeverity(diagnostics []FormatDiagnostic) observe.Severity {
	for _, d := range diagnostics {
		if d.Severity == "error" {
			return observe.SeverityError
		}
	}
	return observe.SeverityInfo
}

func formatOutcome(diagnostics []FormatDiagnostic) observe.Outcome {
	for _, d := range diagnostics {
		if d.Severity == "error" {
			return observe.OutcomeError
		}
	}
	return observe.OutcomeFallback
}

func formatDiagnosticCodes(diagnostics []FormatDiagnostic) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, d := range diagnostics {
		if d.Code != "" && !seen[d.Code] {
			seen[d.Code] = true
			out = append(out, d.Code)
		}
	}
	sort.Strings(out)
	return out
}

func observeFormatDiagnostics(diagnostics []FormatDiagnostic) []observe.Diagnostic {
	if len(diagnostics) == 0 {
		return nil
	}
	out := make([]observe.Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		out = append(out, observe.Diagnostic{
			Code:           d.Code,
			Severity:       d.Severity,
			Component:      d.Component,
			Kind:           d.Kind,
			Locale:         d.Locale,
			FallbackLocale: d.FallbackLocale,
			SpecField:      d.SpecField,
			Source:         d.Source,
			Detail:         d.Detail,
		})
	}
	return out
}

func (r *Resolver) observeResolve(ctx context.Context, result ResolveResult, observations []Observation) {
	observer := r.resolveObserver(ctx)
	if observer == nil {
		return
	}
	attrs := []observe.Attr{
		observe.Low("locale.resolve.sensitivity", string(result.Sensitivity)),
		observe.High("locale.resolve.data_version", result.DataVersion),
		observe.Bounded("locale.resolve.diagnostic_codes", resolveDiagnosticCodes(result.Diagnostics)),
		observe.Bounded("locale.resolve.source_classes", observationSourceClasses(observations)),
		observe.Bounded("locale.language", firstString(result.Snapshot.Languages)),
		observe.Bounded("locale.timezone_present", result.Snapshot.TimeZone != ""),
		observe.Bounded("locale.currency_present", result.Snapshot.Currency != ""),
	}
	attrs = append(attrs, r.observeAttrs...)
	attrs = append(attrs, observe.AttrsFrom(observeContext(ctx))...)
	event := observe.Event{
		Name:        "locale.resolve",
		Severity:    observe.SeverityInfo,
		Component:   "locale.resolver",
		Operation:   "resolve",
		Outcome:     observe.OutcomeSuccess,
		Code:        "resolved_profile",
		Attrs:       attrs,
		Diagnostics: observeResolveDiagnostics(result.Diagnostics),
	}
	event, ok := r.observePolicy.Apply(event)
	if !ok {
		return
	}
	observe.SafeObserve(observeContext(ctx), observer, event)
}

func (r *Resolver) resolveObserver(ctx context.Context) observe.Observer {
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

func resolveDiagnosticCodes(diagnostics []ResolveDiagnostic) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, d := range diagnostics {
		if d.Code != "" && !seen[d.Code] {
			seen[d.Code] = true
			out = append(out, d.Code)
		}
	}
	sort.Strings(out)
	return out
}

func observationSourceClasses(observations []Observation) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, obs := range observations {
		source := string(normalizeSourceClass(obs.SourceClass))
		if source != "" && !seen[source] {
			seen[source] = true
			out = append(out, source)
		}
	}
	sort.Strings(out)
	return out
}

func observeResolveDiagnostics(diagnostics []ResolveDiagnostic) []observe.Diagnostic {
	out := make([]observe.Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		out = append(out, observe.Diagnostic{
			Code:      d.Code,
			Severity:  d.Severity,
			Component: "locale.resolver",
			Kind:      string(d.Field),
			Source:    string(normalizeSourceClass(d.SourceClass)),
			Detail:    d.Reason,
		})
	}
	return out
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
