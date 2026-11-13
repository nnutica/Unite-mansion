// Package postgres implements the billing repository with pgx and sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"unite-mansion/internal/billing/adapter/postgres/sqlcgen"
	"unite-mansion/internal/billing/app"
	"unite-mansion/internal/billing/domain"
	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/shared"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ app.Repository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(platformpg.Conn(ctx, r.pool))
}

// --- utility rates ---

func (r *Repository) ListRates(ctx context.Context) ([]domain.UtilityRate, error) {
	rows, err := r.q(ctx).ListUtilityRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list utility rates: %w", err)
	}
	out := make([]domain.UtilityRate, 0, len(rows))
	for _, row := range rows {
		rate, err := toRate(row)
		if err != nil {
			return nil, err
		}
		out = append(out, rate)
	}
	return out, nil
}

func (r *Repository) LatestRate(ctx context.Context, typ domain.UtilityType) (*domain.UtilityRate, error) {
	row, err := r.q(ctx).GetLatestUtilityRate(ctx, string(typ))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest %s rate: %w", typ, err)
	}
	rate, err := toRate(row)
	if err != nil {
		return nil, err
	}
	return &rate, nil
}

func (r *Repository) InsertRate(ctx context.Context, rate domain.UtilityRate) error {
	id, err := uuid.Parse(rate.ID)
	if err != nil {
		return fmt.Errorf("rate id: %w", err)
	}
	err = r.q(ctx).InsertUtilityRate(ctx, sqlcgen.InsertUtilityRateParams{
		ID: id, UtilityType: string(rate.Type), PerUnit: rate.PerUnit.Decimal(),
		EffectiveFrom: rate.EffectiveFrom, CreatedAt: rate.CreatedAt,
	})
	if platformpg.IsUniqueViolation(err, "utility_rates_type_from_uq") {
		return domain.ErrRateNotAfterLatest
	}
	if err != nil {
		return fmt.Errorf("insert utility rate: %w", err)
	}
	return nil
}

// --- invoices ---

func (r *Repository) InsertInvoice(ctx context.Context, inv *domain.Invoice) error {
	id, roomID, err := invoiceIDs(inv)
	if err != nil {
		return err
	}
	snap := inv.Snapshot()
	e, w := inv.Meter(domain.Electricity), inv.Meter(domain.Water)
	err = r.q(ctx).InsertInvoice(ctx, sqlcgen.InsertInvoiceParams{
		ID: id, InvoiceNumber: inv.InvoiceNumber(), RoomID: roomID, RoomNumber: inv.RoomNumber(),
		BillingPeriod: inv.Period().String(), Status: string(inv.Status()),
		RentAmount: snap.Rent.Decimal(), ElectricRate: snap.ElectricRate.Decimal(), WaterRate: snap.WaterRate.Decimal(),
		ElectricMeterDigits: int32(snap.ElectricDigits), WaterMeterDigits: int32(snap.WaterDigits),
		ElectricPrevious: nullInt8(e.Previous), ElectricCurrent: nullInt8(e.Current), ElectricRollover: e.RolloverConfirmed,
		WaterPrevious: nullInt8(w.Previous), WaterCurrent: nullInt8(w.Current), WaterRollover: w.RolloverConfirmed,
		Total: inv.Total().Decimal(), IssuedAt: inv.IssuedAt(), PaidAt: inv.PaidAt(), CreatedAt: inv.CreatedAt(),
	})
	if platformpg.IsUniqueViolation(err, "invoices_room_period_uq") {
		return domain.ErrInvoiceAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert invoice: %w", err)
	}
	return r.writeLines(ctx, id, inv)
}

func (r *Repository) UpdateInvoice(ctx context.Context, inv *domain.Invoice) error {
	id, _, err := invoiceIDs(inv)
	if err != nil {
		return err
	}
	e, w := inv.Meter(domain.Electricity), inv.Meter(domain.Water)
	err = r.q(ctx).UpdateInvoice(ctx, sqlcgen.UpdateInvoiceParams{
		ID: id, Status: string(inv.Status()),
		ElectricPrevious: nullInt8(e.Previous), ElectricCurrent: nullInt8(e.Current), ElectricRollover: e.RolloverConfirmed,
		WaterPrevious: nullInt8(w.Previous), WaterCurrent: nullInt8(w.Current), WaterRollover: w.RolloverConfirmed,
		Total: inv.Total().Decimal(), IssuedAt: inv.IssuedAt(), PaidAt: inv.PaidAt(),
	})
	if err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}
	if err := r.q(ctx).DeleteInvoiceLines(ctx, id); err != nil {
		return fmt.Errorf("delete invoice lines: %w", err)
	}
	return r.writeLines(ctx, id, inv)
}

// writeLines stores every line. Derived lines get a fresh ID each save;
// discount/penalty lines keep theirs. RECURRING lines are the persisted form
// of the recurring-charge snapshot (INV-4).
func (r *Repository) writeLines(ctx context.Context, invoiceID uuid.UUID, inv *domain.Invoice) error {
	for i, l := range inv.Lines() {
		lineID := uuid.New()
		if l.ID != "" {
			parsed, err := uuid.Parse(l.ID)
			if err != nil {
				return fmt.Errorf("line id: %w", err)
			}
			lineID = parsed
		}
		err := r.q(ctx).InsertInvoiceLine(ctx, sqlcgen.InsertInvoiceLineParams{
			ID: lineID, InvoiceID: invoiceID, Position: int32(i), LineType: string(l.Type),
			Description: l.Description, Amount: l.Amount.Decimal(),
		})
		if err != nil {
			return fmt.Errorf("insert invoice line: %w", err)
		}
	}
	return nil
}

func (r *Repository) Invoice(ctx context.Context, id string, forUpdate bool) (*domain.Invoice, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.ErrInvoiceNotFound
	}
	var row sqlcgen.Invoice
	if forUpdate {
		row, err = r.q(ctx).GetInvoiceForUpdate(ctx, uid)
	} else {
		row, err = r.q(ctx).GetInvoice(ctx, uid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvoiceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}
	return r.load(ctx, row)
}

func (r *Repository) InvoiceByRoomPeriod(ctx context.Context, roomID string, period shared.BillingPeriod) (*domain.Invoice, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, domain.ErrInvoiceNotFound
	}
	row, err := r.q(ctx).GetInvoiceByRoomPeriod(ctx, sqlcgen.GetInvoiceByRoomPeriodParams{RoomID: uid, BillingPeriod: period.String()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvoiceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invoice by room and period: %w", err)
	}
	return r.load(ctx, row)
}

func (r *Repository) IssuedReadingsBefore(ctx context.Context, roomID string, period shared.BillingPeriod) ([]app.PastReading, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, fmt.Errorf("room id: %w", err)
	}
	rows, err := r.q(ctx).ListIssuedReadingsBefore(ctx, sqlcgen.ListIssuedReadingsBeforeParams{RoomID: uid, BillingPeriod: period.String()})
	if err != nil {
		return nil, fmt.Errorf("issued readings: %w", err)
	}
	out := make([]app.PastReading, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.PastReading{
			Electric: pastReading(row.ElectricPrevious, row.ElectricCurrent, row.ElectricMeterDigits),
			Water:    pastReading(row.WaterPrevious, row.WaterCurrent, row.WaterMeterDigits),
		})
	}
	return out, nil
}

func (r *Repository) load(ctx context.Context, row sqlcgen.Invoice) (*domain.Invoice, error) {
	period, err := shared.ParseBillingPeriod(row.BillingPeriod)
	if err != nil {
		return nil, fmt.Errorf("invoice %s: %w", row.ID, err)
	}
	snap := domain.Snapshot{ElectricDigits: int(row.ElectricMeterDigits), WaterDigits: int(row.WaterMeterDigits)}
	if snap.Rent, err = money(row.RentAmount); err != nil {
		return nil, err
	}
	if snap.ElectricRate, err = money(row.ElectricRate); err != nil {
		return nil, err
	}
	if snap.WaterRate, err = money(row.WaterRate); err != nil {
		return nil, err
	}

	lines, err := r.q(ctx).ListInvoiceLines(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("list invoice lines: %w", err)
	}
	var manual []domain.Line
	for _, l := range lines {
		amount, err := money(l.Amount)
		if err != nil {
			return nil, err
		}
		switch domain.LineType(l.LineType) {
		case domain.LineRecurring:
			snap.Recurring = append(snap.Recurring, domain.RecurringItem{Name: l.Description, Amount: amount})
		case domain.LineDiscount, domain.LinePenalty:
			manual = append(manual, domain.Line{ID: l.ID.String(), Type: domain.LineType(l.LineType), Description: l.Description, Amount: amount})
		}
	}

	state := domain.InvoiceState{
		ID: row.ID.String(), InvoiceNumber: row.InvoiceNumber, RoomID: row.RoomID.String(), RoomNumber: row.RoomNumber,
		Period: period, Status: domain.Status(row.Status), Snapshot: snap,
		Electric:    domain.MeterState{Previous: i64(row.ElectricPrevious), Current: i64(row.ElectricCurrent), RolloverConfirmed: row.ElectricRollover, Digits: snap.ElectricDigits},
		Water:       domain.MeterState{Previous: i64(row.WaterPrevious), Current: i64(row.WaterCurrent), RolloverConfirmed: row.WaterRollover, Digits: snap.WaterDigits},
		ManualLines: manual, IssuedAt: row.IssuedAt, PaidAt: row.PaidAt, CreatedAt: row.CreatedAt,
	}
	history, err := r.IssuedReadingsBefore(ctx, state.RoomID, period)
	if err != nil {
		return nil, err
	}
	if len(history) > 0 {
		if e := history[0].Electric; e != nil {
			state.DefaultElectric = &e.Current
		}
		if w := history[0].Water; w != nil {
			state.DefaultWater = &w.Current
		}
	}
	return domain.Rehydrate(state), nil
}

func invoiceIDs(inv *domain.Invoice) (uuid.UUID, uuid.UUID, error) {
	id, err := uuid.Parse(inv.ID())
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invoice id: %w", err)
	}
	roomID, err := uuid.Parse(inv.RoomID())
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("room id: %w", err)
	}
	return id, roomID, nil
}

func pastReading(prev, curr pgtype.Int8, digits int32) *domain.MeterReading {
	if !prev.Valid || !curr.Valid {
		return nil
	}
	return &domain.MeterReading{Previous: prev.Int64, Current: curr.Int64, Digits: int(digits), RolloverConfirmed: curr.Int64 < prev.Int64}
}

func toRate(row sqlcgen.UtilityRate) (domain.UtilityRate, error) {
	perUnit, err := money(row.PerUnit)
	if err != nil {
		return domain.UtilityRate{}, err
	}
	return domain.UtilityRate{
		ID: row.ID.String(), Type: domain.UtilityType(row.UtilityType), PerUnit: perUnit,
		EffectiveFrom: row.EffectiveFrom, CreatedAt: row.CreatedAt,
	}, nil
}

func money(d decimal.Decimal) (shared.Money, error) {
	m, err := shared.MoneyFromDecimal(d)
	if err != nil {
		return shared.Money{}, fmt.Errorf("stored amount: %w", err)
	}
	return m, nil
}

func nullInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func i64(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
