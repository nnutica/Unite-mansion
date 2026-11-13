// Package postgres implements the audit repositories with pgx and sqlc.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/internal/audit/adapter/postgres/sqlcgen"
	"unite-mansion/internal/audit/app"
	"unite-mansion/internal/audit/domain"
	platformpg "unite-mansion/internal/platform/postgres"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ app.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Append(ctx context.Context, e domain.Entry) error {
	q := sqlcgen.New(platformpg.Conn(ctx, r.pool))
	err := q.InsertAuditLog(ctx, sqlcgen.InsertAuditLogParams{
		EventName:   e.EventName,
		AggregateID: e.AggregateID,
		ActorType:   string(e.Actor.Type),
		ActorID:     pgtype.Text{String: e.Actor.ID, Valid: e.Actor.ID != ""},
		Payload:     e.Payload,
		OccurredAt:  e.OccurredAt,
	})
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
