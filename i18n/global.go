package i18n

import "sync/atomic"

var defaultRuntime atomic.Pointer[Runtime]

func init() {
	cat, _ := NewCatalog(nil)
	defaultRuntime.Store(NewRuntime(cat))
}

// SetDefault sets the process-default runtime used by package-level helpers.
func SetDefault(runtime *Runtime) {
	if runtime == nil {
		cat, _ := NewCatalog(nil)
		runtime = NewRuntime(cat)
	}
	defaultRuntime.Store(runtime)
}

// Default returns the process-default localizer.
func Default() *Localizer {
	return defaultRuntime.Load().Localizer()
}

// Locale returns a process-default localizer for preferences.
func Locale(preferences ...string) *Localizer {
	return defaultRuntime.Load().Localizer(preferences...)
}

// T translates using the process-default localizer.
func T(msgid string, vars ...Vars) string {
	return Default().T(msgid, vars...)
}

// Tn translates a plural using the process-default localizer.
func Tn(n int, singular, plural string, vars ...Vars) string {
	return Default().Tn(n, singular, plural, vars...)
}

// Tc translates with gettext message context using the process-default localizer.
func Tc(messageContext, msgid string, vars ...Vars) string {
	return Default().Tc(messageContext, msgid, vars...)
}

// Tnc translates a plural with gettext message context using the process-default localizer.
func Tnc(messageContext string, n int, singular, plural string, vars ...Vars) string {
	return Default().Tnc(messageContext, n, singular, plural, vars...)
}
