package app

import (
	"context"
	"fmt"
	"time"

	"unite-mansion/internal/billing/domain"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
)

// SlipView is a slip for display. PossibleDuplicate is true when another slip
// has the same file hash (§6.4, warning only). ImageURL is a short-lived
// signed URL filled in by the service.
type SlipView struct {
	Slip              *domain.PaymentSlip
	PossibleDuplicate bool
	ImageURL          string
}

// InvoiceSummary is the list form of an invoice.
type InvoiceSummary struct {
	ID             string
	InvoiceNumber  string
	RoomNumber     string
	Period         shared.BillingPeriod
	Total          shared.Money
	Status         domain.Status
	HasPendingSlip bool
	// Filled in by the service:
	DisplayStatus string
	DueDate       time.Time
}

// SlipQueueItem is a slip with the invoice it pays (P-A07).
type SlipQueueItem struct {
	Slip    SlipView
	Invoice InvoiceSummary
}

// --- tenant side; roomID always comes from the server-side session (§6.5) ---

// TenantInvoices lists the room's issued and paid invoices.
func (s *Service) TenantInvoices(ctx context.Context, roomID string) ([]InvoiceSummary, error) {
	list, err := s.repo.InvoiceSummariesByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("tenant invoices: %w", err)
	}
	for i := range list {
		s.fillSummary(&list[i])
	}
	return list, nil
}

// TenantInvoice returns one invoice of the tenant's room. Another room's
// invoice and drafts are reported as not found so nothing leaks.
func (s *Service) TenantInvoice(ctx context.Context, roomID, invoiceID string) (InvoiceView, error) {
	inv, err := s.ownedInvoice(ctx, roomID, invoiceID)
	if err != nil {
		return InvoiceView{}, err
	}
	return s.viewWithHistory(ctx, inv)
}

// CreateSlipUpload presigns an upload for a slip of the tenant's invoice.
func (s *Service) CreateSlipUpload(ctx context.Context, roomID, invoiceID, contentType string, size int64) (storage.PresignedRequest, string, error) {
	inv, err := s.ownedInvoice(ctx, roomID, invoiceID)
	if err != nil {
		return storage.PresignedRequest{}, "", err
	}
	if inv.Status() != domain.StatusIssued && inv.Status() != domain.StatusPaid {
		return storage.PresignedRequest{}, "", domain.ErrSlipNotAllowed
	}
	if err := storage.CheckImage(contentType, size); err != nil {
		return storage.PresignedRequest{}, "", err
	}
	key := storage.ImageKey(slipPrefix(inv.ID()), s.newID(), contentType)
	req, err := s.files.PresignPut(ctx, storage.PutObject{Key: key, ContentType: contentType, ContentLength: size}, storage.UploadURLTTL)
	if err != nil {
		return storage.PresignedRequest{}, "", fmt.Errorf("presign slip upload: %w", err)
	}
	return req, key, nil
}

// SubmitSlip records an uploaded slip after checking the file really is in
// storage, belongs to this invoice and is an allowed image.
func (s *Service) SubmitSlip(ctx context.Context, roomID, invoiceID, fileKey string) (SlipView, error) {
	inv, err := s.ownedInvoice(ctx, roomID, invoiceID)
	if err != nil {
		return SlipView{}, err
	}
	info, err := storage.VerifyImageUpload(ctx, s.files, slipPrefix(inv.ID()), fileKey)
	if err != nil {
		return SlipView{}, err
	}
	hash, err := storage.SHA256(ctx, s.files, fileKey)
	if err != nil {
		return SlipView{}, err
	}

	var slip *domain.PaymentSlip
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if slip, err = domain.SubmitSlip(s.newID(), inv, fileKey, hash, info.ContentType, info.Size, s.now()); err != nil {
			return err
		}
		if err := s.repo.InsertSlip(ctx, slip); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, slip.PullEvents()...)
	})
	if err != nil {
		return SlipView{}, fmt.Errorf("submit slip for invoice %s: %w", invoiceID, err)
	}
	return s.slipView(ctx, inv.ID(), slip.ID)
}

// --- admin side ---

func (s *Service) SlipQueue(ctx context.Context, status *domain.SlipStatus) ([]SlipQueueItem, error) {
	items, err := s.repo.SlipQueue(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("slip queue: %w", err)
	}
	for i := range items {
		if err := s.fillQueueItem(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// ApproveSlip approves a slip and marks its ISSUED invoice PAID in one
// transaction (§6.4).
func (s *Service) ApproveSlip(ctx context.Context, slipID string) (SlipQueueItem, error) {
	return s.reviewSlip(ctx, slipID, "approve", func(slip *domain.PaymentSlip, inv *domain.Invoice) error {
		return slip.Approve(inv, s.now())
	})
}

func (s *Service) RejectSlip(ctx context.Context, slipID string, reason domain.RejectReason, note string) (SlipQueueItem, error) {
	return s.reviewSlip(ctx, slipID, "reject", func(slip *domain.PaymentSlip, _ *domain.Invoice) error {
		return slip.Reject(reason, note, s.now())
	})
}

func (s *Service) reviewSlip(ctx context.Context, slipID, op string, review func(*domain.PaymentSlip, *domain.Invoice) error) (SlipQueueItem, error) {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		slip, err := s.repo.SlipForUpdate(ctx, slipID)
		if err != nil {
			return err
		}
		inv, err := s.repo.Invoice(ctx, slip.InvoiceID, true)
		if err != nil {
			return err
		}
		if err := review(slip, inv); err != nil {
			return err
		}
		if err := s.repo.UpdateSlip(ctx, slip); err != nil {
			return err
		}
		if err := s.repo.UpdateInvoice(ctx, inv); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, append(slip.PullEvents(), inv.PullEvents()...)...)
	})
	if err != nil {
		return SlipQueueItem{}, fmt.Errorf("%s slip %s: %w", op, slipID, err)
	}
	item, err := s.repo.SlipQueueItem(ctx, slipID)
	if err != nil {
		return SlipQueueItem{}, err
	}
	return item, s.fillQueueItem(ctx, &item)
}

func (s *Service) ownedInvoice(ctx context.Context, roomID, invoiceID string) (*domain.Invoice, error) {
	inv, err := s.repo.Invoice(ctx, invoiceID, false)
	if err != nil {
		return nil, err
	}
	if inv.RoomID() != roomID || inv.Status() == domain.StatusDraft {
		return nil, domain.ErrInvoiceNotFound
	}
	return inv, nil
}

func (s *Service) slipView(ctx context.Context, invoiceID, slipID string) (SlipView, error) {
	slips, err := s.repo.SlipsByInvoice(ctx, invoiceID)
	if err != nil {
		return SlipView{}, err
	}
	for _, v := range slips {
		if v.Slip.ID == slipID {
			return v, s.signSlip(ctx, &v)
		}
	}
	return SlipView{}, domain.ErrSlipNotFound
}

func (s *Service) fillQueueItem(ctx context.Context, item *SlipQueueItem) error {
	s.fillSummary(&item.Invoice)
	return s.signSlip(ctx, &item.Slip)
}

func (s *Service) fillSummary(sum *InvoiceSummary) {
	sum.DisplayStatus = domain.DisplayStatusOf(sum.Status, sum.Period, sum.HasPendingSlip, s.now())
	sum.DueDate = shared.DateOf(sum.Period.DueDate())
}

func (s *Service) signSlip(ctx context.Context, v *SlipView) error {
	req, err := s.files.PresignGet(ctx, v.Slip.FileKey, storage.ReadURLTTL)
	if err != nil {
		return fmt.Errorf("sign slip image: %w", err)
	}
	v.ImageURL = req.URL
	return nil
}

func slipPrefix(invoiceID string) string { return "slips/" + invoiceID }
