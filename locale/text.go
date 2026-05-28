package locale

import (
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/search"
)

// TextDataVersion reports the Unicode/CLDR data versions used by the x/text
// helpers. These helpers intentionally do not use generated CLDR 48 collation
// rule tables.
type TextDataVersion struct {
	CollateCLDR    string
	CollateUnicode string
	SearchCLDR     string
	SearchUnicode  string
	CasesUnicode   string
}

// TextVersions returns data-version metadata for locale text helpers.
func TextVersions() TextDataVersion {
	return TextDataVersion{
		CollateCLDR: collate.CLDRVersion, CollateUnicode: collate.UnicodeVersion,
		SearchCLDR: search.CLDRVersion, SearchUnicode: search.UnicodeVersion,
		CasesUnicode: cases.UnicodeVersion,
	}
}

// NewCollator creates a collator for a profile or tag. Unsupported collation
// extension values are left to x/text option handling.
func NewCollator(profile Profile, opts ...collate.Option) *collate.Collator {
	tag := profile.PrimaryLanguage()
	options := []collate.Option{collate.OptionsFromTag(tag)}
	options = append(options, opts...)
	return collate.New(tag, options...)
}

// SortStrings returns a locale-aware sorted copy of values.
func SortStrings(profile Profile, values []string, opts ...collate.Option) []string {
	out := append([]string(nil), values...)
	c := NewCollator(profile, opts...)
	c.SortStrings(out)
	return out
}

// SearchIndex returns the first locale-aware match index for pattern in text.
func SearchIndex(profile Profile, text, pattern string) int {
	m := search.New(profile.PrimaryLanguage())
	start, _ := m.IndexString(text, pattern)
	return start
}

// Contains reports whether text contains pattern under language-aware search.
func Contains(profile Profile, text, pattern string) bool {
	return SearchIndex(profile, text, pattern) >= 0
}

// Upper maps text to upper case using profile language.
func Upper(profile Profile, text string) string {
	return cases.Upper(profile.PrimaryLanguage()).String(text)
}

// Lower maps text to lower case using profile language.
func Lower(profile Profile, text string) string {
	return cases.Lower(profile.PrimaryLanguage()).String(text)
}

// Title maps text to title case using profile language.
func Title(profile Profile, text string) string {
	return cases.Title(profile.PrimaryLanguage()).String(text)
}

// ValidateTextExtension reports whether a BCP-47 text-operation extension is
// known enough to parse for diagnostics. It does not promise CLDR 48 collation
// rule support.
func ValidateTextExtension(key, value string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.ToLower(strings.TrimSpace(value))
	if key == "" || value == "" {
		return false
	}
	for _, rec := range cldrTextExtensions() {
		if rec.key == key && rec.value == value {
			return true
		}
	}
	return false
}

type textExtension struct {
	key   string
	value string
}

func cldrTextExtensions() []textExtension {
	exts := []textExtension{
		{key: "co", value: "standard"},
		{key: "co", value: "phonebk"},
		{key: "co", value: "dict"},
		{key: "co", value: "search"},
		{key: "co", value: "emoji"},
		{key: "ss", value: "standard"},
		{key: "ss", value: "none"},
	}
	sort.Slice(exts, func(i, j int) bool {
		if exts[i].key != exts[j].key {
			return exts[i].key < exts[j].key
		}
		return exts[i].value < exts[j].value
	})
	return exts
}

// ProfileForTag is a small helper for text operations in tests and simple
// callers that already have a language tag.
func ProfileForTag(tag language.Tag) Profile {
	p, _ := NewProfileTags([]language.Tag{tag})
	return p
}
