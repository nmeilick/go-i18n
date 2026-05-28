package web

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/nmeilick/go-i18n/i18n"
	"github.com/nmeilick/go-i18n/locale"
)

const maxDirectLocaleBytes = 256

// Source extracts locale candidates from a request.
type Source func(*http.Request) []locale.Candidate

// ProfileSource builds a formatting profile from a request and locale result.
type ProfileSource func(*http.Request, locale.Resolved) (locale.Profile, error)

// ProfileErrorHandler handles profile construction failures.
type ProfileErrorHandler func(http.ResponseWriter, *http.Request, error)

// Options configures middleware.
type Options struct {
	Sources                   []Source
	Profile                   ProfileSource
	ProfileError              ProfileErrorHandler
	ContentLanguage           bool
	Vary                      bool
	PrivateWhenUserSourceWins bool
}

// Query reads a query parameter locale source.
func Query(name string) Source {
	return func(r *http.Request) []locale.Candidate {
		value, ok := cleanDirectLocale(r.URL.Query().Get(name))
		if !ok {
			return nil
		}
		return []locale.Candidate{{Tag: value, Source: "query:" + name, Weight: 1, Explicit: true}}
	}
}

// Header reads a header locale source.
func Header(name string) Source {
	return func(r *http.Request) []locale.Candidate {
		value, ok := cleanDirectLocale(r.Header.Get(name))
		if !ok {
			return nil
		}
		return []locale.Candidate{{Tag: value, Source: "header:" + name, Weight: 1, Vary: []string{name}, Explicit: true}}
	}
}

// Cookie reads a cookie locale source.
func Cookie(name string) Source {
	return func(r *http.Request) []locale.Candidate {
		c, err := r.Cookie(name)
		if err != nil {
			return nil
		}
		value, ok := cleanDirectLocale(c.Value)
		if !ok {
			return nil
		}
		return []locale.Candidate{{Tag: value, Source: "cookie:" + name, Weight: 1, Private: true, Explicit: true}}
	}
}

// AcceptLanguage reads the Accept-Language header.
func AcceptLanguage() Source {
	return func(r *http.Request) []locale.Candidate {
		return locale.AcceptLanguage(r.Header.Get("Accept-Language"))
	}
}

// StaticProfile returns a profile source that uses resolved locale plus static options.
func StaticProfile(opts ...locale.ProfileOption) ProfileSource {
	return func(r *http.Request, resolved locale.Resolved) (locale.Profile, error) {
		return locale.NewProfile([]string{resolved.Locale}, opts...)
	}
}

type contextKey struct{}

type requestState struct {
	resolved  locale.Resolved
	localizer *i18n.Localizer
}

// Middleware creates net/http middleware.
func Middleware(rt *i18n.Runtime, neg *locale.Negotiator, opts Options) func(http.Handler) http.Handler {
	if rt == nil {
		rt = i18n.NewRuntime(nil)
	}
	if neg == nil {
		neg, _ = locale.NewNegotiator("en", []string{"en"})
	}
	if len(opts.Sources) == 0 {
		opts.Sources = []Source{AcceptLanguage()}
	}
	if opts.Profile == nil {
		opts.Profile = StaticProfile()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var candidates []locale.Candidate
			for _, source := range opts.Sources {
				candidates = append(candidates, source(r)...)
			}
			resolved := neg.Resolve(candidates...)
			profile, err := opts.Profile(r, resolved)
			if err != nil {
				if opts.ProfileError != nil {
					opts.ProfileError(w, r, err)
				} else {
					http.Error(w, "invalid locale profile", http.StatusInternalServerError)
				}
				return
			}
			tr := rt.Translator(profile).WithContext(r.Context())
			if opts.ContentLanguage {
				w.Header().Set("Content-Language", resolved.Locale)
			}
			if opts.Vary {
				AppendVary(w.Header(), resolved.Vary...)
			}
			if opts.PrivateWhenUserSourceWins && resolved.Private {
				setPrivate(w.Header())
			}
			state := requestState{resolved: resolved, localizer: tr}
			ctx := context.WithValue(r.Context(), contextKey{}, state)
			ctx = i18n.WithLocalizer(ctx, tr)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func cleanDirectLocale(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxDirectLocaleBytes {
		return "", false
	}
	return value, true
}

// Localizer returns the request localizer.
func Localizer(ctx context.Context) (*i18n.Localizer, bool) {
	if state, ok := ctx.Value(contextKey{}).(requestState); ok {
		return state.localizer, state.localizer != nil
	}
	return i18n.FromContext(ctx)
}

// Resolved returns the request locale resolution.
func Resolved(ctx context.Context) (locale.Resolved, bool) {
	state, ok := ctx.Value(contextKey{}).(requestState)
	return state.resolved, ok
}

// AppendVary appends deduplicated Vary header names and preserves Vary: *.
func AppendVary(h http.Header, values ...string) {
	if len(values) == 0 {
		return
	}
	existing := h.Values("Vary")
	seen := map[string]string{}
	for _, line := range existing {
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			if part == "*" {
				h.Set("Vary", "*")
				return
			}
			if part != "" {
				seen[strings.ToLower(part)] = http.CanonicalHeaderKey(part)
			}
		}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[strings.ToLower(value)] = http.CanonicalHeaderKey(value)
		}
	}
	out := make([]string, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	h.Set("Vary", strings.Join(out, ", "))
}

func setPrivate(h http.Header) {
	cc := h.Get("Cache-Control")
	if cc == "" {
		h.Set("Cache-Control", "private")
		return
	}
	if !strings.Contains(strings.ToLower(cc), "private") {
		h.Set("Cache-Control", cc+", private")
	}
}
