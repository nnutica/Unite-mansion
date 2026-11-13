// Package app holds the billing use cases: utility rates and the per-room
// invoice workflow draft → issue (context.md §1 flow, §6.1–6.3).
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"unite-mansion/internal/billing/domain"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
)

// ErrRoomNotFound is returned by TenancyReader for an unknown room number.
var ErrRoomNotFound = errors.New("room not found")

// RoomBillingInfo is what tenancy provides for creating a draft.
type RoomBillingInfo struct {
	RoomID           string
	RoomNumber       string
	Rent             shared.Money
	ElectricDigits   *int
	WaterDigits      *int
	HasLeaseInPeriod bool
	Recurring        []domain.RecurringItem
}

// TenancyReader is implemented by tenancy and wired in cmd/api; billing never
// reads or writes tenancy tables itself.
type TenancyReader interface {
	BillingInfo(ctx context.Context, roomNumber string, period shared.BillingPeriod) (RoomBillingInfo, error)
	RoomIDByNumber(ctx context.Context, roomNumber string) (string, error)
}

// PastReading is one earlier issued invoice's readings, newest first.
type PastReading struct {
	Electric *domain.MeterReading
	Water    *domain.MeterReading
}

type Repository interface {
	ListRates(ctx context.Context) ([]domain.UtilityRate, error)
	LatestRate(ctx context.Context, typ domain.UtilityType) (*domain.UtilityRate, error)
	InsertRate(ctx context.Context, r domain.UtilityRate) error

	InsertInvoice(ctx context.Context, inv *domain.Invoice) error // ErrInvoiceAlreadyExists
	UpdateInvoice(ctx context.Context, inv *domain.Invoice) error
	Invoice(ctx context.Context, id string, forUpdate bool) (*domain.Invoice, error) // ErrInvoiceNotFound
	InvoiceByRoomPeriod(ctx context.Context, roomID string, period shared.BillingPeriod) (*domain.Invoice, error)
	// IssuedReadingsBefore returns up to 3 issued/paid invoices of the room
	// before period, newest first (INV-8 default and usage history).
	IssuedReadingsBefore(ctx context.Context, roomID string, period shared.BillingPeriod) ([]PastReading, error)
	InvoiceSummariesByRoom(ctx context.Context, roomID string) ([]InvoiceSummary, error) // non-draft, newest first

	InsertSlip(ctx context.Context, s *domain.PaymentSlip) error // ErrSlipAlreadyExists
	UpdateSlip(ctx context.Context, s *domain.PaymentSlip) error
	SlipForUpdate(ctx context.Context, id string) (*domain.PaymentSlip, error) // ErrSlipNotFound
	SlipsByInvoice(ctx context.Context, invoiceID string) ([]SlipView, error)  // oldest first
	SlipQueue(ctx context.Context, status *domain.SlipStatus) ([]SlipQueueItem, error)
	SlipQueueItem(ctx context.Context, slipID string) (SlipQueueItem, error)
}

type EventDispatcher interface {
	Dispatch(ctx context.Context, events ...shared.Event) error
}

type Service struct {
	repo    Repository
	periods PeriodReader
	tenancy TenancyReader
	files   storage.FileStorage
	tx      shared.TxManager
	events  EventDispatcher
	now     func() time.Time
	newID   func() string
}

func NewService(repo Repository, periods PeriodReader, tenancy TenancyReader, files storage.FileStorage, tx shared.TxManager,
	events EventDispatcher, now func() time.Time, newID func() string) *Service {
	return &Service{repo: repo, periods: periods, tenancy: tenancy, files: files, tx: tx, events: events, now: now, newID: newID}
}

// --- utility rates ---

type RatesView struct {
	Electricity domain.UtilityRate
	Water       domain.UtilityRate
	History     []domain.UtilityRate
}

func (s *Service) ListRates(ctx context.Context) (RatesView, error) {
	rates, err := s.repo.ListRates(ctx)
	if err != nil {
		return RatesView{}, fmt.Errorf("list rates: %w", err)
	}
	today := shared.DateOf(s.now())
	elec, okE := domain.RateOn(rates, domain.Electricity, today)
	water, okW := domain.RateOn(rates, domain.Water, today)
	if !okE || !okW {
		return RatesView{}, domain.ErrRateNotFound
	}
	return RatesView{Electricity: elec, Water: water, History: rates}, nil
}

// CreateRate appends a rate (M-09) and raises UtilityRateChanged (§6.3).
func (s *Service) CreateRate(ctx context.Context, typ domain.UtilityType, perUnit shared.Money, effectiveFrom time.Time, announcement string) (domain.UtilityRate, error) {
	var rate domain.UtilityRate
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		latest, err := s.repo.LatestRate(ctx, typ)
		if err != nil {
			return err
		}
		var ev domain.UtilityRateChanged
		if rate, ev, err = domain.NewUtilityRate(s.newID(), typ, perUnit, effectiveFrom, announcement, latest, s.now()); err != nil {
			return err
		}
		if err := s.repo.InsertRate(ctx, rate); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, ev)
	})
	if err != nil {
		return domain.UtilityRate{}, fmt.Errorf("create %s rate: %w", typ, err)
	}
	return rate, nil
}

// --- invoices ---

// InvoiceView is an invoice with the values computed for display.
type InvoiceView struct {
	Invoice       *domain.Invoice
	DisplayStatus string
	DueDate       time.Time // date
	Electricity   domain.UsageCheck
	Water         domain.UsageCheck
	Slips         []SlipView
}

// CreateDraft creates the room's invoice for period, snapshotting prices at
// today's date in Thai time (INV-4, plan 2.d).
func (s *Service) CreateDraft(ctx context.Context, period shared.BillingPeriod, roomNumber string) (InvoiceView, error) {
	var inv *domain.Invoice
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		info, err := s.tenancy.BillingInfo(ctx, roomNumber, period)
		if err != nil {
			return err
		}
		existing, err := s.repo.InvoiceByRoomPeriod(ctx, info.RoomID, period)
		if err != nil && !errors.Is(err, domain.ErrInvoiceNotFound) {
			return err
		}
		if existing != nil {
			return domain.ErrInvoiceAlreadyExists
		}

		rates, err := s.repo.ListRates(ctx)
		if err != nil {
			return err
		}
		today := shared.DateOf(s.now())
		params := domain.DraftParams{
			ID:               s.newID(),
			RoomID:           info.RoomID,
			RoomNumber:       info.RoomNumber,
			Period:           period,
			HasLeaseInPeriod: info.HasLeaseInPeriod,
			Rent:             info.Rent,
			ElectricDigits:   info.ElectricDigits,
			WaterDigits:      info.WaterDigits,
			Recurring:        info.Recurring,
			Now:              s.now(),
		}
		if r, ok := domain.RateOn(rates, domain.Electricity, today); ok {
			params.ElectricRate = &r.PerUnit
		}
		if r, ok := domain.RateOn(rates, domain.Water, today); ok {
			params.WaterRate = &r.PerUnit
		}
		history, err := s.repo.IssuedReadingsBefore(ctx, info.RoomID, period)
		if err != nil {
			return err
		}
		if len(history) > 0 {
			if e := history[0].Electric; e != nil {
				params.LastElectric = &e.Current
			}
			if w := history[0].Water; w != nil {
				params.LastWater = &w.Current
			}
		}

		if inv, err = domain.NewDraft(params); err != nil {
			return err
		}
		if err := s.repo.InsertInvoice(ctx, inv); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, inv.PullEvents()...)
	})
	if err != nil {
		return InvoiceView{}, fmt.Errorf("create draft %s room %s: %w", period, roomNumber, err)
	}
	return s.viewWithHistory(ctx, inv)
}

func (s *Service) GetInvoice(ctx context.Context, id string) (InvoiceView, error) {
	inv, err := s.repo.Invoice(ctx, id, false)
	if err != nil {
		return InvoiceView{}, fmt.Errorf("get invoice %s: %w", id, err)
	}
	return s.viewWithHistory(ctx, inv)
}

func (s *Service) GetRoomInvoice(ctx context.Context, period shared.BillingPeriod, roomNumber string) (InvoiceView, error) {
	roomID, err := s.tenancy.RoomIDByNumber(ctx, roomNumber)
	if err != nil {
		return InvoiceView{}, fmt.Errorf("get invoice %s room %s: %w", period, roomNumber, err)
	}
	inv, err := s.repo.InvoiceByRoomPeriod(ctx, roomID, period)
	if err != nil {
		return InvoiceView{}, fmt.Errorf("get invoice %s room %s: %w", period, roomNumber, err)
	}
	return s.viewWithHistory(ctx, inv)
}

// MeterUpdate holds the readings to set; nil leaves that meter unchanged.
type MeterUpdate struct {
	Electricity *domain.MeterInput
	Water       *domain.MeterInput
}

func (s *Service) SetMeters(ctx context.Context, id string, u MeterUpdate) (InvoiceView, error) {
	return s.mutate(ctx, id, "set meters", func(inv *domain.Invoice) error {
		if u.Electricity != nil {
			if err := inv.SetMeter(domain.Electricity, *u.Electricity, s.now()); err != nil {
				return err
			}
		}
		if u.Water != nil {
			if err := inv.SetMeter(domain.Water, *u.Water, s.now()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) AddLine(ctx context.Context, id string, typ domain.LineType, description string, amount shared.Money) (InvoiceView, error) {
	return s.mutate(ctx, id, "add line", func(inv *domain.Invoice) error {
		return inv.AddLine(s.newID(), typ, description, amount, s.now())
	})
}

func (s *Service) RemoveLine(ctx context.Context, id, lineID string) (InvoiceView, error) {
	return s.mutate(ctx, id, "remove line", func(inv *domain.Invoice) error {
		return inv.RemoveLine(lineID, s.now())
	})
}

func (s *Service) Issue(ctx context.Context, id string) (InvoiceView, error) {
	return s.mutate(ctx, id, "issue", func(inv *domain.Invoice) error {
		return inv.Issue(s.now())
	})
}

// mutate loads the invoice with a row lock, applies change, saves it and
// dispatches its events in one transaction.
func (s *Service) mutate(ctx context.Context, id, op string, change func(*domain.Invoice) error) (InvoiceView, error) {
	var inv *domain.Invoice
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if inv, err = s.repo.Invoice(ctx, id, true); err != nil {
			return err
		}
		if err := change(inv); err != nil {
			return err
		}
		if err := s.repo.UpdateInvoice(ctx, inv); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, inv.PullEvents()...)
	})
	if err != nil {
		return InvoiceView{}, fmt.Errorf("%s invoice %s: %w", op, id, err)
	}
	return s.viewWithHistory(ctx, inv)
}

// viewWithHistory builds the full invoice view: usage warning, slips with
// signed image URLs and the display status.
func (s *Service) viewWithHistory(ctx context.Context, inv *domain.Invoice) (InvoiceView, error) {
	history, err := s.repo.IssuedReadingsBefore(ctx, inv.RoomID(), inv.Period())
	if err != nil {
		return InvoiceView{}, fmt.Errorf("usage history: %w", err)
	}
	slips, err := s.repo.SlipsByInvoice(ctx, inv.ID())
	if err != nil {
		return InvoiceView{}, fmt.Errorf("slips: %w", err)
	}
	pending := false
	for i := range slips {
		pending = pending || slips[i].Slip.Status == domain.SlipStatusPending
		if err := s.signSlip(ctx, &slips[i]); err != nil {
			return InvoiceView{}, err
		}
	}

	v := InvoiceView{
		Invoice:       inv,
		DisplayStatus: domain.DisplayStatus(inv, pending, s.now()),
		DueDate:       shared.DateOf(inv.Period().DueDate()),
		Slips:         slips,
	}
	v.Electricity = usage(inv.Units(domain.Electricity), history, func(p PastReading) *domain.MeterReading { return p.Electric })
	v.Water = usage(inv.Units(domain.Water), history, func(p PastReading) *domain.MeterReading { return p.Water })
	return v, nil
}

func usage(current *int64, history []PastReading, pick func(PastReading) *domain.MeterReading) domain.UsageCheck {
	if current == nil {
		return domain.UsageCheck{}
	}
	var units []int64
	for _, h := range history {
		if r := pick(h); r != nil {
			units = append(units, r.Units())
		}
	}
	return domain.CheckUsage(*current, units)
}
