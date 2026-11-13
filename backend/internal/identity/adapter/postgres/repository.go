// Package postgres implements the identity repository with pgx and sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/internal/identity/adapter/postgres/sqlcgen"
	"unite-mansion/internal/identity/app"
	"unite-mansion/internal/identity/domain"
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

func (r *Repository) AdminByUsername(ctx context.Context, username string) (domain.Admin, error) {
	row, err := r.q(ctx).GetAdminByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Admin{}, app.ErrNotFound
	}
	if err != nil {
		return domain.Admin{}, fmt.Errorf("get admin by username: %w", err)
	}
	return domain.Admin{ID: row.ID.String(), Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

func (r *Repository) Admin(ctx context.Context, id string) (domain.Admin, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return domain.Admin{}, app.ErrNotFound
	}
	row, err := r.q(ctx).GetAdmin(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Admin{}, app.ErrNotFound
	}
	if err != nil {
		return domain.Admin{}, fmt.Errorf("get admin: %w", err)
	}
	return domain.Admin{ID: row.ID.String(), Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

func (r *Repository) InsertTenantAccount(ctx context.Context, a *domain.TenantAccount) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return fmt.Errorf("account id: %w", err)
	}
	err = r.q(ctx).InsertTenantAccount(ctx, sqlcgen.InsertTenantAccountParams{
		ID: id, Username: a.Username, PasswordHash: a.PasswordHash,
		FullName: a.FullName, Phone: a.Phone, Status: string(a.Status), CreatedAt: a.CreatedAt,
	})
	if platformpg.IsUniqueViolation(err, "tenant_accounts_username_key") {
		return domain.ErrUsernameTaken
	}
	if err != nil {
		return fmt.Errorf("insert tenant account: %w", err)
	}
	return nil
}

func (r *Repository) TenantAccount(ctx context.Context, id string, forUpdate bool) (*domain.TenantAccount, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.ErrAccountNotFound
	}
	var row sqlcgen.TenantAccount
	if forUpdate {
		row, err = r.q(ctx).GetTenantAccountForUpdate(ctx, uid)
	} else {
		row, err = r.q(ctx).GetTenantAccount(ctx, uid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant account: %w", err)
	}
	return toAccount(row), nil
}

func (r *Repository) TenantAccountByUsername(ctx context.Context, username string) (*domain.TenantAccount, error) {
	row, err := r.q(ctx).GetTenantAccountByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant account by username: %w", err)
	}
	return toAccount(row), nil
}

func (r *Repository) ListTenantAccounts(ctx context.Context, status *domain.AccountStatus) ([]*domain.TenantAccount, error) {
	var s pgtype.Text
	if status != nil {
		s = pgtype.Text{String: string(*status), Valid: true}
	}
	rows, err := r.q(ctx).ListTenantAccounts(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("list tenant accounts: %w", err)
	}
	return toAccounts(rows), nil
}

func (r *Repository) ActiveTenantAccounts(ctx context.Context) ([]*domain.TenantAccount, error) {
	rows, err := r.q(ctx).ListActiveTenantAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active tenant accounts: %w", err)
	}
	return toAccounts(rows), nil
}

func (r *Repository) UpdateTenantAccount(ctx context.Context, a *domain.TenantAccount) error {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return domain.ErrAccountNotFound
	}
	var roomID uuid.NullUUID
	if a.RoomID != nil {
		parsed, err := uuid.Parse(*a.RoomID)
		if err != nil {
			return fmt.Errorf("room id: %w", err)
		}
		roomID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	err = r.q(ctx).UpdateTenantAccount(ctx, sqlcgen.UpdateTenantAccountParams{
		ID: id, Status: string(a.Status), RoomID: roomID, UnassignedAt: a.UnassignedAt, PasswordHash: a.PasswordHash,
	})
	if platformpg.IsUniqueViolation(err, "tenant_accounts_one_active_per_room") {
		return domain.ErrRoomAlreadyAssigned
	}
	if err != nil {
		return fmt.Errorf("update tenant account: %w", err)
	}
	return nil
}

func (r *Repository) InsertSession(ctx context.Context, s domain.Session) error {
	subjectID, err := uuid.Parse(s.SubjectID)
	if err != nil {
		return fmt.Errorf("session subject: %w", err)
	}
	err = r.q(ctx).InsertSession(ctx, sqlcgen.InsertSessionParams{
		TokenHash: s.TokenHash, SubjectType: string(s.SubjectType), SubjectID: subjectID, ExpiresAt: s.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *Repository) Session(ctx context.Context, tokenHash []byte) (domain.Session, error) {
	row, err := r.q(ctx).GetSession(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, app.ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	return domain.Session{
		TokenHash: row.TokenHash, SubjectType: domain.SubjectType(row.SubjectType),
		SubjectID: row.SubjectID.String(), ExpiresAt: row.ExpiresAt,
	}, nil
}

func (r *Repository) UpdateSessionExpiry(ctx context.Context, tokenHash []byte, expiresAt time.Time) error {
	if err := r.q(ctx).UpdateSessionExpiry(ctx, sqlcgen.UpdateSessionExpiryParams{TokenHash: tokenHash, ExpiresAt: expiresAt}); err != nil {
		return fmt.Errorf("update session expiry: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if err := r.q(ctx).DeleteSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSessionsFor(ctx context.Context, subject domain.SubjectType, subjectID string) error {
	id, err := uuid.Parse(subjectID)
	if err != nil {
		return fmt.Errorf("session subject: %w", err)
	}
	if err := r.q(ctx).DeleteSessionsForSubject(ctx, sqlcgen.DeleteSessionsForSubjectParams{SubjectType: string(subject), SubjectID: id}); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

// UpsertAdmin creates the single admin or, with reset, changes its password.
// Used by cmd/seed-admin only.
func (r *Repository) UpsertAdmin(ctx context.Context, username, passwordHash string, reset bool) (created bool, err error) {
	existing, err := r.q(ctx).GetAnyAdmin(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := r.q(ctx).InsertAdmin(ctx, sqlcgen.InsertAdminParams{ID: uuid.New(), Username: username, PasswordHash: passwordHash}); err != nil {
			return false, fmt.Errorf("insert admin: %w", err)
		}
		return true, nil
	case err != nil:
		return false, fmt.Errorf("get admin: %w", err)
	case !reset:
		return false, fmt.Errorf("admin %q already exists; use -reset to change its password", existing.Username)
	case existing.Username != username:
		return false, fmt.Errorf("the only admin is %q, not %q", existing.Username, username)
	}
	if err := r.q(ctx).UpdateAdminPassword(ctx, sqlcgen.UpdateAdminPasswordParams{ID: existing.ID, PasswordHash: passwordHash}); err != nil {
		return false, fmt.Errorf("update admin password: %w", err)
	}
	return false, nil
}

func toAccounts(rows []sqlcgen.TenantAccount) []*domain.TenantAccount {
	out := make([]*domain.TenantAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccount(row))
	}
	return out
}

func toAccount(row sqlcgen.TenantAccount) *domain.TenantAccount {
	a := &domain.TenantAccount{
		ID: row.ID.String(), Username: row.Username, PasswordHash: row.PasswordHash,
		FullName: row.FullName, Phone: row.Phone, Status: domain.AccountStatus(row.Status),
		UnassignedAt: row.UnassignedAt, CreatedAt: row.CreatedAt,
	}
	if row.RoomID.Valid {
		id := row.RoomID.UUID.String()
		a.RoomID = &id
	}
	return a
}
