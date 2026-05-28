package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/nmeilick/go-i18n/content"
	"github.com/nmeilick/go-i18n/gettext"
	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/observe"
	"github.com/nmeilick/go-i18n/web"
)

// Embedding PO files is the normal deployment path: the application ships
// catalogs inside the binary and loads them at startup.
//
//go:embed locales/*.po
var localeFS embed.FS

type checkoutResponse struct {
	Locale          string               `json:"locale"`
	ResolvedSource  string               `json:"resolved_source,omitempty"`
	Greeting        string               `json:"greeting"`
	Order           string               `json:"order"`
	Total           string               `json:"total"`
	Delivery        string               `json:"delivery"`
	Items           string               `json:"items"`
	Button          string               `json:"button"`
	Status          string               `json:"status"`
	FieldError      content.FieldMessage `json:"field_error"`
	MissingStatus   string               `json:"missing_status"`
	FormatterIssues int                  `json:"formatter_issues"`
	ObservedEvents  int                  `json:"observed_events"`
}

func main() {
	rt, collector, err := newRuntime()
	if err != nil {
		panic(err)
	}

	// The negotiator maps request locale preferences onto the locales this
	// application supports. de-CH can resolve to de because de is supported.
	neg, err := locale.NewNegotiator("en", []string{"en", "de"})
	if err != nil {
		panic(err)
	}

	// web.Middleware extracts locale candidates, builds a profile, stores a
	// request localizer in context, and can set HTTP language/cache headers.
	middleware := web.Middleware(rt, neg, web.Options{
		Sources:         []web.Source{web.AcceptLanguage()},
		Profile:         checkoutProfile,
		ContentLanguage: true,
		Vary:            true,
	})

	// Use httptest so the example is runnable from the terminal while still
	// exercising the real net/http adapter.
	req := httptest.NewRequest(http.MethodGet, "/checkout", nil)
	req.Header.Set("Accept-Language", "de-CH,de;q=0.9,en;q=0.4")
	rec := httptest.NewRecorder()

	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handlers retrieve the localizer from request context instead of reading
		// global locale state.
		tr, ok := web.Localizer(r.Context())
		if !ok {
			http.Error(w, "missing localizer", http.StatusInternalServerError)
			return
		}
		// Resolved exposes the selected locale and source metadata when handlers
		// need to report or branch on the negotiation result.
		resolved, _ := web.Resolved(r.Context())
		writeCheckout(w, tr, resolved, collector)
	})).ServeHTTP(rec, req)

	fmt.Printf("HTTP %d\n", rec.Code)
	fmt.Printf("Content-Language: %s\n", rec.Header().Get("Content-Language"))
	fmt.Printf("Vary: %s\n", rec.Header().Get("Vary"))
	fmt.Println(rec.Body.String())
}

func newRuntime() (*i18n.Runtime, *observe.Collector, error) {
	// gettext.LoadFS parses all embedded PO files and compiles active entries
	// into an immutable i18n catalog. Fuzzy and obsolete entries are not served.
	cat, err := gettext.LoadFS(localeFS, "locales/*.po", gettext.DefaultLocale("en"))
	if err != nil {
		return nil, nil, err
	}

	// Observer events are optional. This collector makes missing translations
	// and formatter diagnostics visible in the example response.
	collector := observe.NewCollector()
	return i18n.NewRuntime(cat, i18n.WithObserver(collector)), collector, nil
}

func checkoutProfile(_ *http.Request, resolved locale.Resolved) (locale.Profile, error) {
	// ProfileSource lets application code attach timezone, display currency, and
	// formatting-region facts from account settings or other trusted sources.
	if resolved.Locale == "de" {
		return locale.NewProfile(
			[]string{resolved.Locale},
			locale.WithCurrency(locale.Currency("EUR")),
			locale.WithTimeZoneName("Europe/Berlin"),
			locale.WithFormattingRegion("DE"),
		)
	}
	return locale.NewProfile(
		[]string{resolved.Locale},
		locale.WithCurrency(locale.Currency("USD")),
		locale.WithTimeZoneName("America/New_York"),
		locale.WithFormattingRegion("US"),
	)
}

func writeCheckout(w http.ResponseWriter, tr *i18n.Localizer, resolved locale.Resolved, collector *observe.Collector) {
	due := time.Date(2026, 5, 16, 10, 30, 0, 0, time.UTC)

	// Lookup returns rendered text plus status and diagnostics. It is useful for
	// values that may need audit or fallback reporting.
	total := tr.Lookup(i18n.Text("Checkout total: {amount}"), i18n.Arg(
		"amount",
		i18n.Currency(1499.95, i18n.CurrencyCode(tr.Profile().Currency())),
	))
	delivery := tr.Lookup(i18n.Text("Delivery date: {date}"), i18n.Arg("date", i18n.Date(due, i18n.Style("medium"))))
	missing := tr.Lookup(i18n.Text("This checkout notice is intentionally missing"))

	payload := checkoutResponse{
		Locale:         tr.Locale(),
		ResolvedSource: resolved.Source,
		// T handles normal singular messages and named interpolation.
		Greeting: tr.T("Hello {name}", i18n.Arg("name", "Ada")),
		Order:    tr.T("Order {number}", i18n.Arg("number", "A-1042")),
		Total:    total.Text,
		Delivery: delivery.Text,
		// Tn chooses the plural form and provides {n}.
		Items: tr.Tn(3, "One item ready", "{n} items ready"),
		// Tc adds gettext message context for identical source text with
		// different meanings.
		Button: tr.Tc("button", "Pay now"),
		Status: tr.Tc("status", "Pay now"),
		// content.ProjectField is a small adapter for public API/validation payloads.
		// Diagnostic lookup metadata is available through ProjectFieldWithLookup.
		FieldError:      content.ProjectField(tr, "email", "checkout.email", "required", i18n.Text("Email is required")),
		MissingStatus:   string(missing.Status),
		FormatterIssues: len(total.Diagnostics) + len(delivery.Diagnostics),
		ObservedEvents:  len(collector.Events()),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
