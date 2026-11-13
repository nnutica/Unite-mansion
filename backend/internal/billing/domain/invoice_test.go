package domain

import (
	"errors"
	"testing"
	"time"

	"unite-mansion/internal/shared"
)

var testNow = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

func intp(v int) *int                 { return &v }
func i64p(v int64) *int64             { return &v }
func mp(m shared.Money) *shared.Money { return &m }

func draftParams(t *testing.T) DraftParams {
	period, _ := shared.ParseBillingPeriod("2026-11")
	return DraftParams{
		ID:               "inv-1",
		RoomID:           "room-302",
		RoomNumber:       "302",
		Period:           period,
		HasLeaseInPeriod: true,
		Rent:             money(t, "4200.00"),
		ElectricRate:     mp(money(t, "6.00")),
		WaterRate:        mp(money(t, "20.00")),
		ElectricDigits:   intp(5),
		WaterDigits:      intp(4),
		Recurring:        []RecurringItem{{Name: "ค่าอินเทอร์เน็ต", Amount: money(t, "300.00")}},
		LastElectric:     i64p(1200),
		LastWater:        i64p(300),
		Now:              testNow,
	}
}

func newDraft(t *testing.T) *Invoice {
	t.Helper()
	inv, err := NewDraft(draftParams(t))
	if err != nil {
		t.Fatal(err)
	}
	inv.PullEvents()
	return inv
}

// issuedInvoice returns an ISSUED invoice: 150 kWh, 10 m³.
func issuedInvoice(t *testing.T) *Invoice {
	t.Helper()
	inv := newDraft(t)
	if err := inv.SetMeter(Electricity, MeterInput{Current: 1350}, testNow); err != nil {
		t.Fatal(err)
	}
	if err := inv.SetMeter(Water, MeterInput{Current: 310}, testNow); err != nil {
		t.Fatal(err)
	}
	if err := inv.Issue(testNow); err != nil {
		t.Fatal(err)
	}
	inv.PullEvents()
	return inv
}

func TestNewDraftPreconditions(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DraftParams)
		wantErr error
	}{
		{"vacant room cannot be billed", func(p *DraftParams) { p.HasLeaseInPeriod = false }, ErrRoomHasNoLease},
		{"electric digits not set", func(p *DraftParams) { p.ElectricDigits = nil }, ErrMeterDigitsNotSet},
		{"water digits not set", func(p *DraftParams) { p.WaterDigits = nil }, ErrMeterDigitsNotSet},
		{"no electric rate", func(p *DraftParams) { p.ElectricRate = nil }, ErrRateNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := draftParams(t)
			tt.mutate(&p)
			if _, err := NewDraft(p); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewDraftSnapshotsPrices(t *testing.T) { // INV-4
	p := draftParams(t)
	inv, err := NewDraft(p)
	if err != nil {
		t.Fatal(err)
	}

	// Changing the caller's data afterwards must not change the invoice.
	p.Recurring[0].Amount = money(t, "999.00")
	*p.ElectricRate = money(t, "99.00")

	s := inv.Snapshot()
	if s.Rent.String() != "4200.00" || s.ElectricRate.String() != "6.00" || s.WaterRate.String() != "20.00" ||
		s.ElectricDigits != 5 || s.WaterDigits != 4 || len(s.Recurring) != 1 || s.Recurring[0].Amount.String() != "300.00" {
		t.Fatalf("snapshot = %+v", s)
	}
	if inv.Status() != StatusDraft || inv.InvoiceNumber() != "INV-202611-302" {
		t.Fatalf("status %s number %s", inv.Status(), inv.InvoiceNumber())
	}
	// Rent + recurring until meters are entered.
	if got := inv.Total().String(); got != "4500.00" {
		t.Fatalf("Total = %s, want 4500.00", got)
	}
	events := inv.PullEvents()
	if len(events) != 1 || events[0].EventName() != "InvoiceDrafted" {
		t.Fatalf("events = %v", events)
	}
}

func TestDefaultPreviousReadingComesFromLastIssued(t *testing.T) { // INV-8
	inv := newDraft(t)
	if m := inv.Meter(Electricity); m.Previous == nil || *m.Previous != 1200 {
		t.Fatalf("electric previous = %v, want 1200", m.Previous)
	}

	// First invoice of a room: no previous, admin must enter it.
	p := draftParams(t)
	p.LastElectric, p.LastWater = nil, nil
	first, err := NewDraft(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetMeter(Electricity, MeterInput{Current: 50}, testNow); !errors.Is(err, ErrPreviousReadingRequired) {
		t.Fatalf("err = %v, want ErrPreviousReadingRequired", err)
	}
	if err := first.SetMeter(Electricity, MeterInput{Previous: i64p(0), Current: 50}, testNow); err != nil {
		t.Fatal(err)
	}

	// Last reading that does not fit a smaller replacement meter is dropped.
	p = draftParams(t)
	p.LastWater = i64p(12345) // water meter has 4 digits
	inv2, _ := NewDraft(p)
	if m := inv2.Meter(Water); m.Previous != nil {
		t.Fatalf("water previous = %v, want nil", *m.Previous)
	}
}

func TestOverridePreviousReadingIsAudited(t *testing.T) { // INV-8
	inv := newDraft(t)
	if err := inv.SetMeter(Electricity, MeterInput{Previous: i64p(0), Current: 40}, testNow); err != nil {
		t.Fatal(err)
	}
	events := inv.PullEvents()
	ev, ok := events[0].(InvoiceLineChanged)
	if !ok || ev.Meter == nil || !ev.Meter.PreviousOverridden || ev.Meter.DefaultPrevious == nil || *ev.Meter.DefaultPrevious != 1200 {
		t.Fatalf("event = %+v", events[0])
	}
	if u := inv.Units(Electricity); u == nil || *u != 40 {
		t.Fatalf("units = %v, want 40", u)
	}

	// Using the default is not an override.
	inv = newDraft(t)
	_ = inv.SetMeter(Electricity, MeterInput{Current: 1300}, testNow)
	if ev := inv.PullEvents()[0].(InvoiceLineChanged); ev.Meter.PreviousOverridden {
		t.Fatal("default previous reported as overridden")
	}
}

func TestTotalIsComputedFromLines(t *testing.T) { // INV-5
	inv := newDraft(t)
	_ = inv.SetMeter(Electricity, MeterInput{Current: 1350}, testNow) // 150 × 6 = 900
	_ = inv.SetMeter(Water, MeterInput{Current: 310}, testNow)        // 10 × 20 = 200
	if err := inv.AddLine("l1", LineDiscount, "ส่วนลดต่อสัญญา", money(t, "-200.00"), testNow); err != nil {
		t.Fatal(err)
	}
	if err := inv.AddLine("l2", LinePenalty, "ค่าปรับจ่ายช้า", money(t, "100.00"), testNow); err != nil {
		t.Fatal(err)
	}

	// 4200 + 900 + 200 + 300 − 200 + 100
	if got := inv.Total().String(); got != "5500.00" {
		t.Fatalf("Total = %s, want 5500.00", got)
	}
	types := []LineType{}
	for _, l := range inv.Lines() {
		types = append(types, l.Type)
	}
	want := []LineType{LineRent, LineElectricity, LineWater, LineRecurring, LineDiscount, LinePenalty}
	if len(types) != len(want) {
		t.Fatalf("line types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("line types = %v, want %v", types, want)
		}
	}
}

func TestRolloverMustBeConfirmed(t *testing.T) { // INV-7
	p := draftParams(t)
	p.LastElectric = i64p(99998)
	inv, _ := NewDraft(p)

	if err := inv.SetMeter(Electricity, MeterInput{Current: 10}, testNow); !errors.Is(err, ErrMeterRolloverNotConfirmed) {
		t.Fatalf("err = %v, want ErrMeterRolloverNotConfirmed", err)
	}
	if err := inv.SetMeter(Electricity, MeterInput{Current: 10, RolloverConfirmed: true}, testNow); err != nil {
		t.Fatal(err)
	}
	if u := inv.Units(Electricity); *u != 12 {
		t.Fatalf("units = %d, want 12", *u)
	}
	if m := inv.Meter(Electricity); !m.RolloverConfirmed {
		t.Fatal("rollover not stored")
	}
}

func TestDiscountRules(t *testing.T) { // INV-6
	inv := newDraft(t)

	if err := inv.AddLine("d0", LineDiscount, "ผิดเครื่องหมาย", money(t, "100.00"), testNow); !errors.Is(err, ErrInvalidLine) {
		t.Fatalf("positive discount err = %v, want ErrInvalidLine", err)
	}
	if err := inv.AddLine("p0", LinePenalty, "ผิดเครื่องหมาย", money(t, "-100.00"), testNow); !errors.Is(err, ErrInvalidLine) {
		t.Fatalf("negative penalty err = %v, want ErrInvalidLine", err)
	}
	if err := inv.AddLine("x", LineRent, "ห้ามเพิ่มเอง", money(t, "1.00"), testNow); !errors.Is(err, ErrInvalidLine) {
		t.Fatalf("RENT line err = %v, want ErrInvalidLine", err)
	}
	if err := inv.AddLine("d1", LineDiscount, "  ", money(t, "-1.00"), testNow); !errors.Is(err, ErrInvalidLine) {
		t.Fatalf("blank description err = %v, want ErrInvalidLine", err)
	}

	// Rent is 4200: discounts may total exactly the rent, not more.
	if err := inv.AddLine("d2", LineDiscount, "ส่วนลด 1", money(t, "-4000.00"), testNow); err != nil {
		t.Fatal(err)
	}
	if err := inv.AddLine("d3", LineDiscount, "ส่วนลด 2", money(t, "-200.01"), testNow); !errors.Is(err, ErrDiscountExceedsRent) {
		t.Fatalf("err = %v, want ErrDiscountExceedsRent", err)
	}
	if err := inv.AddLine("d4", LineDiscount, "ส่วนลด 2", money(t, "-200.00"), testNow); err != nil {
		t.Fatalf("discount equal to rent rejected: %v", err)
	}
}

func TestRemoveLine(t *testing.T) {
	inv := newDraft(t)
	_ = inv.AddLine("d1", LineDiscount, "ส่วนลด", money(t, "-100.00"), testNow)
	if err := inv.RemoveLine("nope", testNow); !errors.Is(err, ErrLineNotFound) {
		t.Fatalf("err = %v, want ErrLineNotFound", err)
	}
	if err := inv.RemoveLine("d1", testNow); err != nil {
		t.Fatal(err)
	}
	if got := inv.Total().String(); got != "4500.00" {
		t.Fatalf("Total = %s, want 4500.00", got)
	}
}

func TestIssueRequiresBothMeters(t *testing.T) {
	inv := newDraft(t)
	_ = inv.SetMeter(Electricity, MeterInput{Current: 1350}, testNow)
	if err := inv.Issue(testNow); !errors.Is(err, ErrMetersIncomplete) {
		t.Fatalf("err = %v, want ErrMetersIncomplete", err)
	}
}

func TestIssuedInvoiceCannotBeEdited(t *testing.T) { // INV-3
	inv := issuedInvoice(t)
	if inv.Status() != StatusIssued || inv.IssuedAt() == nil {
		t.Fatalf("status %s issuedAt %v", inv.Status(), inv.IssuedAt())
	}
	total := inv.Total()

	if err := inv.SetMeter(Electricity, MeterInput{Current: 2000}, testNow); !errors.Is(err, ErrInvoiceNotDraft) {
		t.Errorf("SetMeter err = %v, want ErrInvoiceNotDraft", err)
	}
	if err := inv.AddLine("d", LineDiscount, "ส่วนลด", money(t, "-1.00"), testNow); !errors.Is(err, ErrInvoiceNotDraft) {
		t.Errorf("AddLine err = %v, want ErrInvoiceNotDraft", err)
	}
	if err := inv.RemoveLine("d", testNow); !errors.Is(err, ErrInvoiceNotDraft) {
		t.Errorf("RemoveLine err = %v, want ErrInvoiceNotDraft", err)
	}
	if !inv.Total().Equal(total) {
		t.Errorf("total changed after rejected edits")
	}
	if len(inv.PullEvents()) != 0 {
		t.Error("rejected edits raised events")
	}
}

func TestStatusOnlyMovesForward(t *testing.T) { // INV-2
	draft := newDraft(t)
	if err := draft.MarkPaid(testNow); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("DRAFT → PAID err = %v, want ErrInvalidStatusTransition", err)
	}

	inv := issuedInvoice(t)
	if err := inv.Issue(testNow); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("ISSUED → ISSUED err = %v", err)
	}
	if err := inv.MarkPaid(testNow); err != nil {
		t.Fatal(err)
	}
	if inv.Status() != StatusPaid || inv.PaidAt() == nil {
		t.Fatalf("status %s paidAt %v", inv.Status(), inv.PaidAt())
	}
	if err := inv.MarkPaid(testNow); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("PAID → PAID err = %v", err)
	}
	if err := inv.Issue(testNow); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("PAID → ISSUED err = %v", err)
	}
	if err := inv.AddLine("d", LineDiscount, "x", money(t, "-1.00"), testNow); !errors.Is(err, ErrInvoiceNotDraft) {
		t.Fatalf("edit after PAID err = %v", err)
	}
}

func TestRehydrateRoundTrip(t *testing.T) {
	inv := issuedInvoice(t)
	state := InvoiceState{
		ID: inv.ID(), InvoiceNumber: inv.InvoiceNumber(), RoomID: inv.RoomID(), RoomNumber: inv.RoomNumber(),
		Period: inv.Period(), Status: inv.Status(), Snapshot: inv.Snapshot(),
		Electric: inv.Meter(Electricity), Water: inv.Meter(Water),
		ManualLines: inv.ManualLines(), IssuedAt: inv.IssuedAt(), CreatedAt: inv.CreatedAt(),
	}
	back := Rehydrate(state)
	if !back.Total().Equal(inv.Total()) || back.Status() != StatusIssued {
		t.Fatalf("rehydrated total %s status %s, want %s %s", back.Total(), back.Status(), inv.Total(), inv.Status())
	}
	if err := back.AddLine("d", LineDiscount, "x", money(t, "-1.00"), testNow); !errors.Is(err, ErrInvoiceNotDraft) {
		t.Fatalf("rehydrated ISSUED invoice is editable: %v", err)
	}
	if len(back.PullEvents()) != 0 {
		t.Fatal("rehydrate raised events")
	}
}
