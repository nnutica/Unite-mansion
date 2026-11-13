package shared

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidBillingPeriod is returned for strings that are not yyyy-mm.
var ErrInvalidBillingPeriod = errors.New("invalid billing period")

// Bangkok is Thailand time (UTC+7, no DST). Business dates such as the cutoff
// and due date are evaluated in this zone; timestamps are stored in UTC.
var Bangkok = time.FixedZone("Asia/Bangkok", 7*60*60)

const (
	cutoffDay = 24 // bill is cut on the 24th of the period month
	dueDay    = 6  // payment is due by the 6th of the following month
)

// BillingPeriod is a billing month, written as yyyy-mm.
type BillingPeriod struct {
	year  int
	month time.Month
}

func NewBillingPeriod(year int, month time.Month) (BillingPeriod, error) {
	if year < 2000 || year > 9999 || month < time.January || month > time.December {
		return BillingPeriod{}, fmt.Errorf("%w: %04d-%02d", ErrInvalidBillingPeriod, year, month)
	}
	return BillingPeriod{year: year, month: month}, nil
}

// ParseBillingPeriod parses "yyyy-mm".
func ParseBillingPeriod(s string) (BillingPeriod, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil || len(s) != len("2006-01") {
		return BillingPeriod{}, fmt.Errorf("%w: %q", ErrInvalidBillingPeriod, s)
	}
	return NewBillingPeriod(t.Year(), t.Month())
}

// BillingPeriodOf returns the period whose month contains t in Bangkok time.
func BillingPeriodOf(t time.Time) BillingPeriod {
	b := t.In(Bangkok)
	return BillingPeriod{year: b.Year(), month: b.Month()}
}

func (p BillingPeriod) Year() int                  { return p.year }
func (p BillingPeriod) Month() time.Month          { return p.month }
func (p BillingPeriod) IsZero() bool               { return p.year == 0 }
func (p BillingPeriod) String() string             { return fmt.Sprintf("%04d-%02d", p.year, p.month) }
func (p BillingPeriod) Equal(o BillingPeriod) bool { return p == o }

func (p BillingPeriod) Before(o BillingPeriod) bool {
	return p.year < o.year || (p.year == o.year && p.month < o.month)
}

func (p BillingPeriod) Next() BillingPeriod {
	t := time.Date(p.year, p.month+1, 1, 0, 0, 0, 0, Bangkok)
	return BillingPeriod{year: t.Year(), month: t.Month()}
}

func (p BillingPeriod) Prev() BillingPeriod {
	t := time.Date(p.year, p.month-1, 1, 0, 0, 0, 0, Bangkok)
	return BillingPeriod{year: t.Year(), month: t.Month()}
}

// CutoffDate is 00:00 Bangkok on the 24th of the period month.
func (p BillingPeriod) CutoffDate() time.Time {
	return time.Date(p.year, p.month, cutoffDay, 0, 0, 0, 0, Bangkok)
}

// DueDate is 00:00 Bangkok on the 6th of the following month.
func (p BillingPeriod) DueDate() time.Time {
	return time.Date(p.year, p.month+1, dueDay, 0, 0, 0, 0, Bangkok)
}

// IsOverdue reports whether now is past the whole due date (from the 7th, Bangkok time).
func (p BillingPeriod) IsOverdue(now time.Time) bool {
	return !now.Before(p.DueDate().AddDate(0, 0, 1))
}

func (p BillingPeriod) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

func (p *BillingPeriod) UnmarshalText(b []byte) error {
	parsed, err := ParseBillingPeriod(string(b))
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
