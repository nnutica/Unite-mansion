package domain

import "unite-mansion/internal/shared"

// Domain events (context.md §6.7). Payloads are what the audit log stores.

type InvoiceDrafted struct {
	shared.EventBase
	InvoiceNumber string       `json:"invoiceNumber"`
	RoomNumber    string       `json:"roomNumber"`
	BillingPeriod string       `json:"billingPeriod"`
	Total         shared.Money `json:"total"`
}

func (InvoiceDrafted) EventName() string { return "InvoiceDrafted" }

// InvoiceLineChanged covers meter readings (including an overridden previous
// reading, INV-8) and discount/penalty lines.
type InvoiceLineChanged struct {
	shared.EventBase
	InvoiceNumber string        `json:"invoiceNumber"`
	Change        string        `json:"change"` // METER_SET | LINE_ADDED | LINE_REMOVED
	LineType      LineType      `json:"lineType"`
	Description   string        `json:"description,omitempty"`
	Amount        *shared.Money `json:"amount,omitempty"`
	Meter         *MeterChange  `json:"meter,omitempty"`
	Total         shared.Money  `json:"total"`
}

type MeterChange struct {
	Previous           int64  `json:"previous"`
	Current            int64  `json:"current"`
	Units              int64  `json:"units"`
	RolloverConfirmed  bool   `json:"rolloverConfirmed"`
	PreviousOverridden bool   `json:"previousOverridden"`
	DefaultPrevious    *int64 `json:"defaultPrevious,omitempty"`
}

func (InvoiceLineChanged) EventName() string { return "InvoiceLineChanged" }

type InvoiceIssued struct {
	shared.EventBase
	InvoiceNumber string       `json:"invoiceNumber"`
	Total         shared.Money `json:"total"`
}

func (InvoiceIssued) EventName() string { return "InvoiceIssued" }

type UtilityRateChanged struct {
	shared.EventBase
	UtilityType     UtilityType   `json:"utilityType"`
	PerUnit         shared.Money  `json:"perUnit"`
	PreviousPerUnit *shared.Money `json:"previousPerUnit,omitempty"`
	EffectiveFrom   string        `json:"effectiveFrom"`
	// Announcement is the text the admin entered in M-09 for tenants (may be empty).
	Announcement string `json:"announcement,omitempty"`
}

func (UtilityRateChanged) EventName() string { return "UtilityRateChanged" }

// RateChangeDetails exposes the event's data to subscribers in other contexts
// without them importing billing types.
func (e UtilityRateChanged) RateChangeDetails() (rateID, utilityType string, perUnit shared.Money, effectiveFrom, announcement string) {
	return e.AggregateID(), string(e.UtilityType), e.PerUnit, e.EffectiveFrom, e.Announcement
}
