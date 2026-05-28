package cldrgen

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nmeilick/go-i18n/locale/cldr"
	"github.com/nmeilick/go-i18n/locale/cldrpack"
)

func TestGenerateDeterministicAndRelativeMetadata(t *testing.T) {
	root := repoRoot(t)
	sourceDir := filepath.Join(root, DefaultSourceDir)
	if _, err := os.Stat(sourceDir); err != nil {
		t.Skipf("CLDR assets not available: %v", err)
	}
	opts := Options{
		SourceDir:  sourceDir,
		OutputPath: filepath.Join(t.TempDir(), "data_gen.go"),
		LockPath:   filepath.Join(t.TempDir(), "cldr.lock.json"),
	}
	a, srcA, lockA, err := Generate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, srcB, lockB, err := Generate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(srcA, srcB) || !bytes.Equal(lockA, lockB) {
		t.Fatal("generation is not deterministic")
	}
	if a.Lock.TreeSHA256 != b.Lock.TreeSHA256 || a.Lock.TreeSHA256 == "" {
		t.Fatalf("lock digests = %q %q", a.Lock.TreeSHA256, b.Lock.TreeSHA256)
	}
	if strings.Contains(string(lockA), root) || strings.Contains(string(srcA[:min(len(srcA), 4096)]), root) {
		t.Fatal("generated metadata contains absolute checkout path")
	}
	if !bytes.Contains(srcA, []byte(`Tag: "de-CH"`)) || !bytes.Contains(srcA, []byte(`Currency: "CHF"`)) {
		t.Fatal("generated data missing expected Swiss locale/defaults")
	}
}

func TestGeneratePackFromNormalizedModel(t *testing.T) {
	root := repoRoot(t)
	sourceDir := filepath.Join(root, DefaultSourceDir)
	if _, err := os.Stat(sourceDir); err != nil {
		t.Skipf("CLDR assets not available: %v", err)
	}
	opts := Options{
		SourceDir:       sourceDir,
		OutputPath:      filepath.Join(t.TempDir(), "app.cldrpack"),
		LockPath:        filepath.Join(t.TempDir(), "cldr.lock.json"),
		SizeBudgetBytes: 8 << 20,
	}
	result, pack, err := GeneratePack(context.Background(), opts, cldr.Selection{
		Languages: []string{"de"},
		Features:  []cldr.FeatureID{cldr.FeatureDatesGregorianPatterns, cldr.FeatureCurrenciesFractions},
	}, cldrpack.CodecRaw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.Locales == nil || !containsString(result.Selection.Locales, "de-CH") {
		t.Fatalf("selection = %#v", result.Selection)
	}
	bundle, err := cldrpack.FromBytes("generated.cldrpack", pack, cldrpack.WithHashVerification(true))
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	if _, ok := bundle.Data().Locale("de-CH"); !ok {
		t.Fatal("generated pack missing selected locale")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
