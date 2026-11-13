// Package postgres implements the tenancy repository with pgx and sqlc.
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

	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/shared"
	"unite-mansion/internal/tenancy/adapter/postgres/sqlcgen"
	"unite-mansion/internal/tenancy/app"
	"unite-mansion/internal/tenancy/domain"
)

const leaseOverlapConstraint = "leases_no_overlap"

type Repository struct {
	pool *pgxpool.Pool
}

var _ app.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(platformpg.Conn(ctx, r.pool))
}

func (r *Repository) ListRooms(ctx context.Context) ([]domain.Room, error) {
	rows, err := r.q(ctx).ListRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	out := make([]domain.Room, 0, len(rows))
	for _, row := range rows {
		room, err := toRoom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, room)
	}
	return out, nil
}

func (r *Repository) RoomByNumber(ctx context.Context, number string, forUpdate bool) (domain.Room, error) {
	var row sqlcgen.Room
	var err error
	if forUpdate {
		row, err = r.q(ctx).GetRoomByNumberForUpdate(ctx, number)
	} else {
		row, err = r.q(ctx).GetRoomByNumber(ctx, number)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	if err != nil {
		return domain.Room{}, fmt.Errorf("get room %s: %w", number, err)
	}
	return toRoom(row)
}

func (r *Repository) RoomByID(ctx context.Context, id string) (domain.Room, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	row, err := r.q(ctx).GetRoomByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	if err != nil {
		return domain.Room{}, fmt.Errorf("get room: %w", err)
	}
	return toRoom(row)
}

func (r *Repository) UpdateRoom(ctx context.Context, room domain.Room) error {
	id, err := uuid.Parse(room.ID)
	if err != nil {
		return domain.ErrRoomNotFound
	}
	err = r.q(ctx).UpdateRoom(ctx, sqlcgen.UpdateRoomParams{
		ID:                  id,
		BasePrice:           room.BasePrice.Decimal(),
		ElectricMeterDigits: int4(room.ElectricMeterDigits),
		WaterMeterDigits:    int4(room.WaterMeterDigits),
	})
	if err != nil {
		return fmt.Errorf("update room %s: %w", room.Number, err)
	}
	return nil
}

func (r *Repository) LeasesByRoom(ctx context.Context, roomID string) ([]domain.Lease, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, domain.ErrRoomNotFound
	}
	rows, err := r.q(ctx).ListLeasesByRoom(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list leases: %w", err)
	}
	return toLeases(rows), nil
}

func (r *Repository) LeasesActiveOn(ctx context.Context, date time.Time) ([]domain.Lease, error) {
	rows, err := r.q(ctx).ListLeasesActiveOn(ctx, date)
	if err != nil {
		return nil, fmt.Errorf("list active leases: %w", err)
	}
	return toLeases(rows), nil
}

func (r *Repository) LeaseForUpdate(ctx context.Context, id string) (domain.Lease, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return domain.Lease{}, domain.ErrLeaseNotFound
	}
	row, err := r.q(ctx).GetLeaseForUpdate(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Lease{}, domain.ErrLeaseNotFound
	}
	if err != nil {
		return domain.Lease{}, fmt.Errorf("get lease: %w", err)
	}
	return toLease(row), nil
}

func (r *Repository) InsertLease(ctx context.Context, l domain.Lease) error {
	id, roomID, err := parseIDs(l.ID, l.RoomID)
	if err != nil {
		return err
	}
	err = r.q(ctx).InsertLease(ctx, sqlcgen.InsertLeaseParams{
		ID: id, RoomID: roomID, TenantName: l.TenantName, StartDate: l.StartDate, EndDate: l.EndDate,
	})
	return leaseWriteError("insert lease", err)
}

func (r *Repository) UpdateLease(ctx context.Context, l domain.Lease) error {
	id, err := uuid.Parse(l.ID)
	if err != nil {
		return domain.ErrLeaseNotFound
	}
	err = r.q(ctx).UpdateLease(ctx, sqlcgen.UpdateLeaseParams{
		ID: id, TenantName: l.TenantName, StartDate: l.StartDate, EndDate: l.EndDate,
	})
	return leaseWriteError("update lease", err)
}

func (r *Repository) ChargesByRoom(ctx context.Context, roomID string) ([]domain.RecurringCharge, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, domain.ErrRoomNotFound
	}
	rows, err := r.q(ctx).ListRecurringChargesByRoom(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list recurring charges: %w", err)
	}
	out := make([]domain.RecurringCharge, 0, len(rows))
	for _, row := range rows {
		c, err := toCharge(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *Repository) ChargeForUpdate(ctx context.Context, id string) (domain.RecurringCharge, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return domain.RecurringCharge{}, domain.ErrRecurringChargeNotFound
	}
	row, err := r.q(ctx).GetRecurringChargeForUpdate(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RecurringCharge{}, domain.ErrRecurringChargeNotFound
	}
	if err != nil {
		return domain.RecurringCharge{}, fmt.Errorf("get recurring charge: %w", err)
	}
	return toCharge(row)
}

func (r *Repository) InsertCharge(ctx context.Context, c domain.RecurringCharge) error {
	id, roomID, err := parseIDs(c.ID, c.RoomID)
	if err != nil {
		return err
	}
	err = r.q(ctx).InsertRecurringCharge(ctx, sqlcgen.InsertRecurringChargeParams{
		ID: id, RoomID: roomID, Name: c.Name, Amount: c.Amount.Decimal(), Active: c.Active,
	})
	if err != nil {
		return fmt.Errorf("insert recurring charge: %w", err)
	}
	return nil
}

func (r *Repository) UpdateCharge(ctx context.Context, c domain.RecurringCharge) error {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return domain.ErrRecurringChargeNotFound
	}
	err = r.q(ctx).UpdateRecurringCharge(ctx, sqlcgen.UpdateRecurringChargeParams{
		ID: id, Name: c.Name, Amount: c.Amount.Decimal(), Active: c.Active,
	})
	if err != nil {
		return fmt.Errorf("update recurring charge: %w", err)
	}
	return nil
}

func leaseWriteError(op string, err error) error {
	if platformpg.IsExclusionViolation(err, leaseOverlapConstraint) {
		return domain.ErrLeaseOverlap
	}
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func parseIDs(id, roomID string) (uuid.UUID, uuid.UUID, error) {
	a, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("id: %w", err)
	}
	b, err := uuid.Parse(roomID)
	if err != nil {
		return uuid.Nil, uuid.Nil, domain.ErrRoomNotFound
	}
	return a, b, nil
}

func toRoom(row sqlcgen.Room) (domain.Room, error) {
	price, err := shared.MoneyFromDecimal(row.BasePrice)
	if err != nil {
		return domain.Room{}, fmt.Errorf("room %s price: %w", row.RoomNumber, err)
	}
	return domain.Room{
		ID:                  row.ID.String(),
		Number:              row.RoomNumber,
		Floor:               int(row.Floor),
		BasePrice:           price,
		ElectricMeterDigits: intPtr(row.ElectricMeterDigits),
		WaterMeterDigits:    intPtr(row.WaterMeterDigits),
	}, nil
}

func toLeases(rows []sqlcgen.Lease) []domain.Lease {
	out := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		out = append(out, toLease(row))
	}
	return out
}

func toLease(row sqlcgen.Lease) domain.Lease {
	return domain.Lease{
		ID: row.ID.String(), RoomID: row.RoomID.String(), TenantName: row.TenantName,
		StartDate: row.StartDate, EndDate: row.EndDate,
	}
}

func toCharge(row sqlcgen.RecurringCharge) (domain.RecurringCharge, error) {
	amount, err := shared.MoneyFromDecimal(row.Amount)
	if err != nil {
		return domain.RecurringCharge{}, fmt.Errorf("charge %s amount: %w", row.ID, err)
	}
	return domain.RecurringCharge{
		ID: row.ID.String(), RoomID: row.RoomID.String(), Name: row.Name, Amount: amount, Active: row.Active,
	}, nil
}

func intPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int32)
	return &i
}

func int4(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true}
}
