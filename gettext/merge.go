package gettext

// MergeReport describes catalog merge results.
type MergeReport struct {
	Added        int
	Kept         int
	Obsolete     int
	Untranslated int
}

// Merge merges template entries into existing, preserving translations and
// translator comments by (domain, context, msgid).
func Merge(template, existing *Document) (*Document, MergeReport, error) {
	return MergeLocale(template, existing, "")
}

// MergeLocale merges template entries into existing and initializes a locale
// header when the existing document has none.
func MergeLocale(template, existing *Document, locale string) (*Document, MergeReport, error) {
	if template == nil {
		template = &Document{}
	}
	if existing == nil {
		existing = &Document{}
	}
	old := map[messageKey]Entry{}
	for _, e := range existing.Entries {
		if e.ID == "" || e.Obsolete {
			continue
		}
		old[messageKey{domain: e.Domain, context: e.Context, id: e.ID}] = e
	}
	seen := map[messageKey]bool{}
	out := &Document{}
	if header, ok := firstHeader(existing); ok {
		out.Entries = append(out.Entries, header)
	} else if header, ok := firstHeader(template); ok {
		out.Entries = append(out.Entries, header)
	} else if locale != "" {
		out.Entries = append(out.Entries, headerEntry(locale))
	}
	nplurals := pluralCount(out)
	report := MergeReport{}
	for _, tmpl := range template.Entries {
		if tmpl.ID == "" {
			continue
		}
		key := messageKey{domain: tmpl.Domain, context: tmpl.Context, id: tmpl.ID}
		seen[key] = true
		if prev, ok := old[key]; ok {
			merged := tmpl
			merged.TranslatorComments = append([]string(nil), prev.TranslatorComments...)
			merged.Strings = normalizedStrings(prev.Strings, tmpl.PluralID != "", nplurals)
			merged.Flags = appendFlags(tmpl.Flags, prev.Flags)
			if prev.PluralID != tmpl.PluralID {
				merged.Flags = appendFlags(merged.Flags, []string{"fuzzy"})
				merged.Previous = Previous{Context: prev.Context, ID: prev.ID, PluralID: prev.PluralID}
			}
			out.Entries = append(out.Entries, merged)
			report.Kept++
			continue
		}
		tmpl.Strings = normalizedStrings(tmpl.Strings, tmpl.PluralID != "", nplurals)
		out.Entries = append(out.Entries, tmpl)
		report.Added++
		if len(tmpl.Strings) == 0 || tmpl.Strings[0] == "" {
			report.Untranslated++
		}
	}
	for _, e := range existing.Entries {
		key := messageKey{domain: e.Domain, context: e.Context, id: e.ID}
		if e.ID == "" || seen[key] {
			continue
		}
		e.Obsolete = true
		out.Entries = append(out.Entries, e)
		report.Obsolete++
	}
	return out, report, nil
}

func headerEntry(locale string) Entry {
	header, _ := PluralHeader(locale)
	return Entry{Strings: []string{
		"Language: " + locale + "\n" +
			"Plural-Forms: " + header + "\n",
	}}
}

func firstHeader(doc *Document) (Entry, bool) {
	if doc == nil {
		return Entry{}, false
	}
	for _, e := range doc.Entries {
		if e.ID == "" && !e.Obsolete {
			header := e
			header.TranslatorComments = append([]string(nil), e.TranslatorComments...)
			header.ExtractedComments = append([]string(nil), e.ExtractedComments...)
			header.References = append([]Reference(nil), e.References...)
			header.Flags = append([]string(nil), e.Flags...)
			header.Strings = append([]string(nil), e.Strings...)
			return header, true
		}
	}
	return Entry{}, false
}

func pluralCount(doc *Document) int {
	n := 2
	if doc != nil {
		if rule, err := ParsePluralRule(doc.Header()["Plural-Forms"]); err == nil && rule.NPlurals > 0 {
			n = rule.NPlurals
		}
	}
	return n
}

func normalizedStrings(strings []string, plural bool, nplurals int) []string {
	want := 1
	if plural {
		want = nplurals
		if want <= 0 {
			want = 2
		}
	}
	out := make([]string, want)
	copy(out, strings)
	return out
}

type messageKey struct {
	domain  string
	context string
	id      string
}

func appendFlags(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, flags := range [][]string{a, b} {
		for _, flag := range flags {
			if flag == "" || seen[flag] {
				continue
			}
			seen[flag] = true
			out = append(out, flag)
		}
	}
	return out
}
