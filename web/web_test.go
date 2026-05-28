package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/observe"
)

func TestMiddlewareHeadersAndContext(t *testing.T) {
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "de", ID: "Hello", Translations: []string{"Hallo"}},
		{Locale: "en", ID: "Hello", Translations: []string{"Hello"}},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		t.Fatal(err)
	}
	rt := i18n.NewRuntime(cat)
	neg, err := locale.NewNegotiator("en", []string{"en", "de"})
	if err != nil {
		t.Fatal(err)
	}
	mw := Middleware(rt, neg, Options{
		Sources:         []Source{AcceptLanguage()},
		ContentLanguage: true,
		Vary:            true,
	})
	var got string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, ok := Localizer(r.Context())
		if !ok {
			t.Fatal("missing localizer")
		}
		got = tr.T("Hello")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got != "Hallo" {
		t.Fatalf("translation = %q", got)
	}
	if rec.Header().Get("Content-Language") != "de" {
		t.Fatalf("content language = %q", rec.Header().Get("Content-Language"))
	}
	if rec.Header().Get("Vary") != "Accept-Language" {
		t.Fatalf("vary = %q", rec.Header().Get("Vary"))
	}
}

func TestMiddlewareCookiePrivate(t *testing.T) {
	cat, _ := i18n.NewCatalog(nil)
	rt := i18n.NewRuntime(cat)
	neg, _ := locale.NewNegotiator("en", []string{"en", "de"})
	mw := Middleware(rt, neg, Options{
		Sources:                   []Source{Cookie("locale")},
		ContentLanguage:           true,
		PrivateWhenUserSourceWins: true,
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "locale", Value: "de"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Cache-Control") != "private" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestAppendVaryPreservesStar(t *testing.T) {
	h := http.Header{}
	h.Set("Vary", "*")
	AppendVary(h, "Accept-Language")
	if h.Get("Vary") != "*" {
		t.Fatalf("vary = %q", h.Get("Vary"))
	}
}

func TestMiddlewareNilDependenciesDoNotPanic(t *testing.T) {
	mw := Middleware(nil, nil, Options{ContentLanguage: true})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := Localizer(r.Context()); !ok {
			t.Fatal("missing localizer")
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMiddlewareUsesProfileErrorHandler(t *testing.T) {
	errProfile := errors.New("bad profile")
	mw := Middleware(nil, nil, Options{
		Profile: func(*http.Request, locale.Resolved) (locale.Profile, error) {
			return locale.Profile{}, errProfile
		},
		ProfileError: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, errProfile) {
				t.Fatalf("error = %v", err)
			}
			http.Error(w, "custom profile error", http.StatusBadRequest)
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "custom profile error") {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestDirectLocaleSourcesDropOversizedValues(t *testing.T) {
	huge := strings.Repeat("x", maxDirectLocaleBytes+1)
	req := httptest.NewRequest(http.MethodGet, "/?lang="+huge, nil)
	req.Header.Set("X-Locale", huge)
	req.AddCookie(&http.Cookie{Name: "locale", Value: huge})
	for name, source := range map[string]Source{
		"query":  Query("lang"),
		"header": Header("X-Locale"),
		"cookie": Cookie("locale"),
	} {
		if got := source(req); len(got) != 0 {
			t.Fatalf("%s source accepted oversized locale: %#v", name, got)
		}
	}
}

func TestMiddlewarePropagatesRequestObserveContext(t *testing.T) {
	collector := observe.NewCollector()
	cat, _ := i18n.NewCatalog(nil)
	rt := i18n.NewRuntime(cat, i18n.WithObserver(collector))
	neg, _ := locale.NewNegotiator("en", []string{"en"})
	mw := Middleware(rt, neg, Options{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, ok := Localizer(r.Context())
		if !ok {
			t.Fatal("missing localizer")
		}
		_ = tr.T("Missing")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(observe.WithAttrs(req.Context(), observe.Bounded("route", "GET /")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if got := webAttrValue(events[0].Attrs, "route"); got != "GET /" {
		t.Fatalf("route attr = %#v", got)
	}
}

func webAttrValue(attrs []observe.Attr, key string) any {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return nil
}
