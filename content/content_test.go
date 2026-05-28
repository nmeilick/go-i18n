package content

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nmeilick/go-i18n/i18n"
)

func TestMessageFromResultOmitsLookupByDefault(t *testing.T) {
	res := i18n.Result{
		Text:            "Hallo Ada",
		Status:          i18n.StatusExact,
		RequestedLocale: "de",
		ResolvedLocale:  "de",
		MessageLocale:   "de",
		FallbackChain:   []string{"de"},
		Diagnostics:     []i18n.Diagnostic{{Code: "x", Placeholder: "secret", Message: "redacted"}},
	}
	msg := MessageFromResult("Hello {name}", res)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "Diagnostics") || strings.Contains(text, "secret") {
		t.Fatalf("serialized diagnostics or vars: %s", text)
	}
	if strings.Contains(text, "lookup") || strings.Contains(text, "Hello {name}") {
		t.Fatalf("serialized internal lookup metadata: %s", text)
	}
	if !strings.Contains(text, `"text":"Hallo Ada"`) || !strings.Contains(text, `"locale":"de"`) {
		t.Fatalf("missing public message fields: %s", text)
	}
}

func TestMessageFromResultWithLookupIncludesLookupMetadata(t *testing.T) {
	res := i18n.Result{
		Text:            "Hallo Ada",
		Status:          i18n.StatusExact,
		RequestedLocale: "de",
		ResolvedLocale:  "de",
		MessageLocale:   "de",
		Domain:          "billing",
		FallbackChain:   []string{"de"},
		Diagnostics:     []i18n.Diagnostic{{Code: "x", Placeholder: "secret", Message: "redacted"}},
	}
	msg := MessageFromResultWithLookup("Hello {name}", res)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "Diagnostics") || strings.Contains(text, "secret") {
		t.Fatalf("serialized diagnostics or vars: %s", text)
	}
	for _, want := range []string{`"id":"Hello {name}"`, `"status":"exact"`, `"domain":"billing"`, `"fallback_chain":["de"]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
}
