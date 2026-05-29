# go-i18n showcase example

This example is a guided, line-oriented terminal app that demonstrates the library as an application would use it:
embedded PO catalogs, per-profile translators, typed formatting values, gettext contexts, domains, HTTP middleware,
content payloads, locale-aware sorting, diagnostics, and workflow checks.

Run it from the repository root:

```sh
go run ./examples/showcase
```

Controls:

| Key | Action |
| --- | --- |
| `1` or `en` | English |
| `2` or `de` | German |
| `3` or `fr` | French |
| `4` or `es` | Spanish |
| `5` or `zh` | Chinese |
| `6` or `ar` | Arabic |
| `n`, `+` | Increment the task count |
| `p`, `-` | Decrement the task count |
| `d` | Toggle the billing domain |
| `q` | Quit |

What it shows:

- `gettext.LoadFS` loading embedded `locales/*.po` files into an immutable `i18n.Catalog`.
- `locale.NewResolver().ResolveDetailed` building per-language profiles with language, region, timezone, and currency.
- `Runtime.Translator(profile)` creating a request-like translator snapshot.
- `T`, `Tn`, and `Tc` for normal messages, plural messages, and gettext message context.
- A tiny `billingT` wrapper extracted into the `billing` domain through `lingo.toml`.
- Direct checked lookups with `tr.Lookup(catalogText(...))`, so the code shows lookup status and diagnostics without
  hiding the main API call.
- Typed values for currency, dates, and percentages, including profile numbering-system behavior.
- Formatter diagnostics surfaced from checked lookup results when formatting genuinely falls back.
- Typed values for CLDR-backed lists, compact numbers, elapsed durations, relative time, intervals, and display names
  can use the same profile-bound formatter surface as currency and dates.
- `content.ProjectFieldWithLookup` projecting a localized field payload with explicit diagnostic lookup metadata.
- `web.Middleware` running through `httptest`, including request locale negotiation, localizer context, `Vary`, and
  `Content-Language`.
- `locale.SortStrings` showing locale-aware text ordering.
- Arabic plural forms with six gettext plural slots and Chinese with one plural slot.

The code in `main.go` is intentionally split into named `render...Section` functions. Each section focuses on one
library feature, and comments call out the significant library steps.

Catalog workflow:

```sh
bin/lingo --config examples/showcase/lingo.toml update
bin/lingo --config examples/showcase/lingo.toml check --strict
bin/lingo --config examples/showcase/lingo.toml format
```

Optional CLDR bundle planning:

```sh
bin/lingo --config examples/showcase/lingo.toml data bundles plan --json
```

The configured showcase bundle uses `features = ["auto"]` and `scan_roots = ["./examples/showcase"]`, so `lingo`
scans this example and includes only the CLDR feature domains implied by its typed formatting and profile-resolution
calls.

The example config uses explicit extraction signatures because it demonstrates app helpers:

```toml
keywords = [
  'T:msg=1,vars=2',
  'Tn:count=1,msg=2,plural=3,vars=4',
  'Tc:ctx=1,msg=2,vars=3',
  'Tnc:ctx=1,count=2,msg=3,plural=4,vars=5',
  "billingT:domain='billing',msg=2,vars=3",
  'catalogText:msg=1',
]
```

`billingT` demonstrates a domain-specific wrapper. `catalogText` marks `i18n.Message` values used with checked lookups
or API payload helpers while keeping the actual `tr.Lookup(...)` and `content.ProjectFieldWithLookup(...)` calls visible.

Run the focused tests:

```sh
go test ./examples/showcase
```
