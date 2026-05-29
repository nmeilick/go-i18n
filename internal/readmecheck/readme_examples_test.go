package readmecheck

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
)

func TestREADMEExampleOutputsMatchRunnableExamples(t *testing.T) {
	root := repoRoot(t)
	readme := readREADME(t, root)

	cases := []struct {
		marker string
		cmd    string
	}{
		{marker: "Basic output:", cmd: "./examples/hello-catalog"},
		{marker: "Medium output:", cmd: "./examples/invoice-summary"},
		{marker: "The comprehensive checkout example runs a request through", cmd: "./examples/localized-checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			want := fencedBlockAfter(t, readme, tc.marker)
			got := runGoExample(t, root, tc.cmd)
			if got != want {
				t.Fatalf("README output for %s differs\nwant:\n%s\ngot:\n%s", tc.cmd, want, got)
			}
		})
	}
}

func TestREADMEGeneratedFormattingSamplesMatchFormatter(t *testing.T) {
	root := repoRoot(t)
	readme := readREADME(t, root)

	if got, want := readmeLocaleSamples(t), fencedBlockAfter(t, readme, "The same application message can render through different locale profiles:"); got != want {
		t.Fatalf("README locale samples differ\nwant:\n%s\ngot:\n%s", want, got)
	}
	if got, want := readmeSwissCurrencyOutput(t), fencedBlockAfter(t, readme, "Typical output:"); got != want {
		t.Fatalf("README Swiss currency sample differs\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func readmeLocaleSamples(t *testing.T) string {
	t.Helper()
	source := "Invoice {number}: {amount} due {date} at {time}; {percent} paid."
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "en-US", ID: source, Translations: []string{source}},
		{Locale: "en-GB", ID: source, Translations: []string{source}},
		{Locale: "de-CH", ID: source, Translations: []string{"Rechnung {number}: {amount} fällig am {date} um {time}; {percent} bezahlt."}},
		{Locale: "fr-FR", ID: source, Translations: []string{"Facture {number} : {amount} à régler le {date} à {time} ; {percent} payé."}},
		{Locale: "ja-JP", ID: source, Translations: []string{"請求書 {number}: {amount} の支払期限は {date} {time}、支払済み {percent}"}},
		{Locale: "ko-KR", ID: source, Translations: []string{"청구서 {number}: {amount} 결제 기한 {date} {time}; {percent} 결제됨"}},
		{Locale: "th-TH", ID: source, Translations: []string{"ใบแจ้งหนี้ {number}: {amount} ครบกำหนด {date} เวลา {time}; ชำระแล้ว {percent}"}},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		t.Fatal(err)
	}

	type sample struct {
		tag      string
		region   string
		currency string
	}
	samples := []sample{
		{tag: "en-US", region: "US", currency: "USD"},
		{tag: "en-GB", region: "GB", currency: "GBP"},
		{tag: "de-CH", region: "CH", currency: "EUR"},
		{tag: "fr-FR", region: "FR", currency: "EUR"},
		{tag: "ja-JP", region: "JP", currency: "JPY"},
		{tag: "ko-KR", region: "KR", currency: "KRW"},
		{tag: "th-TH", region: "TH", currency: "THB"},
	}
	rt := i18n.NewRuntime(cat)
	instant := time.Date(2026, 5, 16, 14, 30, 0, 0, time.UTC)
	var out bytes.Buffer
	for _, s := range samples {
		profile, err := locale.NewProfile(
			[]string{s.tag},
			locale.WithCurrency(locale.Currency(s.currency)),
			locale.WithFormattingRegion(s.region),
			locale.WithTimeZone(time.UTC),
		)
		if err != nil {
			t.Fatal(err)
		}
		tr := rt.Translator(profile)
		text := tr.T(source, i18n.Vars{
			"number":  "INV-1042",
			"amount":  i18n.Currency(1234.5, i18n.CurrencyCode(locale.Currency(s.currency))),
			"date":    i18n.Date(instant, i18n.Style("medium")),
			"time":    i18n.Time(instant, i18n.Style("short")),
			"percent": i18n.Percent(0.74),
		})
		fmt.Fprintf(&out, "%-6s %s\n", s.tag, text)
	}
	return normalizeOutput(out.String())
}

func readmeSwissCurrencyOutput(t *testing.T) string {
	t.Helper()
	source := "Total {amount} due {date}"
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{Locale: "de-CH", ID: source, Translations: []string{source}},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := locale.NewProfile(
		[]string{"de-CH"},
		locale.WithCurrency(locale.Currency("CHF")),
		locale.WithTimeZoneName("Europe/Zurich"),
		locale.WithFormattingRegion("CH"),
	)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	text := i18n.NewRuntime(cat).Translator(profile).T(source, i18n.Vars{
		"amount": i18n.Currency(
			1234.5,
			i18n.CurrencyCode(locale.Currency("EUR")),
		),
		"date": i18n.Date(due, i18n.Style("medium")),
	})
	return normalizeOutput(text + "\n")
}

func runGoExample(t *testing.T, root, pkg string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", pkg)
	cmd.Dir = root
	data, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("go run %s timed out", pkg)
	}
	if err != nil {
		t.Fatalf("go run %s: %v\n%s", pkg, err, data)
	}
	return normalizeOutput(string(data))
}

func readREADME(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func fencedBlockAfter(t *testing.T, markdown, marker string) string {
	t.Helper()
	idx := strings.Index(markdown, marker)
	if idx < 0 {
		t.Fatalf("README marker %q not found", marker)
	}
	rest := markdown[idx:]
	start := strings.Index(rest, "```")
	if start < 0 {
		t.Fatalf("README marker %q has no fenced block", marker)
	}
	content := rest[start+3:]
	if nl := strings.IndexByte(content, '\n'); nl >= 0 {
		content = content[nl+1:]
	}
	end := strings.Index(content, "```")
	if end < 0 {
		t.Fatalf("README marker %q has unterminated fenced block", marker)
	}
	return normalizeOutput(content[:end])
}

func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(file)
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
