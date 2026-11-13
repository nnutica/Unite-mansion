// Package domain holds the audit aggregates and invariants (AuditLog, written from domain events).
// Imports: standard library and internal/shared only.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"unite-mansion/internal/shared"
)

var (
	ErrMissingActor = errors.New("audit: no actor in context")
	ErrInvalidEntry = errors.New("audit: invalid entry")
)

// Entry is one append-only audit log row. It never stores file contents,
// only what happened, to which aggregate, by whom and when.
type Entry struct {
	EventName   string
	AggregateID string
	Actor       shared.Actor
	Payload     json.RawMessage
	OccurredAt  time.Time
}

// NewEntry builds an entry from a domain event and the acting user.
func NewEntry(e shared.Event, actor shared.Actor) (Entry, error) {
	if e.EventName() == "" || e.AggregateID() == "" || e.OccurredAt().IsZero() {
		return Entry{}, fmt.Errorf("%w: event %q missing name, aggregate or time", ErrInvalidEntry, e.EventName())
	}
	switch actor.Type {
	case shared.ActorAdmin, shared.ActorTenant:
		if actor.ID == "" {
			return Entry{}, fmt.Errorf("%w: %s actor without id", ErrInvalidEntry, actor.Type)
		}
	case shared.ActorSystem:
	default:
		return Entry{}, fmt.Errorf("%w: unknown actor type %q", ErrInvalidEntry, actor.Type)
	}

	payload, err := json.Marshal(e)
	if err != nil {
		return Entry{}, fmt.Errorf("audit: marshal %s payload: %w", e.EventName(), err)
	}
	return Entry{
		EventName:   e.EventName(),
		AggregateID: e.AggregateID(),
		Actor:       actor,
		Payload:     payload,
		OccurredAt:  e.OccurredAt().UTC(),
	}, nil
}
