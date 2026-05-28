package locale

import (
	"slices"
	"testing"
	"time"

	"golang.org/x/text/language"
)

func TestNormalize(t *testing.T) {
	got, err := Normalize("pt_BR")
	if err != nil {
		t.Fatal(err)
	}
	if got != "pt-BR" {
		t.Fatalf("Normalize() = %q", got)
	}
}

func TestNegotiatorBestMatch(t *testing.T) {
	neg, err := NewNegotiator("en", []string{"en", "pt-BR"})
	if err != nil {
		t.Fatal(err)
	}
	res := neg.Resolve(Preference("pt-PT"))
	if res.Locale != "pt-BR" {
		t.Fatalf("best match locale = %q, want pt-BR", res.Locale)
	}
}

func TestNegotiatorFallbackChainDedupesExactMatch(t *testing.T) {
	neg, err := NewNegotiator("en", []string{"en", "de"})
	if err != nil {
		t.Fatal(err)
	}
	res := neg.Resolve(Preference("de"))
	if !slices.Equal(res.FallbackChain, []string{"de"}) {
		t.Fatalf("fallback chain = %#v, want [de]", res.FallbackChain)
	}
}

func TestNegotiatorFallbackChainKeepsParentFallback(t *testing.T) {
	neg, err := NewNegotiator("en", []string{"en", "de"}, Mode(MatchStrict))
	if err != nil {
		t.Fatal(err)
	}
	res := neg.Resolve(Preference("de-CH"))
	if !slices.Equal(res.FallbackChain, []string{"de-CH", "de"}) {
		t.Fatalf("fallback chain = %#v, want [de-CH de]", res.FallbackChain)
	}
}

func TestNegotiatorStrictDoesNotSiblingMatch(t *testing.T) {
	neg, err := NewNegotiator("en", []string{"en", "pt-BR"}, Mode(MatchStrict))
	if err != nil {
		t.Fatal(err)
	}
	res := neg.Resolve(Preference("pt-PT"))
	if res.Locale != "en" {
		t.Fatalf("strict locale = %q, want en", res.Locale)
	}
	if len(res.Rejected) != 1 || res.Rejected[0].Reason != "unsupported" {
		t.Fatalf("strict rejected = %#v", res.Rejected)
	}
}

func TestAcceptLanguage(t *testing.T) {
	cands := AcceptLanguage("de-CH,de;q=0.8,en;q=0.4,fr;q=0")
	if len(cands) != 3 {
		t.Fatalf("len = %d, want 3", len(cands))
	}
	if cands[0].Tag != "de-CH" || cands[0].Vary[0] != "Accept-Language" {
		t.Fatalf("first candidate = %#v", cands[0])
	}
}

func TestProfileSnapshotDefensiveCopy(t *testing.T) {
	p, err := NewProfileTags([]language.Tag{language.German}, WithCurrency(Currency("eur")))
	if err != nil {
		t.Fatal(err)
	}
	langs := p.Languages()
	langs[0] = language.French
	if p.PrimaryLanguage() != language.German {
		t.Fatalf("profile languages mutated: %v", p.PrimaryLanguage())
	}
	snap := p.Snapshot()
	if snap.Currency != "EUR" {
		t.Fatalf("currency = %q", snap.Currency)
	}
}

func TestProfileInvalidCurrency(t *testing.T) {
	_, err := NewProfile([]string{"en"}, WithCurrency(Currency("US1")))
	if err == nil {
		t.Fatal("expected invalid currency error")
	}
}

func TestProfileTimeZone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProfile([]string{"de-DE"}, WithTimeZone(loc))
	if err != nil {
		t.Fatal(err)
	}
	if p.TimeZone().String() != "Europe/Berlin" {
		t.Fatalf("timezone = %q", p.TimeZone())
	}
}
