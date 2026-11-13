package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"unite-mansion/internal/billing/adapter/postgres/sqlcgen"
	"unite-mansion/internal/billing/app"
	"unite-mansion/internal/billing/domain"
	platformpg "unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/shared"
)

func (r *Repository) InvoiceSummariesByRoom(ctx context.Context, roomID string) ([]app.InvoiceSummary, error) {
	uid, err := uuid.Parse(roomID)
	if err != nil {
		return nil, fmt.Errorf("room id: %w", err)
	}
	rows, err := r.q(ctx).ListInvoiceSummariesByRoom(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list invoice summaries: %w", err)
	}
	out := make([]app.InvoiceSummary, 0, len(rows))
	for _, row := range rows {
		sum, err := toSummary(row.ID, row.InvoiceNumber, row.RoomNumber, row.BillingPeriod, row.Total, row.Status, row.HasPendingSlip)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

func (r *Repository) InsertSlip(ctx context.Context, s *domain.PaymentSlip) error {
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return fmt.Errorf("slip id: %w", err)
	}
	invoiceID, err := uuid.Parse(s.InvoiceID)
	if err != nil {
		return fmt.Errorf("invoice id: %w", err)
	}
	err = r.q(ctx).InsertSlip(ctx, sqlcgen.InsertSlipParams{
		ID: id, InvoiceID: invoiceID, FileKey: s.FileKey, FileSha256: s.FileSHA256,
		ContentType: s.ContentType, SizeBytes: s.SizeBytes, Status: string(s.Status), SubmittedAt: s.SubmittedAt,
	})
	if platformpg.IsUniqueViolation(err, "payment_slips_file_key_key") {
		return domain.ErrSlipAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert slip: %w", err)
	}
	return nil
}

func (r *Repository) UpdateSlip(ctx context.Context, s *domain.PaymentSlip) error {
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return domain.ErrSlipNotFound
	}
	var reason pgtype.Text
	if s.RejectReason != nil {
		reason = pgtype.Text{String: string(*s.RejectReason), Valid: true}
	}
	err = r.q(ctx).UpdateSlipReview(ctx, sqlcgen.UpdateSlipReviewParams{
		ID: id, Status: string(s.Status), RejectReason: reason,
		RejectNote: pgtype.Text{String: s.RejectNote, Valid: s.RejectNote != ""}, ReviewedAt: s.ReviewedAt,
	})
	if err != nil {
		return fmt.Errorf("update slip: %w", err)
	}
	return nil
}

func (r *Repository) SlipForUpdate(ctx context.Context, id string) (*domain.PaymentSlip, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, domain.ErrSlipNotFound
	}
	row, err := r.q(ctx).GetSlipForUpdate(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrSlipNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get slip: %w", err)
	}
	return toSlip(row), nil
}

func (r *Repository) SlipsByInvoice(ctx context.Context, invoiceID string) ([]app.SlipView, error) {
	uid, err := uuid.Parse(invoiceID)
	if err != nil {
		return nil, domain.ErrInvoiceNotFound
	}
	rows, err := r.q(ctx).ListSlipsByInvoice(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list slips: %w", err)
	}
	out := make([]app.SlipView, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.SlipView{Slip: toSlip(row.PaymentSlip), PossibleDuplicate: row.PossibleDuplicate})
	}
	return out, nil
}

func (r *Repository) SlipQueue(ctx context.Context, status *domain.SlipStatus) ([]app.SlipQueueItem, error) {
	var s pgtype.Text
	if status != nil {
		s = pgtype.Text{String: string(*status), Valid: true}
	}
	rows, err := r.q(ctx).ListSlipQueue(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("list slip queue: %w", err)
	}
	out := make([]app.SlipQueueItem, 0, len(rows))
	for _, row := range rows {
		item, err := toQueueItem(sqlcgen.GetSlipQueueItemRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Repository) SlipQueueItem(ctx context.Context, slipID string) (app.SlipQueueItem, error) {
	uid, err := uuid.Parse(slipID)
	if err != nil {
		return app.SlipQueueItem{}, domain.ErrSlipNotFound
	}
	row, err := r.q(ctx).GetSlipQueueItem(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SlipQueueItem{}, domain.ErrSlipNotFound
	}
	if err != nil {
		return app.SlipQueueItem{}, fmt.Errorf("get slip queue item: %w", err)
	}
	return toQueueItem(row)
}

func toQueueItem(row sqlcgen.GetSlipQueueItemRow) (app.SlipQueueItem, error) {
	sum, err := toSummary(row.PaymentSlip.InvoiceID, row.InvoiceNumber, row.RoomNumber, row.BillingPeriod, row.Total, row.InvoiceStatus, row.HasPendingSlip)
	if err != nil {
		return app.SlipQueueItem{}, err
	}
	return app.SlipQueueItem{
		Slip:    app.SlipView{Slip: toSlip(row.PaymentSlip), PossibleDuplicate: row.PossibleDuplicate},
		Invoice: sum,
	}, nil
}

func toSummary(id uuid.UUID, number, room, period string, total decimal.Decimal, status string, pending bool) (app.InvoiceSummary, error) {
	p, err := shared.ParseBillingPeriod(period)
	if err != nil {
		return app.InvoiceSummary{}, fmt.Errorf("invoice %s: %w", id, err)
	}
	t, err := money(total)
	if err != nil {
		return app.InvoiceSummary{}, err
	}
	return app.InvoiceSummary{
		ID: id.String(), InvoiceNumber: number, RoomNumber: room, Period: p, Total: t,
		Status: domain.Status(status), HasPendingSlip: pending,
	}, nil
}

func toSlip(row sqlcgen.PaymentSlip) *domain.PaymentSlip {
	s := &domain.PaymentSlip{
		ID: row.ID.String(), InvoiceID: row.InvoiceID.String(), FileKey: row.FileKey, FileSHA256: row.FileSha256,
		ContentType: row.ContentType, SizeBytes: row.SizeBytes, Status: domain.SlipStatus(row.Status),
		RejectNote: row.RejectNote.String, SubmittedAt: row.SubmittedAt, ReviewedAt: row.ReviewedAt,
	}
	if row.RejectReason.Valid {
		reason := domain.RejectReason(row.RejectReason.String)
		s.RejectReason = &reason
	}
	return s
}

var _ app.PeriodReader = (*Repository)(nil)

func (r *Repository) PeriodRoomStatus(ctx context.Context, period shared.BillingPeriod) ([]app.RoomStatusRow, error) {
	rows, err := r.q(ctx).ListPeriodRoomStatus(ctx, sqlcgen.ListPeriodRoomStatusParams{
		FirstDay: period.FirstDay(), LastDay: period.LastDay(), Period: period.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("period room status: %w", err)
	}
	out := make([]app.RoomStatusRow, 0, len(rows))
	for _, row := range rows {
		r := app.RoomStatusRow{RoomNumber: row.RoomNumber, Floor: int(row.Floor), HasLease: row.HasLease, HasPendingSlip: row.HasPendingSlip}
		if row.InvoiceID.Valid {
			id := row.InvoiceID.UUID.String()
			r.InvoiceID = &id
		}
		if row.InvoiceNumber.Valid {
			r.InvoiceNumber = &row.InvoiceNumber.String
		}
		if row.Total.Valid {
			t, err := money(row.Total.Decimal)
			if err != nil {
				return nil, err
			}
			r.Total = &t
		}
		if row.Status.Valid {
			st := domain.Status(row.Status.String)
			r.Status = &st
		}
		out = append(out, r)
	}
	return out, nil
}

func (r *Repository) CountPendingSlips(ctx context.Context) (int, error) {
	n, err := r.q(ctx).CountPendingSlips(ctx)
	if err != nil {
		return 0, fmt.Errorf("count pending slips: %w", err)
	}
	return int(n), nil
}
