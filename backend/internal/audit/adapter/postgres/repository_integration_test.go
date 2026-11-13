//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	auditpg "unite-mansion/internal/audit/adapter/postgres"
	"unite-mansion/internal/audit/adapter/postgres/sqlcgen"
	"unite-mansion/internal/audit/app"
	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/platform/postgres/pgtest"
	"unite-mansion/internal/shared"
)

type testEvent struct{ shared.EventBase }

func (testEvent) EventName() string { return "TestHappened" }

func setup(t *testing.T) (context.Context, *platformpg.TxManager, *shared.Dispatcher, *sqlcgen.Queries) {
	pool := pgtest.NewPool(t)
	d := shared.NewDispatcher()
	d.SubscribeAll(app.NewRecorder(auditpg.NewRepository(pool)).Handle)
	ctx := shared.WithActor(context.Background(), shared.Actor{Type: shared.ActorAdmin, ID: "admin-1"})
	return ctx, platformpg.NewTxManager(pool), d, sqlcgen.New(pool)
}

func TestAuditIsWrittenInsideCommittedTx(t *testing.T) {
	ctx, txm, d, q := setup(t)

	err := txm.WithinTx(ctx, func(ctx context.Context) error {
		return d.Dispatch(ctx, testEvent{shared.NewEventBase("agg-1", time.Now())})
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListAuditLogsByAggregate(ctx, "agg-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].EventName != "TestHappened" || rows[0].ActorType != "ADMIN" || rows[0].ActorID.String != "admin-1" {
		t.Fatalf("unexpected rows %+v", rows)
	}
}

func TestAuditIsRolledBackWithTx(t *testing.T) {
	ctx, txm, d, q := setup(t)
	boom := errors.New("use case failed after event")

	err := txm.WithinTx(ctx, func(ctx context.Context) error {
		if err := d.Dispatch(ctx, testEvent{shared.NewEventBase("agg-2", time.Now())}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}

	rows, err := q.ListAuditLogsByAggregate(ctx, "agg-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("audit row survived rollback: %+v", rows)
	}
}
