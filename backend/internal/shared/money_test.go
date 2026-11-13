package shared

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"4512", "4512.00", false},
		{"4512.5", "4512.50", false},
		{"-300.00", "-300.00", false},
		{"0", "0.00", false},
		{"1.005", "", true},
		{"abc", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseMoney(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidMoney) {
					t.Fatalf("ParseMoney(%q) err = %v, want ErrInvalidMoney", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMoney(%q) unexpected err: %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Fatalf("ParseMoney(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestMoneyArithmeticIsExact(t *testing.T) {
	// 0.10 + 0.20 must be exactly 0.30 (would fail with float64).
	a, _ := ParseMoney("0.10")
	b, _ := ParseMoney("0.20")
	want, _ := ParseMoney("0.30")
	if got := a.Add(b); !got.Equal(want) {
		t.Fatalf("0.10 + 0.20 = %s, want 0.30", got)
	}

	rate, _ := ParseMoney("6.00")
	if got := rate.MulInt(12); got.String() != "72.00" {
		t.Fatalf("6.00 * 12 = %s, want 72.00", got)
	}
	if got := MoneyFromBaht(4500).Sub(MoneyFromBaht(300)).Neg(); got.String() != "-4200.00" {
		t.Fatalf("got %s, want -4200.00", got)
	}
}

func TestMoneyJSON(t *testing.T) {
	m, _ := ParseMoney("4512")
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"4512.00"` {
		t.Fatalf("marshal = %s, want \"4512.00\"", b)
	}

	var back Money
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Equal(m) {
		t.Fatalf("round trip = %s, want %s", back, m)
	}

	if err := json.Unmarshal([]byte(`4512.00`), &back); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("number input err = %v, want ErrInvalidMoney", err)
	}
	if err := json.Unmarshal([]byte(`"1.234"`), &back); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("3 decimals err = %v, want ErrInvalidMoney", err)
	}
}
