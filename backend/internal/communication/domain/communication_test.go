package domain

import (
	"errors"
	"testing"
	"time"

	"unite-mansion/internal/shared"
)

func TestAnnouncementForRateChange(t *testing.T) {
	now := time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)
	rc := RateChange{RateID: "rate-1", UtilityType: "ELECTRICITY", PerUnit: shared.MoneyFromBaht(7), EffectiveFrom: shared.NewDate(2026, 12, 1)}

	a := AnnouncementForRateChange("a1", rc, now)
	if a.Title != "ปรับอัตราค่าไฟฟ้า" || a.Message != "อัตราค่าไฟฟ้าใหม่ 7.00 บาท/หน่วย มีผลตั้งแต่วันที่ 1 ธ.ค. 2026" {
		t.Fatalf("default announcement = %+v", a)
	}
	if a.Source != SourceRateChanged || a.SourceID != "rate-1" {
		t.Fatalf("source = %s %s", a.Source, a.SourceID)
	}

	rc.UtilityType, rc.Message = "WATER", "ค่าน้ำปรับตามการประปา"
	if a := AnnouncementForRateChange("a2", rc, now); a.Title != "ปรับอัตราค่าน้ำ" || a.Message != "ค่าน้ำปรับตามการประปา" {
		t.Fatalf("custom announcement = %+v", a)
	}
}

func TestValidateAlertDays(t *testing.T) {
	for _, bad := range []int{0, -1, 366} {
		if err := ValidateAlertDays(bad); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ValidateAlertDays(%d) = %v", bad, err)
		}
	}
	if err := ValidateAlertDays(30); err != nil {
		t.Fatal(err)
	}
}

func TestExpiring(t *testing.T) { // §6.6
	today := shared.NewDate(2026, 11, 13)
	cases := []struct {
		end  time.Time
		want bool
	}{
		{shared.NewDate(2026, 11, 13), true},  // ends today
		{shared.NewDate(2026, 12, 13), true},  // exactly 30 days
		{shared.NewDate(2026, 12, 14), false}, // 31 days
		{shared.NewDate(2026, 11, 12), false}, // already ended
	}
	for _, c := range cases {
		if got := Expiring(c.end, today, 30); got != c.want {
			t.Errorf("Expiring(%s) = %v, want %v", c.end.Format(time.DateOnly), got, c.want)
		}
	}
	if d := DaysLeft(shared.NewDate(2026, 12, 13), today); d != 30 {
		t.Fatalf("DaysLeft = %d", d)
	}
}
