package i18n

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nmeilick/go-i18n/locale"
)

const (
	maxPlaceholderNameRunes = 80
	maxFormattedValueRunes  = 8192
	maxMessageRunes         = 65536
)

func interpolate(template string, vars Vars, ctx locale.FormatContext, formatter locale.Formatter) (string, []Diagnostic) {
	if template == "" {
		return "", nil
	}
	var b strings.Builder
	diagnostics := []Diagnostic{}
	for i := 0; i < len(template); {
		r, size := utf8.DecodeRuneInString(template[i:])
		if r != '{' {
			b.WriteString(template[i : i+size])
			i += size
			continue
		}
		end := strings.IndexByte(template[i+size:], '}')
		if end < 0 {
			b.WriteRune(r)
			i += size
			continue
		}
		name := template[i+size : i+size+end]
		if !validPlaceholder(name) {
			b.WriteString(template[i : i+size+end+1])
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_placeholder", Placeholder: name})
			i += size + end + 1
			continue
		}
		value, ok := vars[name]
		if !ok {
			b.WriteString(template[i : i+size+end+1])
			diagnostics = append(diagnostics, Diagnostic{Code: "missing_variable", Placeholder: name})
			i += size + end + 1
			continue
		}
		formatted, ds := formatInterpolationValue(value, ctx, formatter)
		for _, d := range ds {
			d.Placeholder = name
			diagnostics = append(diagnostics, d)
		}
		if utf8.RuneCountInString(formatted) > maxFormattedValueRunes {
			formatted = "{truncated}"
			diagnostics = append(diagnostics, Diagnostic{Code: "formatted_value_too_large", Placeholder: name})
		}
		b.WriteString(formatted)
		i += size + end + 1
	}
	out := b.String()
	if utf8.RuneCountInString(out) > maxMessageRunes {
		diagnostics = append(diagnostics, Diagnostic{Code: "formatted_message_too_large"})
		return string([]rune(out)[:maxMessageRunes]), diagnostics
	}
	return out, diagnostics
}

func validPlaceholder(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > maxPlaceholderNameRunes {
		return false
	}
	for i, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return false
		}
		if i == 0 && unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func formatInterpolationValue(value any, ctx locale.FormatContext, formatter locale.Formatter) (out string, diagnostics []Diagnostic) {
	defer func() {
		if r := recover(); r != nil {
			out = "{error}"
			diagnostics = append(diagnostics, Diagnostic{Code: "formatter_panic", Severity: "error"})
		}
	}()
	if value == nil {
		return "", nil
	}
	if fv, ok := value.(Value); ok {
		if formatter == nil {
			formatter = locale.DefaultFormatter()
		}
		text, ds := fv.Format(ctx, formatter)
		if len(ds) > 0 {
			return text, formatDiagnostics(ds)
		}
		return text, nil
	}
	return fmt.Sprint(value), nil
}

func formatDiagnostics(ds []locale.FormatDiagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		msg := boundDiagnosticMessage(d.Detail)
		if msg == "" {
			msg = d.Code
		}
		out = append(out, Diagnostic{
			Code:       "formatter_" + d.Code,
			Message:    msg,
			Severity:   d.Severity,
			Component:  d.Component,
			Kind:       d.Kind,
			Locale:     d.Locale,
			Fallback:   d.FallbackLocale,
			SpecField:  d.SpecField,
			Source:     d.Source,
			FormatCode: d.Code,
		})
	}
	return out
}

func hasStrictDiagnostics(ds []Diagnostic) bool {
	found := false
	for i := range ds {
		if ds[i].Severity == "" {
			ds[i].Severity = "error"
		}
		found = true
	}
	return found
}

func boundDiagnosticMessage(value string) string {
	const maxDiagnosticMessageBytes = 1024
	if len(value) <= maxDiagnosticMessageBytes {
		return value
	}
	return value[:maxDiagnosticMessageBytes] + "{truncated}"
}
