package locale

import (
	"strings"

	"github.com/nmeilick/go-i18n/internal/cldrdata"
	"golang.org/x/text/language"
)

// DisplayNameProvider is the data boundary needed by DisplayNameSet.
type DisplayNameProvider interface {
	DisplayName(locale, kind, code string) (string, bool)
	Parent(tag string) (string, bool)
}

// DisplayNameResult reports a localized display-name lookup.
type DisplayNameResult struct {
	Text           string
	Locale         string
	FallbackLocale string
	Diagnostics    []FormatDiagnostic
}

// DisplayNameSet formats language, region, script, and calendar display names
// for one profile.
type DisplayNameSet struct {
	profile Profile
	data    DisplayNameProvider
}

// DisplayNames returns display-name helpers backed by the built-in CLDR core.
func DisplayNames(profile Profile) DisplayNameSet {
	return NewDisplayNames(profile, cldrdata.Default())
}

// NewDisplayNames returns display-name helpers backed by provider.
func NewDisplayNames(profile Profile, provider DisplayNameProvider) DisplayNameSet {
	if provider == nil {
		provider = cldrdata.Default()
	}
	return DisplayNameSet{profile: profile, data: provider}
}

// Language returns a localized language display name or the input tag.
func (n DisplayNameSet) Language(tag string) string {
	return n.LookupLanguage(tag).Text
}

// Region returns a localized region display name or the input region code.
func (n DisplayNameSet) Region(region string) string {
	return n.LookupRegion(region).Text
}

// Script returns a localized script display name or the input script code.
func (n DisplayNameSet) Script(script string) string {
	return n.LookupScript(script).Text
}

// Calendar returns a localized calendar display name or the input calendar id.
func (n DisplayNameSet) Calendar(calendar string) string {
	return n.LookupCalendar(calendar).Text
}

// LookupLanguage returns a checked localized language display-name lookup.
func (n DisplayNameSet) LookupLanguage(tag string) DisplayNameResult {
	code := canonicalDisplayLanguage(tag)
	return n.lookup("language", code)
}

// LookupRegion returns a checked localized region display-name lookup.
func (n DisplayNameSet) LookupRegion(region string) DisplayNameResult {
	code := strings.ToUpper(strings.TrimSpace(region))
	return n.lookup("territory", code)
}

// LookupScript returns a checked localized script display-name lookup.
func (n DisplayNameSet) LookupScript(script string) DisplayNameResult {
	code := strings.TrimSpace(script)
	if len(code) == 4 {
		code = strings.ToUpper(code[:1]) + strings.ToLower(code[1:])
	}
	return n.lookup("script", code)
}

// LookupCalendar returns a checked localized calendar display-name lookup.
func (n DisplayNameSet) LookupCalendar(calendar string) DisplayNameResult {
	return n.lookup("calendar", strings.TrimSpace(calendar))
}

func (n DisplayNameSet) lookup(kind, code string) DisplayNameResult {
	code = strings.TrimSpace(code)
	if code == "" {
		return DisplayNameResult{Diagnostics: []FormatDiagnostic{{
			Code:      "invalid_display_name_code",
			Severity:  "error",
			Component: "display_names",
			Kind:      kind,
		}}}
	}
	requested := cldrLookupTag(n.profile.PrimaryLanguage().String())
	for cur := requested; cur != ""; {
		if text, ok := n.data.DisplayName(cur, kind, code); ok && text != "" {
			return DisplayNameResult{Text: text, Locale: requested, FallbackLocale: cur}
		}
		parent, ok := n.data.Parent(cur)
		if !ok || parent == cur {
			parent = displayParent(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	return DisplayNameResult{
		Text:   code,
		Locale: requested,
		Diagnostics: []FormatDiagnostic{{
			Code:      "display_name_unavailable",
			Severity:  "info",
			Component: "display_names",
			Kind:      kind,
			Locale:    requested,
			Detail:    code,
		}},
	}
}

func canonicalDisplayLanguage(raw string) string {
	tag, err := language.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return cldrLookupTag(tag.String())
}

func displayParent(raw string) string {
	if i := strings.LastIndex(raw, "-"); i > 0 {
		return raw[:i]
	}
	return ""
}
