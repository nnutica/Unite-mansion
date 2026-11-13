package domain

import (
	"fmt"
	"strings"
	"time"

	"unite-mansion/internal/shared"
)

type UtilityType string

const (
	Electricity UtilityType = "ELECTRICITY"
	Water       UtilityType = "WATER"
)

func (t UtilityType) Valid() bool { return t == Electricity || t == Water }

// UtilityRate is an append-only price per unit with an effective date (§6.3).
type UtilityRate struct {
	ID            string
	Type          UtilityType
	PerUnit       shared.Money
	EffectiveFrom time.Time // date
	CreatedAt     time.Time
}

// NewUtilityRate validates a new rate against the latest rate of the same
// type (nil if none). History stays linear: a new rate must start later.
func NewUtilityRate(id string, typ UtilityType, perUnit shared.Money, effectiveFrom time.Time, announcement string, latest *UtilityRate, now time.Time) (UtilityRate, UtilityRateChanged, error) {
	if !typ.Valid() {
		return UtilityRate{}, UtilityRateChanged{}, fmt.Errorf("%w: type %q", ErrInvalidRate, typ)
	}
	if !perUnit.IsPositive() {
		return UtilityRate{}, UtilityRateChanged{}, fmt.Errorf("%w: per unit must be positive", ErrInvalidRate)
	}
	effectiveFrom = shared.NewDate(effectiveFrom.Year(), effectiveFrom.Month(), effectiveFrom.Day())
	if latest != nil && !effectiveFrom.After(latest.EffectiveFrom) {
		return UtilityRate{}, UtilityRateChanged{}, fmt.Errorf("%w: %s is not after %s",
			ErrRateNotAfterLatest, effectiveFrom.Format(time.DateOnly), latest.EffectiveFrom.Format(time.DateOnly))
	}

	rate := UtilityRate{ID: id, Type: typ, PerUnit: perUnit, EffectiveFrom: effectiveFrom, CreatedAt: now.UTC()}
	event := UtilityRateChanged{
		EventBase:     shared.NewEventBase(id, now),
		UtilityType:   typ,
		PerUnit:       perUnit,
		EffectiveFrom: effectiveFrom.Format(time.DateOnly),
		Announcement:  strings.TrimSpace(announcement),
	}
	if latest != nil {
		prev := latest.PerUnit
		event.PreviousPerUnit = &prev
	}
	return rate, event, nil
}

// RateOn returns the rate of typ in effect on date: the one with the latest
// EffectiveFrom that is not after date.
func RateOn(rates []UtilityRate, typ UtilityType, date time.Time) (UtilityRate, bool) {
	var best UtilityRate
	found := false
	for _, r := range rates {
		if r.Type != typ || r.EffectiveFrom.After(date) {
			continue
		}
		if !found || r.EffectiveFrom.After(best.EffectiveFrom) {
			best, found = r, true
		}
	}
	return best, found
}
