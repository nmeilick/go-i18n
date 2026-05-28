package gettext

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxPlaceholderNameRunes = 80

// ValidationReport contains catalog validation diagnostics.
type ValidationReport struct {
	Errors   []error
	Warnings []error
}

// OK reports whether validation had no errors.
func (r ValidationReport) OK() bool { return len(r.Errors) == 0 }

// Validate validates plural form and entry shape.
func Validate(doc *Document) ValidationReport {
	var report ValidationReport
	if doc == nil {
		return report
	}
	header := doc.Header()
	rule, err := ParsePluralRule(header["Plural-Forms"])
	if err != nil {
		report.Errors = append(report.Errors, err)
		rule, _ = ParsePluralRule("nplurals=2; plural=(n != 1);")
	}
	seen := map[string]bool{}
	for _, e := range doc.Entries {
		if e.ID == "" {
			continue
		}
		key := e.Domain + "\x00" + e.Context + "\x00" + e.ID
		if seen[key] && !e.Obsolete {
			report.Errors = append(report.Errors, fmt.Errorf("duplicate active entry %q", e.ID))
		}
		seen[key] = true
		if e.PluralID != "" && len(e.Strings) != rule.NPlurals {
			report.Errors = append(report.Errors, fmt.Errorf("entry %q has %d plural strings, want %d", e.ID, len(e.Strings), rule.NPlurals))
		}
		singularPlaceholders, invalid := placeholderSet(e.ID)
		for _, name := range invalid {
			report.Errors = append(report.Errors, fmt.Errorf("entry %q has invalid placeholder {%s}", e.ID, name))
		}
		pluralPlaceholders := singularPlaceholders
		sourcePlaceholders := singularPlaceholders
		if e.PluralID != "" {
			var pluralInvalid []string
			pluralPlaceholders, pluralInvalid = placeholderSet(e.PluralID)
			for _, name := range pluralInvalid {
				report.Errors = append(report.Errors, fmt.Errorf("entry %q plural has invalid placeholder {%s}", e.ID, name))
			}
			sourcePlaceholders = unionPlaceholders(singularPlaceholders, pluralPlaceholders)
		}
		for i, text := range e.Strings {
			if text == "" {
				continue
			}
			translationPlaceholders, invalid := placeholderSet(text)
			for _, name := range invalid {
				report.Errors = append(report.Errors, fmt.Errorf("entry %q translation has invalid placeholder {%s}", e.ID, name))
			}
			want := singularPlaceholders
			if e.PluralID != "" && i > 0 {
				want = pluralPlaceholders
			}
			for _, missing := range missingPlaceholders(want, translationPlaceholders) {
				report.Errors = append(report.Errors, fmt.Errorf("entry %q translation missing placeholder {%s}", e.ID, missing))
			}
			for _, extra := range missingPlaceholders(translationPlaceholders, sourcePlaceholders) {
				report.Errors = append(report.Errors, fmt.Errorf("entry %q translation has unknown placeholder {%s}", e.ID, extra))
			}
		}
		if e.Obsolete {
			report.Warnings = append(report.Warnings, fmt.Errorf("entry %q is obsolete", e.ID))
		}
		if hasFlag(e, "fuzzy") {
			report.Warnings = append(report.Warnings, fmt.Errorf("entry %q is fuzzy", e.ID))
		}
	}
	return report
}

func placeholderSet(s string) (map[string]bool, []string) {
	out := map[string]bool{}
	var invalid []string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := strings.IndexByte(s[i+1:], '}')
		if end < 0 {
			continue
		}
		name := s[i+1 : i+1+end]
		if validPlaceholderName(name) {
			out[name] = true
		} else if name != "" {
			invalid = append(invalid, name)
		}
		i += end + 1
	}
	sort.Strings(invalid)
	return out, invalid
}

func validPlaceholderName(name string) bool {
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

func missingPlaceholders(want, got map[string]bool) []string {
	var out []string
	for name := range want {
		if !got[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func unionPlaceholders(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for name := range a {
		out[name] = true
	}
	for name := range b {
		out[name] = true
	}
	return out
}
