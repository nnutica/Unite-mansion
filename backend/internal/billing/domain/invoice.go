package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"unite-mansion/internal/shared"
)

type Status string

const (
	StatusDraft  Status = "DRAFT"
	StatusIssued Status = "ISSUED"
	StatusPaid   Status = "PAID"
)

type LineType string

const (
	LineRent        LineType = "RENT"
	LineElectricity LineType = "ELECTRICITY"
	LineWater       LineType = "WATER"
	LineRecurring   LineType = "RECURRING"
	LineDiscount    LineType = "DISCOUNT"
	LinePenalty     LineType = "PENALTY"
)

const maxLineDescription = 200

// Line is one row of an invoice. Rent, meter and recurring lines are derived
// from the snapshot and readings; discount and penalty lines are entered by
// the admin and keep their ID. Derived lines have an empty ID.
type Line struct {
	ID          string
	Type        LineType
	Description string
	Amount      shared.Money
}

// RecurringItem is a monthly service charge copied into the invoice (INV-4).
type RecurringItem struct {
	Name   string
	Amount shared.Money
}

// Snapshot holds every price the invoice depends on, frozen when the draft
// is created so later changes to rooms, rates or charges never alter it (INV-4).
type Snapshot struct {
	Rent           shared.Money
	ElectricRate   shared.Money
	WaterRate      shared.Money
	ElectricDigits int
	WaterDigits    int
	Recurring      []RecurringItem
}

// meter holds one utility's readings. Previous may be unknown on a room's
// first invoice (the admin enters it); Current is unknown until entered.
type meter struct {
	previous        *int64
	defaultPrevious *int64 // from the latest issued invoice (INV-8)
	reading         *MeterReading
}

// Invoice is the billing aggregate (context.md §6.1).
type Invoice struct {
	shared.EventRecorder

	id            string
	invoiceNumber string
	roomID        string
	roomNumber    string
	period        shared.BillingPeriod
	status        Status
	snapshot      Snapshot
	electricity   meter
	water         meter
	manualLines   []Line
	issuedAt      *time.Time
	paidAt        *time.Time
	createdAt     time.Time
}

// DraftParams is everything needed to create a draft. The application layer
// gathers it from tenancy and the rate history.
type DraftParams struct {
	ID         string
	RoomID     string
	RoomNumber string
	Period     shared.BillingPeriod
	// HasLeaseInPeriod: a vacant room cannot be billed (§6.1).
	HasLeaseInPeriod bool
	Rent             shared.Money
	ElectricRate     *shared.Money
	WaterRate        *shared.Money
	ElectricDigits   *int
	WaterDigits      *int
	Recurring        []RecurringItem
	// Current readings of the room's latest issued invoice, used as this
	// invoice's previous readings by default (INV-8). Nil on the first invoice.
	LastElectric *int64
	LastWater    *int64
	Now          time.Time
}

// InvoiceNumber formats INV-yyyymm-room.
func InvoiceNumber(period shared.BillingPeriod, roomNumber string) string {
	return fmt.Sprintf("INV-%04d%02d-%s", period.Year(), period.Month(), roomNumber)
}

// NewDraft creates a DRAFT invoice with its price snapshot (INV-4).
func NewDraft(p DraftParams) (*Invoice, error) {
	if !p.HasLeaseInPeriod {
		return nil, ErrRoomHasNoLease
	}
	if p.ElectricDigits == nil || p.WaterDigits == nil {
		return nil, ErrMeterDigitsNotSet
	}
	if p.ElectricRate == nil || p.WaterRate == nil {
		return nil, ErrRateNotFound
	}
	if p.ID == "" || p.RoomID == "" || p.RoomNumber == "" || p.Period.IsZero() || !p.Rent.IsPositive() {
		return nil, fmt.Errorf("%w: incomplete draft parameters", ErrInvalidLine)
	}

	inv := &Invoice{
		id:            p.ID,
		invoiceNumber: InvoiceNumber(p.Period, p.RoomNumber),
		roomID:        p.RoomID,
		roomNumber:    p.RoomNumber,
		period:        p.Period,
		status:        StatusDraft,
		snapshot: Snapshot{
			Rent:           p.Rent,
			ElectricRate:   *p.ElectricRate,
			WaterRate:      *p.WaterRate,
			ElectricDigits: *p.ElectricDigits,
			WaterDigits:    *p.WaterDigits,
			Recurring:      append([]RecurringItem(nil), p.Recurring...),
		},
		createdAt: p.Now.UTC(),
	}
	// A previous reading that no longer fits (meter replaced with fewer
	// digits) is dropped so the admin enters it.
	if p.LastElectric != nil && fitsDigits(*p.LastElectric, *p.ElectricDigits) {
		inv.electricity = meter{previous: ptr(*p.LastElectric), defaultPrevious: ptr(*p.LastElectric)}
	}
	if p.LastWater != nil && fitsDigits(*p.LastWater, *p.WaterDigits) {
		inv.water = meter{previous: ptr(*p.LastWater), defaultPrevious: ptr(*p.LastWater)}
	}

	inv.Record(InvoiceDrafted{
		EventBase:     shared.NewEventBase(inv.id, p.Now),
		InvoiceNumber: inv.invoiceNumber,
		RoomNumber:    inv.roomNumber,
		BillingPeriod: inv.period.String(),
		Total:         inv.Total(),
	})
	return inv, nil
}

// MeterInput is what the admin enters for one meter. Previous overrides the
// default previous reading, e.g. after a meter replacement (INV-8).
type MeterInput struct {
	Previous          *int64
	Current           int64
	RolloverConfirmed bool
}

// SetMeter records the reading of one utility (DRAFT only, INV-3).
func (inv *Invoice) SetMeter(typ UtilityType, in MeterInput, now time.Time) error {
	if err := inv.requireDraft(); err != nil {
		return err
	}
	m, digits, err := inv.meterFor(typ)
	if err != nil {
		return err
	}

	previous := m.previous
	if in.Previous != nil {
		previous = in.Previous
	}
	if previous == nil {
		return fmt.Errorf("%s: %w", typ, ErrPreviousReadingRequired)
	}
	reading, err := NewMeterReading(*previous, in.Current, digits, in.RolloverConfirmed)
	if err != nil {
		return fmt.Errorf("%s: %w", typ, err)
	}

	m.previous = ptr(*previous)
	m.reading = &reading
	overridden := m.defaultPrevious == nil || *m.defaultPrevious != reading.Previous

	inv.Record(InvoiceLineChanged{
		EventBase:     shared.NewEventBase(inv.id, now),
		InvoiceNumber: inv.invoiceNumber,
		Change:        "METER_SET",
		LineType:      lineTypeFor(typ),
		Meter: &MeterChange{
			Previous:           reading.Previous,
			Current:            reading.Current,
			Units:              reading.Units(),
			RolloverConfirmed:  reading.RolloverConfirmed,
			PreviousOverridden: overridden,
			DefaultPrevious:    m.defaultPrevious,
		},
		Total: inv.Total(),
	})
	return nil
}

// AddLine adds a discount (negative) or penalty (positive) line (DRAFT only).
// Discounts only reduce rent: their sum may not exceed the rent (INV-6).
func (inv *Invoice) AddLine(id string, typ LineType, description string, amount shared.Money, now time.Time) error {
	if err := inv.requireDraft(); err != nil {
		return err
	}
	description = strings.TrimSpace(description)
	if id == "" || description == "" || utf8.RuneCountInString(description) > maxLineDescription {
		return fmt.Errorf("%w: description must be 1–%d characters", ErrInvalidLine, maxLineDescription)
	}
	switch typ {
	case LineDiscount:
		if !amount.IsNegative() {
			return fmt.Errorf("%w: discount must be negative", ErrInvalidLine)
		}
		if inv.discountTotal().Add(amount).Neg().Cmp(inv.snapshot.Rent) > 0 {
			return ErrDiscountExceedsRent
		}
	case LinePenalty:
		if !amount.IsPositive() {
			return fmt.Errorf("%w: penalty must be positive", ErrInvalidLine)
		}
	default:
		return fmt.Errorf("%w: only DISCOUNT or PENALTY can be added, got %q", ErrInvalidLine, typ)
	}

	inv.manualLines = append(inv.manualLines, Line{ID: id, Type: typ, Description: description, Amount: amount})
	inv.Record(InvoiceLineChanged{
		EventBase:     shared.NewEventBase(inv.id, now),
		InvoiceNumber: inv.invoiceNumber,
		Change:        "LINE_ADDED",
		LineType:      typ,
		Description:   description,
		Amount:        &amount,
		Total:         inv.Total(),
	})
	return nil
}

// RemoveLine removes a discount or penalty line (DRAFT only).
func (inv *Invoice) RemoveLine(id string, now time.Time) error {
	if err := inv.requireDraft(); err != nil {
		return err
	}
	for i, l := range inv.manualLines {
		if l.ID != id {
			continue
		}
		inv.manualLines = append(inv.manualLines[:i], inv.manualLines[i+1:]...)
		amount := l.Amount
		inv.Record(InvoiceLineChanged{
			EventBase:     shared.NewEventBase(inv.id, now),
			InvoiceNumber: inv.invoiceNumber,
			Change:        "LINE_REMOVED",
			LineType:      l.Type,
			Description:   l.Description,
			Amount:        &amount,
			Total:         inv.Total(),
		})
		return nil
	}
	return ErrLineNotFound
}

// Issue sends the invoice to the tenant. After this it can no longer be
// edited (INV-3); corrections go into next month's invoice.
func (inv *Invoice) Issue(now time.Time) error {
	if inv.status != StatusDraft {
		return fmt.Errorf("%w: %s → %s", ErrInvalidStatusTransition, inv.status, StatusIssued)
	}
	if inv.electricity.reading == nil || inv.water.reading == nil {
		return ErrMetersIncomplete
	}
	at := now.UTC()
	inv.status = StatusIssued
	inv.issuedAt = &at
	inv.Record(InvoiceIssued{
		EventBase:     shared.NewEventBase(inv.id, now),
		InvoiceNumber: inv.invoiceNumber,
		Total:         inv.Total(),
	})
	return nil
}

// MarkPaid moves an issued invoice to PAID; called when a slip is approved.
func (inv *Invoice) MarkPaid(now time.Time) error {
	if inv.status != StatusIssued {
		return fmt.Errorf("%w: %s → %s", ErrInvalidStatusTransition, inv.status, StatusPaid)
	}
	at := now.UTC()
	inv.status = StatusPaid
	inv.paidAt = &at
	return nil
}

// Lines returns rent, meters, recurring charges, then discounts and penalties.
func (inv *Invoice) Lines() []Line {
	lines := []Line{{Type: LineRent, Description: "ค่าห้อง", Amount: inv.snapshot.Rent}}
	if r := inv.electricity.reading; r != nil {
		lines = append(lines, inv.meterLine(LineElectricity, "ค่าไฟ", *r, inv.snapshot.ElectricRate))
	}
	if r := inv.water.reading; r != nil {
		lines = append(lines, inv.meterLine(LineWater, "ค่าน้ำ", *r, inv.snapshot.WaterRate))
	}
	for _, rc := range inv.snapshot.Recurring {
		lines = append(lines, Line{Type: LineRecurring, Description: rc.Name, Amount: rc.Amount})
	}
	return append(lines, inv.manualLines...)
}

// Total is the sum of all lines, always computed here (INV-5).
func (inv *Invoice) Total() shared.Money {
	total := shared.ZeroMoney
	for _, l := range inv.Lines() {
		total = total.Add(l.Amount)
	}
	return total
}

func (inv *Invoice) meterLine(typ LineType, label string, r MeterReading, rate shared.Money) Line {
	return Line{
		Type:        typ,
		Description: fmt.Sprintf("%s %d หน่วย × %s", label, r.Units(), rate),
		Amount:      rate.MulInt(r.Units()),
	}
}

func (inv *Invoice) discountTotal() shared.Money {
	sum := shared.ZeroMoney
	for _, l := range inv.manualLines {
		if l.Type == LineDiscount {
			sum = sum.Add(l.Amount)
		}
	}
	return sum
}

func (inv *Invoice) requireDraft() error {
	if inv.status != StatusDraft {
		return fmt.Errorf("%w: status is %s", ErrInvoiceNotDraft, inv.status)
	}
	return nil
}

func (inv *Invoice) meterFor(typ UtilityType) (*meter, int, error) {
	switch typ {
	case Electricity:
		return &inv.electricity, inv.snapshot.ElectricDigits, nil
	case Water:
		return &inv.water, inv.snapshot.WaterDigits, nil
	}
	return nil, 0, fmt.Errorf("%w: utility type %q", ErrInvalidLine, typ)
}

func lineTypeFor(typ UtilityType) LineType {
	if typ == Water {
		return LineWater
	}
	return LineElectricity
}

func ptr[T any](v T) *T { return &v }

// Accessors.

func (inv *Invoice) ID() string                   { return inv.id }
func (inv *Invoice) InvoiceNumber() string        { return inv.invoiceNumber }
func (inv *Invoice) RoomID() string               { return inv.roomID }
func (inv *Invoice) RoomNumber() string           { return inv.roomNumber }
func (inv *Invoice) Period() shared.BillingPeriod { return inv.period }
func (inv *Invoice) Status() Status               { return inv.status }
func (inv *Invoice) Snapshot() Snapshot           { return inv.snapshot }
func (inv *Invoice) IssuedAt() *time.Time         { return inv.issuedAt }
func (inv *Invoice) PaidAt() *time.Time           { return inv.paidAt }
func (inv *Invoice) CreatedAt() time.Time         { return inv.createdAt }
func (inv *Invoice) ManualLines() []Line          { return append([]Line(nil), inv.manualLines...) }

// MeterState is a read view of one meter for persistence and responses.
type MeterState struct {
	Previous          *int64
	Current           *int64
	RolloverConfirmed bool
	Digits            int
}

func (inv *Invoice) Meter(typ UtilityType) MeterState {
	m, digits, err := inv.meterFor(typ)
	if err != nil {
		return MeterState{}
	}
	s := MeterState{Digits: digits}
	if m.previous != nil {
		s.Previous = ptr(*m.previous)
	}
	if m.reading != nil {
		s.Current = ptr(m.reading.Current)
		s.RolloverConfirmed = m.reading.RolloverConfirmed
	}
	return s
}

// Units returns the units used for typ, or nil if not entered yet.
func (inv *Invoice) Units(typ UtilityType) *int64 {
	m, _, err := inv.meterFor(typ)
	if err != nil || m.reading == nil {
		return nil
	}
	return ptr(m.reading.Units())
}

// InvoiceState is the persisted form used to rehydrate an Invoice.
type InvoiceState struct {
	ID            string
	InvoiceNumber string
	RoomID        string
	RoomNumber    string
	Period        shared.BillingPeriod
	Status        Status
	Snapshot      Snapshot
	Electric      MeterState
	Water         MeterState
	// DefaultElectric/DefaultWater are the latest issued readings (INV-8),
	// recomputed on load; nil on a room's first invoice.
	DefaultElectric *int64
	DefaultWater    *int64
	ManualLines     []Line
	IssuedAt        *time.Time
	PaidAt          *time.Time
	CreatedAt       time.Time
}

// Rehydrate rebuilds an Invoice from storage without raising events.
func Rehydrate(s InvoiceState) *Invoice {
	inv := &Invoice{
		id:            s.ID,
		invoiceNumber: s.InvoiceNumber,
		roomID:        s.RoomID,
		roomNumber:    s.RoomNumber,
		period:        s.Period,
		status:        s.Status,
		snapshot:      s.Snapshot,
		manualLines:   append([]Line(nil), s.ManualLines...),
		issuedAt:      s.IssuedAt,
		paidAt:        s.PaidAt,
		createdAt:     s.CreatedAt,
	}
	inv.electricity = rehydrateMeter(s.Electric, s.DefaultElectric)
	inv.water = rehydrateMeter(s.Water, s.DefaultWater)
	return inv
}

func rehydrateMeter(s MeterState, defaultPrevious *int64) meter {
	m := meter{previous: s.Previous, defaultPrevious: defaultPrevious}
	if s.Previous != nil && s.Current != nil {
		m.reading = &MeterReading{Previous: *s.Previous, Current: *s.Current, Digits: s.Digits, RolloverConfirmed: s.RolloverConfirmed}
	}
	return m
}
