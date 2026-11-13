package app

import (
	"context"
	"fmt"

	"unite-mansion/internal/billing/domain"
	"unite-mansion/internal/shared"
)

// RoomStatusRow is one row of the period read model, as stored.
type RoomStatusRow struct {
	RoomNumber     string
	Floor          int
	HasLease       bool
	InvoiceID      *string
	InvoiceNumber  *string
	Total          *shared.Money
	Status         *domain.Status // nil = no invoice yet
	HasPendingSlip bool
}

// RoomStatus is a row of "สถานะบิลรอบนี้" (P-A03) with its display status.
type RoomStatus struct {
	RoomStatusRow
	DisplayStatus string
}

// PeriodReader is the read-only query behind P-A03 and the dashboard.
type PeriodReader interface {
	PeriodRoomStatus(ctx context.Context, period shared.BillingPeriod) ([]RoomStatusRow, error)
	CountPendingSlips(ctx context.Context) (int, error)
}

// PeriodRooms returns every room with its billing status for period (§6.1, §7).
func (s *Service) PeriodRooms(ctx context.Context, period shared.BillingPeriod) ([]RoomStatus, error) {
	rows, err := s.periods.PeriodRoomStatus(ctx, period)
	if err != nil {
		return nil, fmt.Errorf("period %s room status: %w", period, err)
	}
	now := s.now()
	out := make([]RoomStatus, 0, len(rows))
	for _, r := range rows {
		st := RoomStatus{RoomStatusRow: r, DisplayStatus: "UNBILLED"}
		if r.Status != nil {
			st.DisplayStatus = domain.DisplayStatusOf(*r.Status, period, r.HasPendingSlip, now)
		}
		out = append(out, st)
	}
	return out, nil
}

// PeriodCounts feeds the dashboard cards for one period.
type PeriodCounts struct {
	Unbilled int // rooms with a lease in the period and no invoice
	Unpaid   int // ISSUED, OVERDUE or PENDING_VERIFICATION
}

func (s *Service) PeriodCounts(ctx context.Context, period shared.BillingPeriod) (PeriodCounts, error) {
	rooms, err := s.PeriodRooms(ctx, period)
	if err != nil {
		return PeriodCounts{}, err
	}
	var c PeriodCounts
	for _, r := range rooms {
		switch r.DisplayStatus {
		case "UNBILLED":
			if r.HasLease {
				c.Unbilled++
			}
		case "ISSUED", "OVERDUE", "PENDING_VERIFICATION":
			c.Unpaid++
		}
	}
	return c, nil
}

func (s *Service) PendingSlipCount(ctx context.Context) (int, error) {
	n, err := s.periods.CountPendingSlips(ctx)
	if err != nil {
		return 0, fmt.Errorf("count pending slips: %w", err)
	}
	return n, nil
}
