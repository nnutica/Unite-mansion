package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"unite-mansion/internal/audit/domain"
	"unite-mansion/internal/shared"
)

type fakeRepo struct{ entries []domain.Entry }

func (f *fakeRepo) Append(_ context.Context, e domain.Entry) error {
	f.entries = append(f.entries, e)
	return nil
}

type invoiceIssued struct {
	shared.EventBase
	Period string `json:"period"`
}

func (invoiceIssued) EventName() string { return "InvoiceIssued" }

func TestRecorderWritesEntryWithActorAndPayload(t *testing.T) {
	repo := &fakeRepo{}
	rec := NewRecorder(repo)
	at := time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)
	ctx := shared.WithActor(context.Background(), shared.Actor{Type: shared.ActorAdmin, ID: "admin-1"})

	if err := rec.Handle(ctx, invoiceIssued{shared.NewEventBase("inv-1", at), "2026-11"}); err != nil {
		t.Fatal(err)
	}

	if len(repo.entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(repo.entries))
	}
	e := repo.entries[0]
	if e.EventName != "InvoiceIssued" || e.AggregateID != "inv-1" || e.Actor.ID != "admin-1" || !e.OccurredAt.Equal(at) {
		t.Fatalf("unexpected entry %+v", e)
	}
	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["period"] != "2026-11" || payload["aggregateId"] != "inv-1" {
		t.Fatalf("payload = %s", e.Payload)
	}
}

func TestRecorderRejectsMissingActor(t *testing.T) {
	repo := &fakeRepo{}
	err := NewRecorder(repo).Handle(context.Background(), invoiceIssued{shared.NewEventBase("inv-1", time.Now()), "2026-11"})
	if !errors.Is(err, domain.ErrMissingActor) {
		t.Fatalf("err = %v, want ErrMissingActor", err)
	}
	if len(repo.entries) != 0 {
		t.Fatal("entry written without actor")
	}
}

func TestRecorderRejectsAdminWithoutID(t *testing.T) {
	ctx := shared.WithActor(context.Background(), shared.Actor{Type: shared.ActorAdmin})
	err := NewRecorder(&fakeRepo{}).Handle(ctx, invoiceIssued{shared.NewEventBase("inv-1", time.Now()), "2026-11"})
	if !errors.Is(err, domain.ErrInvalidEntry) {
		t.Fatalf("err = %v, want ErrInvalidEntry", err)
	}
}
