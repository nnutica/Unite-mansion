package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func submit(t *testing.T, inv *Invoice) *PaymentSlip {
	t.Helper()
	s, err := SubmitSlip("slip-1", inv, "slips/inv-1/a.png", "hash", "image/png", 100, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSubmitSlipOnlyForIssuedOrPaid(t *testing.T) { // §6.4
	if _, err := SubmitSlip("s", newDraft(t), "k", "h", "image/png", 1, testNow); !errors.Is(err, ErrSlipNotAllowed) {
		t.Fatalf("draft err = %v, want ErrSlipNotAllowed", err)
	}
	s := submit(t, issuedInvoice(t))
	if s.Status != SlipStatusPending || s.PullEvents()[0].EventName() != "SlipSubmitted" {
		t.Fatalf("slip = %+v", s)
	}

	paid := issuedInvoice(t)
	_ = paid.MarkPaid(testNow)
	if _, err := SubmitSlip("s2", paid, "k", "h", "image/png", 1, testNow); err != nil {
		t.Fatalf("slip on PAID invoice rejected: %v", err)
	}
}

func TestApproveMarksInvoicePaid(t *testing.T) { // §6.4, §9
	inv := issuedInvoice(t)
	s := submit(t, inv)
	s.PullEvents()

	if err := s.Approve(inv, testNow); err != nil {
		t.Fatal(err)
	}
	if s.Status != SlipStatusApproved || s.ReviewedAt == nil || inv.Status() != StatusPaid {
		t.Fatalf("slip %s invoice %s", s.Status, inv.Status())
	}
	ev := s.PullEvents()[0].(SlipApproved)
	if ev.InvoiceStatus != StatusPaid || ev.Total.String() != inv.Total().String() {
		t.Fatalf("event = %+v", ev)
	}

	// A second review of the same slip is refused.
	if err := s.Approve(inv, testNow); !errors.Is(err, ErrSlipAlreadyReviewed) {
		t.Fatalf("re-approve err = %v", err)
	}
	if err := s.Reject(RejectUnclear, "", testNow); !errors.Is(err, ErrSlipAlreadyReviewed) {
		t.Fatalf("reject after approve err = %v", err)
	}
}

func TestApproveExtraSlipOnPaidInvoice(t *testing.T) {
	inv := issuedInvoice(t)
	first, second := submit(t, inv), submit(t, inv)
	if err := first.Approve(inv, testNow); err != nil {
		t.Fatal(err)
	}
	// plan 3.c: the other PENDING slip stays PENDING until the admin decides.
	if second.Status != SlipStatusPending {
		t.Fatalf("second slip = %s", second.Status)
	}
	if err := second.Approve(inv, testNow); err != nil {
		t.Fatalf("approve slip of PAID invoice: %v", err)
	}
	if inv.Status() != StatusPaid {
		t.Fatalf("invoice = %s", inv.Status())
	}
}

func TestRejectRequiresReason(t *testing.T) { // §6.4
	inv := issuedInvoice(t)
	s := submit(t, inv)
	if err := s.Reject("WRONG", "", testNow); !errors.Is(err, ErrInvalidRejectReason) {
		t.Fatalf("bad reason err = %v", err)
	}
	if err := s.Reject(RejectAmountMismatch, strings.Repeat("ก", 501), testNow); !errors.Is(err, ErrInvalidRejectReason) {
		t.Fatalf("long note err = %v", err)
	}
	if err := s.Reject(RejectAmountMismatch, " โอนขาด 100 บาท ", testNow); err != nil {
		t.Fatal(err)
	}
	if s.Status != SlipStatusRejected || *s.RejectReason != RejectAmountMismatch || s.RejectNote != "โอนขาด 100 บาท" {
		t.Fatalf("slip = %+v", s)
	}
	if inv.Status() != StatusIssued {
		t.Fatalf("rejecting changed the invoice to %s", inv.Status())
	}
}

func TestDisplayStatus(t *testing.T) { // §6.1 table
	inv := issuedInvoice(t) // period 2026-11, due 2026-12-06
	before := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	after := time.Date(2026, 12, 7, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		pending bool
		now     time.Time
		want    string
	}{
		{"issued", false, before, "ISSUED"},
		{"pending slip", true, before, "PENDING_VERIFICATION"},
		{"overdue", false, after, "OVERDUE"},
		{"pending slip wins over overdue", true, after, "PENDING_VERIFICATION"},
	}
	for _, c := range cases {
		if got := DisplayStatus(inv, c.pending, c.now); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	if got := DisplayStatus(newDraft(t), true, after); got != "DRAFT" {
		t.Errorf("draft = %s", got)
	}
	_ = inv.MarkPaid(before)
	if got := DisplayStatus(inv, true, after); got != "PAID" {
		t.Errorf("paid = %s", got)
	}
}
