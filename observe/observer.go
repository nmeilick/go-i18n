package observe

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
)

// Observer receives structured observability events. Implementations must be
// safe for concurrent use when shared across runtimes or requests.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Event)

// Observe calls f.
func (f ObserverFunc) Observe(ctx context.Context, event Event) {
	if f != nil {
		f(ctx, event)
	}
}

type noopObserver struct{}

func (noopObserver) Observe(context.Context, Event) {}

// Noop returns an observer that drops events.
func Noop() Observer {
	return noopObserver{}
}

// SafeObserve reports event to observer and recovers observer panics.
func SafeObserve(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.Observe(ctx, event)
}

// Multi returns an observer that forwards events to all non-nil observers.
func Multi(observers ...Observer) Observer {
	clean := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			clean = append(clean, observer)
		}
	}
	if len(clean) == 0 {
		return Noop()
	}
	if len(clean) == 1 {
		return clean[0]
	}
	return multiObserver(clean)
}

type multiObserver []Observer

func (m multiObserver) Observe(ctx context.Context, event Event) {
	for _, observer := range m {
		SafeObserve(ctx, observer, event)
	}
}

// Filter returns an observer that forwards events accepted by fn.
func Filter(fn func(Event) bool, next Observer) Observer {
	if fn == nil || next == nil {
		return Noop()
	}
	return ObserverFunc(func(ctx context.Context, event Event) {
		if fn(event) {
			next.Observe(ctx, event)
		}
	})
}

// Recover returns an observer that recovers panics from next.
func Recover(next Observer) Observer {
	if next == nil {
		return Noop()
	}
	return ObserverFunc(func(ctx context.Context, event Event) {
		SafeObserve(ctx, next, event)
	})
}

// Sample returns an observer that forwards roughly rate events. The sampling is
// deterministic and process-local; use adapter-level sampling for statistically
// precise exports.
func Sample(rate float64, next Observer) Observer {
	if next == nil || rate <= 0 {
		return Noop()
	}
	if rate >= 1 {
		return next
	}
	period := uint64(math.Round(1 / rate))
	if period == 0 {
		period = 1
	}
	var n atomic.Uint64
	return ObserverFunc(func(ctx context.Context, event Event) {
		if n.Add(1)%period == 0 {
			next.Observe(ctx, event)
		}
	})
}

// Collector records events for tests and local tooling.
type Collector struct {
	mu     sync.Mutex
	events []Event
}

// NewCollector creates an event collector.
func NewCollector() *Collector {
	return &Collector{}
}

// Observe records event.
func (c *Collector) Observe(_ context.Context, event Event) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, CloneEvent(event))
}

// Events returns a defensive copy of collected events.
func (c *Collector) Events() []Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	for i, event := range c.events {
		out[i] = CloneEvent(event)
	}
	return out
}

// Reset clears collected events.
func (c *Collector) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}
