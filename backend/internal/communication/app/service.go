// Package app holds the communication use cases: announcements, owner
// settings (payment QR, alert window) and lease-expiry alerts.
package app

import (
	"context"
	"fmt"
	"time"

	"unite-mansion/internal/communication/domain"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
)

type Repository interface {
	InsertAnnouncement(ctx context.Context, a domain.Announcement) error // no-op if the source was already announced
	ListAnnouncements(ctx context.Context) ([]domain.Announcement, error)
	Settings(ctx context.Context) (domain.Settings, error)
	SaveSettings(ctx context.Context, s domain.Settings) error
}

// Lease is what communication needs to know about a lease.
type Lease struct {
	ID         string
	RoomNumber string
	TenantName string
	StartDate  time.Time
	EndDate    time.Time
}

// LeaseReader is implemented by tenancy and wired in cmd/api.
type LeaseReader interface {
	LeasesEndingBetween(ctx context.Context, from, to time.Time) ([]Lease, error)
	ActiveLeaseOfRoom(ctx context.Context, roomID string) (*Lease, error)
}

type Service struct {
	repo   Repository
	leases LeaseReader
	files  storage.FileStorage
	now    func() time.Time
	newID  func() string
}

func NewService(repo Repository, leases LeaseReader, files storage.FileStorage, now func() time.Time, newID func() string) *Service {
	return &Service{repo: repo, leases: leases, files: files, now: now, newID: newID}
}

// --- announcements ---

// rateChanged is satisfied by billing's UtilityRateChanged event.
type rateChanged interface {
	RateChangeDetails() (rateID, utilityType string, perUnit shared.Money, effectiveFrom, announcement string)
}

// HandleUtilityRateChanged creates the tenant announcement for a rate change
// (§6.3). It runs inside the transaction that saved the rate.
func (s *Service) HandleUtilityRateChanged(ctx context.Context, e shared.Event) error {
	ev, ok := e.(rateChanged)
	if !ok {
		return fmt.Errorf("announce %s: unexpected event type %T", e.EventName(), e)
	}
	rateID, typ, perUnit, from, msg := ev.RateChangeDetails()
	effective, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return fmt.Errorf("announce rate change: effective date: %w", err)
	}
	a := domain.AnnouncementForRateChange(s.newID(), domain.RateChange{
		RateID: rateID, UtilityType: typ, PerUnit: perUnit, EffectiveFrom: effective, Message: msg,
	}, s.now())
	if err := s.repo.InsertAnnouncement(ctx, a); err != nil {
		return fmt.Errorf("announce rate change: %w", err)
	}
	return nil
}

func (s *Service) Announcements(ctx context.Context) ([]domain.Announcement, error) {
	list, err := s.repo.ListAnnouncements(ctx)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	return list, nil
}

// --- settings ---

// SettingsView is the settings with a signed URL of the payment QR.
type SettingsView struct {
	LeaseExpiryAlertDays int
	PaymentQRURL         *string
}

func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	st, err := s.repo.Settings(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: %w", err)
	}
	v := SettingsView{LeaseExpiryAlertDays: st.LeaseExpiryAlertDays}
	if st.PaymentQRKey != nil {
		url, err := s.signQR(ctx, *st.PaymentQRKey)
		if err != nil {
			return SettingsView{}, err
		}
		v.PaymentQRURL = &url
	}
	return v, nil
}

func (s *Service) UpdateAlertDays(ctx context.Context, days *int) (SettingsView, error) {
	if days != nil {
		if err := domain.ValidateAlertDays(*days); err != nil {
			return SettingsView{}, err
		}
		st, err := s.repo.Settings(ctx)
		if err != nil {
			return SettingsView{}, fmt.Errorf("settings: %w", err)
		}
		st.LeaseExpiryAlertDays = *days
		if err := s.repo.SaveSettings(ctx, st); err != nil {
			return SettingsView{}, fmt.Errorf("save settings: %w", err)
		}
	}
	return s.Settings(ctx)
}

const qrPrefix = "settings/payment-qr"

func (s *Service) CreatePaymentQRUpload(ctx context.Context, contentType string, size int64) (storage.PresignedRequest, string, error) {
	if err := storage.CheckImage(contentType, size); err != nil {
		return storage.PresignedRequest{}, "", err
	}
	key := storage.ImageKey(qrPrefix, s.newID(), contentType)
	req, err := s.files.PresignPut(ctx, storage.PutObject{Key: key, ContentType: contentType, ContentLength: size}, storage.UploadURLTTL)
	if err != nil {
		return storage.PresignedRequest{}, "", fmt.Errorf("presign payment QR: %w", err)
	}
	return req, key, nil
}

// SetPaymentQR makes an uploaded image the QR tenants scan (P-T06). The
// previous image is kept in storage; it is tiny and not personal data.
func (s *Service) SetPaymentQR(ctx context.Context, fileKey string) (SettingsView, error) {
	if _, err := storage.VerifyImageUpload(ctx, s.files, qrPrefix, fileKey); err != nil {
		return SettingsView{}, err
	}
	st, err := s.repo.Settings(ctx)
	if err != nil {
		return SettingsView{}, fmt.Errorf("settings: %w", err)
	}
	st.PaymentQRKey = &fileKey
	if err := s.repo.SaveSettings(ctx, st); err != nil {
		return SettingsView{}, fmt.Errorf("save settings: %w", err)
	}
	return s.Settings(ctx)
}

// PaymentQRURL is the signed URL tenants use to see and scan the QR.
func (s *Service) PaymentQRURL(ctx context.Context) (string, error) {
	st, err := s.repo.Settings(ctx)
	if err != nil {
		return "", fmt.Errorf("settings: %w", err)
	}
	if st.PaymentQRKey == nil {
		return "", domain.ErrPaymentQRNotSet
	}
	return s.signQR(ctx, *st.PaymentQRKey)
}

func (s *Service) signQR(ctx context.Context, key string) (string, error) {
	req, err := s.files.PresignGet(ctx, key, storage.ReadURLTTL)
	if err != nil {
		return "", fmt.Errorf("sign payment QR: %w", err)
	}
	return req.URL, nil
}

// --- lease-expiry alerts (a query, no scheduler, §6.6) ---

type ExpiringLease struct {
	Lease
	DaysLeft int
}

func (s *Service) ExpiringLeases(ctx context.Context) ([]ExpiringLease, error) {
	st, err := s.repo.Settings(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	today := shared.DateOf(s.now())
	leases, err := s.leases.LeasesEndingBetween(ctx, today, today.AddDate(0, 0, st.LeaseExpiryAlertDays))
	if err != nil {
		return nil, err
	}
	out := make([]ExpiringLease, 0, len(leases))
	for _, l := range leases {
		out = append(out, ExpiringLease{Lease: l, DaysLeft: domain.DaysLeft(l.EndDate, today)})
	}
	return out, nil
}

// ExpiringRoomNumbers is the set of rooms whose lease is expiring (P-A05 filter).
func (s *Service) ExpiringRoomNumbers(ctx context.Context) (map[string]bool, error) {
	list, err := s.ExpiringLeases(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, l := range list {
		out[l.RoomNumber] = true
	}
	return out, nil
}

// LeaseAlert is the tenant's banner (P-T04).
type LeaseAlert struct {
	Expiring bool
	EndDate  *time.Time
	DaysLeft *int
}

func (s *Service) TenantLeaseAlert(ctx context.Context, roomID string) (LeaseAlert, error) {
	lease, err := s.leases.ActiveLeaseOfRoom(ctx, roomID)
	if err != nil {
		return LeaseAlert{}, err
	}
	if lease == nil {
		return LeaseAlert{}, nil
	}
	st, err := s.repo.Settings(ctx)
	if err != nil {
		return LeaseAlert{}, fmt.Errorf("settings: %w", err)
	}
	today := shared.DateOf(s.now())
	if !domain.Expiring(lease.EndDate, today, st.LeaseExpiryAlertDays) {
		return LeaseAlert{}, nil
	}
	days := domain.DaysLeft(lease.EndDate, today)
	end := lease.EndDate
	return LeaseAlert{Expiring: true, EndDate: &end, DaysLeft: &days}, nil
}
