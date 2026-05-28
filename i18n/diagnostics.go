package i18n

import "sync"

// DiagnosticSink receives redacted runtime diagnostics. Implementations must be
// safe for concurrent use when shared across runtimes.
type DiagnosticSink interface {
	Report(Diagnostic)
}

type noopSink struct{}

func (noopSink) Report(Diagnostic) {}

// NoopDiagnostics returns a sink that drops diagnostics.
func NoopDiagnostics() DiagnosticSink {
	return noopSink{}
}

// CollectorSink collects diagnostics for tests and local tooling.
type CollectorSink struct {
	mu          sync.Mutex
	diagnostics []Diagnostic
}

// NewCollectorSink creates a diagnostic collector.
func NewCollectorSink() *CollectorSink {
	return &CollectorSink{}
}

// Report records a diagnostic.
func (c *CollectorSink) Report(d Diagnostic) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diagnostics = append(c.diagnostics, d)
}

// Diagnostics returns a defensive copy of collected diagnostics.
func (c *CollectorSink) Diagnostics() []Diagnostic {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Diagnostic, len(c.diagnostics))
	copy(out, c.diagnostics)
	return out
}

// Reset clears collected diagnostics.
func (c *CollectorSink) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diagnostics = nil
}
