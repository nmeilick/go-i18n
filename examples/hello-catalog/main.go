package main

import (
	"fmt"

	"github.com/nmeilick/go-i18n/i18n"
)

func main() {
	// NewCatalog compiles translation entries into an immutable catalog. English
	// is the default/source locale, so only the German translations need entries.
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "de", ID: "Hello {name}", Translations: []string{"Hallo {name}"}},
		{Locale: "de", ID: "Save", Translations: []string{"Speichern"}},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		panic(err)
	}

	// A Runtime owns catalog lookup state. Localizer("de") binds calls to the
	// requested response language.
	tr := i18n.NewRuntime(cat).Localizer("de")

	// T renders a source-string message. Arg adds one named interpolation value.
	fmt.Println(tr.T("Hello {name}", i18n.Arg("name", "Ada")))
	fmt.Println(tr.T("Save"))

	// Lookup returns metadata instead of only text, so callers can detect exact,
	// fallback, missing, or error states without string comparisons.
	res := tr.Lookup(i18n.Text("Missing example"))
	fmt.Printf("missing status: %s\n", res.Status)
}
