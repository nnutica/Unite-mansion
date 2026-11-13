// Package domain holds the tenancy aggregates: Room, Lease and
// RecurringCharge (context.md §1, §6.2, §6.6).
// Imports: standard library and internal/shared only.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"unite-mansion/internal/shared"
)

var (
	ErrRoomNotFound            = errors.New("room not found")
	ErrLeaseNotFound           = errors.New("lease not found")
	ErrRecurringChargeNotFound = errors.New("recurring charge not found")
	ErrInvalidInput            = errors.New("invalid input")
	ErrLeaseOverlap            = errors.New("lease overlaps another lease of this room")
)

const (
	minMeterDigits = 1
	maxMeterDigits = 9
)

type Room struct {
	ID                  string
	Number              string
	Floor               int
	BasePrice           shared.Money
	ElectricMeterDigits *int
	WaterMeterDigits    *int
}

// RoomChanges is a partial update; nil fields are left unchanged.
type RoomChanges struct {
	BasePrice           *shared.Money
	ElectricMeterDigits *int
	WaterMeterDigits    *int
}

// Update changes rent or meter digits. Existing invoices keep their snapshot
// (INV-4), so this only affects drafts created afterwards.
func (r *Room) Update(c RoomChanges) error {
	if c.BasePrice != nil && !c.BasePrice.IsPositive() {
		return fmt.Errorf("%w: base price must be positive", ErrInvalidInput)
	}
	for _, d := range []*int{c.ElectricMeterDigits, c.WaterMeterDigits} {
		if d != nil && (*d < minMeterDigits || *d > maxMeterDigits) {
			return fmt.Errorf("%w: meter digits must be %d–%d", ErrInvalidInput, minMeterDigits, maxMeterDigits)
		}
	}
	if c.BasePrice != nil {
		r.BasePrice = *c.BasePrice
	}
	if c.ElectricMeterDigits != nil {
		r.ElectricMeterDigits = c.ElectricMeterDigits
	}
	if c.WaterMeterDigits != nil {
		r.WaterMeterDigits = c.WaterMeterDigits
	}
	return nil
}

// Lease is a tenancy period of a room; both dates are inclusive.
type Lease struct {
	ID         string
	RoomID     string
	TenantName string
	StartDate  time.Time // date
	EndDate    time.Time // date
}

// NewLease validates a lease (§6.6: end ≥ start). Overlap with the room's
// other leases is checked with CheckNoOverlap and again by the database.
func NewLease(id, roomID, tenantName string, start, end time.Time) (Lease, error) {
	l := Lease{ID: id, RoomID: roomID}
	if err := l.Change(tenantName, start, end); err != nil {
		return Lease{}, err
	}
	return l, nil
}

func (l *Lease) Change(tenantName string, start, end time.Time) error {
	tenantName = strings.TrimSpace(tenantName)
	if n := utf8.RuneCountInString(tenantName); n < 1 || n > 200 {
		return fmt.Errorf("%w: tenant name must be 1–200 characters", ErrInvalidInput)
	}
	start = toDate(start)
	end = toDate(end)
	if end.Before(start) {
		return fmt.Errorf("%w: end date must not be before start date", ErrInvalidInput)
	}
	l.TenantName, l.StartDate, l.EndDate = tenantName, start, end
	return nil
}

// ActiveOn reports whether the lease covers date.
func (l Lease) ActiveOn(date time.Time) bool {
	d := toDate(date)
	return !d.Before(l.StartDate) && !d.After(l.EndDate)
}

// Overlaps reports whether two leases share at least one day.
func (l Lease) Overlaps(o Lease) bool {
	return !l.EndDate.Before(o.StartDate) && !o.EndDate.Before(l.StartDate)
}

// CoversPeriod reports whether the lease overlaps the billing period's month
// (Decision log 2026-11-13, plan 2.a).
func (l Lease) CoversPeriod(p shared.BillingPeriod) bool {
	return !l.EndDate.Before(p.FirstDay()) && !p.LastDay().Before(l.StartDate)
}

// CheckNoOverlap rejects l if it overlaps any other lease in existing.
func CheckNoOverlap(l Lease, existing []Lease) error {
	for _, o := range existing {
		if o.ID != l.ID && l.Overlaps(o) {
			return fmt.Errorf("%w: %s – %s", ErrLeaseOverlap, o.StartDate.Format(time.DateOnly), o.EndDate.Format(time.DateOnly))
		}
	}
	return nil
}

// ActiveLease returns the lease covering date, if any.
func ActiveLease(leases []Lease, date time.Time) *Lease {
	for i := range leases {
		if leases[i].ActiveOn(date) {
			return &leases[i]
		}
	}
	return nil
}

// RecurringCharge is a monthly service charge of a room, e.g. internet.
// Active charges are copied into new invoices (INV-4).
type RecurringCharge struct {
	ID     string
	RoomID string
	Name   string
	Amount shared.Money
	Active bool
}

func NewRecurringCharge(id, roomID, name string, amount shared.Money, active bool) (RecurringCharge, error) {
	c := RecurringCharge{ID: id, RoomID: roomID, Active: active}
	if err := c.Update(&name, &amount, nil); err != nil {
		return RecurringCharge{}, err
	}
	return c, nil
}

func (c *RecurringCharge) Update(name *string, amount *shared.Money, active *bool) error {
	if name != nil {
		n := strings.TrimSpace(*name)
		if l := utf8.RuneCountInString(n); l < 1 || l > 100 {
			return fmt.Errorf("%w: name must be 1–100 characters", ErrInvalidInput)
		}
		c.Name = n
	}
	if amount != nil {
		if !amount.IsPositive() {
			return fmt.Errorf("%w: amount must be positive", ErrInvalidInput)
		}
		c.Amount = *amount
	}
	if active != nil {
		c.Active = *active
	}
	return nil
}

func toDate(t time.Time) time.Time { return shared.NewDate(t.Year(), t.Month(), t.Day()) }
