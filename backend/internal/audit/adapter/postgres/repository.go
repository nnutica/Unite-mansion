// Package postgres implements the audit repositories with pgx and sqlc.
package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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

var _ app.LogReader = (*Repository)(nil)

func (r *Repository) List(ctx context.Context, q app.ListQuery) ([]app.LogEntry, error) {
	var agg pgtype.Text
	if q.AggregateID != nil {
		agg = pgtype.Text{String: *q.AggregateID, Valid: true}
	}
	params := sqlcgen.ListAuditLogsParams{AggregateID: agg, RowLimit: int32(q.Limit)}
	if q.After != nil {
		id, err := uuid.Parse(q.After.ID)
		if err != nil {
			return nil, app.ErrInvalidCursor
		}
		params.CursorAt = &q.After.OccurredAt
		params.CursorID = uuid.NullUUID{UUID: id, Valid: true}
	}
	rows, err := sqlcgen.New(platformpg.Conn(ctx, r.pool)).ListAuditLogs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	out := make([]app.LogEntry, 0, len(rows))
	for _, row := range rows {
		e := app.LogEntry{
			ID: row.ID.String(), EventName: row.EventName, AggregateID: row.AggregateID,
			ActorType: row.ActorType, Payload: row.Payload, OccurredAt: row.OccurredAt,
		}
		if row.ActorID.Valid {
			e.ActorID = &row.ActorID.String
		}
		out = append(out, e)
	}
	return out, nil
}
