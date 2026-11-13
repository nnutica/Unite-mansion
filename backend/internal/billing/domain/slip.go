package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"unite-mansion/internal/shared"
)

var (
	ErrSlipNotFound        = errors.New("payment slip not found")
	ErrSlipNotAllowed      = errors.New("slips can only be attached to issued or paid invoices")
	ErrSlipAlreadyReviewed = errors.New("payment slip has already been reviewed")
	ErrInvalidRejectReason = errors.New("invalid reject reason")
	ErrSlipAlreadyExists   = errors.New("this upload was already submitted")
)

type SlipStatus string

const (
	SlipStatusPending  SlipStatus = "PENDING"
	SlipStatusApproved SlipStatus = "APPROVED"
	SlipStatusRejected SlipStatus = "REJECTED"
)

type RejectReason string

const (
	RejectAmountMismatch RejectReason = "AMOUNT_MISMATCH"
	RejectDuplicate      RejectReason = "DUPLICATE"
	RejectUnclear        RejectReason = "UNCLEAR"
)

func (r RejectReason) Valid() bool {
	return r == RejectAmountMismatch || r == RejectDuplicate || r == RejectUnclear
}

const maxRejectNote = 500

// PaymentSlip is a transfer slip uploaded by the tenant (§6.4). The system
// never reads amounts from it; the admin checks it by eye.
type PaymentSlip struct {
	shared.EventRecorder

	ID           string
	InvoiceID    string
	FileKey      string
	FileSHA256   string // used only to warn about duplicates
	ContentType  string
	SizeBytes    int64
	Status       SlipStatus
	RejectReason *RejectReason
	RejectNote   string
	SubmittedAt  time.Time
	ReviewedAt   *time.Time
}

// SubmitSlip attaches a slip to an ISSUED or PAID invoice (§6.4: paying
// again after PAID is allowed; the admin decides).
func SubmitSlip(id string, inv *Invoice, fileKey, sha256, contentType string, size int64, now time.Time) (*PaymentSlip, error) {
	if inv.Status() != StatusIssued && inv.Status() != StatusPaid {
		return nil, fmt.Errorf("%w: invoice is %s", ErrSlipNotAllowed, inv.Status())
	}
	s := &PaymentSlip{
		ID: id, InvoiceID: inv.ID(), FileKey: fileKey, FileSHA256: sha256,
		ContentType: contentType, SizeBytes: size, Status: SlipStatusPending, SubmittedAt: now.UTC(),
	}
	s.Record(SlipSubmitted{
		EventBase: shared.NewEventBase(id, now), InvoiceID: inv.ID(), InvoiceNumber: inv.InvoiceNumber(), FileKey: fileKey,
	})
	return s, nil
}

// Approve accepts the slip and marks an ISSUED invoice PAID; the caller
// saves both in one transaction (§6.4). A slip for an already PAID invoice
// is approved without changing the invoice.
func (s *PaymentSlip) Approve(inv *Invoice, now time.Time) error {
	if s.Status != SlipStatusPending {
		return fmt.Errorf("%w: status is %s", ErrSlipAlreadyReviewed, s.Status)
	}
	if inv.ID() != s.InvoiceID {
		return fmt.Errorf("slip %s does not belong to invoice %s", s.ID, inv.ID())
	}
	if inv.Status() == StatusIssued {
		if err := inv.MarkPaid(now); err != nil {
			return err
		}
	}
	at := now.UTC()
	s.Status = SlipStatusApproved
	s.ReviewedAt = &at
	s.Record(SlipApproved{
		EventBase: shared.NewEventBase(s.ID, now), InvoiceID: inv.ID(), InvoiceNumber: inv.InvoiceNumber(),
		InvoiceStatus: inv.Status(), Total: inv.Total(),
	})
	return nil
}

// Reject refuses the slip with a reason; the tenant may submit another.
func (s *PaymentSlip) Reject(reason RejectReason, note string, now time.Time) error {
	if s.Status != SlipStatusPending {
		return fmt.Errorf("%w: status is %s", ErrSlipAlreadyReviewed, s.Status)
	}
	if !reason.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidRejectReason, reason)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxRejectNote {
		return fmt.Errorf("%w: note longer than %d characters", ErrInvalidRejectReason, maxRejectNote)
	}
	at := now.UTC()
	s.Status = SlipStatusRejected
	s.RejectReason = &reason
	s.RejectNote = note
	s.ReviewedAt = &at
	s.Record(SlipRejected{
		EventBase: shared.NewEventBase(s.ID, now), InvoiceID: s.InvoiceID, Reason: reason, Note: note,
	})
	return nil
}

type SlipSubmitted struct {
	shared.EventBase
	InvoiceID     string `json:"invoiceId"`
	InvoiceNumber string `json:"invoiceNumber"`
	FileKey       string `json:"fileKey"`
}

func (SlipSubmitted) EventName() string { return "SlipSubmitted" }

type SlipApproved struct {
	shared.EventBase
	InvoiceID     string       `json:"invoiceId"`
	InvoiceNumber string       `json:"invoiceNumber"`
	InvoiceStatus Status       `json:"invoiceStatus"`
	Total         shared.Money `json:"total"`
}

func (SlipApproved) EventName() string { return "SlipApproved" }

type SlipRejected struct {
	shared.EventBase
	InvoiceID string       `json:"invoiceId"`
	Reason    RejectReason `json:"reason"`
	Note      string       `json:"note,omitempty"`
}

func (SlipRejected) EventName() string { return "SlipRejected" }

// DisplayStatus derives the §6.1 display status of an invoice from its stored
// status, whether it has a PENDING slip, and the due date.
func DisplayStatus(inv *Invoice, hasPendingSlip bool, now time.Time) string {
	return DisplayStatusOf(inv.Status(), inv.Period(), hasPendingSlip, now)
}

// DisplayStatusOf is DisplayStatus for list queries that have no aggregate.
func DisplayStatusOf(status Status, period shared.BillingPeriod, hasPendingSlip bool, now time.Time) string {
	switch status {
	case StatusDraft:
		return "DRAFT"
	case StatusPaid:
		return "PAID"
	}
	if hasPendingSlip {
		return "PENDING_VERIFICATION"
	}
	if period.IsOverdue(now) {
		return "OVERDUE"
	}
	return "ISSUED"
}
