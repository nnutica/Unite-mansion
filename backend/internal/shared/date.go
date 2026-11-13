package shared

import "time"

// Calendar dates (lease start/end, rate effective dates) are represented as
// time.Time at 00:00 UTC carrying the Thai calendar day, which matches how
// Postgres DATE values round-trip through pgx.

// DateOf returns the Bangkok calendar day of t as a date value.
func DateOf(t time.Time) time.Time {
	b := t.In(Bangkok)
	return time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
}

// NewDate builds a date value.
func NewDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// FirstDay is the first calendar day of the period month.
func (p BillingPeriod) FirstDay() time.Time {
	return NewDate(p.year, p.month, 1)
}

// LastDay is the last calendar day of the period month.
func (p BillingPeriod) LastDay() time.Time {
	return NewDate(p.year, p.month+1, 0)
}
