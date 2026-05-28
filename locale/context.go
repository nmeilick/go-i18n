package locale

import "context"

type scopeKey struct{}

// Scope is any request or job object that exposes a locale profile.
type Scope interface {
	Profile() Profile
}

// WithScope stores a locale scope in ctx.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeFrom returns the locale scope from ctx.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeKey{}).(Scope)
	return scope, ok
}
