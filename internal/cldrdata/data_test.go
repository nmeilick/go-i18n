package cldrdata

import (
	"strings"
	"testing"
)

func TestDefaultProviderSwissData(t *testing.T) {
	p := Default()
	meta := p.Metadata()
	if meta.CLDRVersion != "48" || meta.UnicodeVersion == "" {
		t.Fatalf("metadata = %#v", meta)
	}
	if strings.HasPrefix(meta.SourceIdentity, "/") || strings.Contains(meta.SourceIdentity, "\\") {
		t.Fatalf("source identity is not repository-relative: %q", meta.SourceIdentity)
	}
	rec, ok := p.Locale("de-CH")
	if !ok {
		t.Fatal("de-CH missing")
	}
	if rec.NumberingSystem != "latn" || rec.DateFormats[WidthMedium] == "" {
		t.Fatalf("locale record = %#v", rec)
	}
	region, ok := p.RegionDefaults("CH")
	if !ok || region.Currency != "CHF" || region.TimeZone == "" {
		t.Fatalf("CH defaults = %#v ok=%v", region, ok)
	}
	if fraction := p.CurrencyFraction("JPY"); fraction.Digits != 0 {
		t.Fatalf("JPY fraction = %#v", fraction)
	}
	if sym, ok := p.CurrencySymbol("de-CH", "CHF"); !ok || sym == "" {
		t.Fatalf("CHF symbol = %q ok=%v", sym, ok)
	}
	if types := p.BCP47Types("nu"); len(types) == 0 {
		t.Fatal("numbering-system BCP-47 types missing")
	}
	if raw, ok := rawLocale("de-CH"); !ok || raw.DateFormats[WidthMedium] != "" || raw.CurrencyPattern == "" {
		t.Fatalf("de-CH raw sparse record = %#v ok=%v", raw, ok)
	}
}

func rawLocale(tag string) (LocaleRecord, bool) {
	for _, rec := range localeRecords {
		if rec.Tag == tag {
			return rec, true
		}
	}
	return LocaleRecord{}, false
}
