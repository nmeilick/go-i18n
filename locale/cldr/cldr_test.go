package cldr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/locale/cldr"
)

func TestBuiltinLeanServicesMatchDefaultFormatter(t *testing.T) {
	svc, err := cldr.NewServices()
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	profile, err := locale.NewProfile([]string{"de-CH"}, locale.WithCurrency(locale.Currency("CHF")))
	if err != nil {
		t.Fatal(err)
	}
	ctx := locale.FormatContext{Profile: profile, Locale: profile.PrimaryLanguage(), Policy: locale.DefaultFormatPolicy()}
	spec := locale.CurrencySpec{Value: 1234.5, Code: locale.Currency("CHF"), CodeSource: locale.CurrencyExplicit}
	got, gotDiag := svc.Formatter().FormatCurrency(ctx, spec)
	want, wantDiag := locale.DefaultFormatter().FormatCurrency(ctx, spec)
	if got != want {
		t.Fatalf("service formatter = %q, default = %q", got, want)
	}
	if len(gotDiag) != len(wantDiag) {
		t.Fatalf("service diagnostics = %#v, default = %#v", gotDiag, wantDiag)
	}
	defs, ok := svc.DefaultsProvider().DefaultsForRegion("CH")
	if !ok || defs.DisplayCurrency.String() != "CHF" {
		t.Fatalf("defaults CH = %#v ok=%v", defs, ok)
	}
}

func TestComposeRejectsAuthoritativeOverlap(t *testing.T) {
	_, err := cldr.Compose([]cldr.Bundle{cldr.BuiltinLean(), cldr.BuiltinLean()})
	if err == nil {
		t.Fatal("expected overlap error")
	}
	var cerr *cldr.Error
	if !errors.As(err, &cerr) || cerr.Code != cldr.ErrCoverageConflict {
		t.Fatalf("error = %v", err)
	}
}

func TestComposeReplacementAndDependencyOverlap(t *testing.T) {
	replaced, err := cldr.Compose(
		[]cldr.Bundle{cldr.BuiltinLean(), cldr.BuiltinLean()},
		cldr.WithAuthoritativeReplacement(),
	)
	if err != nil {
		t.Fatalf("replacement compose: %v", err)
	}
	if got := replaced.Info().Mode; got != "composed" {
		t.Fatalf("mode = %q", got)
	}
	base := cldr.BuiltinLean()
	info := base.Info()
	info.ID = "dependency-only"
	dep, err := cldr.NewBundle(info, []cldr.Coverage{{
		Feature: cldr.FeatureDatesGregorianPatterns,
		Scope:   cldr.ScopeLocale,
		Role:    cldr.RoleDependency,
		Status:  cldr.StatusDataAvailable,
		All:     true,
	}}, base.Data(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cldr.Compose([]cldr.Bundle{base, dep}); err != nil {
		t.Fatalf("dependency-only overlap should be allowed: %v", err)
	}
}

func TestServicesClosedFormatterDiagnostics(t *testing.T) {
	svc, err := cldr.NewServices()
	if err != nil {
		t.Fatal(err)
	}
	formatter := svc.Formatter()
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	profile, err := locale.NewProfile([]string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	out, diag := formatter.FormatNumber(locale.FormatContext{Profile: profile}, locale.NumberSpec{Value: 1})
	if out != "" || len(diag) != 1 || diag[0].Code != string(cldr.ErrClosedService) {
		t.Fatalf("closed formatter out=%q diag=%#v", out, diag)
	}
	stats := svc.Stats()
	if stats.ClosedServiceHits == 0 {
		t.Fatalf("closed hits not recorded: %#v", stats)
	}
}

func TestBundleInfoIsDefensivelyCopied(t *testing.T) {
	b := cldr.BuiltinLean()
	info := b.Info()
	if len(info.Locales) == 0 {
		t.Fatal("expected locales")
	}
	info.Locales[0] = "mutated"
	if strings.Contains(strings.Join(b.Info().Locales, ","), "mutated") {
		t.Fatal("bundle info leaked mutable locale slice")
	}
}

func TestSelectionExpandsLanguagesAndFeatures(t *testing.T) {
	bundle, plan, err := cldr.SelectBundle(cldr.BuiltinLean(), cldr.Selection{
		Languages: []string{"de"},
		Features:  []cldr.FeatureID{cldr.FeatureDatesGregorianPatterns, cldr.FeatureCurrenciesFractions},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Locales) == 0 || !contains(plan.Locales, "de-CH") {
		t.Fatalf("expanded locales = %#v", plan.Locales[:min(len(plan.Locales), 8)])
	}
	if contains(bundle.Info().Features, cldr.FeatureProfileDefaults) {
		t.Fatalf("unexpected unselected feature: %#v", bundle.Info().Features)
	}
	if _, ok := bundle.Data().Locale("fr"); ok {
		t.Fatal("unselected locale unexpectedly available")
	}
	if _, ok := bundle.Data().Locale("de-CH"); !ok {
		t.Fatal("selected language-family locale missing")
	}
}

func TestSelectionRejectsUnavailableFeature(t *testing.T) {
	_, _, err := cldr.SelectBundle(cldr.BuiltinLean(), cldr.Selection{Features: []cldr.FeatureID{cldr.FeatureUnitsPatterns}})
	if err == nil {
		t.Fatal("expected unsupported feature")
	}
	var cerr *cldr.Error
	if !errors.As(err, &cerr) || cerr.Code != cldr.ErrUnsupportedCapability {
		t.Fatalf("error = %v", err)
	}
}

func contains[T comparable](values []T, want T) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
