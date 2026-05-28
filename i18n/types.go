package i18n

import (
	"fmt"
	"strings"
)

const DefaultDomain = ""

// Vars contains named interpolation variables.
type Vars map[string]any

// Arg creates a one-item Vars map for call sites that avoid map literals.
func Arg(name string, value any) Vars {
	return Vars{name: value}
}

func mergeVars(vars []Vars) Vars {
	if len(vars) == 0 {
		return nil
	}
	out := Vars{}
	for _, v := range vars {
		for key, value := range v {
			out[key] = value
		}
	}
	return out
}

// Status describes lookup quality.
type Status string

const (
	StatusExact    Status = "exact"
	StatusFallback Status = "fallback"
	StatusMissing  Status = "missing"
	StatusError    Status = "error"
)

// Diagnostic is a redacted lookup or interpolation diagnostic.
type Diagnostic struct {
	Code        string `json:"code"`
	Placeholder string `json:"placeholder,omitempty"`
	Message     string `json:"message,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Component   string `json:"component,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Fallback    string `json:"fallback_locale,omitempty"`
	SpecField   string `json:"spec_field,omitempty"`
	Source      string `json:"source,omitempty"`
	FormatCode  string `json:"format_code,omitempty"`
}

// Result is the metadata-rich result of a checked lookup.
type Result struct {
	Text            string
	Status          Status
	RequestedLocale string
	ResolvedLocale  string
	MessageLocale   string
	Domain          string
	FallbackChain   []string
	CatalogVersion  string
	Diagnostics     []Diagnostic
}

// Message describes a source-string message lookup.
type Message struct {
	Domain    string
	Context   string
	ID        string
	PluralID  string
	Count     int64
	HasPlural bool
}

// Text creates a singular message.
func Text(msgid string) Message {
	return Message{ID: msgid}
}

// Context creates a message with gettext message context.
func Context(messageContext, msgid string) Message {
	return Message{Context: messageContext, ID: msgid}
}

// Plural creates a plural message.
func Plural(n int, singular, plural string) Message {
	return Message{ID: singular, PluralID: plural, Count: int64(n), HasPlural: true}
}

// ContextPlural creates a plural message with gettext message context.
func ContextPlural(messageContext string, n int, singular, plural string) Message {
	return Message{Context: messageContext, ID: singular, PluralID: plural, Count: int64(n), HasPlural: true}
}

func (m Message) withDomain(domain string) Message {
	if m.Domain == "" {
		m.Domain = domain
	}
	return m
}

type messageKey struct {
	context string
	id      string
}

func keyFor(context, id string) messageKey {
	return messageKey{context: context, id: id}
}

// Error is a package error with a stable code.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Code
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func cleanDomain(domain string) string {
	return strings.TrimSpace(domain)
}
