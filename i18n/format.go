package i18n

import (
	"strings"
	"time"

	"github.com/nmeilick/go-i18n/locale"
)

// Value is a profile-aware plain-text interpolation value. Implementations
// receive the resolved formatting context, not raw catalog template syntax.
type Value interface {
	Format(locale.FormatContext, locale.Formatter) (string, []locale.FormatDiagnostic)
}

type valueFunc func(locale.FormatContext, locale.Formatter) (string, []locale.FormatDiagnostic)

func (f valueFunc) Format(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
	return f(ctx, formatter)
}

// Number formats a numeric value using the bound formatter locale.
func Number(v any, opts ...NumberOption) Value {
	spec := locale.NumberSpec{Value: v, Kind: locale.NumberDecimal}
	grouping := true
	spec.Grouping = &grouping
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatNumber(ctx, spec)
	})
}

// NumberOption configures number formatting.
type NumberOption func(*locale.NumberSpec)

// FractionDigits fixes the number of fraction digits.
func FractionDigits(n int) NumberOption {
	return func(c *locale.NumberSpec) {
		c.MinFractionDigits = &n
		c.MaxFractionDigits = &n
	}
}

// Grouping enables or disables grouping separators.
func Grouping(enabled bool) NumberOption {
	return func(c *locale.NumberSpec) { c.Grouping = &enabled }
}

// Precision sets significant-digit precision when supported by the formatter.
func Precision(n int) NumberOption {
	return func(c *locale.NumberSpec) { c.Precision = &n }
}

// Compact requests compact decimal formatting.
func Compact(width ...locale.UnitWidth) NumberOption {
	return func(c *locale.NumberSpec) {
		c.Kind = locale.NumberCompact
		if len(width) > 0 {
			c.CompactDisplayName = string(width[0])
		}
	}
}

// CurrencyOption configures currency formatting.
type CurrencyOption func(*locale.CurrencySpec)

// CurrencyCode overrides the profile display currency.
func CurrencyCode(code locale.CurrencyCode) CurrencyOption {
	return func(c *locale.CurrencySpec) {
		c.Code = locale.Currency(code.String())
		c.CodeSource = locale.CurrencyExplicit
	}
}

type CurrencyDisplayMode = locale.CurrencyDisplayMode

const (
	CurrencyDisplaySymbol       = locale.CurrencyDisplaySymbol
	CurrencyDisplayNarrowSymbol = locale.CurrencyDisplayNarrowSymbol
	CurrencyDisplayCode         = locale.CurrencyDisplayCode
)

// CurrencyDisplay selects symbol, narrow symbol, or currency code rendering.
func CurrencyDisplay(mode CurrencyDisplayMode) CurrencyOption {
	return func(c *locale.CurrencySpec) { c.Display = locale.CurrencyDisplayMode(mode) }
}

// CurrencyAccounting uses the locale accounting pattern for negative amounts.
func CurrencyAccounting() CurrencyOption {
	return func(c *locale.CurrencySpec) { c.Accounting = true }
}

// CurrencyFractionDigits overrides currency fraction digits.
func CurrencyFractionDigits(n int) CurrencyOption {
	return func(c *locale.CurrencySpec) { c.FractionDigits = &n }
}

// CashDigits uses CLDR cash precision for currencies that distinguish it.
func CashDigits() CurrencyOption {
	return func(c *locale.CurrencySpec) { c.CashDigits = true }
}

// Currency formats a currency amount using an explicit code or, when policy
// allows it, the profile display currency.
func Currency(v any, opts ...CurrencyOption) Value {
	spec := locale.CurrencySpec{Value: v, Display: locale.CurrencyDisplaySymbol, CodeSource: locale.CurrencyProfile}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatCurrency(ctx, spec)
	})
}

// Percent formats a ratio as a localized percent. Percent(0.123) renders 12%.
func Percent(v any, opts ...NumberOption) Value {
	spec := locale.NumberSpec{Value: v, Kind: locale.NumberPercent}
	grouping := true
	spec.Grouping = &grouping
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatNumber(ctx, spec)
	})
}

// TimeOption configures date/time formatting.
type TimeOption func(*locale.DateTimeSpec)

// Style sets one of short, medium, long, full.
func Style(style string) TimeOption {
	return func(c *locale.DateTimeSpec) { c.Width = strings.TrimSpace(style) }
}

// InTimeZone overrides the profile timezone.
func InTimeZone(loc *time.Location) TimeOption {
	return func(c *locale.DateTimeSpec) { c.TimeZone = loc }
}

// PreserveTimeZone formats the time in its existing location.
func PreserveTimeZone() TimeOption {
	return func(c *locale.DateTimeSpec) { c.PreserveZone = true }
}

// Date formats a date.
func Date(t time.Time, opts ...TimeOption) Value {
	return timeValue(t, locale.DateOnly, opts...)
}

// Time formats a time of day.
func Time(t time.Time, opts ...TimeOption) Value {
	return timeValue(t, locale.TimeOnly, opts...)
}

// DateTime formats a date and time.
func DateTime(t time.Time, opts ...TimeOption) Value {
	return timeValue(t, locale.DateTime, opts...)
}

// DateInterval formats a localized date range.
func DateInterval(start, end time.Time, opts ...TimeOption) Value {
	return intervalValue(start, end, locale.DateOnly, opts...)
}

// TimeInterval formats a localized time range.
func TimeInterval(start, end time.Time, opts ...TimeOption) Value {
	return intervalValue(start, end, locale.TimeOnly, opts...)
}

// DateTimeInterval formats a localized datetime range.
func DateTimeInterval(start, end time.Time, opts ...TimeOption) Value {
	return intervalValue(start, end, locale.DateTime, opts...)
}

func timeValue(t time.Time, kind locale.DateTimeKind, opts ...TimeOption) Value {
	spec := locale.DateTimeSpec{Value: t, Kind: kind, Width: "medium", Calendar: "gregory"}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatDateTime(ctx, spec)
	})
}

func intervalValue(start, end time.Time, kind locale.DateTimeKind, opts ...TimeOption) Value {
	spec := locale.DateTimeIntervalSpec{Start: start, End: end, Kind: kind, Width: "medium", Calendar: "gregory"}
	tmp := locale.DateTimeSpec{Value: start, Kind: kind, Width: spec.Width, TimeZone: spec.TimeZone, PreserveZone: spec.PreserveZone, Calendar: spec.Calendar}
	for _, opt := range opts {
		opt(&tmp)
	}
	spec.Width = tmp.Width
	spec.TimeZone = tmp.TimeZone
	spec.PreserveZone = tmp.PreserveZone
	spec.Calendar = tmp.Calendar
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatDateTimeInterval(ctx, spec)
	})
}

// DurationOption configures elapsed duration formatting.
type DurationOption func(*locale.DurationSpec)

// DurationWidth sets long, short, or narrow duration width.
func DurationWidth(width locale.UnitWidth) DurationOption {
	return func(c *locale.DurationSpec) { c.Width = width }
}

// Duration formats an elapsed duration conservatively.
func Duration(d time.Duration, opts ...DurationOption) Value {
	spec := locale.DurationSpec{Value: d, Width: locale.UnitLong}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatDuration(ctx, spec)
	})
}

// PeriodOption configures calendar-style period formatting.
type PeriodOption func(*locale.PeriodSpec)

// PeriodWidth sets long, short, or narrow period unit width.
func PeriodWidth(width locale.UnitWidth) PeriodOption {
	return func(c *locale.PeriodSpec) { c.Width = width }
}

// Period formats a caller-supplied calendar-style period.
func Period(p locale.Period, opts ...PeriodOption) Value {
	spec := locale.PeriodSpec{Value: p, Width: locale.UnitLong}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatPeriod(ctx, spec)
	})
}

// RelativeOption configures explicit relative-time formatting.
type RelativeOption func(*locale.RelativeSpec)

// RelativeWidth sets long, short, or narrow relative-time width.
func RelativeWidth(width locale.UnitWidth) RelativeOption {
	return func(c *locale.RelativeSpec) { c.Width = width }
}

// RelativeNumeric controls whether named forms such as "yesterday" are allowed.
func RelativeNumeric(mode locale.RelativeNumericMode) RelativeOption {
	return func(c *locale.RelativeSpec) { c.Numeric = mode }
}

// Relative formats an explicit relative quantity.
func Relative(n int64, unit locale.RelativeUnit, dir locale.RelativeDirection, opts ...RelativeOption) Value {
	spec := locale.RelativeSpec{Value: n, Unit: unit, Direction: dir, Width: locale.UnitLong, Numeric: locale.RelativeAuto}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatRelative(ctx, spec)
	})
}

// RelativeTimeOption configures relative-time derivation from two instants.
type RelativeTimeOption func(*locale.RelativeTimeSpec)

// RelativeTimeWidth sets long, short, or narrow relative-time width.
func RelativeTimeWidth(width locale.UnitWidth) RelativeTimeOption {
	return func(c *locale.RelativeTimeSpec) { c.Width = width }
}

// RelativeTimeNumeric controls whether named forms such as "yesterday" are allowed.
func RelativeTimeNumeric(mode locale.RelativeNumericMode) RelativeTimeOption {
	return func(c *locale.RelativeTimeSpec) { c.Numeric = mode }
}

// RelativeTime formats target relative to reference.
func RelativeTime(target, reference time.Time, opts ...RelativeTimeOption) Value {
	spec := locale.RelativeTimeSpec{Target: target, Reference: reference, Width: locale.UnitLong, Numeric: locale.RelativeAuto}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatRelativeTime(ctx, spec)
	})
}

// ListOption configures list formatting.
type ListOption func(*locale.ListSpec)

// ListStyle sets the CLDR list type: standard, or, unit.
func ListStyle(kind locale.ListType) ListOption {
	return func(c *locale.ListSpec) { c.Type = kind }
}

// ListWidth sets the CLDR list width.
func ListWidth(width string) ListOption {
	return func(c *locale.ListSpec) { c.Width = width }
}

// List formats a conjunction list.
func List(items []any, opts ...ListOption) Value {
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			text, ds := formatInterpolationValue(item, ctx, formatter)
			if len(ds) > 0 {
				return "", []locale.FormatDiagnostic{{Code: "list_item_format_error", Severity: "error", Component: "formatter", Kind: "list"}}
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		spec := locale.ListSpec{Items: parts, Type: locale.ListStandard, Width: "long"}
		for _, opt := range opts {
			opt(&spec)
		}
		return formatter.FormatList(ctx, spec)
	})
}

// UnitOption configures unit formatting.
type UnitOption func(*locale.UnitSpec)

// UnitWidth sets long, short, or narrow unit width.
func UnitWidth(width locale.UnitWidth) UnitOption {
	return func(c *locale.UnitSpec) { c.Width = width }
}

// Unit formats a value with a unit label. It does not convert units.
func Unit(v any, unit string, opts ...UnitOption) Value {
	spec := locale.UnitSpec{Value: v, Unit: strings.TrimSpace(unit), Width: locale.UnitLong}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatUnit(ctx, spec)
	})
}
