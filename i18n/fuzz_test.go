package i18n

import (
	"testing"

	"github.com/nmeilick/go-i18n/locale"
	"golang.org/x/text/language"
)

func FuzzInterpolate(f *testing.F) {
	profile, _ := locale.NewProfile([]string{"en"})
	f.Add("Hello {name}", "Ada")
	f.Add("{bad", "Ada")
	f.Fuzz(func(t *testing.T, tmpl, value string) {
		ctx := locale.FormatContext{Profile: profile, Locale: language.English, Policy: locale.DefaultFormatPolicy()}
		_, _ = interpolate(tmpl, Vars{"name": value}, ctx, locale.DefaultFormatter())
	})
}

func BenchmarkLookup(b *testing.B) {
	rt := benchmarkRuntime(b)
	tr := rt.Localizer("de")
	for i := 0; i < b.N; i++ {
		_ = tr.T("Hello {name}", Vars{"name": "Ada"})
	}
}

func BenchmarkTypedCurrency(b *testing.B) {
	profile, _ := locale.NewProfile([]string{"de-DE"}, locale.WithCurrency(locale.Currency("EUR")))
	rt := benchmarkRuntime(b)
	tr := rt.Translator(profile)
	for i := 0; i < b.N; i++ {
		_ = tr.T("Amount {amount}", Vars{"amount": Currency(1234.56)})
	}
}

func benchmarkRuntime(tb testing.TB) *Runtime {
	tb.Helper()
	cat, err := NewCatalog([]CatalogEntry{
		{Locale: "de", ID: "Hello {name}", Translations: []string{"Hallo {name}"}},
		{Locale: "de", ID: "Amount {amount}", Translations: []string{"Betrag {amount}"}},
	}, DefaultLocale("en"))
	if err != nil {
		tb.Fatal(err)
	}
	return NewRuntime(cat)
}
