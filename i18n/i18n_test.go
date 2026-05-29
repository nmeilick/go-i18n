package i18n

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/observe"
)

func testRuntime(t *testing.T) *Runtime {
	t.Helper()
	cat, err := NewCatalog([]CatalogEntry{
		{Locale: "de", ID: "Hello {name}", Translations: []string{"Hallo {name}"}},
		{Locale: "de", Context: "button", ID: "Open", Translations: []string{"Öffnen"}},
		{Locale: "de", ID: "One file", PluralID: "{n} files", Translations: []string{"Eine Datei", "{n} Dateien"}},
		{Locale: "en", ID: "Hello {name}", Translations: []string{"Hello {name}"}},
	}, DefaultLocale("en"))
	if err != nil {
		t.Fatal(err)
	}
	return NewRuntime(cat)
}

func TestTAndLookup(t *testing.T) {
	tr := testRuntime(t).Localizer("de")
	got := tr.T("Hello {name}", Vars{"name": "Ada"})
	if got != "Hallo Ada" {
		t.Fatalf("T = %q", got)
	}
	res := tr.Lookup(Text("Hello {name}"), Vars{"name": "Ada"})
	if res.Status != StatusExact || res.MessageLocale != "de" {
		t.Fatalf("result = %#v", res)
	}
}

func TestFallbackAndMissing(t *testing.T) {
	tr := testRuntime(t).Localizer("fr")
	res := tr.Lookup(Text("Hello {name}"), Vars{"name": "Ada"})
	if res.Text != "Hello Ada" || res.Status != StatusExact || res.ResolvedLocale != "en" {
		t.Fatalf("default resolved result = %#v", res)
	}
	missing := tr.Lookup(Text("Missing {x}"))
	if missing.Status != StatusMissing || len(missing.Diagnostics) != 2 {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestPluralAndContext(t *testing.T) {
	tr := testRuntime(t).Localizer("de")
	if got := tr.Tn(3, "One file", "{n} files"); got != "3 Dateien" {
		t.Fatalf("plural = %q", got)
	}
	if got := tr.Tc("button", "Open"); got != "Öffnen" {
		t.Fatalf("context = %q", got)
	}
}

func TestTypedFormatting(t *testing.T) {
	p, err := locale.NewProfile([]string{"de-DE"}, locale.WithCurrency(locale.Currency("EUR")), locale.WithTimeZoneName("Europe/Berlin"))
	if err != nil {
		t.Fatal(err)
	}
	tr := testRuntime(t).Translator(p)
	paid := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	got := tr.T("Amount {amount} due {due}", Vars{
		"amount": Currency(1234.5),
		"due":    Date(paid),
	})
	if got != "Amount 1.234,50\u00a0€ due 13.05.2026" {
		t.Fatalf("typed formatting = %q", got)
	}
}

func TestRuntimeSwapRaceSurface(t *testing.T) {
	rt := testRuntime(t)
	next, err := NewCatalog([]CatalogEntry{{Locale: "de", ID: "Hello", Translations: []string{"Guten Tag"}}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = rt.Localizer("de").T("Hello")
			}
		}()
	}
	if _, err := rt.Swap(next); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestRuntimeSwapObserver(t *testing.T) {
	collector := observe.NewCollector()
	rt := NewRuntime(testRuntime(t).Catalog(), WithObserver(collector))
	next, err := NewCatalog([]CatalogEntry{{Locale: "de", ID: "Hello", Translations: []string{"Guten Tag"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := observe.WithAttrs(context.Background(), observe.Bounded("job", "reload"))
	if _, err := rt.SwapContext(ctx, next); err != nil {
		t.Fatal(err)
	}
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Name != "i18n.catalog.swap" || events[0].Code != "catalog_swapped" {
		t.Fatalf("event = %#v", events[0])
	}
	if got := attrValue(events[0].Attrs, "job"); got != "reload" {
		t.Fatalf("job attr = %#v", got)
	}
}

func TestCatalogNormalizesDefaultLocaleAndChecksumOrder(t *testing.T) {
	entries := []CatalogEntry{
		{Locale: "de-DE", ID: "Hello", Translations: []string{"Hallo"}},
		{Locale: "en-US", ID: "Hello", Translations: []string{"Hello"}},
	}
	a, err := NewCatalog(entries, DefaultLocale("en_US"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewCatalog([]CatalogEntry{entries[1], entries[0]}, DefaultLocale("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if a.snapshot.defaultLocale != "en-US" {
		t.Fatalf("default locale = %q", a.snapshot.defaultLocale)
	}
	if a.Version() != b.Version() {
		t.Fatalf("checksum depends on entry order: %s != %s", a.Version(), b.Version())
	}
	tr := NewRuntime(a).Localizer("fr")
	if got := tr.T("Hello"); got != "Hello" {
		t.Fatalf("default locale fallback = %q", got)
	}
}

func TestCatalogRejectsPluralRuleConflictsAndDuplicates(t *testing.T) {
	ruleA := EnglishPluralRule()
	ruleB := PluralRule{NPlurals: 1, Header: "nplurals=1; plural=0;", Select: func(int64) int { return 0 }}
	if _, err := NewCatalog([]CatalogEntry{
		{Locale: "en", ID: "One", PluralRule: ruleA, Translations: []string{"One"}},
		{Locale: "en", ID: "Two", PluralRule: ruleB, Translations: []string{"Two"}},
	}); err == nil {
		t.Fatal("expected plural rule conflict")
	}
	if _, err := NewCatalog([]CatalogEntry{
		{Locale: "en", ID: "One", Translations: []string{"One"}},
		{Locale: "en", ID: "One", Translations: []string{"Uno"}},
	}); err == nil {
		t.Fatal("expected duplicate message error")
	}
	if _, err := NewCatalog([]CatalogEntry{
		{Locale: "en", ID: "One", PluralID: "Many", Translations: []string{"One", "Many"}},
		{Locale: "en", ID: "One", PluralID: "Several", Translations: []string{"One", "Several"}},
	}); err == nil {
		t.Fatal("expected plural id conflict")
	}
	if _, err := NewCatalog([]CatalogEntry{{
		Locale: "en", ID: "One", PluralID: "Many", Translations: []string{"One"},
		PluralRule: EnglishPluralRule(),
	}}); err == nil {
		t.Fatal("expected plural translation count error")
	}
}

func TestStrictFormatterDiagnosticsPromoteLookupStatusToError(t *testing.T) {
	rt := NewRuntime(testRuntime(t).Catalog(), WithFormatPolicy(locale.StrictFormatPolicy()))
	tr := rt.Localizer("de")
	res := tr.Lookup(Text("Hello {name}"), Arg("name", Unit(2, "length-meter")))
	if res.Status != StatusError {
		t.Fatalf("status = %s diagnostics=%#v", res.Status, res.Diagnostics)
	}
	if len(res.Diagnostics) == 0 || res.Diagnostics[0].Severity != "error" {
		t.Fatalf("strict diagnostics = %#v", res.Diagnostics)
	}
}

func TestProcessDefault(t *testing.T) {
	old := defaultRuntime.Load()
	defer defaultRuntime.Store(old)
	SetDefault(testRuntime(t))
	if got := Locale("de").T("Hello {name}", Arg("name", "Ada")); got != "Hallo Ada" {
		t.Fatalf("default = %q", got)
	}
}

func TestDiagnosticCollector(t *testing.T) {
	sink := NewCollectorSink()
	rt := testRuntime(t)
	rt.sink = sink
	tr := rt.Localizer("de")
	_ = tr.T("Hello {name}")
	got := sink.Diagnostics()
	if len(got) == 0 || got[0].Code != "missing_variable" {
		t.Fatalf("diagnostics = %#v", got)
	}
	got[0].Code = "mutated"
	if sink.Diagnostics()[0].Code != "missing_variable" {
		t.Fatal("collector returned mutable diagnostics")
	}
}

func TestLookupObserverReceivesFullMetadataWithoutVariableValues(t *testing.T) {
	collector := observe.NewCollector()
	rt := NewRuntime(testRuntime(t).Catalog(), WithObserver(collector))
	ctx := observe.WithAttrs(context.Background(), observe.Private("tenant_id", "tenant-1"))
	tr := rt.Localizer("de").WithContext(ctx)
	_ = tr.T("Missing {x}", Arg("x", "secret-value"))

	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	event := events[0]
	if event.Name != "i18n.lookup" || event.Code != "missing_translation" || event.Outcome != observe.OutcomeMissing {
		t.Fatalf("event = %#v", event)
	}
	if got := attrValue(event.Attrs, "i18n.message_id"); got != "Missing {x}" {
		t.Fatalf("message id attr = %#v", got)
	}
	if got := attrValue(event.Attrs, "i18n.status"); got != "missing" {
		t.Fatalf("status attr = %#v", got)
	}
	if got := attrValue(event.Attrs, "tenant_id"); got != "tenant-1" {
		t.Fatalf("tenant attr = %#v", got)
	}
	if containsAttrValue(event.Attrs, "secret-value") {
		t.Fatalf("event captured interpolation value: %#v", event.Attrs)
	}
}

func TestLookupObserverMessageIdentityPolicyHash(t *testing.T) {
	collector := observe.NewCollector()
	rt := NewRuntime(testRuntime(t).Catalog(),
		WithObserver(collector),
		WithObservePolicy(ObservePolicy{MessageIdentity: MessageIdentityHash}),
	)
	_ = rt.Localizer("de").T("Missing")
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if got := attrValue(events[0].Attrs, "i18n.message_id"); got != nil {
		t.Fatalf("raw message id emitted with hash policy: %#v", events[0].Attrs)
	}
	if got := attrValue(events[0].Attrs, "i18n.message_fingerprint"); got == nil {
		t.Fatalf("missing fingerprint: %#v", events[0].Attrs)
	}
}

func TestLookupObserverMessageIdentityPolicyOmit(t *testing.T) {
	collector := observe.NewCollector()
	rt := NewRuntime(testRuntime(t).Catalog(),
		WithObserver(collector),
		WithObservePolicy(ObservePolicy{MessageIdentity: MessageIdentityOmit}),
	)
	_ = rt.Localizer("de").T("Missing")
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	for _, key := range []string{"i18n.message_id", "i18n.message_context", "i18n.plural_id", "i18n.message_fingerprint"} {
		if got := attrValue(events[0].Attrs, key); got != nil {
			t.Fatalf("%s emitted with omit policy: %#v", key, events[0].Attrs)
		}
	}
}

func TestLookupObserverReceivesFormatterDiagnosticsForExactLookup(t *testing.T) {
	collector := observe.NewCollector()
	rt := NewRuntime(testRuntime(t).Catalog(), WithObserver(collector))
	tr := rt.Localizer("de")
	_ = tr.T("Hello {name}", Arg("name", Unit(2, "length-meter")))
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Code != "formatter_unit_unavailable" || attrValue(events[0].Attrs, "i18n.status") != "exact" {
		t.Fatalf("event = %#v", events[0])
	}
}

func TestLookupObserverPanicDoesNotBreakTranslation(t *testing.T) {
	rt := NewRuntime(testRuntime(t).Catalog(), WithObserver(observe.ObserverFunc(func(context.Context, observe.Event) {
		panic("observer failed")
	})))
	if got := rt.Localizer("de").T("Missing"); got != "Missing" {
		t.Fatalf("translation = %q", got)
	}
}

func attrValue(attrs []observe.Attr, key string) any {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return nil
}

func containsAttrValue(attrs []observe.Attr, value string) bool {
	for _, attr := range attrs {
		if attr.Value == value {
			return true
		}
		if values, ok := attr.Value.([]string); ok {
			for _, item := range values {
				if item == value {
					return true
				}
			}
		}
	}
	return false
}
