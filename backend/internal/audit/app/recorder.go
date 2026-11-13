// Package app holds the audit use cases and the ports they depend on.
package app

import (
	"context"
	"fmt"

	"unite-mansion/internal/audit/domain"
	"unite-mansion/internal/shared"
)

// Repository stores audit entries. It must join the caller's transaction.
type Repository interface {
	Append(ctx context.Context, e domain.Entry) error
}

// Recorder writes every domain event to the audit log (context.md §6.7).
type Recorder struct {
	repo Repository
}

func NewRecorder(repo Repository) *Recorder {
	return &Recorder{repo: repo}
}

// Handle is a shared.EventHandler. A missing actor is an error so that a
// change without a known author rolls back instead of being logged anonymously.
func (r *Recorder) Handle(ctx context.Context, e shared.Event) error {
	actor, ok := shared.ActorFrom(ctx)
	if !ok {
		return fmt.Errorf("record %s: %w", e.EventName(), domain.ErrMissingActor)
	}
	entry, err := domain.NewEntry(e, actor)
	if err != nil {
		return err
	}
	if err := r.repo.Append(ctx, entry); err != nil {
		return fmt.Errorf("record %s: %w", e.EventName(), err)
	}
	return nil
}
