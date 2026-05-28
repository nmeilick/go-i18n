package observe

import (
	"time"
	"unicode/utf8"
)

const (
	defaultMaxAttrs          = 64
	defaultMaxAttrValueBytes = 4096
	defaultMaxDiagnostics    = 32
	defaultMaxAttrListItems  = 64
)

// EventName identifies one stable event family.
type EventName string

// Severity describes operational event severity.
type Severity string

const (
	SeverityDebug Severity = "debug"
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Component identifies the emitting library component.
type Component string

// Operation identifies the work represented by the event.
type Operation string

// Outcome describes the result of the operation.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeFallback Outcome = "fallback"
	OutcomeMissing  Outcome = "missing"
	OutcomeError    Outcome = "error"
)

// Event is a single structured observability record.
type Event struct {
	Name        EventName
	Time        time.Time
	Duration    time.Duration
	Severity    Severity
	Component   Component
	Operation   Operation
	Outcome     Outcome
	Code        string
	Attrs       []Attr
	Diagnostics []Diagnostic
}

// CloneEvent returns a copy of event with defensive slice copies.
func CloneEvent(event Event) Event {
	if len(event.Attrs) > 0 {
		event.Attrs = append([]Attr(nil), event.Attrs...)
	}
	if len(event.Diagnostics) > 0 {
		event.Diagnostics = append([]Diagnostic(nil), event.Diagnostics...)
	}
	return event
}

// Diagnostic is a package-neutral event diagnostic.
type Diagnostic struct {
	Code           string
	Severity       string
	Component      string
	Kind           string
	Locale         string
	FallbackLocale string
	SpecField      string
	Source         string
	Detail         string
}

// Visibility tells adapters how broadly an attribute should be exported.
type Visibility string

const (
	VisibilityPublic     Visibility = "public"
	VisibilitySourceText Visibility = "source_text"
	VisibilityInternal   Visibility = "internal"
	VisibilityPrivate    Visibility = "private"
)

// Cardinality tells adapters whether an attribute is suitable for labels.
type Cardinality string

const (
	CardinalityLow     Cardinality = "low"
	CardinalityBounded Cardinality = "bounded"
	CardinalityHigh    Cardinality = "high"
)

// Attr is a structured event attribute.
type Attr struct {
	Key         string
	Value       any
	Visibility  Visibility
	Cardinality Cardinality
}

// Any creates an internal high-cardinality attribute.
func Any(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityInternal, Cardinality: CardinalityHigh}
}

// String creates an internal high-cardinality string attribute.
func String(key, value string) Attr {
	return Any(key, value)
}

// Int creates an internal high-cardinality integer attribute.
func Int(key string, value int) Attr {
	return Any(key, value)
}

// Int64 creates an internal high-cardinality integer attribute.
func Int64(key string, value int64) Attr {
	return Any(key, value)
}

// Bool creates an internal low-cardinality boolean attribute.
func Bool(key string, value bool) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityInternal, Cardinality: CardinalityLow}
}

// Low creates a public low-cardinality attribute suitable for metric labels.
func Low(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityPublic, Cardinality: CardinalityLow}
}

// Bounded creates a public bounded-cardinality attribute.
func Bounded(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityPublic, Cardinality: CardinalityBounded}
}

// High creates an internal high-cardinality attribute.
func High(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityInternal, Cardinality: CardinalityHigh}
}

// Private creates a private high-cardinality app-owned attribute.
func Private(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilityPrivate, Cardinality: CardinalityHigh}
}

// SourceText creates a high-cardinality source-text attribute.
func SourceText(key string, value any) Attr {
	return Attr{Key: key, Value: value, Visibility: VisibilitySourceText, Cardinality: CardinalityHigh}
}

// Policy bounds and filters emitted events.
type Policy struct {
	MinSeverity       Severity
	MaxAttrs          int
	MaxAttrValueBytes int
	MaxAttrListItems  int
	MaxDiagnostics    int
}

// DefaultPolicy returns production-safe event bounds.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttrs:          defaultMaxAttrs,
		MaxAttrValueBytes: defaultMaxAttrValueBytes,
		MaxAttrListItems:  defaultMaxAttrListItems,
		MaxDiagnostics:    defaultMaxDiagnostics,
	}
}

// Apply returns a bounded copy of event and whether it should be emitted.
func (p Policy) Apply(event Event) (Event, bool) {
	p = p.normalized()
	if p.MinSeverity != "" && severityRank(event.Severity) < severityRank(p.MinSeverity) {
		return Event{}, false
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if len(event.Attrs) > p.MaxAttrs {
		event.Attrs = append([]Attr(nil), event.Attrs[:p.MaxAttrs]...)
	} else if len(event.Attrs) > 0 {
		event.Attrs = append([]Attr(nil), event.Attrs...)
	}
	for i := range event.Attrs {
		event.Attrs[i].Visibility = normalizeVisibility(event.Attrs[i].Visibility)
		event.Attrs[i].Cardinality = normalizeCardinality(event.Attrs[i].Cardinality)
		event.Attrs[i].Value = boundValue(event.Attrs[i].Value, p.MaxAttrValueBytes, p.MaxAttrListItems)
	}
	if len(event.Diagnostics) > p.MaxDiagnostics {
		event.Diagnostics = append([]Diagnostic(nil), event.Diagnostics[:p.MaxDiagnostics]...)
	} else if len(event.Diagnostics) > 0 {
		event.Diagnostics = append([]Diagnostic(nil), event.Diagnostics...)
	}
	for i := range event.Diagnostics {
		event.Diagnostics[i] = boundDiagnostic(event.Diagnostics[i], p.MaxAttrValueBytes)
	}
	return event, true
}

func (p Policy) normalized() Policy {
	if p.MaxAttrs <= 0 {
		p.MaxAttrs = defaultMaxAttrs
	}
	if p.MaxAttrValueBytes <= 0 {
		p.MaxAttrValueBytes = defaultMaxAttrValueBytes
	}
	if p.MaxAttrListItems <= 0 {
		p.MaxAttrListItems = defaultMaxAttrListItems
	}
	if p.MaxDiagnostics <= 0 {
		p.MaxDiagnostics = defaultMaxDiagnostics
	}
	return p
}

func boundValue(value any, maxBytes, maxItems int) any {
	switch v := value.(type) {
	case string:
		return truncateString(v, maxBytes)
	case []string:
		if len(v) > maxItems {
			v = v[:maxItems]
		}
		out := make([]string, len(v))
		for i, item := range v {
			out[i] = truncateString(item, maxBytes)
		}
		return out
	default:
		return value
	}
}

func boundDiagnostic(d Diagnostic, maxBytes int) Diagnostic {
	d.Code = truncateString(d.Code, maxBytes)
	d.Severity = truncateString(d.Severity, maxBytes)
	d.Component = truncateString(d.Component, maxBytes)
	d.Kind = truncateString(d.Kind, maxBytes)
	d.Locale = truncateString(d.Locale, maxBytes)
	d.FallbackLocale = truncateString(d.FallbackLocale, maxBytes)
	d.SpecField = truncateString(d.SpecField, maxBytes)
	d.Source = truncateString(d.Source, maxBytes)
	d.Detail = truncateString(d.Detail, maxBytes)
	return d
}

func normalizeVisibility(v Visibility) Visibility {
	switch v {
	case VisibilityPublic, VisibilitySourceText, VisibilityInternal, VisibilityPrivate:
		return v
	default:
		return VisibilityInternal
	}
}

func normalizeCardinality(c Cardinality) Cardinality {
	switch c {
	case CardinalityLow, CardinalityBounded, CardinalityHigh:
		return c
	default:
		return CardinalityHigh
	}
}

func truncateString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityDebug:
		return 1
	case SeverityInfo:
		return 2
	case SeverityWarn, "":
		return 3
	case SeverityError:
		return 4
	default:
		return 3
	}
}
