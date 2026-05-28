package gettext

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nmeilick/go-i18n/i18n"
)

const samplePO = `msgid ""
msgstr ""
"Language: de\n"
"Plural-Forms: nplurals=2; plural=(n != 1);\n"

# Translator owned
#. Placeholder {n}: number
#: app.go:10
#, go-i18n-brace-format
msgid "One file"
msgid_plural "{n} files"
msgstr[0] "Eine Datei"
msgstr[1] "{n} Dateien"

#. Domain: billing
msgid "Invoice"
msgstr "Rechnung"

#, fuzzy
msgid "Fuzzy"
msgstr "Unscharf"

#~ msgid "Old"
#~ msgstr "Alt"
`

func TestParseWriteRoundTripSemantic(t *testing.T) {
	doc, err := ParsePO(strings.NewReader(samplePO))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 5 {
		t.Fatalf("entries = %d", len(doc.Entries))
	}
	if doc.Header()["Language"] != "de" {
		t.Fatalf("header = %#v", doc.Header())
	}
	var out bytes.Buffer
	if err := WritePO(&out, doc); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"#. Placeholder {n}: number", "#, go-i18n-brace-format", "#~ msgid \"Old\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("written PO missing %q:\n%s", want, text)
		}
	}
	for _, entry := range doc.Entries {
		if entry.ID == "Invoice" && entry.Domain != "billing" {
			t.Fatalf("domain = %q", entry.Domain)
		}
	}
}

func TestPluralRule(t *testing.T) {
	rule, err := ParsePluralRule("nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : n != 0 ? 1 : 2);")
	if err != nil {
		t.Fatal(err)
	}
	if got := rule.Select(1); got != 0 {
		t.Fatalf("plural(1) = %d", got)
	}
	if got := rule.Select(2); got != 1 {
		t.Fatalf("plural(2) = %d", got)
	}
	if got := rule.Select(0); got != 2 {
		t.Fatalf("plural(0) = %d", got)
	}
}

func TestPluralRuleRejectsOversizedAndDeepExpressions(t *testing.T) {
	if _, err := ParsePluralRule("nplurals=33; plural=0;"); err == nil {
		t.Fatal("expected nplurals limit error")
	}
	if _, err := ParsePluralRule("nplurals=2; plural=" + strings.Repeat("n?", maxPluralDepth+2) + "0" + strings.Repeat(":0", maxPluralDepth+2) + ";"); err == nil {
		t.Fatal("expected depth limit error")
	}
	if _, err := ParsePluralRule("nplurals=2; plural=" + strings.Repeat("n+", maxPluralTokens+2) + "0;"); err == nil {
		t.Fatal("expected token limit error")
	}
}

func TestParsePORejectsOversizedPluralIndexes(t *testing.T) {
	_, err := ParsePO(strings.NewReader(`msgid "One file"
msgid_plural "{n} files"
msgstr[999999] "too far"
`))
	if err == nil {
		t.Fatal("expected plural index limit error")
	}
}

func TestWritePOReturnsWriterErrors(t *testing.T) {
	errBoom := errors.New("boom")
	w := errWriter{err: errBoom}
	err := WritePO(w, &Document{Entries: []Entry{{Previous: Previous{ID: "Old"}, ID: "New", Strings: []string{"Neu"}}}})
	if !errors.Is(err, errBoom) {
		t.Fatalf("error = %v, want %v", err, errBoom)
	}
}

type errWriter struct {
	err error
}

func (w errWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestLoadFSExcludesFuzzyAndObsolete(t *testing.T) {
	fsys := fstest.MapFS{"de.po": {Data: []byte(samplePO)}}
	cat, err := LoadFS(fsys, "*.po")
	if err != nil {
		t.Fatal(err)
	}
	tr := i18n.NewRuntime(cat).Localizer("de")
	if got := tr.Tn(2, "One file", "{n} files"); got != "2 Dateien" {
		t.Fatalf("plural = %q", got)
	}
	if got := tr.T("Fuzzy"); got != "Fuzzy" {
		t.Fatalf("fuzzy should not compile, got %q", got)
	}
	if got := tr.WithDomain("billing").T("Invoice"); got != "Rechnung" {
		t.Fatalf("domain translation = %q", got)
	}
}

func TestMergePreservesTranslationAndObsoletesRemoved(t *testing.T) {
	template := &Document{Entries: []Entry{{ID: "Keep"}, {ID: "New"}}}
	existing := &Document{Entries: []Entry{
		{Strings: []string{"Language: de\nPlural-Forms: nplurals=2; plural=(n != 1);\n"}},
		{ID: "Keep", Strings: []string{"Behalten"}},
		{ID: "Old", Strings: []string{"Alt"}},
	}}
	merged, report, err := Merge(template, existing)
	if err != nil {
		t.Fatal(err)
	}
	if report.Kept != 1 || report.Added != 1 || report.Obsolete != 1 {
		t.Fatalf("report = %#v", report)
	}
	if got := merged.Header()["Language"]; got != "de" {
		t.Fatalf("header language = %q", got)
	}
	var foundObsolete bool
	for _, e := range merged.Entries {
		if e.ID == "Old" && e.Obsolete {
			foundObsolete = true
		}
	}
	if !foundObsolete {
		t.Fatalf("merged did not obsolete old entry: %#v", merged.Entries)
	}
}

func TestMergeNormalizesPluralStringCountFromHeader(t *testing.T) {
	template := &Document{Entries: []Entry{{ID: "One item", PluralID: "{n} items", Strings: []string{"", ""}}}}
	existing := &Document{Entries: []Entry{{
		Strings: []string{"Language: ar\nPlural-Forms: nplurals=6; plural=(n==0 ? 0 : n==1 ? 1 : n==2 ? 2 : n%100>=3 && n%100<=10 ? 3 : n%100>=11 && n%100<=99 ? 4 : 5);\n"},
	}}}
	merged, _, err := Merge(template, existing)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range merged.Entries {
		if entry.ID == "One item" && len(entry.Strings) != 6 {
			t.Fatalf("plural strings = %d, want 6", len(entry.Strings))
		}
	}
}

func TestMergeLocaleInitializesHeaderAndMarksPluralIDChangesFuzzy(t *testing.T) {
	template := &Document{Entries: []Entry{{ID: "One file", PluralID: "{n} files", Strings: []string{"", ""}}}}
	existing := &Document{Entries: []Entry{{ID: "One file", PluralID: "Many files", Strings: []string{"Eine Datei", "Viele Dateien"}}}}
	merged, _, err := MergeLocale(template, existing, "de")
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.Header()["Language"]; got != "de" {
		t.Fatalf("language header = %q", got)
	}
	var entry Entry
	for _, candidate := range merged.Entries {
		if candidate.ID == "One file" {
			entry = candidate
			break
		}
	}
	if !hasFlag(entry, "fuzzy") {
		t.Fatalf("plural id change was not marked fuzzy: %#v", entry.Flags)
	}
	if entry.Previous.PluralID != "Many files" {
		t.Fatalf("previous plural id = %#v", entry.Previous)
	}
}
