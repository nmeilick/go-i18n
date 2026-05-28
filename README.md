# go-i18n

Internationalization for Go apps with gettext catalogs, locale-aware formatting, HTTP localization, and translation
workflow tooling.

## What You Get
- Core translation runtime with immutable catalogs, localizers, domains, `T/Tn/Tc/Tnc`, checked lookup, and named
  interpolation.
- Gettext PO/POT parsing, loading, validation, merging, formatting, and stale-entry checks.
- Locale negotiation, profile resolution, timezone/currency/region preferences, and locale-aware sort/search/case
  helpers.
- Typed number, currency, percent, date, time, duration, list, and unit formatting through the localizer profile.
- `net/http` middleware plus small API payload helpers for localized messages.
- `lingo`, a workflow CLI for extraction, catalog updates, CI checks, and CLDR data maintenance.
- Generated CLDR data for core formatting and profile defaults included in the module.
- Dependency-light observability events for missing translations, fallback lookups, resolver warnings, and formatter
  diagnostics.

## Install
For application code:

```sh
go get github.com/nmeilick/go-i18n
```

For the workflow CLI:

```sh
go install github.com/nmeilick/go-i18n/cmd/lingo@latest
```

Repository development uses the Go version in [go.mod](go.mod):

```sh
make help
make test
make build
bin/lingo version
```

Expected local build output:

```text
lingo dev (<commit>, <date>)
```

## Quick Start
The smallest useful setup is an in-memory catalog and a localizer:

```go
package main

import (
	"fmt"

	"github.com/nmeilick/go-i18n/i18n"
)

func main() {
	cat, err := i18n.NewCatalog([]i18n.CatalogEntry{
		{
			Locale:       "de",
			ID:           "Hello {name}",
			Translations: []string{"Hallo {name}"},
		},
	}, i18n.DefaultLocale("en"))
	if err != nil {
		panic(err)
	}

	tr := i18n.NewRuntime(cat).Localizer("de")
	fmt.Println(tr.T("Hello {name}", i18n.Arg("name", "Ada")))
}
```

Output:

```text
Hallo Ada
```

Use checked lookup when application code needs status and metadata:

```go
res := tr.Lookup(i18n.Text("Hello {name}"), i18n.Arg("name", "Ada"))
if res.Status != i18n.StatusExact {
	// res.Status, res.MessageLocale, res.FallbackChain, and res.Diagnostics explain what happened.
}
```

## Examples
Start with the small examples, then read the HTTP checkout example to see the pieces together.

| Example | Shows | Run |
| --- | --- | --- |
| [examples/hello-catalog](examples/hello-catalog) | In-memory catalog, `T`, named variables, checked missing lookup. | `go run ./examples/hello-catalog` |
| [examples/invoice-summary](examples/invoice-summary) | Profile-aware currency/date/percent formatting, plurals, checked lookup. | `go run ./examples/invoice-summary` |
| [examples/localized-checkout](examples/localized-checkout) | Embedded gettext catalogs, HTTP middleware, locale negotiation, typed formatting, contexts, content payloads, and observability. | `go run ./examples/localized-checkout` |

Basic output:

```text
Hallo Ada
Speichern
missing status: missing
```

Medium output:

```text
Rechnung A-1042
Gesamtbetrag: 1.234,50 €
Fällig: 16.05.2026
Bezahlt: 74 %
3 Positionen
lookup: exact de
```

The comprehensive checkout example runs a request through `web.Middleware` and prints the response:

```text
HTTP 200
Content-Language: de
Vary: Accept-Language
{"locale":"de","resolved_source":"accept-language","greeting":"Hallo Ada","order":"Bestellung A-1042","total":"Summe: 1.499,95 €",...}
```

For a guided feature tour, run the interactive terminal showcase in [examples/showcase](examples/showcase). It combines
embedded catalogs, profile-aware formatting, HTTP middleware, content payloads, locale-aware text, diagnostics, and the
`lingo` workflow in one example:

```sh
go run ./examples/showcase
```

## Gettext Catalogs
Most applications keep translations in PO files and embed them into the binary.

```text
locales/
  messages.pot
  de.po
  fr.po
```

```go
package app

import (
	"embed"

	"github.com/nmeilick/go-i18n/gettext"
	"github.com/nmeilick/go-i18n/i18n"
)

//go:embed locales/*.po
var localeFS embed.FS

func NewI18nRuntime() (*i18n.Runtime, error) {
	cat, err := gettext.LoadFS(localeFS, "locales/*.po", gettext.DefaultLocale("en"))
	if err != nil {
		return nil, err
	}
	return i18n.NewRuntime(cat), nil
}
```

Minimal German PO file:

```po
msgid ""
msgstr ""
"Language: de\n"
"Plural-Forms: nplurals=2; plural=(n != 1);\n"

msgid "Hello {name}"
msgstr "Hallo {name}"
```

Fuzzy and obsolete entries are preserved in PO documents but are not served from runtime catalogs.

## Translation API
```go
tr.T("Save")
tr.T("Hello {name}", i18n.Arg("name", "Ada"))

tr.Tn(count, "One file", "{n} files")

tr.Tc("button", "Open")
tr.Tc("status", "Open")

billing := tr.WithDomain("billing")
billing.T("Invoice")
```

Process-default helpers exist for small programs:

```go
i18n.SetDefault(rt)
text := i18n.Locale("de").T("Hello {name}", i18n.Arg("name", "Ada"))
```

For servers, prefer an explicit `Runtime` and per-request `Localizer`. Do not store a request locale in package-level
state.

## Formatting and Profiles
Profiles carry language, timezone, display currency, numbering, calendar, measurement, and region preferences for one
request, job, or operation. Typed interpolation values render through the bound profile.

```go
profile, err := locale.NewProfile(
	[]string{"de-CH"},
	locale.WithCurrency(locale.Currency("CHF")),
	locale.WithTimeZoneName("Europe/Zurich"),
	locale.WithFormattingRegion("CH"),
)
if err != nil {
	return err
}

tr := rt.Translator(profile)
due := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)

text := tr.T("Total {amount} due {date}", i18n.Vars{
	"amount": i18n.Currency(1234.5, i18n.CurrencyCode(locale.Currency("CHF"))),
	"date":   i18n.Date(due, i18n.Style("medium")),
})
```

Typical output:

```text
Total CHF 1'234.50 due 16.05.2026
```

Available typed values:

| Value | Example |
| --- | --- |
| Number | `i18n.Number(1234.5, i18n.FractionDigits(1))` |
| Percent | `i18n.Percent(0.74)` |
| Currency | `i18n.Currency(1234.5, i18n.CurrencyCode(locale.Currency("EUR")))` |
| Date | `i18n.Date(t, i18n.Style("medium"))` |
| Time | `i18n.Time(t, i18n.Style("short"))` |
| DateTime | `i18n.DateTime(t, i18n.Style("long"))` |
| Duration | `i18n.Duration(90 * time.Minute)` |
| List | `i18n.List([]any{"Catalogs", "Profiles", "Formatting"})` |
| Unit | `i18n.Unit(13, "kilometer")` |

Currency is not inferred from language alone. Pass the real financial currency with `i18n.CurrencyCode`; profile currency
is only a display/default preference.

Use `locale.Resolver` when the application has multiple signals:

```go
obs := locale.AcceptLanguageObservations(
	req.Header.Get("Accept-Language"),
	locale.FromSource(locale.SourceClientLocale, locale.TrustMedium, locale.SensitivityRequest),
)

tz, err := locale.TimeZoneObservation(
	"Europe/Zurich",
	locale.FromSource(locale.SourceClientTimeZone, locale.TrustMedium, locale.SensitivityRequest),
)
if err == nil {
	obs = append(obs, tz)
}

result, err := locale.NewResolver().ResolveDetailed(obs...)
if err != nil {
	return err
}

profile := result.Profile
```

Resolver diagnostics are redaction-safe. They report fields, source classes, confidence, reasons, and selected
candidates, not raw headers, cookies, IP addresses, account IDs, or environment strings.

## HTTP and API Payloads
The `web` package adapts a runtime to `net/http`:

```go
neg, err := locale.NewNegotiator("en", []string{"en", "de", "fr"})
if err != nil {
	return err
}

mw := web.Middleware(rt, neg, web.Options{
	Sources: []web.Source{
		web.Query("lang"),
		web.Cookie("locale"),
		web.AcceptLanguage(),
	},
	ContentLanguage:           true,
	Vary:                      true,
	PrivateWhenUserSourceWins: true,
})

handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	tr, ok := web.Localizer(r.Context())
	if !ok {
		http.Error(w, "missing localizer", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(tr.T("Hello")))
}))
```

The middleware can set `Content-Language`, merge `Vary`, and mark responses private when a user-specific source wins.
Use `Options.ProfileError` when the application should map profile-construction failures to its own HTTP error shape.

The `content` package projects localized messages into payloads without forcing an error envelope:

```go
msg := content.ProjectField(
	tr,
	"email",
	"user.email",
	"required",
	i18n.Text("Email is required"),
)
```

Example JSON:

```json
{
  "field": "email",
  "path": "user.email",
  "code": "required",
  "message": {
    "text": "E-Mail ist erforderlich",
    "locale": "de"
  }
}
```

Use `content.ProjectFieldWithLookup` when a debug, QA, or admin payload should include lookup status and fallback
metadata. The default payload stays client-facing.

## Catalog Workflow with lingo
Create a project config:

```sh
lingo config init
```

By default this writes `./lingo.toml`. `--config`, `LINGO_CONFIG`, and discovered project config files are resolved
before the user-level XDG config path. Relative paths inside a config file are resolved from that config file's
directory.

Example config:

```toml
version = 1

[extract]
roots = ['.']
keywords = []

[catalogs]
template = 'locales/messages.pot'
locale_dir = 'locales'
locales = ['de', 'fr']
```

With empty `keywords`, extraction recognizes the built-in `T`, `Tn`, `Tc`, and `Tnc` call shapes. Add signatures for
wrappers:

```toml
[extract]
roots = ['.']
keywords = [
  'Tr:msg=1,vars=2',
  'Trn:count=1,msg=2,plural=3,vars=4',
  "Billing:domain='billing',msg=1,vars=2",
]
```

Common workflow:

```sh
lingo --config lingo.toml update
lingo --config lingo.toml check --strict
lingo --config lingo.toml format
lingo --config lingo.toml stale
```

What the commands do:

| Command | Purpose |
| --- | --- |
| `lingo extract` | Print the extracted POT document to stdout. |
| `lingo update` | Update the POT template and merge configured locale PO files. |
| `lingo update --dry-run` | Report files that would change without writing them. |
| `lingo check --strict` | Validate catalogs and fail when generated outputs are stale or strict checks fail. |
| `lingo format` | Rewrite configured catalogs deterministically. |
| `lingo stale` | Report active locale entries that are no longer extracted from source. |
| `lingo data sources` | Print CLDR source metadata and checksums. |
| `lingo data check` | Verify generated CLDR data is current. |
| `lingo data diff` | Show which generated CLDR files would change. |
| `lingo data update --dry-run` | Preview a CLDR data update without writing files. |

JSON output is available for automation:

```sh
lingo --config lingo.toml --json check --strict
```

## CLDR Data Maintenance
Runtime formatting and profile defaults use generated CLDR data included in the module. Maintainers can refresh that
data from local CLDR assets with `lingo`; the source and checksums are recorded in `cldr.lock.json`:

```sh
lingo data sources
lingo data check
lingo data diff
lingo data update --dry-run
lingo data update
```

The update command uses the configured CLDR source directory, normally `assets/cldr-json-48.2.0`, and writes changes
atomically.

## Locale-Aware Text
The `locale` package also exposes helpers over `golang.org/x/text`:

```go
profile := locale.ProfileForTag(language.Swedish)
sorted := locale.SortStrings(profile, []string{"z", "å", "ä", "a"})

tr := locale.ProfileForTag(language.Turkish)
lower := locale.Lower(tr, "Iİ")
```

These helpers use the CLDR/Unicode data bundled with `golang.org/x/text`, not the generated formatting data. Use
`locale.TextVersions()` when you need to report those data versions.

## Observability
Per-call diagnostics are returned through `Lookup`. App-level events use the `observe` package:

```go
collector := observe.NewCollector()
rt := i18n.NewRuntime(cat, i18n.WithObserver(collector))

ctx := observe.WithAttrs(context.Background(),
	observe.Bounded("route", "GET /invoices/{id}"),
	observe.Private("tenant_id", tenantID),
)

tr := rt.Localizer("de").WithContext(ctx)
_ = tr.T("Invoice amount: {amount}", i18n.Arg(
	"amount",
	i18n.Currency(total, i18n.CurrencyCode(locale.Currency("EUR"))),
))

for _, event := range collector.Events() {
	log.Printf("i18n event: %s %s", event.Name, event.Code)
}
```

Lookup issue events can include bounded source message identity, lookup metadata, catalog versions, formatter data
versions, diagnostics, and app-owned context attributes. Interpolation values and rendered localized text are not
captured automatically.

The core library does not import logging, metrics, tracing, HTTP, or OpenTelemetry packages. Applications adapt events
to their own observability stack.

## Limits and Non-goals
- No full ICU MessageFormat language.
- No HTML-safe output from plain `T` APIs.
- No forced API error envelope.
- No automatic financial currency inference.
- No CLDR list/unit patterns, compact decimals, plural-rule tables, localized display names, or full timezone names in
  the current generated formatting data.

## Development
Repository developer commands:

```sh
make help
make test
make race
make lint
make build
make vuln
```

## Troubleshooting
`lingo check --strict` says catalogs are stale:

Run `lingo update`, review the PO/POT diff, fill translations as needed, then run `lingo check --strict` again.

`gettext.LoadFS` cannot parse a PO file:

Run `lingo format` or `lingo check --strict` to get a deterministic parser or validation failure. Check multiline string
quoting, plural forms, and placeholder parity.

Translated output is still English:

Check that the `.po` file is embedded, its file name or `Language` header matches the requested locale, the entry is not
fuzzy or obsolete, and the runtime was built from the loaded catalog.

Currency formatting picks an unexpected currency:

Pass the financial currency explicitly with `i18n.CurrencyCode`. Profile currency is only a display/default preference.

HTTP responses vary by language but caches serve the wrong content:

Enable `web.Options{Vary: true}` and consider `PrivateWhenUserSourceWins` when using cookies, sessions, or profile
sources.

## License
This repository is licensed under the [MIT License](LICENSE). Generated Unicode/CLDR data metadata records the Unicode
data license used by the generated data.
