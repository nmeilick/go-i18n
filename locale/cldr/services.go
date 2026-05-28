package cldr

import (
	"sync"
	"sync/atomic"

	"github.com/nmeilick/go-i18n/locale"
	"golang.org/x/text/language"
)

// ServiceStats exposes coarse service and pack-loading counters.
type ServiceStats struct {
	BundleCount       int   `json:"bundle_count"`
	FormatterCalls    int64 `json:"formatter_calls"`
	DefaultsCalls     int64 `json:"defaults_calls"`
	ClosedServiceHits int64 `json:"closed_service_hits"`
}

// ServiceOption configures Services.
type ServiceOption func(*serviceConfig)

type serviceConfig struct {
	compose []ComposeOption
}

// WithComposeOptions forwards options to bundle composition.
func WithComposeOptions(opts ...ComposeOption) ServiceOption {
	return func(c *serviceConfig) { c.compose = append(c.compose, opts...) }
}

// Services owns a composed CLDR bundle and exposes semantic locale services.
type Services struct {
	bundle   Bundle
	data     DataProvider
	format   locale.Formatter
	defaults locale.DefaultsProvider
	stats    ServiceStats
	closed   atomic.Bool
	closeMu  sync.Mutex
	closeErr error
}

// NewServices composes bundles and constructs formatter/defaults services. If
// no bundle is provided, the built-in lean bundle is used.
func NewServices(bundles ...Bundle) (*Services, error) {
	return NewServicesWithOptions(bundles)
}

// NewServicesWithOptions composes bundles with options and constructs services.
func NewServicesWithOptions(bundles []Bundle, opts ...ServiceOption) (*Services, error) {
	cfg := serviceConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(bundles) == 0 {
		bundles = []Bundle{BuiltinLean()}
	}
	bundle, err := Compose(bundles, cfg.compose...)
	if err != nil {
		return nil, err
	}
	data := bundle.Data()
	s := &Services{bundle: bundle, data: data, stats: ServiceStats{BundleCount: len(bundles)}}
	s.format = serviceFormatter{services: s, next: locale.NewCLDRFormatter(toInternalProvider{p: data})}
	s.defaults = serviceDefaults{services: s, data: data}
	return s, nil
}

// Formatter returns a service-owned formatter. Calls after Close return
// structured diagnostics rather than reading closed data.
func (s *Services) Formatter() locale.Formatter {
	if s == nil {
		svc, _ := NewServices()
		return svc.Formatter()
	}
	return s.format
}

// DefaultsProvider returns a service-owned defaults provider.
func (s *Services) DefaultsProvider() locale.DefaultsProvider {
	if s == nil {
		svc, _ := NewServices()
		return svc.DefaultsProvider()
	}
	return s.defaults
}

// Info returns composed bundle metadata.
func (s *Services) Info() Info {
	if s == nil || s.bundle == nil {
		return Info{}
	}
	return s.bundle.Info()
}

// Coverage returns composed coverage metadata.
func (s *Services) Coverage() []Coverage {
	if s == nil || s.bundle == nil {
		return nil
	}
	return s.bundle.Coverage()
}

// Stats returns a point-in-time copy of service statistics.
func (s *Services) Stats() ServiceStats {
	if s == nil {
		return ServiceStats{}
	}
	stats := s.stats
	stats.FormatterCalls = atomic.LoadInt64(&s.stats.FormatterCalls)
	stats.DefaultsCalls = atomic.LoadInt64(&s.stats.DefaultsCalls)
	stats.ClosedServiceHits = atomic.LoadInt64(&s.stats.ClosedServiceHits)
	return stats
}

// Close closes all composed closeable data sources. It is idempotent and safe
// to call concurrently.
func (s *Services) Close() error {
	if s == nil {
		return nil
	}
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed.Load() {
		return s.closeErr
	}
	s.closed.Store(true)
	if s.bundle != nil {
		s.closeErr = s.bundle.Close()
	}
	return s.closeErr
}

func (s *Services) isClosed() bool {
	if s == nil || s.closed.Load() {
		if s != nil {
			atomic.AddInt64(&s.stats.ClosedServiceHits, 1)
		}
		return true
	}
	return false
}

type serviceFormatter struct {
	services *Services
	next     locale.Formatter
}

func (f serviceFormatter) DataVersion() string {
	if f.services.isClosed() {
		return ""
	}
	return f.next.DataVersion()
}

func (f serviceFormatter) FormatNumber(ctx locale.FormatContext, spec locale.NumberSpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("number")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatNumber(ctx, spec)
}

func (f serviceFormatter) FormatCurrency(ctx locale.FormatContext, spec locale.CurrencySpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("currency")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatCurrency(ctx, spec)
}

func (f serviceFormatter) FormatDateTime(ctx locale.FormatContext, spec locale.DateTimeSpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("datetime")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatDateTime(ctx, spec)
}

func (f serviceFormatter) FormatList(ctx locale.FormatContext, spec locale.ListSpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("list")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatList(ctx, spec)
}

func (f serviceFormatter) FormatUnit(ctx locale.FormatContext, spec locale.UnitSpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("unit")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatUnit(ctx, spec)
}

func (f serviceFormatter) FormatDuration(ctx locale.FormatContext, spec locale.DurationSpec) (string, []locale.FormatDiagnostic) {
	if f.services.isClosed() {
		return closedFormatDiagnostic("duration")
	}
	atomic.AddInt64(&f.services.stats.FormatterCalls, 1)
	return f.next.FormatDuration(ctx, spec)
}

func closedFormatDiagnostic(kind string) (string, []locale.FormatDiagnostic) {
	return "", []locale.FormatDiagnostic{{
		Code:      string(ErrClosedService),
		Severity:  "error",
		Component: "cldr",
		Kind:      kind,
		Detail:    "CLDR services are closed",
	}}
}

type serviceDefaults struct {
	services *Services
	data     DataProvider
}

func (d serviceDefaults) DefaultsForRegion(region string) (locale.RegionDefaults, bool) {
	if d.services.isClosed() {
		return locale.RegionDefaults{}, false
	}
	atomic.AddInt64(&d.services.stats.DefaultsCalls, 1)
	rec, ok := d.data.RegionDefaults(region)
	if !ok {
		return locale.RegionDefaults{}, false
	}
	return locale.RegionDefaults{
		Region:            rec.Region,
		DisplayCurrency:   locale.Currency(rec.Currency),
		MeasurementSystem: rec.MeasurementSystem,
		FirstDay:          rec.FirstDay,
		TimeZone:          rec.TimeZone,
	}, true
}

func (d serviceDefaults) LocaleDefaults(tag language.Tag) locale.LocaleDefaults {
	if d.services.isClosed() {
		return locale.LocaleDefaults{}
	}
	atomic.AddInt64(&d.services.stats.DefaultsCalls, 1)
	for cur := tag.String(); cur != ""; {
		if rec, ok := d.data.Locale(cur); ok {
			return locale.LocaleDefaults{NumberingSystem: rec.NumberingSystem, Calendar: "gregory"}
		}
		parent, ok := d.data.Parent(cur)
		if !ok || parent == cur {
			break
		}
		cur = parent
	}
	return locale.LocaleDefaults{NumberingSystem: "latn", Calendar: "gregory"}
}

func (d serviceDefaults) CanonicalTimeZoneForRegion(region string) (string, bool) {
	rec, ok := d.DefaultsForRegion(region)
	return rec.TimeZone, ok && rec.TimeZone != ""
}

func (d serviceDefaults) CurrencyDigits(code locale.CurrencyCode) int {
	if d.services.isClosed() {
		return 2
	}
	atomic.AddInt64(&d.services.stats.DefaultsCalls, 1)
	return d.data.CurrencyFraction(code.String()).Digits
}

func (d serviceDefaults) KnownExtension(key, typ string) bool {
	if d.services.isClosed() {
		return false
	}
	atomic.AddInt64(&d.services.stats.DefaultsCalls, 1)
	for _, rec := range d.data.BCP47Types(key) {
		if rec.Type == typ || rec.Alias == typ {
			return true
		}
	}
	return false
}

func (d serviceDefaults) DataVersion() string {
	if d.services.isClosed() {
		return ""
	}
	return d.data.Metadata().CLDRVersion
}
