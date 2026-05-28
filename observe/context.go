package observe

import "context"

type observerKey struct{}
type attrsKey struct{}

// WithObserver stores an observer in ctx.
func WithObserver(ctx context.Context, observer Observer) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, observerKey{}, observer)
}

// ObserverFrom returns the observer stored in ctx.
func ObserverFrom(ctx context.Context) (Observer, bool) {
	if ctx == nil {
		return nil, false
	}
	observer, ok := ctx.Value(observerKey{}).(Observer)
	return observer, ok && observer != nil
}

// WithAttrs appends observability attributes to ctx.
func WithAttrs(ctx context.Context, attrs ...Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(attrs) == 0 {
		return ctx
	}
	existing := AttrsFrom(ctx)
	out := make([]Attr, 0, len(existing)+len(attrs))
	out = append(out, existing...)
	out = append(out, attrs...)
	return context.WithValue(ctx, attrsKey{}, out)
}

// AttrsFrom returns a defensive copy of attributes stored in ctx.
func AttrsFrom(ctx context.Context) []Attr {
	if ctx == nil {
		return nil
	}
	attrs, ok := ctx.Value(attrsKey{}).([]Attr)
	if !ok || len(attrs) == 0 {
		return nil
	}
	out := make([]Attr, len(attrs))
	copy(out, attrs)
	return out
}
