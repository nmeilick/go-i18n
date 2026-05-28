package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteWritesProcessErrorsToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"--json", "version", "extra"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "error:") || strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestConfigValidateRequiresExistingConfig(t *testing.T) {
	dir := isolatedWorkingDir(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"config", "validate"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected config validate to fail without a config file")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "config file") || !strings.Contains(stderr.String(), "does not exist") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConfigInitDefaultsToProjectConfig(t *testing.T) {
	dir := isolatedWorkingDir(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"config", "init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("config init failed: code=%d stderr=%q", code, stderr.String())
	}
	path := filepath.Join(dir, "lingo.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected project config at %s: %v", path, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"config", "validate"}, &stdout, &stderr); code != 0 {
		t.Fatalf("config validate failed: code=%d stderr=%q", code, stderr.String())
	}
}

func TestConfigValidateRejectsUnsafeOutputPath(t *testing.T) {
	dir := isolatedWorkingDir(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	if err := os.WriteFile(filepath.Join(dir, "lingo.toml"), []byte(`version = 1

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
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"config", "validate"}, &stdout, &stderr); code == 0 {
		t.Fatal("expected unsafe output path to fail validation")
	}
	if !strings.Contains(stderr.String(), "escapes project root") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConfigPathAllJSONUsesSnakeCaseDTO(t *testing.T) {
	dir := isolatedWorkingDir(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"config", "path", "--all", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("config path failed: code=%d stderr=%q", code, stderr.String())
	}
	var payload struct {
		Paths []configLocation `json:"paths"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if len(payload.Paths) == 0 {
		t.Fatalf("paths = %#v", payload.Paths)
	}
	for _, loc := range payload.Paths {
		if loc.Path == "" || loc.Source == "" {
			t.Fatalf("invalid location = %#v", loc)
		}
	}
	if strings.Contains(stdout.String(), "Required") || strings.Contains(stdout.String(), "Source") {
		t.Fatalf("JSON did not use configured field names: %s", stdout.String())
	}
}

func TestDataBundlesGenerateDefaultOutputResolvesInsideProject(t *testing.T) {
	dir := isolatedWorkingDir(t)
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"data", "bundles", "generate", "--features", "numbers", "--locales", "de"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("generate failed: code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "cldr.cldrpack")); err != nil {
		t.Fatalf("expected default pack output: %v", err)
	}
	if !strings.Contains(stdout.String(), "cldr.cldrpack") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDataBundlesGenerateRelativeOutputResolvesInsideProject(t *testing.T) {
	dir := isolatedWorkingDir(t)
	var stdout, stderr bytes.Buffer
	code := Execute([]string{
		"data", "bundles", "generate",
		"--features", "numbers",
		"--locales", "de",
		"--output", filepath.Join("build", "de-numbers.cldrpack"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("generate failed: code=%d stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "build", "de-numbers.cldrpack")); err != nil {
		t.Fatalf("expected relative pack output: %v", err)
	}
}

func TestDataBundlesGenerateRejectsEscapingOutput(t *testing.T) {
	dir := isolatedWorkingDir(t)
	outsideName := filepath.Base(dir) + "-outside.cldrpack"
	var stdout, stderr bytes.Buffer
	code := Execute([]string{
		"data", "bundles", "generate",
		"--features", "numbers",
		"--locales", "de",
		"--output", filepath.Join("..", outsideName),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected escaping output to fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "escapes project root") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), outsideName)); !os.IsNotExist(err) {
		t.Fatalf("escaping output was written or stat failed unexpectedly: %v", err)
	}
}

func isolatedWorkingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
	t.Setenv("LINGO_CONFIG", "")
	return dir
}
