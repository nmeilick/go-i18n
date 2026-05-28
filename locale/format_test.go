package locale

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nmeilick/go-i18n/observe"
	"golang.org/x/text/language"
)

func TestCLDRFormatterCurrencyDateListAndUnit(t *testing.T) {
	p, err := NewProfile([]string{"de-CH"}, WithCurrency(Currency("CHF")), WithTimeZoneName("Europe/Zurich"))
	if err != nil {
		t.Fatal(err)
	}
	f := DefaultFormatter()
	ctx := FormatContext{Profile: p, Locale: language.MustParse("de-CH"), Policy: DefaultFormatPolicy()}
	amount, ds := f.FormatCurrency(ctx, CurrencySpec{Value: 1234.5, Code: Currency("CHF"), CodeSource: CurrencyExplicit})
	if len(ds) != 0 {
		t.Fatalf("currency diagnostics = %#v", ds)
	}
	if !strings.Contains(amount, "CHF") || !strings.Contains(amount, "1’234") {
		t.Fatalf("currency = %q", amount)
	}
	date, ds := f.FormatDateTime(ctx, DateTimeSpec{Value: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC), Kind: DateOnly, Width: "medium"})
	if len(ds) != 0 {
		t.Fatalf("date diagnostics = %#v", ds)
	}
	if date != "13.05.2026" {
		t.Fatalf("date = %q", date)
	}
	list, ds := f.FormatList(ctx, ListSpec{Items: []string{"A", "B", "C"}, Type: ListStandard, Width: "long"})
	if !hasFormatDiagnostic(ds, "unsupported_list_patterns") {
		t.Fatalf("list diagnostics = %#v, want fallback diagnostic", ds)
	}
	if list != "A, B und C" {
		t.Fatalf("list = %q", list)
	}
	unit, ds := f.FormatUnit(ctx, UnitSpec{Value: 2, Unit: "length-meter", Width: UnitLong})
	if !hasFormatDiagnostic(ds, "unsupported_unit") {
		t.Fatalf("unit diagnostics = %#v, want fallback diagnostic", ds)
	}
	if unit != "2 length-meter" {
		t.Fatalf("unit = %q", unit)
	}
	compact, ds := f.FormatNumber(ctx, NumberSpec{Value: 1200000, Kind: NumberCompact})
	if !hasFormatDiagnostic(ds, "unsupported_compact_number") {
		t.Fatalf("compact diagnostics = %#v, want fallback diagnostic", ds)
	}
	if !strings.Contains(compact, "1’200’000") {
		t.Fatalf("compact = %q", compact)
	}
}

func TestCLDRFormatterStrictFailures(t *testing.T) {
	p, err := NewProfile([]string{"en"}, WithCurrency(Currency("USD")))
	if err != nil {
		t.Fatal(err)
	}
	f := DefaultFormatter()
	ctx := FormatContext{Profile: p, Locale: language.English, Policy: StrictFormatPolicy()}
	if text, ds := f.FormatCurrency(ctx, CurrencySpec{Value: 10}); text != "" || len(ds) == 0 || ds[0].Code != "profile_currency_forbidden" {
		t.Fatalf("strict profile currency = %q %#v", text, ds)
	}
	if text, ds := f.FormatDateTime(ctx, DateTimeSpec{Value: time.Now(), Kind: DateOnly, Calendar: "buddhist"}); text != "" || len(ds) == 0 || ds[0].Code != "unsupported_calendar" {
		t.Fatalf("strict calendar = %q %#v", text, ds)
	}
	if text, ds := f.FormatNumber(ctx, NumberSpec{Value: 1200, Kind: NumberCompact}); text != "" || len(ds) == 0 || ds[0].Code != "unsupported_compact_number" {
		t.Fatalf("strict compact = %q %#v", text, ds)
	}
	if text, ds := f.FormatUnit(ctx, UnitSpec{Value: 2, Unit: "length-meter"}); text != "" || len(ds) == 0 || ds[0].Code != "unsupported_unit" {
		t.Fatalf("strict unit = %q %#v", text, ds)
	}
	if text, ds := f.FormatDuration(ctx, DurationSpec{Value: time.Hour}); text != "" || len(ds) == 0 || ds[0].Code != "unsupported_unit" {
		t.Fatalf("strict duration = %q %#v", text, ds)
	}
}

func TestCLDRFormatterNegativeCurrencyFallbackKeepsNumber(t *testing.T) {
	f := DefaultFormatter()
	p, err := NewProfile([]string{"en"}, WithCurrency(Currency("USD")))
	if err != nil {
		t.Fatal(err)
	}
	ctx := FormatContext{Profile: p, Locale: language.English, Policy: DefaultFormatPolicy()}
	text, ds := f.FormatCurrency(ctx, CurrencySpec{Value: -12.5, Code: Currency("USD"), CodeSource: CurrencyExplicit})
	if len(ds) != 0 {
		t.Fatalf("currency diagnostics = %#v", ds)
	}
	if !strings.Contains(text, "12.50") {
		t.Fatalf("negative currency lost number: %q", text)
	}
}

func TestCLDRFormatterLenientDurationReportsFallbacks(t *testing.T) {
	f := DefaultFormatter()
	p, err := NewProfile([]string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := FormatContext{Profile: p, Locale: language.English, Policy: DefaultFormatPolicy()}
	text, ds := f.FormatDuration(ctx, DurationSpec{Value: 90 * time.Minute})
	if text == "" {
		t.Fatal("duration fallback returned empty text")
	}
	for _, code := range []string{"unsupported_unit", "unsupported_list_patterns", "unsupported_duration_units"} {
		if !hasFormatDiagnostic(ds, code) {
			t.Fatalf("duration diagnostics = %#v, want %s", ds, code)
		}
	}
}

func TestCLDRFormatterUsesProfileNumberingSystemForArabic(t *testing.T) {
	f := DefaultFormatter()
	tm := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)

	latn, err := NewProfile([]string{"ar"}, WithFormattingRegion("EG"), WithNumberingSystem("latn"), WithCurrency(Currency("EGP")))
	if err != nil {
		t.Fatal(err)
	}
	latnCtx := FormatContext{Profile: latn, Locale: language.MustParse("ar"), Policy: DefaultFormatPolicy()}
	amount, ds := f.FormatCurrency(latnCtx, CurrencySpec{Value: 1234.5, Code: Currency("EGP"), CodeSource: CurrencyExplicit})
	if !hasFormatDiagnostic(ds, "currency_symbol_unavailable") {
		t.Fatalf("currency diagnostics = %#v, want symbol fallback", ds)
	}
	if !strings.Contains(amount, "1,234.50") || strings.Contains(amount, "١") {
		t.Fatalf("latn amount = %q", amount)
	}
	date, ds := f.FormatDateTime(latnCtx, DateTimeSpec{Value: tm, Kind: DateOnly, Width: "medium"})
	if len(ds) != 0 {
		t.Fatalf("latn date diagnostics = %#v", ds)
	}
	if !strings.Contains(date, "16") || strings.Contains(date, "١٦") {
		t.Fatalf("latn date = %q", date)
	}

	arab, err := NewProfile([]string{"ar"}, WithFormattingRegion("EG"), WithNumberingSystem("arab"), WithCurrency(Currency("EGP")))
	if err != nil {
		t.Fatal(err)
	}
	arabCtx := FormatContext{Profile: arab, Locale: language.MustParse("ar"), Policy: DefaultFormatPolicy()}
	amount, _ = f.FormatCurrency(arabCtx, CurrencySpec{Value: 1234.5, Code: Currency("EGP"), CodeSource: CurrencyExplicit})
	if !strings.Contains(amount, "١٬٢٣٤٫٥٠") {
		t.Fatalf("arab amount = %q", amount)
	}
	date, ds = f.FormatDateTime(arabCtx, DateTimeSpec{Value: tm, Kind: DateOnly, Width: "medium"})
	if len(ds) != 0 {
		t.Fatalf("arab date diagnostics = %#v", ds)
	}
	if !strings.Contains(date, "١٦") || strings.Contains(date, "16") {
		t.Fatalf("arab date = %q", date)
	}
}

func TestCLDRFormatterObserverReceivesFallbackDiagnostics(t *testing.T) {
	collector := observe.NewCollector()
	f := DefaultFormatter()
	p, err := NewProfile([]string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := FormatContext{
		Profile:        p,
		Locale:         language.English,
		Policy:         DefaultFormatPolicy(),
		Observer:       collector,
		ObserveContext: observe.WithAttrs(context.Background(), observe.Bounded("route", "job")),
	}
	_, ds := f.FormatUnit(ctx, UnitSpec{Value: 2, Unit: "length-meter", Width: UnitLong})
	if !hasFormatDiagnostic(ds, "unsupported_unit") {
		t.Fatalf("format diagnostics = %#v", ds)
	}
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Name != "locale.format" || events[0].Code != "unsupported_unit" {
		t.Fatalf("event = %#v", events[0])
	}
	if got := attrValue(events[0].Attrs, "route"); got != "job" {
		t.Fatalf("route attr = %#v", got)
	}

	collector.Reset()
	_, ds = f.FormatDuration(ctx, DurationSpec{Value: 90 * time.Minute})
	if !hasFormatDiagnostic(ds, "unsupported_duration_units") {
		t.Fatalf("duration diagnostics = %#v", ds)
	}
	events = collector.Events()
	if len(events) != 1 || events[0].Code != "unsupported_duration_units" {
		t.Fatalf("duration events = %#v", events)
	}
}

func hasFormatDiagnostic(ds []FormatDiagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func attrValue(attrs []observe.Attr, key string) any {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return nil
}
