package extract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nmeilick/go-i18n/locale/cldr"
)

func TestScanCLDRFeaturesEntrypointClosure(t *testing.T) {
	dir := newFeatureScanModule(t)
	writeFeatureScanFile(t, dir, "cmd/app/main.go", `package main

import (
	"example.com/autoscan/internal/ui"
	"github.com/nmeilick/go-i18n/locale"
)

func main() {
	locale.NewResolver()
	ui.Render()
}
`)
	writeFeatureScanFile(t, dir, "internal/ui/ui.go", `package ui

import (
	"time"
	"github.com/nmeilick/go-i18n/i18n"
)

func Render() {
	i18n.Currency(123)
	i18n.Date(time.Now())
	i18n.Percent(0.25)
}
`)
	writeFeatureScanFile(t, dir, "internal/unused/unused.go", `package unused

import "github.com/nmeilick/go-i18n/i18n"

func Unused() {
	i18n.Unit(3, "meter")
}
`)
	tidyFeatureScanModule(t, dir)

	report, err := ScanCLDRFeatures(context.Background(), FeatureScanOptions{
		ModuleDir:      dir,
		Entrypoints:    []string{"./cmd/app"},
		FeatureExtra:   []cldr.FeatureID{cldr.FeatureBCP47Extensions},
		FeatureExclude: []cldr.FeatureID{cldr.FeatureNumbersPercent},
	})
	if err != nil {
		t.Fatal(err)
	}
	features := featureScanSet(report.Features)
	for _, want := range []cldr.FeatureID{
		cldr.FeatureNumbersDecimal,
		cldr.FeatureNumbersSymbols,
		cldr.FeatureCurrenciesFractions,
		cldr.FeatureCurrenciesSymbols,
		cldr.FeatureDatesGregorianPatterns,
		cldr.FeatureDatesGregorianNames,
		cldr.FeatureProfileDefaults,
		cldr.FeatureBCP47Extensions,
	} {
		if !features[want] {
			t.Fatalf("missing feature %s in %#v", want, report.Features)
		}
	}
	if features[cldr.FeatureNumbersPercent] {
		t.Fatalf("excluded feature %s still present in %#v", cldr.FeatureNumbersPercent, report.Features)
	}
	for _, usage := range report.Usages {
		if strings.Contains(usage.Path, "unused") {
			t.Fatalf("unused package was scanned: %#v", usage)
		}
		if usage.Reason == "known_api" && usage.Line == 0 {
			t.Fatalf("known API usage missing line: %#v", usage)
		}
	}
	if !hasFeatureScanWarning(report, "feature_excluded") {
		t.Fatalf("missing feature_excluded warning: %#v", report.Warnings)
	}
}

func TestScanCLDRFeaturesImporterFallback(t *testing.T) {
	dir := newFeatureScanModule(t)
	writeFeatureScanFile(t, dir, "lib/lib.go", `package lib

import "github.com/nmeilick/go-i18n/i18n"

func Format() {
	i18n.Number(42)
}
`)
	tidyFeatureScanModule(t, dir)

	report, err := ScanCLDRFeatures(context.Background(), FeatureScanOptions{ModuleDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	features := featureScanSet(report.Features)
	if !features[cldr.FeatureNumbersDecimal] || !features[cldr.FeatureNumbersSymbols] {
		t.Fatalf("number features not detected: %#v", report.Features)
	}
	if len(report.Packages) != 1 || report.Packages[0].Reason != "importer_fallback" {
		t.Fatalf("packages = %#v", report.Packages)
	}
}

func TestScanCLDRFeaturesWrapperRule(t *testing.T) {
	dir := newFeatureScanModule(t)
	writeFeatureScanFile(t, dir, "cmd/app/main.go", `package main

import "example.com/autoscan/internal/money"

func main() {
	money.Amount(12)
}
`)
	writeFeatureScanFile(t, dir, "internal/money/money.go", `package money

func Amount(v any) {}
`)
	tidyFeatureScanModule(t, dir)

	report, err := ScanCLDRFeatures(context.Background(), FeatureScanOptions{
		ModuleDir: dir,
		Rules: []FeatureRule{{
			Target:   "Amount",
			Features: []cldr.FeatureID{cldr.FeatureCurrenciesFractions, cldr.FeatureCurrenciesSymbols},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	features := featureScanSet(report.Features)
	if !features[cldr.FeatureCurrenciesFractions] || !features[cldr.FeatureCurrenciesSymbols] {
		t.Fatalf("wrapper rule did not add currency features: %#v", report.Features)
	}
	if !hasFeatureScanUsageReason(report, "wrapper_rule") {
		t.Fatalf("missing wrapper rule evidence: %#v", report.Usages)
	}
}

func TestScanCLDRFeaturesAvoidsBareSelectorFalsePositives(t *testing.T) {
	dir := newFeatureScanModule(t)
	writeFeatureScanFile(t, dir, "cmd/app/main.go", `package main

import (
	"time"
	"golang.org/x/text/language"
	"github.com/nmeilick/go-i18n/locale"
)

func main() {
	_ = time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)
	profile := locale.ProfileForTag(language.English)
	_ = profile.Currency()
}
`)
	tidyFeatureScanModule(t, dir)

	report, err := ScanCLDRFeatures(context.Background(), FeatureScanOptions{ModuleDir: dir, Entrypoints: []string{"./cmd/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Features) != 0 || len(report.Usages) != 0 {
		t.Fatalf("unexpected false-positive features: features=%#v usages=%#v", report.Features, report.Usages)
	}
}

func TestScanCLDRFeaturesDetectsFormatterMethods(t *testing.T) {
	dir := newFeatureScanModule(t)
	writeFeatureScanFile(t, dir, "cmd/app/main.go", `package main

import "github.com/nmeilick/go-i18n/locale"

func main() {
	var spec locale.CurrencySpec
	locale.DefaultFormatter().FormatCurrency(locale.FormatContext{}, spec)
}
`)
	tidyFeatureScanModule(t, dir)

	report, err := ScanCLDRFeatures(context.Background(), FeatureScanOptions{ModuleDir: dir, Entrypoints: []string{"./cmd/app"}})
	if err != nil {
		t.Fatal(err)
	}
	features := featureScanSet(report.Features)
	if !features[cldr.FeatureCurrenciesFractions] || !features[cldr.FeatureCurrenciesSymbols] {
		t.Fatalf("formatter method did not add currency features: %#v", report.Features)
	}
	if !hasFeatureScanUsageReason(report, "known_formatter_method") {
		t.Fatalf("missing formatter method evidence: %#v", report.Usages)
	}
}

func newFeatureScanModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := repoRoot(t)
	writeFeatureScanFile(t, dir, "go.mod", "module example.com/autoscan\n\ngo 1.25\n\nrequire github.com/nmeilick/go-i18n v0.0.0\n\nreplace github.com/nmeilick/go-i18n => "+filepath.ToSlash(root)+"\n")
	return dir
}

func writeFeatureScanFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func featureScanSet(features []cldr.FeatureID) map[cldr.FeatureID]bool {
	out := map[cldr.FeatureID]bool{}
	for _, feature := range features {
		out[feature] = true
	}
	return out
}

func hasFeatureScanWarning(report FeatureScanReport, code string) bool {
	for _, warning := range report.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func hasFeatureScanUsageReason(report FeatureScanReport, reason string) bool {
	for _, usage := range report.Usages {
		if usage.Reason == reason {
			return true
		}
	}
	return false
}

func tidyFeatureScanModule(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, string(out))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}
