package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
)

func ExampleMiddleware() {
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "en", ID: "Hello", Translations: []string{"Hello"}},
		{Locale: "de", ID: "Hello", Translations: []string{"Hallo"}},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		panic(err)
	}
	neg, err := locale.NewNegotiator("en", []string{"en", "de"})
	if err != nil {
		panic(err)
	}

	handler := Middleware(i18n.NewRuntime(cat), neg, Options{
		Sources:         []Source{AcceptLanguage()},
		ContentLanguage: true,
		Vary:            true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, _ := Localizer(r.Context())
		fmt.Fprint(w, tr.T("Hello"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Content-Language"))
	fmt.Println(rec.Header().Get("Vary"))
	fmt.Println(rec.Body.String())

	// Output:
	// 200
	// de
	// Accept-Language
	// Hallo
}
