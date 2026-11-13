package domain

import "fmt"

const (
	MinMeterDigits = 1
	MaxMeterDigits = 9
)

// MeterReading is one meter's previous and current value for an invoice.
type MeterReading struct {
	Previous          int64
	Current           int64
	Digits            int
	RolloverConfirmed bool
}

// NewMeterReading validates a reading. When the current value is lower than
// the previous one the meter wrapped around, which must be confirmed (INV-7).
func NewMeterReading(previous, current int64, digits int, rolloverConfirmed bool) (MeterReading, error) {
	if digits < MinMeterDigits || digits > MaxMeterDigits {
		return MeterReading{}, fmt.Errorf("%w: digits %d", ErrMeterReadingOutOfRange, digits)
	}
	limit := pow10(digits)
	if previous < 0 || previous >= limit || current < 0 || current >= limit {
		return MeterReading{}, fmt.Errorf("%w: %d → %d with %d digits", ErrMeterReadingOutOfRange, previous, current, digits)
	}
	if current < previous && !rolloverConfirmed {
		return MeterReading{}, fmt.Errorf("%w: %d → %d", ErrMeterRolloverNotConfirmed, previous, current)
	}
	return MeterReading{
		Previous:          previous,
		Current:           current,
		Digits:            digits,
		RolloverConfirmed: rolloverConfirmed && current < previous,
	}, nil
}

// Units = (current − previous + 10^digits) mod 10^digits (INV-7).
func (m MeterReading) Units() int64 {
	limit := pow10(m.Digits)
	return (m.Current - m.Previous + limit) % limit
}

// fitsDigits reports whether v is a valid reading for a meter with digits.
func fitsDigits(v int64, digits int) bool {
	return v >= 0 && v < pow10(digits)
}

func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
