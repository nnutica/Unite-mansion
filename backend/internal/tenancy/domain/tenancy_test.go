package domain

import (
	"errors"
	"testing"
	"time"

	"unite-mansion/internal/shared"
)

var d = shared.NewDate

func mustLease(t *testing.T, id string, start, end [3]int) Lease {
	t.Helper()
	l, err := NewLease(id, "room-1", "ผู้เช่า", d(start[0], timeMonth(start[1]), start[2]), d(end[0], timeMonth(end[1]), end[2]))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewLeaseValidation(t *testing.T) {
	if _, err := NewLease("l", "r", "ผู้เช่า", d(2026, 5, 2), d(2026, 5, 1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("end before start err = %v", err)
	}
	if _, err := NewLease("l", "r", " ", d(2026, 5, 1), d(2026, 5, 1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank name err = %v", err)
	}
	if _, err := NewLease("l", "r", "ผู้เช่า", d(2026, 5, 1), d(2026, 5, 1)); err != nil {
		t.Fatalf("one-day lease rejected: %v", err)
	}
}

func TestLeaseOverlap(t *testing.T) { // §6.6
	existing := []Lease{mustLease(t, "a", [3]int{2026, 1, 1}, [3]int{2026, 6, 30})}

	cases := []struct {
		name       string
		start, end [3]int
		overlap    bool
	}{
		{"starts the day after", [3]int{2026, 7, 1}, [3]int{2026, 12, 31}, false},
		{"ends the day before", [3]int{2025, 1, 1}, [3]int{2025, 12, 31}, false},
		{"shares the last day", [3]int{2026, 6, 30}, [3]int{2026, 12, 31}, true},
		{"inside", [3]int{2026, 3, 1}, [3]int{2026, 3, 31}, true},
		{"covers", [3]int{2025, 1, 1}, [3]int{2027, 1, 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckNoOverlap(mustLease(t, "b", c.start, c.end), existing)
			if c.overlap != errors.Is(err, ErrLeaseOverlap) {
				t.Fatalf("err = %v, overlap want %v", err, c.overlap)
			}
		})
	}

	// Editing a lease does not conflict with itself.
	self := existing[0]
	if err := CheckNoOverlap(self, existing); err != nil {
		t.Fatalf("self overlap: %v", err)
	}
}

func TestLeaseCoversPeriod(t *testing.T) { // plan 2.a
	l := mustLease(t, "a", [3]int{2026, 3, 15}, [3]int{2026, 11, 3})
	tests := []struct {
		period string
		want   bool
	}{
		{"2026-02", false},
		{"2026-03", true}, // starts mid-month
		{"2026-11", true}, // ends early in the month
		{"2026-12", false},
	}
	for _, tt := range tests {
		p, _ := shared.ParseBillingPeriod(tt.period)
		if got := l.CoversPeriod(p); got != tt.want {
			t.Errorf("CoversPeriod(%s) = %v, want %v", tt.period, got, tt.want)
		}
	}
}

func TestActiveLease(t *testing.T) {
	leases := []Lease{
		mustLease(t, "old", [3]int{2025, 1, 1}, [3]int{2025, 12, 31}),
		mustLease(t, "cur", [3]int{2026, 1, 1}, [3]int{2026, 12, 31}),
	}
	if l := ActiveLease(leases, d(2026, 12, 31)); l == nil || l.ID != "cur" {
		t.Fatalf("ActiveLease on last day = %v", l)
	}
	if l := ActiveLease(leases, d(2027, 1, 1)); l != nil {
		t.Fatalf("ActiveLease after end = %v", l.ID)
	}
}

func TestRoomUpdate(t *testing.T) {
	r := Room{BasePrice: shared.MoneyFromBaht(4200)}
	zero := shared.ZeroMoney
	if err := r.Update(RoomChanges{BasePrice: &zero}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero price err = %v", err)
	}
	ten := 10
	if err := r.Update(RoomChanges{WaterMeterDigits: &ten}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("10 digits err = %v", err)
	}
	price, five, four := shared.MoneyFromBaht(4500), 5, 4
	if err := r.Update(RoomChanges{BasePrice: &price, ElectricMeterDigits: &five, WaterMeterDigits: &four}); err != nil {
		t.Fatal(err)
	}
	if r.BasePrice.String() != "4500.00" || *r.ElectricMeterDigits != 5 || *r.WaterMeterDigits != 4 {
		t.Fatalf("room = %+v", r)
	}
}

func TestRecurringCharge(t *testing.T) {
	if _, err := NewRecurringCharge("c", "r", "ค่าเน็ต", shared.ZeroMoney, true); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero amount err = %v", err)
	}
	c, err := NewRecurringCharge("c", "r", " ค่าเน็ต ", shared.MoneyFromBaht(300), true)
	if err != nil || c.Name != "ค่าเน็ต" {
		t.Fatalf("charge = %+v err %v", c, err)
	}
	off := false
	if err := c.Update(nil, nil, &off); err != nil || c.Active {
		t.Fatalf("switch off: %+v %v", c, err)
	}
}

func timeMonth(m int) time.Month { return time.Month(m) }
