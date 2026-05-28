package extract

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTypedPlaceholders(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

import "github.com/nmeilick/go-i18n/i18n"

func render(tr interface{ T(string, ...i18n.Vars) string }, total float64) string {
	// TRANSLATORS: Invoice summary line.
	return tr.T("Invoice amount: {amount}", i18n.Vars{
		"amount": i18n.Currency(total),
	})
}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, report, err := Extract(context.Background(), Options{Roots: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Messages != 1 {
		t.Fatalf("messages = %d warnings=%#v", report.Messages, report.Warnings)
	}
	entry := doc.Entries[0]
	if entry.ID != "Invoice amount: {amount}" {
		t.Fatalf("id = %q", entry.ID)
	}
	if entry.PluralID != "" {
		t.Fatalf("singular call unexpectedly extracted plural id %q", entry.PluralID)
	}
	comments := strings.Join(entry.ExtractedComments, "\n")
	if !strings.Contains(comments, "Invoice summary") || !strings.Contains(comments, "currency") {
		t.Fatalf("comments = %q", comments)
	}
}

func TestParseKeyword(t *testing.T) {
	kw, err := ParseKeyword("WriteError:msg=5,ctx='api-error',domain='api'")
	if err != nil {
		t.Fatal(err)
	}
	if kw.Msg.Arg != 4 || kw.Context.Literal != "api-error" || kw.Domain.Literal != "api" {
		t.Fatalf("keyword = %#v", kw)
	}
	if kw.Plural.Arg != -1 || kw.Vars.Arg != -1 {
		t.Fatalf("unset roles should stay disabled: %#v", kw)
	}
}

func TestExtractKeepsDomainAndPluralIdentity(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

func render(tr interface {
	T(string) string
	Tn(int, string, string) string
}) {
	_ = app.T("Save")
	_ = admin.T("Save")
	_ = tr.Tn(1, "file", "files")
	_ = tr.Tn(1, "file", "files selected")
}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	app := newKeyword("app.T")
	app.Msg = ValueSpec{Arg: 0}
	app.Domain = ValueSpec{Literal: "app"}
	admin := newKeyword("admin.T")
	admin.Msg = ValueSpec{Arg: 0}
	admin.Domain = ValueSpec{Literal: "admin"}
	tn := newKeyword("Tn")
	tn.Count = ValueSpec{Arg: 0}
	tn.Msg = ValueSpec{Arg: 1}
	tn.Plural = ValueSpec{Arg: 2}
	doc, report, err := Extract(context.Background(), Options{
		Roots:    []string{dir},
		Keywords: []Keyword{app, admin, tn},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Messages != 4 {
		t.Fatalf("messages = %d entries=%#v warnings=%#v", report.Messages, doc.Entries, report.Warnings)
	}
	var domains []string
	var plurals []string
	for _, entry := range doc.Entries {
		if entry.ID == "Save" {
			domains = append(domains, entry.Domain)
		}
		if entry.ID == "file" {
			plurals = append(plurals, entry.PluralID)
		}
	}
	if strings.Join(domains, ",") != "admin,app" {
		t.Fatalf("domains for Save = %#v", domains)
	}
	if strings.Join(plurals, ",") != "files,files selected" {
		t.Fatalf("plural ids for file = %#v", plurals)
	}
}

func TestExtractSelectorTargetDoesNotMatchUnrelatedSelector(t *testing.T) {
	dir := t.TempDir()
	source := `package sample

func render() {
	_ = apierror.WriteError(nil, nil, nil, nil, "Translated")
	_ = other.WriteError(nil, nil, nil, nil, "Do not extract")
}
`
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	kw, err := ParseKeyword("apierror.WriteError:msg=5,ctx='api-error',domain='api'")
	if err != nil {
		t.Fatal(err)
	}
	doc, report, err := Extract(context.Background(), Options{Roots: []string{dir}, Keywords: []Keyword{kw}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Messages != 1 {
		t.Fatalf("messages = %d entries=%#v warnings=%#v", report.Messages, doc.Entries, report.Warnings)
	}
	entry := doc.Entries[0]
	if entry.ID != "Translated" || entry.Context != "api-error" || entry.Domain != "api" {
		t.Fatalf("entry = %#v", entry)
	}
}
