package main

import (
	"fmt"
	"time"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
)

func main() {
	// A runtime can be reused for many requests or jobs. This example uses an
	// in-memory catalog so the formatting APIs are easy to see.
	rt := i18n.NewRuntime(mustCatalog())

	// Profiles carry localization facts for one operation. Formatting values use
	// this profile for language, timezone, currency, and regional defaults.
	profile, err := locale.NewProfile(
		[]string{"de-DE"},
		locale.WithCurrency(locale.Currency("EUR")),
		locale.WithTimeZoneName("Europe/Berlin"),
		locale.WithFormattingRegion("DE"),
	)
	if err != nil {
		panic(err)
	}

	// Translator binds both the locale profile and the runtime snapshot.
	tr := rt.Translator(profile)
	due := time.Date(2026, 5, 16, 10, 30, 0, 0, time.UTC)
	total := 1234.5

	// Named interpolation keeps catalog strings translator-friendly and avoids
	// positional placeholders.
	fmt.Println(tr.T("Invoice {number}", i18n.Arg("number", "A-1042")))

	// Typed values render through the bound profile. CurrencyCode is explicit
	// because a financial amount's real currency should come from domain data.
	fmt.Println(tr.T("Total: {amount}", i18n.Arg("amount", i18n.Currency(total, i18n.CurrencyCode(locale.Currency("EUR"))))))
	fmt.Println(tr.T("Due: {date}", i18n.Arg("date", i18n.Date(due, i18n.Style("medium")))))
	fmt.Println(tr.T("Paid: {percent}", i18n.Arg("percent", i18n.Percent(0.74))))

	// Tn chooses the plural translation, and the runtime supplies {n} when the
	// caller does not provide it explicitly.
	fmt.Println(tr.Tn(3, "{n} line item", "{n} line items"))

	// Lookup is useful in business flows that need to record whether a message
	// was exact, fallback, missing, or rendered with diagnostics.
	lookup := tr.Lookup(i18n.Text("Total: {amount}"), i18n.Arg("amount", i18n.Currency(total, i18n.CurrencyCode(locale.Currency("EUR")))))
	fmt.Printf("lookup: %s %s\n", lookup.Status, lookup.MessageLocale)
}

func mustCatalog() *i18n.Catalog {
	// Catalog entries use source strings as message IDs. Translators can reorder
	// or reuse named placeholders such as {amount}, {date}, and {n}.
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "de", ID: "Invoice {number}", Translations: []string{"Rechnung {number}"}},
		{Locale: "de", ID: "Total: {amount}", Translations: []string{"Gesamtbetrag: {amount}"}},
		{Locale: "de", ID: "Due: {date}", Translations: []string{"Fällig: {date}"}},
		{Locale: "de", ID: "Paid: {percent}", Translations: []string{"Bezahlt: {percent}"}},
		{
			Locale:       "de",
			ID:           "{n} line item",
			PluralID:     "{n} line items",
			Translations: []string{"{n} Position", "{n} Positionen"},
		},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		panic(err)
	}
	return cat
}
