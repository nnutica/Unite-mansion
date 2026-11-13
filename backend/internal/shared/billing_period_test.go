package shared

import (
	"errors"
	"testing"
	"time"
)

func TestParseBillingPeriod(t *testing.T) {
	p, err := ParseBillingPeriod("2026-11")
	if err != nil {
		t.Fatal(err)
	}
	if p.Year() != 2026 || p.Month() != time.November || p.String() != "2026-11" {
		t.Fatalf("got %v", p)
	}

	for _, bad := range []string{"", "2026-13", "2026-1", "26-11", "2026/11", "2026-11-01"} {
		if _, err := ParseBillingPeriod(bad); !errors.Is(err, ErrInvalidBillingPeriod) {
			t.Errorf("ParseBillingPeriod(%q) err = %v, want ErrInvalidBillingPeriod", bad, err)
		}
	}
}

func TestBillingPeriodNextPrevAcrossYear(t *testing.T) {
	dec, _ := ParseBillingPeriod("2026-12")
	if got := dec.Next().String(); got != "2027-01" {
		t.Fatalf("Next = %s, want 2027-01", got)
	}
	jan, _ := ParseBillingPeriod("2027-01")
	if got := jan.Prev().String(); got != "2026-12" {
		t.Fatalf("Prev = %s, want 2026-12", got)
	}
	if !dec.Before(jan) || jan.Before(dec) {
		t.Fatal("Before ordering wrong")
	}
}

func TestBillingPeriodDates(t *testing.T) {
	p, _ := ParseBillingPeriod("2026-12")
	if got := p.CutoffDate(); !got.Equal(time.Date(2026, 12, 24, 0, 0, 0, 0, Bangkok)) {
		t.Fatalf("CutoffDate = %v", got)
	}
	if got := p.DueDate(); !got.Equal(time.Date(2027, 1, 6, 0, 0, 0, 0, Bangkok)) {
		t.Fatalf("DueDate = %v", got)
	}
}

func TestBillingPeriodIsOverdue(t *testing.T) {
	p, _ := ParseBillingPeriod("2026-11")
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"on due date, last minute (Bangkok)", time.Date(2026, 12, 6, 23, 59, 0, 0, Bangkok), false},
		{"day after due date (Bangkok)", time.Date(2026, 12, 7, 0, 0, 0, 0, Bangkok), true},
		// 2026-12-06 17:30 UTC is 2026-12-07 00:30 in Bangkok.
		{"UTC still the 6th but Bangkok is the 7th", time.Date(2026, 12, 6, 17, 30, 0, 0, time.UTC), true},
		{"before due date", time.Date(2026, 11, 30, 12, 0, 0, 0, Bangkok), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.IsOverdue(tt.now); got != tt.want {
				t.Fatalf("IsOverdue(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestBillingPeriodOfUsesBangkok(t *testing.T) {
	// 2026-11-30 18:00 UTC is already 2026-12-01 in Bangkok.
	if got := BillingPeriodOf(time.Date(2026, 11, 30, 18, 0, 0, 0, time.UTC)).String(); got != "2026-12" {
		t.Fatalf("BillingPeriodOf = %s, want 2026-12", got)
	}
}
