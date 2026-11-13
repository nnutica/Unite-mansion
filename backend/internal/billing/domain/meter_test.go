package domain

import (
	"errors"
	"testing"
)

func TestMeterUnits(t *testing.T) {
	tests := []struct {
		name               string
		prev, curr         int64
		digits             int
		rollover           bool
		wantUnits          int64
		wantErr            error
		wantRolloverStored bool
	}{
		{"normal", 1200, 1350, 5, false, 150, nil, false},
		{"no usage", 500, 500, 4, false, 0, nil, false},
		// §9: 99998 → 00010 on a 5-digit meter = 12 units.
		{"rollover 5 digits", 99998, 10, 5, true, 12, nil, true},
		{"rollover 4 digits", 9990, 5, 4, true, 15, nil, true},
		{"rollover not confirmed", 99998, 10, 5, false, 0, ErrMeterRolloverNotConfirmed, false},
		{"rollover flag ignored when not needed", 10, 20, 5, true, 10, nil, false},
		{"current exceeds digits", 0, 100000, 5, false, 0, ErrMeterReadingOutOfRange, false},
		{"negative previous", -1, 10, 5, false, 0, ErrMeterReadingOutOfRange, false},
		{"digits zero", 0, 1, 0, false, 0, ErrMeterReadingOutOfRange, false},
		{"digits ten", 0, 1, 10, false, 0, ErrMeterReadingOutOfRange, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMeterReading(tt.prev, tt.curr, tt.digits, tt.rollover)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Units(); got != tt.wantUnits {
				t.Fatalf("Units = %d, want %d", got, tt.wantUnits)
			}
			if m.RolloverConfirmed != tt.wantRolloverStored {
				t.Fatalf("RolloverConfirmed = %v, want %v", m.RolloverConfirmed, tt.wantRolloverStored)
			}
		})
	}
}

func TestCheckUsage(t *testing.T) {
	pct := func(v int64) *int64 { return &v }
	tests := []struct {
		name    string
		current int64
		history []int64
		want    UsageCheck
	}{
		{"no history never warns", 1000, nil, UsageCheck{}},
		{"exactly double is not abnormal", 200, []int64{100, 100, 100}, UsageCheck{false, pct(200)}},
		{"just over double", 201, []int64{100, 100, 100}, UsageCheck{true, pct(201)}},
		{"one month of history", 250, []int64{100}, UsageCheck{true, pct(250)}},
		{"below average", 50, []int64{100, 120, 80}, UsageCheck{false, pct(50)}},
		{"zero average with usage", 5, []int64{0, 0}, UsageCheck{Abnormal: true}},
		{"zero average without usage", 0, []int64{0}, UsageCheck{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckUsage(tt.current, tt.history)
			if got.Abnormal != tt.want.Abnormal {
				t.Fatalf("Abnormal = %v, want %v", got.Abnormal, tt.want.Abnormal)
			}
			if (got.PercentOfAverage == nil) != (tt.want.PercentOfAverage == nil) ||
				(got.PercentOfAverage != nil && *got.PercentOfAverage != *tt.want.PercentOfAverage) {
				t.Fatalf("PercentOfAverage = %v, want %v", got.PercentOfAverage, tt.want.PercentOfAverage)
			}
		})
	}
}
