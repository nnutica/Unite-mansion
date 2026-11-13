package shared

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testEvent struct {
	EventBase
	name string
}

func (e testEvent) EventName() string { return e.name }

func TestDispatcherDeliversInOrderToNamedThenAll(t *testing.T) {
	d := NewDispatcher()
	var got []string
	d.Subscribe("A", func(_ context.Context, e Event) error {
		got = append(got, "named:"+e.EventName())
		return nil
	})
	d.SubscribeAll(func(_ context.Context, e Event) error {
		got = append(got, "all:"+e.EventName())
		return nil
	})

	base := NewEventBase("agg-1", time.Now())
	if err := d.Dispatch(context.Background(), testEvent{base, "A"}, testEvent{base, "B"}); err != nil {
		t.Fatal(err)
	}

	want := []string{"named:A", "all:A", "all:B"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDispatcherStopsAtFirstError(t *testing.T) {
	d := NewDispatcher()
	boom := errors.New("boom")
	calls := 0
	d.SubscribeAll(func(context.Context, Event) error {
		calls++
		return boom
	})

	base := NewEventBase("agg-1", time.Now())
	err := d.Dispatch(context.Background(), testEvent{base, "A"}, testEvent{base, "B"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped boom", err)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
}

func TestEventRecorderPullClears(t *testing.T) {
	var r EventRecorder
	r.Record(testEvent{NewEventBase("x", time.Now()), "A"})
	if n := len(r.PullEvents()); n != 1 {
		t.Fatalf("first pull = %d events, want 1", n)
	}
	if n := len(r.PullEvents()); n != 0 {
		t.Fatalf("second pull = %d events, want 0", n)
	}
}
