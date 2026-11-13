package shared

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

// ErrInvalidMoney is returned when a value is not a decimal with at most 2 places.
var ErrInvalidMoney = errors.New("invalid money amount")

// Money is an amount in baht with at most 2 decimal places. It is never a
// float: in JSON it is a string such as "4512.00" and in the DB NUMERIC(12,2).
type Money struct {
	d decimal.Decimal
}

// ZeroMoney is 0.00 baht.
var ZeroMoney = Money{}

// ParseMoney parses a decimal string such as "4512", "4512.5" or "-300.00".
func ParseMoney(s string) (Money, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q", ErrInvalidMoney, s)
	}
	return MoneyFromDecimal(d)
}

// MoneyFromDecimal wraps a decimal, rejecting more than 2 decimal places.
func MoneyFromDecimal(d decimal.Decimal) (Money, error) {
	if !d.Equal(d.Round(2)) {
		return Money{}, fmt.Errorf("%w: %s has more than 2 decimal places", ErrInvalidMoney, d)
	}
	return Money{d: d}, nil
}

// MoneyFromBaht builds a whole-baht amount.
func MoneyFromBaht(baht int64) Money {
	return Money{d: decimal.NewFromInt(baht)}
}

func (m Money) Add(o Money) Money    { return Money{d: m.d.Add(o.d)} }
func (m Money) Sub(o Money) Money    { return Money{d: m.d.Sub(o.d)} }
func (m Money) Neg() Money           { return Money{d: m.d.Neg()} }
func (m Money) MulInt(n int64) Money { return Money{d: m.d.Mul(decimal.NewFromInt(n))} }

func (m Money) Cmp(o Money) int          { return m.d.Cmp(o.d) }
func (m Money) Equal(o Money) bool       { return m.d.Equal(o.d) }
func (m Money) IsZero() bool             { return m.d.IsZero() }
func (m Money) IsNegative() bool         { return m.d.IsNegative() }
func (m Money) IsPositive() bool         { return m.d.IsPositive() }
func (m Money) Decimal() decimal.Decimal { return m.d }

// String formats with exactly 2 decimal places, e.g. "4512.00".
func (m Money) String() string { return m.d.StringFixed(2) }

func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

func (m *Money) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("%w: must be a JSON string", ErrInvalidMoney)
	}
	parsed, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
