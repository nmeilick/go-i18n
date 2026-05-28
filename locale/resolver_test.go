package locale

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nmeilick/go-i18n/observe"
)

func TestResolverExplicitPreferenceOverridesImplicitSignals(t *testing.T) {
	userLang, err := LanguageObservation("de-CH",
		FromSource(SourceExplicitUser, TrustExplicit, SensitivityUserPrivate),
		Explicit(),
	)
	if err != nil {
		t.Fatal(err)
	}
	geo, err := RegionObservation("JP", RegionFormatting)
	if err != nil {
		t.Fatal(err)
	}
	tz, err := TimeZoneObservation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver().ResolveDetailed(geo, tz, userLang)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.PrimaryLanguage().String(); got != "de-CH" {
		t.Fatalf("language = %q", got)
	}
	if got := res.Profile.FormattingRegion(); got != "CH" {
		t.Fatalf("formatting region = %q", got)
	}
	if got := res.Profile.CurrentRegion(); got != "JP" {
		t.Fatalf("current region = %q", got)
	}
	if got := res.Profile.Currency(); got != Currency("CHF") {
		t.Fatalf("display currency = %q", got)
	}
	if got := res.Profile.TimeZone().String(); got != "Asia/Tokyo" {
		t.Fatalf("timezone = %q", got)
	}
}

func TestResolverLanguageOnlyDoesNotInferHighConfidenceCurrency(t *testing.T) {
	for _, tag := range []string{"pt", "en", "ar", "zh-Hant"} {
		obs, err := LanguageObservation(tag)
		if err != nil {
			t.Fatal(err)
		}
		res, err := NewResolver().ResolveDetailed(obs)
		if err != nil {
			t.Fatal(err)
		}
		if res.Profile.Currency() != "" {
			t.Fatalf("%s inferred currency %q", tag, res.Profile.Currency())
		}
		for _, cand := range res.Candidates {
			if cand.Field == FieldDisplayCurrency && cand.Confidence == ConfidenceHigh {
				t.Fatalf("%s produced high-confidence currency candidate %#v", tag, cand)
			}
		}
	}
}

func TestResolverDeterministicOrderAndNoRawInputs(t *testing.T) {
	obs1, err := LanguageObservation("de-CH")
	if err != nil {
		t.Fatal(err)
	}
	obs2, err := TimeZoneObservation("Asia/Tokyo", WithAge(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	obs3, err := RegionObservation("JP", RegionCurrent)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewResolver().ResolveDetailed(obs1, obs2, obs3, obs1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewResolver().ResolveDetailed(obs3, obs1, obs2)
	if err != nil {
		t.Fatal(err)
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatalf("resolver output not deterministic\nA=%s\nB=%s", aj, bj)
	}
	forbidden := []string{"Accept-Language", "sessionid", "203.0.113.1", "account-123", "LANG=de_CH"}
	for _, raw := range forbidden {
		if strings.Contains(string(aj), raw) {
			t.Fatalf("diagnostics leaked raw input %q in %s", raw, aj)
		}
	}
}

func TestResolverUnicodeExtensionsTrustAware(t *testing.T) {
	obs, err := LanguageObservation("en-US-u-ca-gregory-nu-latn-hc-h23-cu-usd",
		FromSource(SourceExplicitUser, TrustExplicit, SensitivityUserPrivate),
		Explicit(),
	)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewResolver().Resolve(obs)
	if err != nil {
		t.Fatal(err)
	}
	if p.Calendar() != "gregory" || p.NumberingSystem() != "latn" || p.HourCycle() != "h23" || p.Currency() != "USD" {
		t.Fatalf("profile extensions not applied: %#v", p.Snapshot())
	}
}

func TestResolverStrictIgnoresLowTrustInferredRegion(t *testing.T) {
	obs, err := LanguageObservation("de-CH")
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver(WithResolverPolicy(StrictResolverPolicy())).ResolveDetailed(obs)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.FormattingRegion(); got != "" {
		t.Fatalf("strict inferred formatting region = %q", got)
	}
}

func TestResolverAppliesCLDRWeekDefaultsByRegion(t *testing.T) {
	obs, err := LanguageObservation("en-US")
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver().ResolveDetailed(obs)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.FirstDay(); got != "sun" {
		t.Fatalf("first day = %q", got)
	}
	if !res.Profile.Defaulted(FieldFirstDay) {
		t.Fatalf("first day should be marked as defaulted")
	}
}

func TestResolverDisabledFieldPolicyStaysDisabled(t *testing.T) {
	policy := HelpfulResolverPolicy()
	fp := policy.Fields[FieldDisplayCurrency]
	fp.Enabled = false
	policy.Fields[FieldDisplayCurrency] = fp
	region, err := RegionObservation("US", RegionFormatting)
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver(WithResolverPolicy(policy)).ResolveDetailed(region)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.Currency(); got != "" {
		t.Fatalf("disabled display currency was resolved as %q", got)
	}
	for _, cand := range res.Candidates {
		if cand.Field == FieldDisplayCurrency && cand.Selected {
			t.Fatalf("disabled display currency candidate selected: %#v", cand)
		}
	}
}

func TestResolverInferenceFlagsGateRegionDefaults(t *testing.T) {
	policy := HelpfulResolverPolicy()
	currency := policy.Fields[FieldDisplayCurrency]
	currency.AllowCurrencyInfer = false
	policy.Fields[FieldDisplayCurrency] = currency
	tz := policy.Fields[FieldTimeZone]
	tz.AllowTimeZoneInfer = false
	policy.Fields[FieldTimeZone] = tz
	region, err := RegionObservation("DE", RegionFormatting)
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver(WithResolverPolicy(policy)).ResolveDetailed(region)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.Currency(); got != "" {
		t.Fatalf("currency inferred despite disabled currency inference: %q", got)
	}
	if got := res.Profile.TimeZone().String(); got != "UTC" {
		t.Fatalf("timezone inferred despite disabled timezone inference: %q", got)
	}
}

func TestResolverCapsCandidatesPerField(t *testing.T) {
	policy := HelpfulResolverPolicy()
	policy.MaxCandidatesPerField = 1
	obs := []Observation{}
	for _, tag := range []string{"de-DE", "fr-FR", "es-ES"} {
		o, err := LanguageObservation(tag, WithWeight(1000))
		if err != nil {
			t.Fatal(err)
		}
		obs = append(obs, o)
	}
	res, err := NewResolver(WithResolverPolicy(policy)).ResolveDetailed(obs...)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Field]int{}
	for _, cand := range res.Candidates {
		counts[cand.Field]++
		if counts[cand.Field] > 1 {
			t.Fatalf("field %s has too many candidates: %#v", cand.Field, res.Candidates)
		}
	}
}

func TestTimeZoneObservationRejectsPathLikeNames(t *testing.T) {
	for _, name := range []string{"../Europe/Berlin", "/etc/localtime", "Europe\\Berlin", "Europe/Berlin\n"} {
		if _, err := TimeZoneObservation(name); err == nil {
			t.Fatalf("TimeZoneObservation(%q) succeeded", name)
		}
		if _, err := NewProfile([]string{"en"}, WithTimeZoneName(name)); err == nil {
			t.Fatalf("WithTimeZoneName(%q) succeeded", name)
		}
	}
}

func TestResolverObserverReceivesProfileEvent(t *testing.T) {
	collector := observe.NewCollector()
	obs, err := LanguageObservation("de-CH")
	if err != nil {
		t.Fatal(err)
	}
	ctx := observe.WithAttrs(context.Background(), observe.Bounded("job", "nightly"))
	res, err := NewResolver(WithObserver(collector)).ResolveDetailedContext(ctx, obs)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.PrimaryLanguage().String(); got != "de-CH" {
		t.Fatalf("language = %q", got)
	}
	events := collector.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Name != "locale.resolve" || events[0].Code != "resolved_profile" {
		t.Fatalf("event = %#v", events[0])
	}
	if got := attrValue(events[0].Attrs, "locale.language"); got != "de-CH" {
		t.Fatalf("locale attr = %#v", got)
	}
	if got := attrValue(events[0].Attrs, "job"); got != "nightly" {
		t.Fatalf("job attr = %#v", got)
	}
}

func TestResolverNormalizesUnknownSourceClass(t *testing.T) {
	obs, err := LanguageObservation("de", FromSource(SourceClass("custom-raw-source"), TrustMedium, SensitivityRequest))
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver().ResolveDetailed(obs)
	if err != nil {
		t.Fatal(err)
	}
	for _, diag := range res.Diagnostics {
		if diag.SourceClass != "" && diag.SourceClass != SourceLibraryDefault {
			t.Fatalf("diagnostic source class = %#v", diag.SourceClass)
		}
	}
	for _, cand := range res.Candidates {
		if cand.SourceClass != SourceLibraryDefault {
			t.Fatalf("candidate source class = %#v", cand.SourceClass)
		}
	}
}
