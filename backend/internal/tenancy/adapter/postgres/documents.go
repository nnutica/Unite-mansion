package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/tenancy/adapter/postgres/sqlcgen"
	"unite-mansion/internal/tenancy/app"
	"unite-mansion/internal/tenancy/domain"
)

var _ app.DocumentRepository = (*Repository)(nil)

func (r *Repository) LatestConsentText(ctx context.Context) (domain.ConsentText, error) {
	row, err := r.q(ctx).GetLatestConsentText(ctx)
	if err != nil {
		return domain.ConsentText{}, fmt.Errorf("latest consent text: %w", err)
	}
	return domain.ConsentText{Version: int(row.Version), Body: row.Body, CreatedAt: row.CreatedAt}, nil
}

func (r *Repository) InsertConsentText(ctx context.Context, c domain.ConsentText) error {
	err := r.q(ctx).InsertConsentText(ctx, sqlcgen.InsertConsentTextParams{Version: int32(c.Version), Body: c.Body, CreatedAt: c.CreatedAt})
	if err != nil {
		return fmt.Errorf("insert consent text: %w", err)
	}
	return nil
}

func (r *Repository) ConsentAcceptedAt(ctx context.Context, accountID string, version int) (*time.Time, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	at, err := r.q(ctx).GetConsent(ctx, sqlcgen.GetConsentParams{AccountID: id, Version: int32(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get consent: %w", err)
	}
	return &at, nil
}

func (r *Repository) InsertConsent(ctx context.Context, accountID string, version int, at time.Time) error {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return fmt.Errorf("account id: %w", err)
	}
	if err := r.q(ctx).InsertConsent(ctx, sqlcgen.InsertConsentParams{AccountID: id, Version: int32(version), AcceptedAt: at}); err != nil {
		return fmt.Errorf("insert consent: %w", err)
	}
	return nil
}

func (r *Repository) InsertDocument(ctx context.Context, d domain.TenantDocument) error {
	id, err := uuid.Parse(d.ID)
	if err != nil {
		return fmt.Errorf("document id: %w", err)
	}
	accountID, roomID, err := parseIDs(d.AccountID, d.RoomID)
	if err != nil {
		return err
	}
	err = r.q(ctx).InsertDocument(ctx, sqlcgen.InsertDocumentParams{
		ID: id, AccountID: accountID, RoomID: roomID, DocumentType: string(d.Type), FileKey: d.FileKey,
		ContentType: d.ContentType, SizeBytes: d.SizeBytes, UploadedAt: d.UploadedAt,
	})
	if platformpg.IsUniqueViolation(err, "tenant_documents_file_key_key") {
		return storage.ErrInvalidFileKey // this upload was already submitted
	}
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

func (r *Repository) DocumentsByAccount(ctx context.Context, accountID string) ([]domain.TenantDocument, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	rows, err := r.q(ctx).ListDocumentsByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return toDocuments(rows), nil
}

func (r *Repository) DocumentsByRoom(ctx context.Context, roomID string) ([]domain.TenantDocument, error) {
	id, err := uuid.Parse(roomID)
	if err != nil {
		return nil, domain.ErrRoomNotFound
	}
	rows, err := r.q(ctx).ListDocumentsByRoom(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	return toDocuments(rows), nil
}

func (r *Repository) SetPurgeAt(ctx context.Context, accountID string, at *time.Time) error {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return fmt.Errorf("account id: %w", err)
	}
	if err := r.q(ctx).SetDocumentsPurgeAt(ctx, sqlcgen.SetDocumentsPurgeAtParams{AccountID: id, PurgeAt: at}); err != nil {
		return fmt.Errorf("set purge time: %w", err)
	}
	return nil
}

func (r *Repository) DocumentsDueForPurge(ctx context.Context, now time.Time) ([]domain.TenantDocument, error) {
	rows, err := r.q(ctx).ListDocumentsDueForPurge(ctx, &now)
	if err != nil {
		return nil, fmt.Errorf("documents due: %w", err)
	}
	return toDocuments(rows), nil
}

func (r *Repository) DeleteDocument(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("document id: %w", err)
	}
	if err := r.q(ctx).DeleteDocument(ctx, uid); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	return nil
}

func toDocuments(rows []sqlcgen.TenantDocument) []domain.TenantDocument {
	out := make([]domain.TenantDocument, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.TenantDocument{
			ID: row.ID.String(), AccountID: row.AccountID.String(), RoomID: row.RoomID.String(),
			Type: domain.DocumentType(row.DocumentType), FileKey: row.FileKey, ContentType: row.ContentType,
			SizeBytes: row.SizeBytes, UploadedAt: row.UploadedAt, PurgeAt: row.PurgeAt,
		})
	}
	return out
}
