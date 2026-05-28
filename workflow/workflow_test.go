package workflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestWorkflowUpdate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(`package app
func f(tr interface{ T(string) string }) string { return tr.T("Hello") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Extract.Roots = []string{src}
	cfg.Catalogs.Template = filepath.Join(dir, "locales", "messages.pot")
	cfg.Catalogs.LocaleDir = filepath.Join(dir, "locales")
	cfg.Catalogs.Locales = []string{"de"}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.Update(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Messages != 1 {
		t.Fatalf("messages = %d", report.Messages)
	}
	data, err := os.ReadFile(cfg.Catalogs.Template)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `msgid "Hello"`) {
		t.Fatalf("template = %s", string(data))
	}
	if _, err := os.Stat(filepath.Join(cfg.Catalogs.LocaleDir, "de.po")); err != nil {
		t.Fatal(err)
	}
	if report, err := w.Check(context.Background(), true); err == nil {
		t.Fatalf("expected strict untranslated check failure, report=%#v", report)
	}
	localePath := filepath.Join(cfg.Catalogs.LocaleDir, "de.po")
	localeData, err := os.ReadFile(localePath)
	if err != nil {
		t.Fatal(err)
	}
	localeText := strings.Replace(string(localeData), "msgid \"Hello\"\nmsgstr \"\"", "msgid \"Hello\"\nmsgstr \"Hallo\"", 1)
	if err := os.WriteFile(localePath, []byte(localeText), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := w.Check(context.Background(), true); err != nil {
		t.Fatalf("strict check after translation: %v report=%#v", err, report)
	}
	if err := os.WriteFile(cfg.Catalogs.Template, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := w.Check(context.Background(), false); err == nil {
		t.Fatalf("expected stale check failure, report=%#v", report)
	}
}

func TestWorkflowStale(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(`package app
func f(tr interface{ T(string) string }) string { return tr.T("Current") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	locales := filepath.Join(dir, "locales")
	if err := os.MkdirAll(locales, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locales, "de.po"), []byte(`msgid "Old"
msgstr "Alt"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Extract.Roots = []string{src}
	cfg.Catalogs.Template = filepath.Join(locales, "messages.pot")
	cfg.Catalogs.LocaleDir = locales
	cfg.Catalogs.Locales = []string{"de"}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.Stale(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Stale) != 1 || !strings.Contains(report.Stale[0], "Old") {
		t.Fatalf("stale = %#v", report.Stale)
	}
}

func TestWorkflowFormatDryRun(t *testing.T) {
	dir := t.TempDir()
	locales := filepath.Join(dir, "locales")
	if err := os.MkdirAll(locales, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(locales, "messages.pot")
	if err := os.WriteFile(path, []byte(`msgstr "Hallo"
msgid "Hello"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Catalogs.Template = path
	cfg.Catalogs.LocaleDir = locales
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.Format(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changed) != 1 {
		t.Fatalf("changed = %#v", report.Changed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `msgstr`) {
		t.Fatalf("dry-run mutated file: %s", string(data))
	}
}

func TestWorkflowUpdateInitializesPluralHeadersForLocale(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(`package app
func f(tr interface{ Tn(int, string, string) string }) string { return tr.Tn(3, "One file", "{n} files") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Extract.Roots = []string{src}
	cfg.Catalogs.Template = filepath.Join(dir, "locales", "messages.pot")
	cfg.Catalogs.LocaleDir = filepath.Join(dir, "locales")
	cfg.Catalogs.Locales = []string{"ar"}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.Catalogs.LocaleDir, "ar.po"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`msgstr "Language: ar\nPlural-Forms: nplurals=6;`, `msgstr[5] ""`} {
		if !strings.Contains(text, want) {
			t.Fatalf("ar.po missing %q:\n%s", want, text)
		}
	}
}

func TestWorkflowUpdateReportsChangedFilesSorted(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(`package app
func f(tr interface{ T(string) string }) string { return tr.T("Hello") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Extract.Roots = []string{src}
	cfg.Catalogs.Template = filepath.Join(dir, "locales", "messages.pot")
	cfg.Catalogs.LocaleDir = filepath.Join(dir, "locales")
	cfg.Catalogs.Locales = []string{"zh", "de", "ar", "fr"}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.Update(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		cfg.Catalogs.Template,
		filepath.Join(cfg.Catalogs.LocaleDir, "ar.po"),
		filepath.Join(cfg.Catalogs.LocaleDir, "de.po"),
		filepath.Join(cfg.Catalogs.LocaleDir, "fr.po"),
		filepath.Join(cfg.Catalogs.LocaleDir, "zh.po"),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(report.Changed, want) {
		t.Fatalf("changed = %#v, want %#v", report.Changed, want)
	}
}

func TestLoadConfigResolvesRelativePathsFromConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(project, "lingo.toml")
	if err := os.WriteFile(configPath, []byte(`version = 1

[extract]
roots = ["app"]

[catalogs]
template = "locales/messages.pot"
locale_dir = "locales"
locales = ["de"]

[data]
source_dir = "cldr-json"
lock_file = "cldr.lock.json"
output_file = "internal/cldrdata/generated.go"
size_budget_bytes = 2097152
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := w.Config.Extract.Roots[0], filepath.Join(project, "app"); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
	if got, want := w.Config.Catalogs.Template, filepath.Join(project, "locales", "messages.pot"); got != want {
		t.Fatalf("template = %q, want %q", got, want)
	}
}

func TestWorkflowUsesConfigRelativeExtractionReferences(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(filepath.Join(project, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "app", "main.go"), []byte(`package app
func f(tr interface{ T(string) string }) string { return tr.T("Hello") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(project, "lingo.toml")
	if err := os.WriteFile(configPath, []byte(`version = 1

[extract]
roots = ["app"]

[catalogs]
template = "locales/messages.pot"
locale_dir = "locales"
locales = []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Update(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.Config.Catalogs.Template)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "#: app/main.go:2") || strings.Contains(text, project) {
		t.Fatalf("unstable references:\n%s", text)
	}
}

func TestLoadConfigRejectsOutputsOutsideProjectByDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "lingo.toml")
	if err := os.WriteFile(configPath, []byte(`version = 1

[extract]
roots = ["."]

[catalogs]
template = "../messages.pot"
locale_dir = "locales"
locales = ["de"]

[data]
source_dir = "cldr-json"
lock_file = "cldr.lock.json"
output_file = "internal/cldrdata/generated.go"
size_budget_bytes = 2097152
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("expected escaped output path to be rejected")
	}
}

func TestValidateDataBundleConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Data.Bundles = []DataBundleConfig{{
		Name:         "app",
		Mode:         "embedded-pack",
		Output:       "internal/appcldr/bundle.go",
		Package:      "appcldr",
		Codec:        "zstd",
		Languages:    []string{"de"},
		Features:     []string{"dates"},
		ScanMode:     "auto",
		Entrypoints:  []string{"./cmd/app"},
		FeatureRules: []string{"Money=currencies"},
	}}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Data.Bundles[0].Codec = "brotli"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected invalid codec error")
	}
	cfg.Data.Bundles[0].Codec = "zstd"
	cfg.Data.Bundles[0].ScanMode = "whole-world"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("expected invalid scan mode error")
	}
}
