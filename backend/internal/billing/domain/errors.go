// Package domain holds the billing aggregates and invariants: Invoice
// (INV-1…8), MeterReading and UtilityRate (context.md §6.1–6.3).
// Imports: standard library and internal/shared only.
package domain

import "errors"

var (
	ErrInvoiceNotFound           = errors.New("invoice not found")
	ErrInvoiceAlreadyExists      = errors.New("invoice already exists for this room and period") // INV-1
	ErrInvoiceNotDraft           = errors.New("invoice is not a draft")                          // INV-3
	ErrInvalidStatusTransition   = errors.New("invalid invoice status transition")               // INV-2
	ErrRoomHasNoLease            = errors.New("room has no lease in this billing period")
	ErrMeterDigitsNotSet         = errors.New("room meter digits are not set")
	ErrRateNotFound              = errors.New("no utility rate in effect")
	ErrMeterReadingOutOfRange    = errors.New("meter reading out of range for meter digits")
	ErrMeterRolloverNotConfirmed = errors.New("meter reading went down; rollover must be confirmed") // INV-7
	ErrPreviousReadingRequired   = errors.New("previous meter reading is required")
	ErrMetersIncomplete          = errors.New("electricity and water readings are required before issuing")
	ErrInvalidLine               = errors.New("invalid invoice line")
	ErrDiscountExceedsRent       = errors.New("discounts exceed the rent of this invoice") // INV-6
	ErrLineNotFound              = errors.New("invoice line not found")
	ErrInvalidRate               = errors.New("invalid utility rate")
	ErrRateNotAfterLatest        = errors.New("new rate must take effect after the latest rate")
)
