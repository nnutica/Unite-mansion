// Package postgres implements the communication repository with pgx and sqlc.
package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/internal/communication/adapter/postgres/sqlcgen"
	"unite-mansion/internal/communication/app"
	"unite-mansion/internal/communication/domain"
	platformpg "unite-mansion/internal/platform/postgres"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ app.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(platformpg.Conn(ctx, r.pool))
}

func (r *Repository) InsertAnnouncement(ctx context.Context, a domain.Announcement) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("announcement id: %w", err)
	}
	err = r.q(ctx).InsertAnnouncement(ctx, sqlcgen.InsertAnnouncementParams{
		ID: id, Title: a.Title, Message: a.Message, Source: a.Source, SourceID: a.SourceID, CreatedAt: a.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("insert announcement: %w", err)
	}
	return nil
}

func (r *Repository) ListAnnouncements(ctx context.Context) ([]domain.Announcement, error) {
	rows, err := r.q(ctx).ListAnnouncements(ctx)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	out := make([]domain.Announcement, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Announcement{
			ID: row.ID.String(), Title: row.Title, Message: row.Message,
			Source: row.Source, SourceID: row.SourceID, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func (r *Repository) Settings(ctx context.Context) (domain.Settings, error) {
	row, err := r.q(ctx).GetSettings(ctx)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	s := domain.Settings{LeaseExpiryAlertDays: int(row.LeaseExpiryAlertDays)}
	if row.PaymentQrKey.Valid {
		s.PaymentQRKey = &row.PaymentQrKey.String
	}
	return s, nil
}

func (r *Repository) SaveSettings(ctx context.Context, s domain.Settings) error {
	var qr pgtype.Text
	if s.PaymentQRKey != nil {
		qr = pgtype.Text{String: *s.PaymentQRKey, Valid: true}
	}
	err := r.q(ctx).UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{LeaseExpiryAlertDays: int32(s.LeaseExpiryAlertDays), PaymentQrKey: qr})
	if err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return nil
}
