package locale

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nmeilick/go-i18n/internal/cldrdata"
	"github.com/nmeilick/go-i18n/observe"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

const defaultMaxFormattedRunes = 8192

// NumberKind describes numeric formatting intent.
type NumberKind string

const (
	NumberDecimal NumberKind = "decimal"
	NumberPercent NumberKind = "percent"
	NumberCompact NumberKind = "compact"
)

// CurrencySource describes where a currency code came from.
type CurrencySource string

const (
	CurrencyExplicit CurrencySource = "explicit"
	CurrencyProfile  CurrencySource = "profile"
)

// CurrencyDisplayMode selects how a currency code is rendered.
type CurrencyDisplayMode string

const (
	CurrencyDisplaySymbol       CurrencyDisplayMode = "symbol"
	CurrencyDisplayNarrowSymbol CurrencyDisplayMode = "narrow-symbol"
	CurrencyDisplayCode         CurrencyDisplayMode = "code"
)

// DateTimeKind describes date/time formatting intent.
type DateTimeKind string

const (
	DateOnly DateTimeKind = "date"
	TimeOnly DateTimeKind = "time"
	DateTime DateTimeKind = "datetime"
)

// ListType describes CLDR list pattern families.
type ListType string

const (
	ListStandard ListType = "standard"
	ListOr       ListType = "or"
	ListUnit     ListType = "unit"
)

// UnitWidth describes localized unit width.
type UnitWidth string

const (
	UnitLong   UnitWidth = "long"
	UnitShort  UnitWidth = "short"
	UnitNarrow UnitWidth = "narrow"
)

// RelativeUnit is a CLDR relative-time field.
type RelativeUnit string

const (
	RelativeSecond RelativeUnit = "second"
	RelativeMinute RelativeUnit = "minute"
	RelativeHour   RelativeUnit = "hour"
	RelativeDay    RelativeUnit = "day"
	RelativeWeek   RelativeUnit = "week"
	RelativeMonth  RelativeUnit = "month"
	RelativeYear   RelativeUnit = "year"
)

// RelativeDirection describes whether a relative value is in the past or future.
type RelativeDirection string

const (
	RelativePast   RelativeDirection = "past"
	RelativeFuture RelativeDirection = "future"
)

// RelativeNumericMode controls whether named forms such as "yesterday" are allowed.
type RelativeNumericMode string

const (
	RelativeAuto   RelativeNumericMode = "auto"
	RelativeAlways RelativeNumericMode = "always"
)

// NumberSpec is a semantic request to format a number.
type NumberSpec struct {
	Value              any
	Kind               NumberKind
	MinFractionDigits  *int
	MaxFractionDigits  *int
	Precision          *int
	Grouping           *bool
	NumberingSystem    string
	CompactDisplayName string
}

// CurrencySpec is a semantic request to format a currency amount.
type CurrencySpec struct {
	Value          any
	Code           CurrencyCode
	CodeSource     CurrencySource
	Display        CurrencyDisplayMode
	Accounting     bool
	CashDigits     bool
	FractionDigits *int
	Number         NumberSpec
}

// DateTimeSpec is a semantic request to format a date, time, or datetime.
type DateTimeSpec struct {
	Value        time.Time
	Kind         DateTimeKind
	Width        string
	TimeZone     *time.Location
	PreserveZone bool
	Calendar     string
}

// ListSpec is a semantic request to format a list.
type ListSpec struct {
	Items []string
	Type  ListType
	Width string
}

// UnitSpec is a semantic request to format a value and unit. It does not
// convert units.
type UnitSpec struct {
	Value  any
	Unit   string
	Width  UnitWidth
	Number NumberSpec
}

// DurationSpec is a semantic request to format a duration.
type DurationSpec struct {
	Value time.Duration
	Width UnitWidth
}

// Period is a calendar-style quantity. It is not normalized and does not
// compute calendar differences from two dates.
type Period struct {
	Years  int
	Months int
	Weeks  int
	Days   int
}

// PeriodSpec is a semantic request to format a calendar-style period.
type PeriodSpec struct {
	Value Period
	Width UnitWidth
}

// RelativeSpec is a semantic request to format an explicit relative quantity.
type RelativeSpec struct {
	Value     int64
	Unit      RelativeUnit
	Direction RelativeDirection
	Width     UnitWidth
	Numeric   RelativeNumericMode
}

// RelativeTimeSpec derives a relative quantity from two instants.
type RelativeTimeSpec struct {
	Target    time.Time
	Reference time.Time
	Width     UnitWidth
	Numeric   RelativeNumericMode
	TimeZone  *time.Location
}

// DateTimeIntervalSpec is a semantic request to format a date/time range.
type DateTimeIntervalSpec struct {
	Start        time.Time
	End          time.Time
	Kind         DateTimeKind
	Width        string
	TimeZone     *time.Location
	PreserveZone bool
	Calendar     string
}

// FormatPolicy controls strictness and safety limits for formatter calls.
type FormatPolicy struct {
	Strict                      bool
	MaxFormattedRunes           int
	AllowProfileCurrency        bool
	AllowUnsupportedPreferences bool
	AllowTimeZoneNameFallback   bool
}

// DefaultFormatPolicy returns production-safe lenient defaults.
func DefaultFormatPolicy() FormatPolicy {
	return FormatPolicy{
		MaxFormattedRunes:           defaultMaxFormattedRunes,
		AllowProfileCurrency:        true,
		AllowUnsupportedPreferences: true,
		AllowTimeZoneNameFallback:   true,
	}
}

// StrictFormatPolicy returns CI/release-oriented defaults.
func StrictFormatPolicy() FormatPolicy {
	p := DefaultFormatPolicy()
	p.Strict = true
	p.AllowProfileCurrency = false
	p.AllowUnsupportedPreferences = false
	p.AllowTimeZoneNameFallback = false
	return p
}

// FormatContext binds formatting to a resolved locale and immutable profile.
type FormatContext struct {
	Profile        Profile
	Locale         language.Tag
	MessageLocale  string
	Policy         FormatPolicy
	DataVersion    string
	ObserveContext context.Context
	Observer       observe.Observer
	ObservePolicy  observe.Policy
	ObserveAttrs   []observe.Attr
}

// FormatDiagnostic is a stable, redaction-safe formatter diagnostic.
type FormatDiagnostic struct {
	Code           string `json:"code"`
	Severity       string `json:"severity,omitempty"`
	Component      string `json:"component,omitempty"`
	Kind           string `json:"kind,omitempty"`
	SpecField      string `json:"spec_field,omitempty"`
	Locale         string `json:"locale,omitempty"`
	FallbackLocale string `json:"fallback_locale,omitempty"`
	Source         string `json:"source,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

// FormatError carries formatter diagnostics as an error.
type FormatError struct {
	Diagnostic FormatDiagnostic
}

func (e *FormatError) Error() string {
	if e == nil {
		return ""
	}
	if e.Diagnostic.Detail != "" {
		return e.Diagnostic.Code + ": " + e.Diagnostic.Detail
	}
	return e.Diagnostic.Code
}

// Formatter formats semantic value specs as bounded plain text.
type Formatter interface {
	FormatNumber(FormatContext, NumberSpec) (string, []FormatDiagnostic)
	FormatCurrency(FormatContext, CurrencySpec) (string, []FormatDiagnostic)
	FormatDateTime(FormatContext, DateTimeSpec) (string, []FormatDiagnostic)
	FormatList(FormatContext, ListSpec) (string, []FormatDiagnostic)
	FormatUnit(FormatContext, UnitSpec) (string, []FormatDiagnostic)
	FormatDuration(FormatContext, DurationSpec) (string, []FormatDiagnostic)
	FormatPeriod(FormatContext, PeriodSpec) (string, []FormatDiagnostic)
	FormatRelative(FormatContext, RelativeSpec) (string, []FormatDiagnostic)
	FormatRelativeTime(FormatContext, RelativeTimeSpec) (string, []FormatDiagnostic)
	FormatDateTimeInterval(FormatContext, DateTimeIntervalSpec) (string, []FormatDiagnostic)
	DataVersion() string
}

// CLDRFormatter is the default formatter backed by the generated compact CLDR
// provider and x/text numeric formatting.
type CLDRFormatter struct {
	data cldrdata.Provider
}

// NewCLDRFormatter creates a CLDR formatter from an internal provider. A nil
// provider uses the built-in generated data bundle. Most applications can use
// DefaultFormatter or locale/cldr.Services instead of constructing this
// directly.
func NewCLDRFormatter(provider cldrdata.Provider) *CLDRFormatter {
	if provider == nil {
		provider = cldrdata.Default()
	}
	return &CLDRFormatter{data: provider}
}

// DefaultFormatter returns the default generated-data formatter.
func DefaultFormatter() Formatter { return NewCLDRFormatter(nil) }

func (f *CLDRFormatter) DataVersion() string {
	meta := f.data.Metadata()
	if meta.CLDRVersion == "" {
		return ""
	}
	return "cldr-" + meta.CLDRVersion
}

func (f *CLDRFormatter) FormatNumber(ctx FormatContext, spec NumberSpec) (string, []FormatDiagnostic) {
	if err := f.validateFormatPreferences(ctx, "number"); err != nil {
		return finishFormat(ctx, "number", "", []FormatDiagnostic{*err})
	}
	tag := effectiveFormatTag(ctx)
	opts := []number.Option{}
	if spec.Grouping != nil && !*spec.Grouping {
		opts = append(opts, number.NoSeparator())
	}
	if spec.MinFractionDigits != nil {
		opts = append(opts, number.MinFractionDigits(*spec.MinFractionDigits))
	}
	if spec.MaxFractionDigits != nil {
		opts = append(opts, number.MaxFractionDigits(*spec.MaxFractionDigits))
	}
	if spec.Precision != nil {
		opts = append(opts, number.Precision(*spec.Precision))
	}
	if err := validateNumber(spec.Value); err != nil {
		return finishFormatFallback(ctx, "number", "invalid_number", err.Error())
	}
	p := message.NewPrinter(tag)
	var text string
	switch spec.Kind {
	case NumberPercent:
		text = p.Sprintf("%v", number.Percent(spec.Value, opts...))
	case NumberCompact:
		return f.formatCompactNumber(ctx, spec, opts)
	default:
		text = p.Sprintf("%v", number.Decimal(spec.Value, opts...))
	}
	return finishBoundFormatted(ctx, "number", text)
}

func (f *CLDRFormatter) formatCompactNumber(ctx FormatContext, spec NumberSpec, opts []number.Option) (string, []FormatDiagnostic) {
	value, err := numberAsFloat(spec.Value)
	if err != nil {
		return finishFormatFallback(ctx, "number", "invalid_number", err.Error())
	}
	magnitude := compactMagnitude(value)
	if magnitude == 0 {
		spec.Kind = NumberDecimal
		return f.FormatNumber(ctx, spec)
	}
	width := normalizeCompactWidth(spec.CompactDisplayName)
	pattern, ok := f.data.CompactPattern(effectiveFormatTag(ctx).String(), width, magnitude, f.pluralCategory(ctx, compactPluralValue(value, magnitude)))
	if !ok {
		pattern, ok = f.data.CompactPattern(effectiveFormatTag(ctx).String(), width, magnitude, "other")
	}
	if !ok || pattern == "0" {
		if policy(ctx).Strict {
			return finishFormatFallback(ctx, "number", "compact_pattern_unavailable", fmt.Sprint(magnitude))
		}
		spec.Kind = NumberDecimal
		text, ds := f.FormatNumber(ctx, spec)
		ds = append(ds, fallbackFormatDiagnostic(ctx, "number", "compact_pattern_unavailable", fmt.Sprint(magnitude), "info"))
		return finishFormat(ctx, "number", text, ds)
	}
	scaled := value / compactDivisor(pattern, magnitude)
	numSpec := spec
	numSpec.Value = scaled
	numSpec.Kind = NumberDecimal
	digits := compactFractionDigits(pattern)
	numSpec.MinFractionDigits = &digits
	numSpec.MaxFractionDigits = &digits
	num, ds := f.FormatNumber(withoutFormatObserver(ctx), numSpec)
	if len(ds) > 0 && policy(ctx).Strict {
		return finishFormat(ctx, "number", num, ds)
	}
	text := replaceCompactNumber(pattern, num)
	out, bds := boundFormatted(ctx, text, "number")
	return finishFormat(ctx, "number", out, append(ds, bds...))
}

func (f *CLDRFormatter) FormatCurrency(ctx FormatContext, spec CurrencySpec) (string, []FormatDiagnostic) {
	code := spec.Code
	source := spec.CodeSource
	if code == "" {
		code = ctx.Profile.Currency()
		source = CurrencyProfile
	}
	if code == "" || !code.Valid() {
		return finishFormatFallback(ctx, "currency", "currency_required", "currency code is required")
	}
	if source == CurrencyProfile && !policy(ctx).AllowProfileCurrency {
		return finishFormatFallback(ctx, "currency", "profile_currency_forbidden", "profile display currency is not allowed")
	}
	fraction := f.data.CurrencyFraction(code.String())
	digits := fraction.Digits
	if spec.CashDigits {
		digits = fraction.CashDigits
	}
	if spec.FractionDigits != nil {
		digits = *spec.FractionDigits
	}
	min := digits
	max := digits
	numSpec := spec.Number
	numSpec.Value = spec.Value
	numSpec.Kind = NumberDecimal
	numSpec.MinFractionDigits = &min
	numSpec.MaxFractionDigits = &max
	grouping := true
	numSpec.Grouping = &grouping
	num, ds := f.FormatNumber(withoutFormatObserver(ctx), numSpec)
	if len(ds) > 0 && policy(ctx).Strict {
		return finishFormat(ctx, "currency", num, ds)
	}
	diagnostics := append([]FormatDiagnostic(nil), ds...)
	tag := effectiveFormatTag(ctx)
	display := spec.Display
	if display == "" {
		display = CurrencyDisplaySymbol
	}
	symbol := code.String()
	if display != CurrencyDisplayCode {
		if s, ok := f.data.CurrencySymbol(tag.String(), code.String(), cldrdata.CurrencyDisplayMode(display)); ok && s != "" {
			symbol = s
		} else {
			diagnostics = append(diagnostics, FormatDiagnostic{
				Code:      "currency_symbol_unavailable",
				Severity:  "info",
				Component: "formatter",
				Kind:      "currency",
				Locale:    tag.String(),
				Detail:    code.String(),
			})
		}
	}
	rec, _ := f.localeRecord(tag.String())
	pattern := rec.CurrencyPattern
	if spec.Accounting && rec.Accounting != "" {
		pattern = rec.Accounting
	}
	if pattern == "" {
		pattern = "¤#,##0.00"
	}
	text := applyCurrencyPattern(pattern, symbol, num)
	out, bds := boundFormatted(ctx, text, "currency")
	return finishFormat(ctx, "currency", out, append(diagnostics, bds...))
}

func (f *CLDRFormatter) FormatDateTime(ctx FormatContext, spec DateTimeSpec) (string, []FormatDiagnostic) {
	calendar := spec.Calendar
	if calendar == "" {
		calendar = ctx.Profile.Calendar()
	}
	if calendar != "" && calendar != "gregory" {
		return finishFormatFallback(ctx, "datetime", "unsupported_calendar", calendar)
	}
	if err := f.validateFormatPreferences(ctx, "datetime"); err != nil {
		return finishFormat(ctx, "datetime", "", []FormatDiagnostic{*err})
	}
	t := spec.Value
	if !spec.PreserveZone {
		loc := spec.TimeZone
		if loc == nil {
			loc = ctx.Profile.TimeZone()
		}
		if loc != nil {
			t = t.In(loc)
		}
	}
	rec, localeUsed := f.localeRecord(effectiveFormatTag(ctx).String())
	cldrRequested := cldrLookupTag(effectiveFormatTag(ctx).String())
	width, ok := cldrdata.WidthIndex(spec.Width)
	if !ok && policy(ctx).Strict {
		return finishFormatFallback(ctx, "datetime", "unsupported_width", spec.Width)
	}
	var pattern string
	switch spec.Kind {
	case DateOnly:
		pattern = rec.DateFormats[width]
	case TimeOnly:
		pattern = rec.TimeFormats[width]
	default:
		tag := effectiveFormatTag(ctx)
		if !policy(ctx).AllowTimeZoneNameFallback && containsTimeZoneNameToken(rec.TimeFormats[width]) {
			return finishFormatFallback(ctx, "datetime", "timezone_name_unavailable", "full timezone names are not in the active CLDR data")
		}
		date := formatCLDRPattern(t, rec.DateFormats[width], rec, tag)
		tm := formatCLDRPattern(t, rec.TimeFormats[width], rec, tag)
		pattern = rec.DateTimeFormats[width]
		if pattern == "" {
			pattern = "{1}, {0}"
		}
		text := strings.ReplaceAll(strings.ReplaceAll(pattern, "{1}", date), "{0}", tm)
		out, ds := boundFormatted(ctx, text, "datetime")
		if localeUsed != cldrRequested {
			ds = append(ds, FormatDiagnostic{Code: "format_locale_fallback", Severity: "info", Component: "formatter", Kind: "datetime", Locale: cldrRequested, FallbackLocale: localeUsed})
		}
		return finishFormat(ctx, "datetime", out, ds)
	}
	if pattern == "" {
		return finishFormatFallback(ctx, "datetime", "missing_datetime_pattern", string(spec.Kind))
	}
	if !policy(ctx).AllowTimeZoneNameFallback && containsTimeZoneNameToken(pattern) {
		return finishFormatFallback(ctx, "datetime", "timezone_name_unavailable", "full timezone names are not in the active CLDR data")
	}
	text := formatCLDRPattern(t, pattern, rec, effectiveFormatTag(ctx))
	out, ds := boundFormatted(ctx, text, "datetime")
	if localeUsed != cldrRequested {
		ds = append(ds, FormatDiagnostic{Code: "format_locale_fallback", Severity: "info", Component: "formatter", Kind: "datetime", Locale: cldrRequested, FallbackLocale: localeUsed})
	}
	return finishFormat(ctx, "datetime", out, ds)
}

func (f *CLDRFormatter) FormatList(ctx FormatContext, spec ListSpec) (string, []FormatDiagnostic) {
	items := make([]string, 0, len(spec.Items))
	for _, item := range spec.Items {
		if strings.TrimSpace(item) != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return "", nil
	}
	if len(items) == 1 {
		return finishBoundFormatted(ctx, "list", items[0])
	}
	pattern, ok := f.data.ListPattern(effectiveFormatTag(ctx).String(), string(spec.Type), spec.Width)
	if !ok {
		if policy(ctx).Strict {
			return finishFormatFallback(ctx, "list", "list_pattern_unavailable", string(spec.Type))
		}
		pattern = fallbackListPattern(effectiveFormatTag(ctx), spec.Type)
	}
	text := applyListPattern(pattern, items)
	out, ds := boundFormatted(ctx, text, "list")
	if !ok {
		ds = append(ds, fallbackFormatDiagnostic(ctx, "list", "list_pattern_unavailable", string(spec.Type), "info"))
	}
	return finishFormat(ctx, "list", out, ds)
}

func (f *CLDRFormatter) FormatUnit(ctx FormatContext, spec UnitSpec) (string, []FormatDiagnostic) {
	numSpec := spec.Number
	numSpec.Value = spec.Value
	num, ds := f.FormatNumber(withoutFormatObserver(ctx), numSpec)
	if len(ds) > 0 && policy(ctx).Strict {
		return finishFormat(ctx, "unit", num, ds)
	}
	category := f.pluralCategory(ctx, spec.Value)
	pattern, ok := f.data.UnitPattern(effectiveFormatTag(ctx).String(), spec.Unit, string(spec.Width), category)
	if !ok {
		if policy(ctx).Strict {
			return finishFormatFallback(ctx, "unit", "unit_unavailable", spec.Unit)
		}
		pattern = "{0} " + spec.Unit
	}
	text := strings.ReplaceAll(pattern, "{0}", num)
	out, bds := boundFormatted(ctx, text, "unit")
	diagnostics := append([]FormatDiagnostic(nil), ds...)
	if !ok {
		diagnostics = append(diagnostics, fallbackFormatDiagnostic(ctx, "unit", "unit_unavailable", spec.Unit, "info"))
	}
	return finishFormat(ctx, "unit", out, append(diagnostics, bds...))
}

func (f *CLDRFormatter) FormatDuration(ctx FormatContext, spec DurationSpec) (string, []FormatDiagnostic) {
	d := spec.Value
	if d < 0 {
		d = -d
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	parts := []string{}
	diagnostics := []FormatDiagnostic{}
	subCtx := withoutFormatObserver(ctx)
	add := func(value int64, unit string) bool {
		text, ds := f.FormatUnit(subCtx, UnitSpec{Value: value, Unit: unit, Width: spec.Width})
		if len(ds) > 0 && policy(ctx).Strict {
			diagnostics = append(diagnostics, ds...)
			return false
		}
		diagnostics = append(diagnostics, ds...)
		parts = append(parts, text)
		return true
	}
	if h > 0 && !add(int64(h), "duration-hour") {
		return finishFormat(ctx, "duration", "", diagnostics)
	}
	if m > 0 && !add(int64(m), "duration-minute") {
		return finishFormat(ctx, "duration", "", diagnostics)
	}
	if (h == 0 && m == 0) || s > 0 {
		if !add(int64(s), "duration-second") {
			return finishFormat(ctx, "duration", "", diagnostics)
		}
	}
	text, lds := f.FormatList(subCtx, ListSpec{Items: parts, Type: ListUnit, Width: string(spec.Width)})
	diagnostics = append(diagnostics, lds...)
	return finishFormat(ctx, "duration", text, diagnostics)
}

func (f *CLDRFormatter) FormatPeriod(ctx FormatContext, spec PeriodSpec) (string, []FormatDiagnostic) {
	values := []struct {
		value int
		unit  string
	}{
		{spec.Value.Years, "duration-year"},
		{spec.Value.Months, "duration-month"},
		{spec.Value.Weeks, "duration-week"},
		{spec.Value.Days, "duration-day"},
	}
	sign := 0
	for _, item := range values {
		if item.value == 0 {
			continue
		}
		if item.value < 0 {
			if sign > 0 {
				return finishFormatFallback(ctx, "period", "invalid_period_mixed_sign", "period fields must have one sign")
			}
			sign = -1
			continue
		}
		if sign < 0 {
			return finishFormatFallback(ctx, "period", "invalid_period_mixed_sign", "period fields must have one sign")
		}
		sign = 1
	}
	parts := []string{}
	diagnostics := []FormatDiagnostic{}
	subCtx := withoutFormatObserver(ctx)
	for _, item := range values {
		if item.value == 0 {
			continue
		}
		value := item.value
		if value < 0 {
			value = -value
		}
		text, ds := f.FormatUnit(subCtx, UnitSpec{Value: value, Unit: item.unit, Width: spec.Width})
		if len(ds) > 0 && policy(ctx).Strict {
			return finishFormat(ctx, "period", text, ds)
		}
		diagnostics = append(diagnostics, ds...)
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		text, ds := f.FormatUnit(subCtx, UnitSpec{Value: 0, Unit: "duration-day", Width: spec.Width})
		return finishFormat(ctx, "period", text, ds)
	}
	text, ds := f.FormatList(subCtx, ListSpec{Items: parts, Type: ListUnit, Width: string(spec.Width)})
	diagnostics = append(diagnostics, ds...)
	return finishFormat(ctx, "period", text, diagnostics)
}

func (f *CLDRFormatter) FormatRelative(ctx FormatContext, spec RelativeSpec) (string, []FormatDiagnostic) {
	value := spec.Value
	if value < 0 {
		value = -value
	}
	width := spec.Width
	if width == "" {
		width = UnitLong
	}
	numeric := spec.Numeric
	if numeric == "" {
		numeric = RelativeAuto
	}
	if numeric == RelativeAuto {
		offset := relativeOffset(spec.Direction, value)
		if text, ok := f.data.RelativeSpecial(effectiveFormatTag(ctx).String(), string(spec.Unit), string(width), offset); ok {
			return finishBoundFormatted(ctx, "relative_time", text)
		}
	}
	category := f.pluralCategory(ctx, value)
	pattern, ok := f.data.RelativeTimePattern(effectiveFormatTag(ctx).String(), string(spec.Unit), string(width), string(spec.Direction), category)
	if !ok {
		if policy(ctx).Strict {
			return finishFormatFallback(ctx, "relative_time", "relative_time_pattern_unavailable", string(spec.Unit))
		}
		if spec.Direction == RelativeFuture {
			pattern = "in {0} " + string(spec.Unit)
		} else {
			pattern = "{0} " + string(spec.Unit) + " ago"
		}
	}
	num, ds := f.FormatNumber(withoutFormatObserver(ctx), NumberSpec{Value: value, Kind: NumberDecimal})
	if len(ds) > 0 && policy(ctx).Strict {
		return finishFormat(ctx, "relative_time", num, ds)
	}
	text := strings.ReplaceAll(pattern, "{0}", num)
	out, bds := boundFormatted(ctx, text, "relative_time")
	return finishFormat(ctx, "relative_time", out, append(ds, bds...))
}

func (f *CLDRFormatter) FormatRelativeTime(ctx FormatContext, spec RelativeTimeSpec) (string, []FormatDiagnostic) {
	loc := spec.TimeZone
	if loc == nil {
		loc = ctx.Profile.TimeZone()
	}
	target := spec.Target
	reference := spec.Reference
	if loc != nil {
		target = target.In(loc)
		reference = reference.In(loc)
	}
	if days := calendarDayDelta(target, reference); days >= -1 && days <= 1 {
		dir := RelativeFuture
		value := int64(days)
		if value < 0 {
			dir = RelativePast
			value = -value
		}
		return f.FormatRelative(ctx, RelativeSpec{Value: value, Unit: RelativeDay, Direction: dir, Width: spec.Width, Numeric: spec.Numeric})
	}
	delta := target.Sub(reference)
	dir := RelativeFuture
	if delta < 0 {
		dir = RelativePast
		delta = -delta
	}
	unit := RelativeSecond
	value := int64(math.Round(delta.Seconds()))
	switch {
	case delta >= 365*24*time.Hour:
		unit = RelativeYear
		value = int64(math.Round(delta.Hours() / (365 * 24)))
	case delta >= 30*24*time.Hour:
		unit = RelativeMonth
		value = int64(math.Round(delta.Hours() / (30 * 24)))
	case delta >= 7*24*time.Hour:
		unit = RelativeWeek
		value = int64(math.Round(delta.Hours() / (7 * 24)))
	case delta >= 24*time.Hour:
		unit = RelativeDay
		value = int64(math.Round(delta.Hours() / 24))
	case delta >= time.Hour:
		unit = RelativeHour
		value = int64(math.Round(delta.Hours()))
	case delta >= time.Minute:
		unit = RelativeMinute
		value = int64(math.Round(delta.Minutes()))
	}
	if value < 1 {
		value = 0
	}
	return f.FormatRelative(ctx, RelativeSpec{Value: value, Unit: unit, Direction: dir, Width: spec.Width, Numeric: spec.Numeric})
}

func (f *CLDRFormatter) FormatDateTimeInterval(ctx FormatContext, spec DateTimeIntervalSpec) (string, []FormatDiagnostic) {
	if spec.End.Before(spec.Start) {
		return finishFormatFallback(ctx, "datetime_interval", "invalid_interval_order", "interval end is before start")
	}
	startSpec := DateTimeSpec{Value: spec.Start, Kind: spec.Kind, Width: spec.Width, TimeZone: spec.TimeZone, PreserveZone: spec.PreserveZone, Calendar: spec.Calendar}
	endSpec := DateTimeSpec{Value: spec.End, Kind: spec.Kind, Width: spec.Width, TimeZone: spec.TimeZone, PreserveZone: spec.PreserveZone, Calendar: spec.Calendar}
	skeleton := intervalSkeleton(spec)
	field := greatestDifferentField(spec.Start, spec.End, spec.Kind)
	pattern, ok := f.data.IntervalPattern(effectiveFormatTag(ctx).String(), skeleton, field)
	if ok {
		return f.formatIntervalPattern(ctx, spec, pattern)
	}
	left, lds := f.FormatDateTime(withoutFormatObserver(ctx), startSpec)
	right, rds := f.FormatDateTime(withoutFormatObserver(ctx), endSpec)
	diagnostics := append(lds, rds...)
	if policy(ctx).Strict && len(diagnostics) > 0 {
		return finishFormat(ctx, "datetime_interval", "", diagnostics)
	}
	if !ok {
		diagnostics = append(diagnostics, fallbackFormatDiagnostic(ctx, "datetime_interval", "interval_pattern_unavailable", skeleton, "info"))
	}
	out, bds := boundFormatted(ctx, left+" – "+right, "datetime_interval")
	return finishFormat(ctx, "datetime_interval", out, append(diagnostics, bds...))
}

func (f *CLDRFormatter) localeRecord(tag string) (cldrdata.LocaleRecord, string) {
	for cur := cldrLookupTag(tag); cur != ""; {
		if rec, ok := f.data.Locale(cur); ok {
			return rec, cur
		}
		parent, ok := f.data.Parent(cur)
		if !ok || parent == cur {
			parent = bcp47Parent(cur)
		}
		if parent == "" || parent == cur {
			break
		}
		cur = parent
	}
	rec, _ := f.data.Locale("en")
	return rec, "en"
}

func bcp47Parent(raw string) string {
	tag, err := language.Parse(raw)
	if err == nil {
		parent := tag.Parent()
		if parent != tag && parent != language.Und {
			return cldrLookupTag(parent.String())
		}
	}
	if i := strings.LastIndex(raw, "-"); i > 0 {
		return raw[:i]
	}
	return ""
}

func (f *CLDRFormatter) validateFormatPreferences(ctx FormatContext, kind string) *FormatDiagnostic {
	if policy(ctx).AllowUnsupportedPreferences {
		return nil
	}
	if system := strings.TrimSpace(ctx.Profile.NumberingSystem()); system != "" && !f.supportsBCP47Type("nu", system) {
		return &FormatDiagnostic{
			Code:      "unsupported_numbering_system",
			Severity:  "error",
			Component: "formatter",
			Kind:      kind,
			SpecField: "numbering_system",
			Locale:    cldrLookupTag(effectiveFormatTag(ctx).String()),
			Detail:    system,
		}
	}
	return nil
}

func (f *CLDRFormatter) supportsBCP47Type(key, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return true
	}
	for _, rec := range f.data.BCP47Types(key) {
		if strings.EqualFold(rec.Type, value) || strings.EqualFold(rec.Alias, value) {
			return true
		}
	}
	return false
}

func containsTimeZoneNameToken(pattern string) bool {
	inQuote := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\'' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch pattern[i] {
		case 'z', 'v', 'V', 'O':
			return true
		}
	}
	return false
}

func cldrLookupTag(raw string) string {
	tag, err := language.Parse(raw)
	if err != nil {
		return raw
	}
	parts := []any{}
	if base, conf := tag.Base(); conf >= language.Exact {
		parts = append(parts, base)
	}
	if script, conf := tag.Script(); conf >= language.Exact {
		parts = append(parts, script)
	}
	if region, conf := tag.Region(); conf >= language.Exact {
		parts = append(parts, region)
	}
	if len(parts) == 0 {
		return raw
	}
	out, err := language.Compose(parts...)
	if err != nil {
		return raw
	}
	return out.String()
}

func effectiveFormatTag(ctx FormatContext) language.Tag {
	tag := ctx.Locale
	if tag == language.Und {
		tag = ctx.Profile.PrimaryLanguage()
	}
	return tagWithProfileFormatPreferences(tag, ctx.Profile)
}

func tagWithProfileFormatPreferences(tag language.Tag, profile Profile) language.Tag {
	parts := []any{tag}
	if region := profile.FormattingRegion(); region != "" {
		if parsed, err := language.ParseRegion(region); err == nil {
			parts = append(parts, parsed)
		}
	}
	if system := strings.TrimSpace(profile.NumberingSystem()); system != "" {
		if ext, err := language.ParseExtension("u-nu-" + system); err == nil {
			parts = append(parts, ext)
		}
	}
	if len(parts) == 1 {
		return tag
	}
	out, err := language.Compose(parts...)
	if err != nil {
		return tag
	}
	return out
}

func policy(ctx FormatContext) FormatPolicy {
	p := ctx.Policy
	if p.MaxFormattedRunes <= 0 {
		p.MaxFormattedRunes = defaultMaxFormattedRunes
	}
	return p
}

func validateNumber(v any) error {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("non-finite number")
		}
	case float32:
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return fmt.Errorf("non-finite number")
		}
	case string:
		if _, err := strconv.ParseFloat(n, 64); err != nil {
			return fmt.Errorf("invalid numeric string")
		}
	}
	return nil
}

func numberAsFloat(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("non-finite number")
		}
		return n, nil
	case float32:
		f := float64(n)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("non-finite number")
		}
		return f, nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("unsupported number type %T", v)
	}
}

func compactMagnitude(v float64) int64 {
	if v < 0 {
		v = -v
	}
	var mag int64
	for _, candidate := range []int64{100000000000000, 10000000000000, 1000000000000, 100000000000, 10000000000, 1000000000, 100000000, 10000000, 1000000, 100000, 10000, 1000} {
		if v >= float64(candidate) {
			mag = candidate
			break
		}
	}
	return mag
}

func compactPluralValue(v float64, magnitude int64) any {
	if magnitude <= 0 {
		return v
	}
	return int64(math.Round(math.Abs(v) / compactDivisor("0", magnitude)))
}

func compactDivisor(pattern string, magnitude int64) float64 {
	zeros := 0
	for _, r := range pattern {
		if r == '0' {
			zeros++
		}
	}
	if zeros <= 1 {
		return float64(magnitude)
	}
	divisor := float64(magnitude)
	for i := 1; i < zeros; i++ {
		divisor /= 10
	}
	if divisor < 1 {
		return 1
	}
	return divisor
}

func compactFractionDigits(pattern string) int {
	if i := strings.IndexByte(pattern, '.'); i >= 0 {
		n := 0
		for _, r := range pattern[i+1:] {
			if r == '0' || r == '#' {
				n++
				continue
			}
			break
		}
		return n
	}
	return 0
}

func replaceCompactNumber(pattern, num string) string {
	start := strings.IndexAny(pattern, "0#")
	if start < 0 {
		return num + pattern
	}
	end := start
	for end < len(pattern) && (pattern[end] == '0' || pattern[end] == '#' || pattern[end] == '.' || pattern[end] == ',') {
		end++
	}
	return pattern[:start] + num + pattern[end:]
}

func normalizeCompactWidth(width string) string {
	switch strings.ToLower(strings.TrimSpace(width)) {
	case "long":
		return "long"
	default:
		return "short"
	}
}

func (f *CLDRFormatter) pluralCategory(ctx FormatContext, value any) string {
	tag := effectiveFormatTag(ctx)
	n, err := numberAsFloat(value)
	if err != nil {
		return "other"
	}
	n = math.Abs(n)
	i := int(math.Floor(n))
	if math.Abs(n-float64(i)) > 0.0000001 {
		return "other"
	}
	switch plural.Cardinal.MatchPlural(tag, i, 0, 0, 0, 0) {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}

func relativeOffset(direction RelativeDirection, value int64) int {
	if direction == RelativePast {
		return -int(value)
	}
	return int(value)
}

func calendarDayDelta(target, reference time.Time) int {
	t := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, target.Location())
	r := time.Date(reference.Year(), reference.Month(), reference.Day(), 0, 0, 0, 0, reference.Location())
	return int(t.Sub(r).Hours() / 24)
}

func intervalSkeleton(spec DateTimeIntervalSpec) string {
	switch spec.Kind {
	case TimeOnly:
		return "Hm"
	case DateTime:
		return "yMMMdHm"
	default:
		switch strings.ToLower(strings.TrimSpace(spec.Width)) {
		case "short":
			return "yMd"
		case "long", "full":
			return "yMMMMd"
		default:
			return "yMMMd"
		}
	}
}

func greatestDifferentField(start, end time.Time, kind DateTimeKind) string {
	if kind == TimeOnly {
		if start.Hour() != end.Hour() {
			return "H"
		}
		return "m"
	}
	if start.Year() != end.Year() {
		return "y"
	}
	if start.Month() != end.Month() {
		return "M"
	}
	if start.Day() != end.Day() {
		return "d"
	}
	if kind == DateTime {
		if start.Hour() != end.Hour() {
			return "H"
		}
		return "m"
	}
	return "d"
}

func (f *CLDRFormatter) formatIntervalPattern(ctx FormatContext, spec DateTimeIntervalSpec, pattern string) (string, []FormatDiagnostic) {
	start := spec.Start
	end := spec.End
	if !spec.PreserveZone {
		loc := spec.TimeZone
		if loc == nil {
			loc = ctx.Profile.TimeZone()
		}
		if loc != nil {
			start = start.In(loc)
			end = end.In(loc)
		}
	}
	rec, localeUsed := f.localeRecord(effectiveFormatTag(ctx).String())
	idx := strings.IndexRune(pattern, '–')
	sepLen := len("–")
	if idx < 0 {
		idx = strings.IndexRune(pattern, '-')
		sepLen = 1
	}
	if idx < 0 {
		left, lds := f.FormatDateTime(withoutFormatObserver(ctx), DateTimeSpec{Value: start, Kind: spec.Kind, Width: spec.Width, TimeZone: spec.TimeZone, PreserveZone: spec.PreserveZone, Calendar: spec.Calendar})
		right, rds := f.FormatDateTime(withoutFormatObserver(ctx), DateTimeSpec{Value: end, Kind: spec.Kind, Width: spec.Width, TimeZone: spec.TimeZone, PreserveZone: spec.PreserveZone, Calendar: spec.Calendar})
		return finishFormat(ctx, "datetime_interval", left+" – "+right, append(lds, rds...))
	}
	leftPattern := strings.TrimSpace(pattern[:idx])
	rightPattern := strings.TrimSpace(pattern[idx+sepLen:])
	sep := pattern[idx : idx+sepLen]
	text := formatCLDRPattern(start, leftPattern, rec, effectiveFormatTag(ctx)) + sep + formatCLDRPattern(end, rightPattern, rec, effectiveFormatTag(ctx))
	out, ds := boundFormatted(ctx, text, "datetime_interval")
	if localeUsed != cldrLookupTag(effectiveFormatTag(ctx).String()) {
		ds = append(ds, FormatDiagnostic{Code: "format_locale_fallback", Severity: "info", Component: "formatter", Kind: "datetime_interval", Locale: cldrLookupTag(effectiveFormatTag(ctx).String()), FallbackLocale: localeUsed})
	}
	return finishFormat(ctx, "datetime_interval", out, ds)
}

func boundFormatted(ctx FormatContext, text, kind string) (string, []FormatDiagnostic) {
	max := policy(ctx).MaxFormattedRunes
	if max <= 0 {
		max = defaultMaxFormattedRunes
	}
	if utf8.RuneCountInString(text) <= max {
		return text, nil
	}
	runes := []rune(text)
	diag := FormatDiagnostic{Code: "formatted_value_too_large", Severity: "error", Component: "formatter", Kind: kind}
	if policy(ctx).Strict {
		return string(runes[:max]), []FormatDiagnostic{diag}
	}
	return string(runes[:max]), []FormatDiagnostic{diag}
}

func formatFallback(ctx FormatContext, kind, code, detail string) (string, []FormatDiagnostic) {
	diag := FormatDiagnostic{Code: code, Severity: "error", Component: "formatter", Kind: kind, Detail: detail}
	if policy(ctx).Strict {
		return "", []FormatDiagnostic{diag}
	}
	return "{error}", []FormatDiagnostic{diag}
}

func fallbackFormatDiagnostic(ctx FormatContext, kind, code, detail, severity string) FormatDiagnostic {
	tag := effectiveFormatTag(ctx)
	return FormatDiagnostic{
		Code:      code,
		Severity:  severity,
		Component: "formatter",
		Kind:      kind,
		Locale:    cldrLookupTag(tag.String()),
		Detail:    detail,
	}
}

func applyCurrencyPattern(pattern, symbol, numberText string) string {
	positive := pattern
	negative := ""
	if i := strings.IndexByte(pattern, ';'); i >= 0 {
		positive = pattern[:i]
		negative = pattern[i+1:]
	}
	target := positive
	if strings.HasPrefix(numberText, "-") {
		numberText = strings.TrimPrefix(numberText, "-")
		if negative != "" {
			target = negative
		} else {
			target = "-" + strings.ReplaceAll(positive, "¤", symbol)
			return replaceNumberPattern(target, numberText)
		}
	}
	target = strings.ReplaceAll(target, "¤", symbol)
	return replaceNumberPattern(target, numberText)
}

func replaceNumberPattern(pattern, num string) string {
	start := -1
	end := -1
	for i, r := range pattern {
		if (r == '#' || r == '0' || r == ',' || r == '.') && start == -1 {
			start = i
		}
		if start != -1 && !(r == '#' || r == '0' || r == ',' || r == '.') {
			end = i
			break
		}
	}
	if start == -1 {
		return pattern + num
	}
	if end == -1 {
		end = len(pattern)
	}
	return pattern[:start] + num + pattern[end:]
}

func applyListPattern(pattern cldrdata.ListPattern, items []string) string {
	if len(items) == 2 {
		return replaceTwo(pattern.Two, items[0], items[1])
	}
	out := replaceTwo(pattern.Start, items[0], items[1])
	for _, item := range items[2 : len(items)-1] {
		out = replaceTwo(pattern.Middle, out, item)
	}
	return replaceTwo(pattern.End, out, items[len(items)-1])
}

func fallbackListPattern(tag language.Tag, kind ListType) cldrdata.ListPattern {
	base, _ := tag.Base()
	switch base.String() {
	case "de":
		if kind == ListOr {
			return cldrdata.ListPattern{Two: "{0} oder {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0} oder {1}"}
		}
		return cldrdata.ListPattern{Two: "{0} und {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0} und {1}"}
	case "fr":
		if kind == ListOr {
			return cldrdata.ListPattern{Two: "{0} ou {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0} ou {1}"}
		}
		return cldrdata.ListPattern{Two: "{0} et {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0} et {1}"}
	case "ja":
		return cldrdata.ListPattern{Two: "{0}、{1}", Start: "{0}、{1}", Middle: "{0}、{1}", End: "{0}、{1}"}
	default:
		if kind == ListOr {
			return cldrdata.ListPattern{Two: "{0} or {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0}, or {1}"}
		}
		return cldrdata.ListPattern{Two: "{0} and {1}", Start: "{0}, {1}", Middle: "{0}, {1}", End: "{0}, and {1}"}
	}
}

func replaceTwo(pattern, a, b string) string {
	if pattern == "" {
		pattern = "{0}, {1}"
	}
	return strings.ReplaceAll(strings.ReplaceAll(pattern, "{0}", a), "{1}", b)
}

func formatCLDRPattern(t time.Time, pattern string, rec cldrdata.LocaleRecord, tag language.Tag) string {
	var b strings.Builder
	p := message.NewPrinter(tag)
	writeNumber := func(value, width int) {
		opts := []number.Option{number.NoSeparator()}
		if width > 1 {
			opts = append(opts, number.MinIntegerDigits(width))
		}
		b.WriteString(p.Sprintf("%v", number.Decimal(value, opts...)))
	}
	for i := 0; i < len(pattern); {
		r := rune(pattern[i])
		if r == '\'' {
			next := strings.IndexByte(pattern[i+1:], '\'')
			if next < 0 {
				i++
				continue
			}
			b.WriteString(pattern[i+1 : i+1+next])
			i += next + 2
			continue
		}
		j := i + 1
		for j < len(pattern) && pattern[j] == pattern[i] {
			j++
		}
		count := j - i
		token := pattern[i]
		switch token {
		case 'y':
			if count == 2 {
				writeNumber(t.Year()%100, 2)
			} else {
				writeNumber(t.Year(), 1)
			}
		case 'M', 'L':
			month := int(t.Month())
			switch {
			case count >= 4:
				b.WriteString(rec.MonthsWide[month-1])
			case count == 3:
				b.WriteString(rec.MonthsAbbr[month-1])
			case count == 2:
				writeNumber(month, 2)
			default:
				writeNumber(month, 1)
			}
		case 'd':
			if count == 2 {
				writeNumber(t.Day(), 2)
			} else {
				writeNumber(t.Day(), 1)
			}
		case 'E', 'e', 'c':
			day := int(t.Weekday())
			if count >= 4 {
				b.WriteString(rec.WeekdaysWide[day])
			} else {
				b.WriteString(rec.WeekdaysAbbr[day])
			}
		case 'H':
			if count == 2 {
				writeNumber(t.Hour(), 2)
			} else {
				writeNumber(t.Hour(), 1)
			}
		case 'h':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			if count == 2 {
				writeNumber(h, 2)
			} else {
				writeNumber(h, 1)
			}
		case 'm':
			writeNumber(t.Minute(), minWidth(count))
		case 's':
			writeNumber(t.Second(), minWidth(count))
		case 'a':
			if t.Hour() < 12 {
				if rec.DayPeriods[0] != "" {
					b.WriteString(rec.DayPeriods[0])
				} else {
					b.WriteString("AM")
				}
			} else {
				if rec.DayPeriods[1] != "" {
					b.WriteString(rec.DayPeriods[1])
				} else {
					b.WriteString("PM")
				}
			}
		case 'z', 'v', 'V', 'O':
			b.WriteString(t.Format("MST"))
		default:
			b.WriteString(pattern[i:j])
		}
		i = j
	}
	return b.String()
}

func minWidth(n int) int {
	if n <= 1 {
		return 1
	}
	return 2
}
