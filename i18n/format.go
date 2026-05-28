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

// Compact requests compact decimal formatting. The built-in formatter falls
// back to localized decimal formatting unless a richer data provider is used.
func Compact(width ...string) NumberOption {
	return func(c *locale.NumberSpec) {
		c.Kind = locale.NumberCompact
		if len(width) > 0 {
			c.CompactDisplayName = width[0]
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

// CurrencyStyle sets a currency style: standard, code, or accounting.
func CurrencyStyle(style string) CurrencyOption {
	return func(c *locale.CurrencySpec) {
		switch strings.TrimSpace(style) {
		case "code":
			c.Display = "code"
		case "accounting":
			c.Accounting = true
		default:
			c.Display = "symbol"
			c.Accounting = false
		}
	}
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
	spec := locale.CurrencySpec{Value: v, Display: "symbol", CodeSource: locale.CurrencyProfile}
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

func timeValue(t time.Time, kind locale.DateTimeKind, opts ...TimeOption) Value {
	spec := locale.DateTimeSpec{Value: t, Kind: kind, Width: "medium", Calendar: "gregory"}
	for _, opt := range opts {
		opt(&spec)
	}
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatDateTime(ctx, spec)
	})
}

// Duration formats an elapsed duration conservatively.
func Duration(d time.Duration) Value {
	return valueFunc(func(ctx locale.FormatContext, formatter locale.Formatter) (string, []locale.FormatDiagnostic) {
		return formatter.FormatDuration(ctx, locale.DurationSpec{Value: d, Width: locale.UnitLong})
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
