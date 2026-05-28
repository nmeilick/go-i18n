package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nmeilick/go-i18n/content"
	"github.com/nmeilick/go-i18n/gettext"
	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
	"github.com/nmeilick/go-i18n/web"
)

// Embed the PO files so the example behaves like a normal Go application:
// catalogs are compiled into the binary and loaded once at startup.
//
//go:embed locales/*.po
var localeFS embed.FS

type showcaseLanguage struct {
	Key      string
	Tag      string
	Name     string
	Person   string
	Region   string
	Currency locale.CurrencyCode
	TimeZone string
	Words    []string
}

var languages = []showcaseLanguage{
	{Key: "1", Tag: "en", Name: "English", Person: "Ada", Region: "US", Currency: "USD", TimeZone: "America/New_York", Words: []string{"zoo", "apple", "eclair"}},
	{Key: "2", Tag: "de", Name: "Deutsch", Person: "Ada", Region: "DE", Currency: "EUR", TimeZone: "Europe/Berlin", Words: []string{"zoo", "äpfel", "apfel"}},
	{Key: "3", Tag: "fr", Name: "Français", Person: "Ada", Region: "FR", Currency: "EUR", TimeZone: "Europe/Paris", Words: []string{"zèbre", "éclair", "eau"}},
	{Key: "4", Tag: "es", Name: "Español", Person: "Ada", Region: "ES", Currency: "EUR", TimeZone: "Europe/Madrid", Words: []string{"zapato", "ñandú", "naranja"}},
	{Key: "5", Tag: "zh", Name: "中文", Person: "小林", Region: "CN", Currency: "CNY", TimeZone: "Asia/Shanghai", Words: []string{"上海", "北京", "广州"}},
	{Key: "6", Tag: "ar", Name: "العربية", Person: "ليلى", Region: "EG", Currency: "EGP", TimeZone: "Africa/Cairo", Words: []string{"كتاب", "أب", "بيت"}},
}

type appModel struct {
	runtime       *i18n.Runtime
	selected      int
	count         int
	billingDomain bool
	now           time.Time
	lastError     string
}

type showcaseView struct {
	model    *appModel
	lang     showcaseLanguage
	profile  locale.Profile
	resolved locale.ResolveResult
	tr       *i18n.Localizer
}

func main() {
	rt, err := newRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "showcase: %v\n", err)
		os.Exit(1)
	}
	model := appModel{runtime: rt, selected: 0, count: 3, now: time.Date(2026, 5, 13, 15, 30, 0, 0, time.UTC)}
	if err := run(os.Stdin, os.Stdout, &model); err != nil {
		fmt.Fprintf(os.Stderr, "showcase: %v\n", err)
		os.Exit(1)
	}
}

func newRuntime() (*i18n.Runtime, error) {
	// gettext.LoadFS parses every embedded PO file and returns an immutable
	// catalog. Fuzzy and obsolete PO entries are kept in files but not served.
	cat, err := gettext.LoadFS(localeFS, "locales/*.po", gettext.DefaultLocale("en"))
	if err != nil {
		return nil, err
	}
	return i18n.NewRuntime(cat), nil
}

func run(in io.Reader, out io.Writer, model *appModel) error {
	reader := bufio.NewReader(in)
	for {
		fmt.Fprint(out, "\x1b[2J\x1b[H")
		fmt.Fprint(out, render(model))
		fmt.Fprint(out, "\n> ")
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if handleCommand(model, strings.TrimSpace(line)) {
			return nil
		}
	}
}

func handleCommand(model *appModel, command string) bool {
	model.lastError = ""
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "", "r":
		return false
	case "q", "quit", "exit":
		return true
	case "n", "+", "next":
		model.count++
		return false
	case "p", "-", "prev":
		if model.count > 0 {
			model.count--
		}
		return false
	case "d", "domain":
		model.billingDomain = !model.billingDomain
		return false
	}
	for i, lang := range languages {
		if command == lang.Key || strings.EqualFold(command, lang.Tag) {
			model.selected = i
			return false
		}
	}
	model.lastError = command
	return false
}

func render(model *appModel) string {
	view, err := newShowcaseView(model)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	renderHeader(&b, view)
	renderRuntimeSection(&b, view)
	formatterDiagnostics := renderFormattingSection(&b, view)
	renderResolverSection(&b, view)
	renderLocaleTextSection(&b, view)
	renderPayloadSection(&b, view)
	renderWebAdapterSection(&b, view)
	renderDiagnosticsSection(&b, view, formatterDiagnostics)
	line(&b, "%s", view.tr.T("Run lingo check from this directory to validate the example catalogs."))
	return b.String()
}

func newShowcaseView(model *appModel) (showcaseView, error) {
	lang := languages[model.selected]
	profile, resolved, err := profileFor(lang)
	if err != nil {
		return showcaseView{}, err
	}
	return showcaseView{
		model:    model,
		lang:     lang,
		profile:  profile,
		resolved: resolved,
		// Runtime.Translator returns a localizer tied to this profile. In a web
		// app this is usually one per request; in this terminal app it is one per
		// selected language.
		tr: model.runtime.Translator(profile),
	}, nil
}

func renderHeader(b *strings.Builder, view showcaseView) {
	line(b, "go-i18n :: %s", view.tr.T("Go i18n showcase"))
	line(b, "%s", strings.Repeat("=", 76))
	line(b, "%s: %s  %s", view.tr.T("Language"), view.lang.Name, languageMenu(view.model.selected))
	line(b, "%s", view.tr.T("Use 1-6 or a locale code to switch language. n/p changes the count, d toggles the domain, q quits."))
	if view.model.lastError != "" {
		line(b, "! %s", view.tr.T("Unknown command: {command}", i18n.Arg("command", view.model.lastError)))
	}
	line(b, "")
}

func renderRuntimeSection(b *strings.Builder, view showcaseView) {
	section(b, view.tr.T("Runtime"))

	// T translates a singular message and interpolates named variables.
	line(b, "%-20s %s", view.tr.T("Greeting"), view.tr.T("Hello {name}", i18n.Arg("name", view.lang.Person)))

	// Tn chooses the right gettext plural form for the active locale and exposes
	// the count as {n}.
	line(b, "%-20s %s", view.tr.T("Plural"), view.tr.Tn(view.model.count, "{n} task", "{n} tasks"))

	// Tc adds gettext context so the same source text can have different
	// translations in different UI meanings.
	line(b, "%-20s %s / %s", view.tr.T("Context"), view.tr.Tc("button", "Open"), view.tr.Tc("status", "Open"))

	// Domain-specific wrappers are common in larger apps. lingo.toml maps this
	// wrapper to the "billing" gettext domain during extraction.
	if view.model.billingDomain {
		line(b, "%-20s %s", view.tr.T("Domain"), billingT(view.tr, "Invoice {number}", i18n.Arg("number", "A-1042")))
		line(b, "%-20s %s", "", view.tr.T("Billing domain is active"))
	} else {
		line(b, "%-20s %s", view.tr.T("Domain"), view.tr.T("Default domain is active"))
	}
	line(b, "")
}

func renderFormattingSection(b *strings.Builder, view showcaseView) []i18n.Diagnostic {
	section(b, view.tr.T("Formatting"))

	// Checked lookups return text plus status/diagnostics. Typed values such as
	// Currency, Date, and Percent format through the active locale profile.
	due := view.model.now.Add(72 * time.Hour)
	total := view.tr.Lookup(catalogText("Total {amount} due {date}"), i18n.Vars{
		"amount": i18n.Currency(1234.5, i18n.CurrencyCode(view.lang.Currency)),
		"date":   i18n.Date(due, i18n.Style("medium")),
	})
	formatterDiagnostics := appendFormatterDiagnostics(nil, total.Diagnostics)
	line(b, "%-20s %s", "", total.Text)
	line(b, "%-20s %s", "", view.tr.T("Progress: {percent}", i18n.Arg("percent", i18n.Percent(0.742))))
	line(b, "%-20s %s", "", view.tr.T("Visible sections: {catalogs}, {profiles}, and {formatting}", i18n.Vars{
		"catalogs":   view.tr.T("Catalogs"),
		"profiles":   view.tr.T("Profiles"),
		"formatting": view.tr.T("Formatting"),
	}))

	// Units and intervals are ordinary catalog messages here, so the example does
	// not imply that CLDR list or unit pattern data is bundled.
	line(b, "%-20s %s", "", view.tr.Tn(13, "Distance: {n} kilometer", "Distance: {n} kilometers"))
	line(b, "%-20s %s", "", view.tr.T("Refresh interval: {interval}", i18n.Arg("interval", intervalText(view.tr, 90*time.Minute))))
	line(b, "")
	return formatterDiagnostics
}

func renderResolverSection(b *strings.Builder, view showcaseView) {
	section(b, view.tr.T("Resolver profile"))
	line(b, "%-20s %s", "", view.tr.T("Profile language: {language}", i18n.Arg("language", view.profile.PrimaryLanguage().String())))
	line(b, "%-20s %s", "", view.tr.T("Display currency: {currency}", i18n.Arg("currency", view.profile.Currency().String())))
	line(b, "%-20s %s", "", view.tr.T("Timezone: {timezone}", i18n.Arg("timezone", view.profile.TimeZone().String())))
	line(b, "%-20s %s", "", view.tr.T("Formatting region: {region}", i18n.Arg("region", view.profile.FormattingRegion())))
	line(b, "%-20s %s", "CLDR", view.resolved.DataVersion)
	line(b, "")
}

func renderLocaleTextSection(b *strings.Builder, view showcaseView) {
	section(b, view.tr.T("Locale text"))

	// Locale text helpers use the same profile that drives translation and
	// formatting. This shows collation order changing by selected language.
	sorted := locale.SortStrings(view.profile, view.lang.Words)
	line(b, "%-20s %s", "", view.tr.T("Sorted words: {first}, {second}, and {third}", i18n.Vars{
		"first":  sorted[0],
		"second": sorted[1],
		"third":  sorted[2],
	}))
	line(b, "")
}

func renderPayloadSection(b *strings.Builder, view showcaseView) {
	section(b, view.tr.T("API payload"))
	line(b, "%s", payloadPreview(view.tr))
	line(b, "")
}

func renderWebAdapterSection(b *strings.Builder, view showcaseView) {
	section(b, view.tr.T("Web adapter"))
	preview, err := middlewarePreview(view.model.runtime, view.lang)
	if err != nil {
		line(b, "%-20s %s", "error", err)
		line(b, "")
		return
	}
	line(b, "%-20s %s", "Content-Language", preview.ContentLanguage)
	line(b, "%-20s %s", "Vary", preview.Vary)
	line(b, "%-20s %s", view.tr.T("Greeting"), preview.Body)
	line(b, "")
}

func renderDiagnosticsSection(b *strings.Builder, view showcaseView, formatterDiagnostics []i18n.Diagnostic) {
	section(b, view.tr.T("Diagnostics"))

	// This message is deliberately not listed in lingo.toml keywords, so it stays
	// missing and demonstrates how checked lookup reports fallback behavior.
	missing := view.tr.Lookup(i18n.Text("This message is intentionally missing"))
	line(b, "%-20s %s", "", view.tr.T("Missing lookup status: {status}", i18n.Arg("status", string(missing.Status))))
	line(b, "%-20s %s", "", diagnosticsText(view.tr, formatterDiagnostics))
	line(b, "")
}

func profileFor(lang showcaseLanguage) (locale.Profile, locale.ResolveResult, error) {
	// The resolver accepts observations from different sources. This example uses
	// explicit user preferences for language, region, timezone, and currency.
	langObs, err := locale.LanguageObservation(lang.Tag,
		locale.FromSource(locale.SourceExplicitUser, locale.TrustExplicit, locale.SensitivityUserPrivate),
		locale.Explicit(),
	)
	if err != nil {
		return locale.Profile{}, locale.ResolveResult{}, err
	}
	regionObs, err := locale.RegionObservation(lang.Region, locale.RegionFormatting,
		locale.FromSource(locale.SourceExplicitUser, locale.TrustExplicit, locale.SensitivityUserPrivate),
		locale.Explicit(),
	)
	if err != nil {
		return locale.Profile{}, locale.ResolveResult{}, err
	}
	timeObs, err := locale.TimeZoneObservation(lang.TimeZone,
		locale.FromSource(locale.SourceExplicitUser, locale.TrustExplicit, locale.SensitivityUserPrivate),
		locale.Explicit(),
	)
	if err != nil {
		return locale.Profile{}, locale.ResolveResult{}, err
	}
	curObs, err := locale.CurrencyObservation(lang.Currency, locale.CurrencyDisplay,
		locale.FromSource(locale.SourceExplicitUser, locale.TrustExplicit, locale.SensitivityUserPrivate),
		locale.Explicit(),
	)
	if err != nil {
		return locale.Profile{}, locale.ResolveResult{}, err
	}
	res, err := locale.NewResolver().ResolveDetailed(langObs, regionObs, timeObs, curObs)
	return res.Profile, res, err
}

func supportedLocaleTags() []string {
	tags := make([]string, 0, len(languages))
	for _, lang := range languages {
		tags = append(tags, lang.Tag)
	}
	return tags
}

func languageByTag(tag string) (showcaseLanguage, bool) {
	for _, lang := range languages {
		if lang.Tag == tag {
			return lang, true
		}
	}
	return showcaseLanguage{}, false
}

func languageMenu(selected int) string {
	parts := make([]string, 0, len(languages))
	for i, lang := range languages {
		marker := " "
		if i == selected {
			marker = "*"
		}
		parts = append(parts, fmt.Sprintf("[%s%s %s]", marker, lang.Key, lang.Tag))
	}
	return strings.Join(parts, " ")
}

func payloadPreview(tr *i18n.Localizer) string {
	// ProjectFieldWithLookup is the diagnostic variant. Normal public API
	// responses should usually use ProjectField, which omits lookup metadata.
	payload := content.ProjectFieldWithLookup(tr, "email", "user.email", "required", catalogText("Email is required"))
	data, err := json.Marshal(payload)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

type middlewarePreviewResult struct {
	ContentLanguage string
	Vary            string
	Body            string
}

func middlewarePreview(rt *i18n.Runtime, lang showcaseLanguage) (middlewarePreviewResult, error) {
	neg, err := locale.NewNegotiator("en", supportedLocaleTags())
	if err != nil {
		return middlewarePreviewResult{}, err
	}
	middleware := web.Middleware(rt, neg, web.Options{
		Sources:         []web.Source{web.AcceptLanguage()},
		Profile:         showcaseHTTPProfile,
		ContentLanguage: true,
		Vary:            true,
	})

	// httptest keeps the example terminal-only while exercising the real
	// net/http middleware and request context integration.
	req := httptest.NewRequest(http.MethodGet, "/showcase", nil)
	req.Header.Set("Accept-Language", lang.Tag+",en;q=0.4")
	rec := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr, ok := web.Localizer(r.Context())
		if !ok {
			http.Error(w, "missing localizer", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, tr.T("Hello {name}", i18n.Arg("name", lang.Person)))
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return middlewarePreviewResult{}, fmt.Errorf("middleware returned HTTP %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	return middlewarePreviewResult{
		ContentLanguage: rec.Header().Get("Content-Language"),
		Vary:            rec.Header().Get("Vary"),
		Body:            strings.TrimSpace(rec.Body.String()),
	}, nil
}

func showcaseHTTPProfile(_ *http.Request, resolved locale.Resolved) (locale.Profile, error) {
	lang, ok := languageByTag(resolved.Locale)
	if !ok {
		return locale.NewProfile([]string{resolved.Locale})
	}
	profile, _, err := profileFor(lang)
	return profile, err
}

func intervalText(tr *i18n.Localizer, d time.Duration) string {
	if d < 0 {
		d = -d
	}
	totalMinutes := int(d / time.Minute)
	hours := totalMinutes / 60
	minutes := totalMinutes % 60
	if hours > 0 && minutes > 0 {
		return tr.T("{hours} and {minutes}", i18n.Vars{
			"hours":   tr.Tn(hours, "{n} hour", "{n} hours"),
			"minutes": tr.Tn(minutes, "{n} minute", "{n} minutes"),
		})
	}
	if hours > 0 {
		return tr.Tn(hours, "{n} hour", "{n} hours")
	}
	return tr.Tn(minutes, "{n} minute", "{n} minutes")
}

// billingT demonstrates a domain-specific app helper. The extractor config maps
// this function to the "billing" domain, while runtime code uses WithDomain.
func billingT(tr *i18n.Localizer, msg string, vars ...i18n.Vars) string {
	return tr.WithDomain("billing").Lookup(i18n.Text(msg), vars...).Text
}

// catalogText marks non-T/Tn/Tc message values that should be extracted into
// PO catalogs while keeping the actual tr.Lookup call visible at the call site.
func catalogText(msg string) i18n.Message {
	return i18n.Text(msg)
}

func appendFormatterDiagnostics(out []i18n.Diagnostic, diagnostics []i18n.Diagnostic) []i18n.Diagnostic {
	for _, diagnostic := range diagnostics {
		if diagnostic.Component == "formatter" || diagnostic.FormatCode != "" {
			out = append(out, diagnostic)
		}
	}
	return out
}

func diagnosticsText(tr *i18n.Localizer, diagnostics []i18n.Diagnostic) string {
	if len(diagnostics) == 0 {
		return tr.T("No formatter diagnostics")
	}
	parts := make([]string, 0, len(diagnostics))
	seen := map[string]bool{}
	for _, diagnostic := range diagnostics {
		code := diagnostic.FormatCode
		if code == "" {
			code = diagnostic.Code
		}
		if diagnostic.Placeholder != "" {
			code += "(" + diagnostic.Placeholder + ")"
		}
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		parts = append(parts, code)
	}
	sort.Strings(parts)
	return tr.T("Formatter diagnostics: {diagnostics}", i18n.Arg("diagnostics", strings.Join(parts, ", ")))
}

func section(b *strings.Builder, title string) {
	line(b, "-- %s %s", title, strings.Repeat("-", max(1, 70-len([]rune(title)))))
}

func line(b *strings.Builder, format string, args ...any) {
	fmt.Fprintf(b, format, args...)
	b.WriteByte('\n')
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
