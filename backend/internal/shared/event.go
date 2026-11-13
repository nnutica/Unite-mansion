package shared

import (
	"context"
	"fmt"
	"time"
)

// Event is a domain event. Events are dispatched in-process, inside the same
// transaction as the change that raised them (context.md §6.7).
type Event interface {
	EventName() string
	AggregateID() string
	OccurredAt() time.Time
}

// EventBase carries the fields every event has. Embed it in concrete events.
type EventBase struct {
	Aggregate string    `json:"aggregateId"`
	At        time.Time `json:"occurredAt"`
}

func NewEventBase(aggregateID string, at time.Time) EventBase {
	return EventBase{Aggregate: aggregateID, At: at.UTC()}
}

func (e EventBase) AggregateID() string   { return e.Aggregate }
func (e EventBase) OccurredAt() time.Time { return e.At }

// EventRecorder collects events raised by an aggregate until they are dispatched.
type EventRecorder struct {
	events []Event
}

func (r *EventRecorder) Record(e Event) {
	r.events = append(r.events, e)
}

// PullEvents returns the recorded events and clears them.
func (r *EventRecorder) PullEvents() []Event {
	events := r.events
	r.events = nil
	return events
}

// EventHandler handles one event. Returning an error aborts the dispatch so
// the surrounding transaction rolls back.
type EventHandler func(ctx context.Context, e Event) error

// Dispatcher delivers events synchronously to subscribed handlers.
// Subscribe everything during startup; it is not safe for concurrent Subscribe.
type Dispatcher struct {
	byName map[string][]EventHandler
	all    []EventHandler
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{byName: map[string][]EventHandler{}}
}

// Subscribe registers h for events with the given name.
func (d *Dispatcher) Subscribe(eventName string, h EventHandler) {
	d.byName[eventName] = append(d.byName[eventName], h)
}

// SubscribeAll registers h for every event (used by audit).
func (d *Dispatcher) SubscribeAll(h EventHandler) {
	d.all = append(d.all, h)
}

// Dispatch delivers events in order and stops at the first handler error.
// Call it inside TxManager.WithinTx so handlers share the transaction.
func (d *Dispatcher) Dispatch(ctx context.Context, events ...Event) error {
	for _, e := range events {
		for _, h := range d.byName[e.EventName()] {
			if err := h(ctx, e); err != nil {
				return fmt.Errorf("handle %s: %w", e.EventName(), err)
			}
		}
		for _, h := range d.all {
			if err := h(ctx, e); err != nil {
				return fmt.Errorf("handle %s: %w", e.EventName(), err)
			}
		}
	}
	return nil
}
