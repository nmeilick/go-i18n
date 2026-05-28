package observe

import (
	"context"
	"testing"
)

func TestContextAttrsAreAppendedAndCopied(t *testing.T) {
	ctx := WithAttrs(context.Background(), Low("component", "test"))
	ctx = WithAttrs(ctx, Private("tenant_id", "t1"))
	attrs := AttrsFrom(ctx)
	if len(attrs) != 2 {
		t.Fatalf("attrs = %#v", attrs)
	}
	attrs[0].Value = "mutated"
	if got := AttrsFrom(ctx)[0].Value; got != "test" {
		t.Fatalf("attrs were mutable through copy: %#v", got)
	}
}

func TestCollectorReturnsDefensiveCopy(t *testing.T) {
	c := NewCollector()
	c.Observe(context.Background(), Event{Name: "test", Attrs: []Attr{Low("status", "ok")}})
	events := c.Events()
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	events[0].Name = "mutated"
	events[0].Attrs[0].Value = "mutated"
	if c.Events()[0].Name != "test" {
		t.Fatal("collector returned mutable event slice")
	}
	if c.Events()[0].Attrs[0].Value != "ok" {
		t.Fatal("collector returned mutable event attrs")
	}
}

func TestPolicyBoundsAttrsAndDiagnostics(t *testing.T) {
	event, ok := (Policy{MaxAttrs: 2, MaxAttrValueBytes: 3, MaxAttrListItems: 1, MaxDiagnostics: 1}).Apply(Event{
		Severity: SeverityInfo,
		Attrs: []Attr{
			SourceText("msgid", "äbcdef"),
			Low("list", []string{"abcdef", "extra"}),
			Low("extra", "x"),
		},
		Diagnostics: []Diagnostic{{Code: "abcdef", Detail: "äbcdef"}, {Code: "b"}},
	})
	if !ok {
		t.Fatal("event was filtered")
	}
	if len(event.Attrs) != 2 || event.Attrs[0].Value != "äb" {
		t.Fatalf("bounded attrs = %#v", event.Attrs)
	}
	if got := event.Attrs[1].Value; len(got.([]string)) != 1 || got.([]string)[0] != "abc" {
		t.Fatalf("bounded list attr = %#v", got)
	}
	if len(event.Diagnostics) != 1 || event.Diagnostics[0].Code != "abc" || event.Diagnostics[0].Detail != "äb" {
		t.Fatalf("bounded diagnostics = %#v", event.Diagnostics)
	}
}

func TestPolicyNormalizesUnknownAttrClasses(t *testing.T) {
	event, ok := DefaultPolicy().Apply(Event{
		Attrs: []Attr{{Key: "x", Value: "y", Visibility: "surprise", Cardinality: "massive"}},
	})
	if !ok {
		t.Fatal("event was filtered")
	}
	if event.Attrs[0].Visibility != VisibilityInternal || event.Attrs[0].Cardinality != CardinalityHigh {
		t.Fatalf("attr classes = %#v", event.Attrs[0])
	}
}

func TestSafeObserveRecoversPanics(t *testing.T) {
	SafeObserve(context.Background(), ObserverFunc(func(context.Context, Event) {
		panic("observer failed")
	}), Event{Name: "test"})
}
