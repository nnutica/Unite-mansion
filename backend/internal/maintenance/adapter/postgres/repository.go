// Package postgres implements the maintenance repository with pgx and sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/internal/maintenance/adapter/postgres/sqlcgen"
	"unite-mansion/internal/maintenance/app"
	"unite-mansion/internal/maintenance/domain"
	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/platform/storage"
)

type Repository struct {
	pool *pgxpool.Pool
	tx   *platformpg.TxManager
}

var _ app.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, tx: platformpg.NewTxManager(pool)}
}

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(platformpg.Conn(ctx, r.pool))
}

// Insert writes the request and its photos atomically (joins an outer tx if any).
func (r *Repository) Insert(ctx context.Context, req *domain.Request) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return fmt.Errorf("request id: %w", err)
	}
	roomID, err := uuid.Parse(req.RoomID)
	if err != nil {
		return fmt.Errorf("room id: %w", err)
	}
	return r.tx.WithinTx(ctx, func(ctx context.Context) error {
		err := r.q(ctx).InsertRequest(ctx, sqlcgen.InsertRequestParams{
			ID: id, RoomID: roomID, Category: string(req.Category), Description: req.Description,
			Status: string(req.Status), CreatedAt: req.CreatedAt, UpdatedAt: req.UpdatedAt,
		})
		if err != nil {
			return fmt.Errorf("insert maintenance request: %w", err)
		}
		for i, key := range req.ImageKeys {
			err := r.q(ctx).InsertImage(ctx, sqlcgen.InsertImageParams{RequestID: id, Position: int32(i), FileKey: key})
			if platformpg.IsUniqueViolation(err, "maintenance_images_file_key_key") {
				return storage.ErrInvalidFileKey // the photo is already attached to another request
			}
			if err != nil {
				return fmt.Errorf("insert maintenance image: %w", err)
			}
		}
		return nil
	})
}

func (r *Repository) UpdateStatus(ctx context.Context, req *domain.Request) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return domain.ErrRequestNotFound
	}
	err = r.q(ctx).UpdateRequestStatus(ctx, sqlcgen.UpdateRequestStatusParams{ID: id, Status: string(req.Status), UpdatedAt: req.UpdatedAt})
	if err != nil {
		return fmt.Errorf("update maintenance status: %w", err)
	}
	return nil
}

func (r *Repository) Request(ctx context.Context, id string, forUpdate bool) (*domain.Request, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.ErrRequestNotFound
	}
	var row sqlcgen.MaintenanceRequest
	if forUpdate {
		row, err = r.q(ctx).GetRequestForUpdate(ctx, uid)
	} else {
		row, err = r.q(ctx).GetRequest(ctx, uid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get maintenance request: %w", err)
	}
	list, err := r.withImages(ctx, []sqlcgen.MaintenanceRequest{row})
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

func (r *Repository) ListByRoom(ctx context.Context, roomID string) ([]*domain.Request, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, fmt.Errorf("room id: %w", err)
	}
	rows, err := r.q(ctx).ListRequestsByRoom(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list maintenance requests: %w", err)
	}
	return r.withImages(ctx, rows)
}

func (r *Repository) List(ctx context.Context, status *domain.Status) ([]*domain.Request, error) {
	var s pgtype.Text
	if status != nil {
		s = pgtype.Text{String: string(*status), Valid: true}
	}
	rows, err := r.q(ctx).ListRequests(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("list maintenance requests: %w", err)
	}
	return r.withImages(ctx, rows)
}

func (r *Repository) withImages(ctx context.Context, rows []sqlcgen.MaintenanceRequest) ([]*domain.Request, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	images, err := r.q(ctx).ListImages(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list maintenance images: %w", err)
	}
	keys := map[uuid.UUID][]string{}
	for _, img := range images {
		keys[img.RequestID] = append(keys[img.RequestID], img.FileKey)
	}
	out := make([]*domain.Request, 0, len(rows))
	for _, row := range rows {
		out = append(out, &domain.Request{
			ID: row.ID.String(), RoomID: row.RoomID.String(), Category: domain.Category(row.Category),
			Description: row.Description, Status: domain.Status(row.Status), ImageKeys: keys[row.ID],
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}
