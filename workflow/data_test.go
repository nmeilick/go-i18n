package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nmeilick/go-i18n/internal/cldrgen"
)

func TestDataUpdateDryRunDoesNotWriteTrackedFiles(t *testing.T) {
	root := repoRoot(t)
	sourceDir := filepath.Join(root, cldrgen.DefaultSourceDir)
	if _, err := os.Stat(sourceDir); err != nil {
		t.Skipf("CLDR assets not available: %v", err)
	}
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Data.SourceDir = sourceDir
	cfg.Data.OutputFile = filepath.Join(dir, "internal", "cldrdata", "data_gen.go")
	cfg.Data.LockFile = filepath.Join(dir, "cldr.lock.json")
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := w.DataUpdate(context.Background(), true, false)
	if !errors.Is(err, ErrDataStale) {
		t.Fatalf("dry-run error = %v, want ErrDataStale", err)
	}
	if len(report.Changed) != 2 {
		t.Fatalf("changed = %#v", report.Changed)
	}
	if _, err := os.Stat(cfg.Data.OutputFile); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote output: %v", err)
	}
	if _, err := os.Stat(cfg.Data.LockFile); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote lock: %v", err)
	}
}

func TestDataUpdateAndCheck(t *testing.T) {
	root := repoRoot(t)
	sourceDir := filepath.Join(root, cldrgen.DefaultSourceDir)
	if _, err := os.Stat(sourceDir); err != nil {
		t.Skipf("CLDR assets not available: %v", err)
	}
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Data.SourceDir = sourceDir
	cfg.Data.OutputFile = filepath.Join(dir, "internal", "cldrdata", "data_gen.go")
	cfg.Data.LockFile = filepath.Join(dir, "cldr.lock.json")
	w, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.DataUpdate(context.Background(), false, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	report, err := w.DataCheck(context.Background())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.Changed) != 0 || !report.SizeBudgetOK {
		t.Fatalf("report = %#v", report)
	}
	if !hasDataSizeRow(report.SizeReport, "generated_go") || !hasDataSizeRow(report.SizeReport, "currency_symbols") {
		t.Fatalf("size report = %#v", report.SizeReport)
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

func hasDataSizeRow(rows []cldrgen.SizeRow, domain string) bool {
	for _, row := range rows {
		if row.Domain == domain && (row.SourceBytes > 0 || row.EncodedBytes > 0) {
			return true
		}
	}
	return false
}
