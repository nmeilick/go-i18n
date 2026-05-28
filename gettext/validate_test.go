package gettext

import (
	"strings"
	"testing"
)

func TestValidatePlaceholderMismatch(t *testing.T) {
	doc, err := ParsePO(strings.NewReader(`msgid ""
msgstr "Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "Hello {name}"
msgstr "Hallo"
`))
	if err != nil {
		t.Fatal(err)
	}
	report := Validate(doc)
	if len(report.Errors) == 0 {
		t.Fatal("expected placeholder mismatch")
	}
}

func TestValidateRejectsInvalidPlaceholderNames(t *testing.T) {
	doc, err := ParsePO(strings.NewReader(`msgid "Hello {1name}"
msgstr "Hallo {bad space}"
`))
	if err != nil {
		t.Fatal(err)
	}
	report := Validate(doc)
	if len(report.Errors) < 2 {
		t.Fatalf("expected invalid placeholder errors, got %#v", report.Errors)
	}
}

func TestValidatePluralPlaceholdersAreFormAware(t *testing.T) {
	doc, err := ParsePO(strings.NewReader(`msgid ""
msgstr "Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "One file"
msgid_plural "{n} files"
msgstr[0] "Eine Datei"
msgstr[1] "{n} Dateien"
`))
	if err != nil {
		t.Fatal(err)
	}
	if report := Validate(doc); len(report.Errors) != 0 {
		t.Fatalf("expected form-aware plural placeholders to pass, got %#v", report.Errors)
	}

	bad, err := ParsePO(strings.NewReader(`msgid ""
msgstr "Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "One file"
msgid_plural "{n} files"
msgstr[0] "Eine Datei"
msgstr[1] "{count} Dateien"
`))
	if err != nil {
		t.Fatal(err)
	}
	if report := Validate(bad); len(report.Errors) == 0 {
		t.Fatal("expected unknown plural placeholder error")
	}
}
