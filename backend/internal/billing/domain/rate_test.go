package domain

import (
	"errors"
	"testing"
	"time"

	"unite-mansion/internal/shared"
)

func money(t *testing.T, s string) shared.Money {
	t.Helper()
	m, err := shared.ParseMoney(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNewUtilityRate(t *testing.T) {
	now := time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)
	latest := &UtilityRate{Type: Electricity, PerUnit: money(t, "6.00"), EffectiveFrom: shared.NewDate(2000, 1, 1)}

	rate, ev, err := NewUtilityRate("r2", Electricity, money(t, "7.50"), shared.NewDate(2026, 12, 1), "", latest, now)
	if err != nil {
		t.Fatal(err)
	}
	if rate.PerUnit.String() != "7.50" || !rate.EffectiveFrom.Equal(shared.NewDate(2026, 12, 1)) {
		t.Fatalf("rate = %+v", rate)
	}
	if ev.EventName() != "UtilityRateChanged" || ev.PreviousPerUnit == nil || ev.PreviousPerUnit.String() != "6.00" || ev.EffectiveFrom != "2026-12-01" {
		t.Fatalf("event = %+v", ev)
	}

	cases := []struct {
		name    string
		typ     UtilityType
		perUnit string
		from    time.Time
		wantErr error
	}{
		{"same day as latest", Electricity, "7.00", shared.NewDate(2000, 1, 1), ErrRateNotAfterLatest},
		{"before latest", Electricity, "7.00", shared.NewDate(1999, 12, 31), ErrRateNotAfterLatest},
		{"zero", Electricity, "0", shared.NewDate(2027, 1, 1), ErrInvalidRate},
		{"negative", Electricity, "-1.00", shared.NewDate(2027, 1, 1), ErrInvalidRate},
		{"bad type", UtilityType("GAS"), "1.00", shared.NewDate(2027, 1, 1), ErrInvalidRate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := NewUtilityRate("x", c.typ, money(t, c.perUnit), c.from, "", latest, now); !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestRateOn(t *testing.T) {
	rates := []UtilityRate{
		{ID: "e1", Type: Electricity, PerUnit: money(t, "6.00"), EffectiveFrom: shared.NewDate(2000, 1, 1)},
		{ID: "e2", Type: Electricity, PerUnit: money(t, "7.00"), EffectiveFrom: shared.NewDate(2026, 12, 1)},
		{ID: "w1", Type: Water, PerUnit: money(t, "20.00"), EffectiveFrom: shared.NewDate(2000, 1, 1)},
	}
	tests := []struct {
		typ  UtilityType
		date time.Time
		want string
	}{
		{Electricity, shared.NewDate(2026, 11, 30), "e1"},
		{Electricity, shared.NewDate(2026, 12, 1), "e2"}, // effective on its first day
		{Electricity, shared.NewDate(2027, 5, 1), "e2"},
		{Water, shared.NewDate(2027, 5, 1), "w1"},
	}
	for _, tt := range tests {
		got, ok := RateOn(rates, tt.typ, tt.date)
		if !ok || got.ID != tt.want {
			t.Errorf("RateOn(%s, %s) = %q, want %q", tt.typ, tt.date.Format(time.DateOnly), got.ID, tt.want)
		}
	}
	if _, ok := RateOn(rates, Electricity, shared.NewDate(1999, 1, 1)); ok {
		t.Error("expected no rate before the first one")
	}
}
