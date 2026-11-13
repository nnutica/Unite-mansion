// Package domain holds the communication model: Announcement, owner Settings
// and the lease-expiry alert rule (context.md §2, §6.3, §6.6).
// Imports: standard library and internal/shared only.
package domain

import (
	"errors"
	"fmt"
	"time"

	"unite-mansion/internal/shared"
)

var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrPaymentQRNotSet = errors.New("payment QR has not been uploaded")
)

const (
	SourceRateChanged = "UTILITY_RATE_CHANGED"
	minAlertDays      = 1
	maxAlertDays      = 365
)

var (
	thaiMonthsAbbrev = [...]string{"ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.", "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.", "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."}
	utilityTitles    = map[string]string{"ELECTRICITY": "ปรับอัตราค่าไฟฟ้า", "WATER": "ปรับอัตราค่าน้ำ"}
	utilityNames     = map[string]string{"ELECTRICITY": "ค่าไฟฟ้า", "WATER": "ค่าน้ำ"}
)

// Announcement is shown to tenants (P-T11). It is only created automatically
// when a utility rate changes (Decision log 2026-11-13, plan 5.a).
type Announcement struct {
	ID        string
	Title     string
	Message   string
	Source    string
	SourceID  string
	CreatedAt time.Time
}

// RateChange is what communication needs from billing's UtilityRateChanged.
type RateChange struct {
	RateID        string
	UtilityType   string // ELECTRICITY | WATER
	PerUnit       shared.Money
	EffectiveFrom time.Time // date
	Message       string    // entered by the admin in M-09; may be empty
}

// AnnouncementForRateChange builds the tenant announcement for a new rate.
// Dates use the AD year (§7).
func AnnouncementForRateChange(id string, rc RateChange, now time.Time) Announcement {
	title, ok := utilityTitles[rc.UtilityType]
	if !ok {
		title = "ปรับอัตราค่าสาธารณูปโภค"
	}
	msg := rc.Message
	if msg == "" {
		msg = fmt.Sprintf("อัตรา%sใหม่ %s บาท/หน่วย มีผลตั้งแต่วันที่ %s",
			utilityNames[rc.UtilityType], rc.PerUnit, ThaiDate(rc.EffectiveFrom))
	}
	return Announcement{ID: id, Title: title, Message: msg, Source: SourceRateChanged, SourceID: rc.RateID, CreatedAt: now.UTC()}
}

// ThaiDate formats a date as "1 ธ.ค. 2026" (AD year).
func ThaiDate(d time.Time) string {
	return fmt.Sprintf("%d %s %d", d.Day(), thaiMonthsAbbrev[d.Month()-1], d.Year())
}

// Settings are the owner's settings (P-A13).
type Settings struct {
	LeaseExpiryAlertDays int
	PaymentQRKey         *string
}

func ValidateAlertDays(days int) error {
	if days < minAlertDays || days > maxAlertDays {
		return fmt.Errorf("%w: lease expiry alert days must be %d–%d", ErrInvalidInput, minAlertDays, maxAlertDays)
	}
	return nil
}

// DaysLeft is the number of days from today until end (0 = ends today).
func DaysLeft(end, today time.Time) int {
	return int(end.Sub(today).Hours() / 24)
}

// Expiring reports whether a lease ending on end is within the alert window
// (§6.6): it has not ended yet and ends within alertDays.
func Expiring(end, today time.Time, alertDays int) bool {
	d := DaysLeft(end, today)
	return d >= 0 && d <= alertDays
}
