package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
	"unite-mansion/internal/tenancy/domain"
)

// DocumentRepository stores consent and tenant documents (§6.6).
type DocumentRepository interface {
	LatestConsentText(ctx context.Context) (domain.ConsentText, error)
	InsertConsentText(ctx context.Context, c domain.ConsentText) error
	ConsentAcceptedAt(ctx context.Context, accountID string, version int) (*time.Time, error)
	InsertConsent(ctx context.Context, accountID string, version int, at time.Time) error

	InsertDocument(ctx context.Context, d domain.TenantDocument) error
	DocumentsByAccount(ctx context.Context, accountID string) ([]domain.TenantDocument, error)
	DocumentsByRoom(ctx context.Context, roomID string) ([]domain.TenantDocument, error)
	SetPurgeAt(ctx context.Context, accountID string, at *time.Time) error
	DocumentsDueForPurge(ctx context.Context, now time.Time) ([]domain.TenantDocument, error)
	DeleteDocument(ctx context.Context, id string) error
}

// UsernameDirectory is implemented by identity and wired in cmd/api.
type UsernameDirectory interface {
	Usernames(ctx context.Context) (map[string]string, error) // account ID → username
}

type EventDispatcher interface {
	Dispatch(ctx context.Context, events ...shared.Event) error
}

// Documents holds the consent and document use cases. It is separate from
// Service because it needs storage, events and identity lookups.
type Documents struct {
	repo      DocumentRepository
	rooms     Repository
	usernames UsernameDirectory
	files     storage.FileStorage
	tx        shared.TxManager
	events    EventDispatcher
	now       func() time.Time
	newID     func() string
}

func NewDocuments(repo DocumentRepository, rooms Repository, usernames UsernameDirectory, files storage.FileStorage,
	tx shared.TxManager, events EventDispatcher, now func() time.Time, newID func() string) *Documents {
	return &Documents{repo: repo, rooms: rooms, usernames: usernames, files: files, tx: tx, events: events, now: now, newID: newID}
}

// --- consent ---

type ConsentStatus struct {
	Text       domain.ConsentText
	AcceptedAt *time.Time // nil = the latest version is not accepted
}

func (d *Documents) ConsentStatus(ctx context.Context, accountID string) (ConsentStatus, error) {
	text, err := d.repo.LatestConsentText(ctx)
	if err != nil {
		return ConsentStatus{}, fmt.Errorf("consent text: %w", err)
	}
	at, err := d.repo.ConsentAcceptedAt(ctx, accountID, text.Version)
	if err != nil {
		return ConsentStatus{}, fmt.Errorf("consent: %w", err)
	}
	return ConsentStatus{Text: text, AcceptedAt: at}, nil
}

// AcceptConsent records consent to version, which must be the latest so a
// tenant never agrees to text they were not shown.
func (d *Documents) AcceptConsent(ctx context.Context, accountID string, version int) (ConsentStatus, error) {
	text, err := d.repo.LatestConsentText(ctx)
	if err != nil {
		return ConsentStatus{}, fmt.Errorf("consent text: %w", err)
	}
	if version != text.Version {
		return ConsentStatus{}, fmt.Errorf("%w: got %d, latest is %d", domain.ErrConsentVersionOutdated, version, text.Version)
	}
	if err := d.repo.InsertConsent(ctx, accountID, version, d.now()); err != nil {
		return ConsentStatus{}, fmt.Errorf("accept consent: %w", err)
	}
	return d.ConsentStatus(ctx, accountID)
}

func (d *Documents) ConsentText(ctx context.Context) (domain.ConsentText, error) {
	return d.repo.LatestConsentText(ctx)
}

// UpdateConsentText publishes a new version (P-A13).
func (d *Documents) UpdateConsentText(ctx context.Context, body string) (domain.ConsentText, error) {
	var next domain.ConsentText
	err := d.tx.WithinTx(ctx, func(ctx context.Context) error {
		latest, err := d.repo.LatestConsentText(ctx)
		if err != nil {
			return err
		}
		if next, err = domain.NextConsentText(latest, body, d.now()); err != nil {
			return err
		}
		return d.repo.InsertConsentText(ctx, next)
	})
	if err != nil {
		return domain.ConsentText{}, fmt.Errorf("update consent text: %w", err)
	}
	return next, nil
}

func (d *Documents) requireConsent(ctx context.Context, accountID string) error {
	st, err := d.ConsentStatus(ctx, accountID)
	if err != nil {
		return err
	}
	if st.AcceptedAt == nil {
		return domain.ErrConsentRequired
	}
	return nil
}

// --- documents ---

func (d *Documents) CreateUpload(ctx context.Context, accountID string, typ domain.DocumentType, contentType string, size int64) (storage.PresignedRequest, string, error) {
	if !typ.Valid() {
		return storage.PresignedRequest{}, "", fmt.Errorf("%w: %q", domain.ErrInvalidDocumentType, typ)
	}
	if err := d.requireConsent(ctx, accountID); err != nil {
		return storage.PresignedRequest{}, "", err
	}
	if err := storage.CheckDocument(contentType, size); err != nil {
		return storage.PresignedRequest{}, "", err
	}
	key := storage.DocumentKey(documentPrefix(accountID), d.newID(), contentType)
	req, err := d.files.PresignPut(ctx, storage.PutObject{Key: key, ContentType: contentType, ContentLength: size}, storage.UploadURLTTL)
	if err != nil {
		return storage.PresignedRequest{}, "", fmt.Errorf("presign document upload: %w", err)
	}
	return req, key, nil
}

func (d *Documents) Submit(ctx context.Context, accountID, roomID string, typ domain.DocumentType, fileKey string) (DocumentView, error) {
	if !typ.Valid() {
		return DocumentView{}, fmt.Errorf("%w: %q", domain.ErrInvalidDocumentType, typ)
	}
	if err := d.requireConsent(ctx, accountID); err != nil {
		return DocumentView{}, err
	}
	info, err := storage.VerifyDocumentUpload(ctx, d.files, documentPrefix(accountID), fileKey)
	if err != nil {
		return DocumentView{}, err
	}
	doc := domain.TenantDocument{
		ID: d.newID(), AccountID: accountID, RoomID: roomID, Type: typ, FileKey: fileKey,
		ContentType: info.ContentType, SizeBytes: info.Size, UploadedAt: d.now().UTC(),
	}
	if err := d.repo.InsertDocument(ctx, doc); err != nil {
		return DocumentView{}, fmt.Errorf("submit document: %w", err)
	}
	views, err := d.views(ctx, []domain.TenantDocument{doc})
	if err != nil {
		return DocumentView{}, err
	}
	return views[0], nil
}

// DocumentView is a document with a signed URL and the uploader's username.
type DocumentView struct {
	domain.TenantDocument
	URL      string
	Username string
}

func (d *Documents) TenantDocuments(ctx context.Context, accountID string) ([]DocumentView, error) {
	docs, err := d.repo.DocumentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("tenant documents: %w", err)
	}
	return d.views(ctx, docs)
}

// RoomDocuments lists every document uploaded for a room, including those of
// accounts that moved out and are scheduled for deletion (P-A06).
func (d *Documents) RoomDocuments(ctx context.Context, roomNumber string) ([]DocumentView, error) {
	room, err := d.rooms.RoomByNumber(ctx, roomNumber, false)
	if err != nil {
		return nil, err
	}
	docs, err := d.repo.DocumentsByRoom(ctx, room.ID)
	if err != nil {
		return nil, fmt.Errorf("room documents: %w", err)
	}
	return d.views(ctx, docs)
}

func (d *Documents) views(ctx context.Context, docs []domain.TenantDocument) ([]DocumentView, error) {
	names, err := d.usernames.Usernames(ctx)
	if err != nil {
		return nil, fmt.Errorf("usernames: %w", err)
	}
	out := make([]DocumentView, 0, len(docs))
	for _, doc := range docs {
		req, err := d.files.PresignGet(ctx, doc.FileKey, storage.ReadURLTTL)
		if err != nil {
			return nil, fmt.Errorf("sign document: %w", err)
		}
		out = append(out, DocumentView{TenantDocument: doc, URL: req.URL, Username: names[doc.AccountID]})
	}
	return out, nil
}

// --- retention (§6.6) ---

// OnAccountUnassigned schedules deletion of the account's documents 30 days
// after it was unbound. It runs inside the unassign transaction.
func (d *Documents) OnAccountUnassigned(ctx context.Context, e shared.Event) error {
	at := domain.PurgeTimeAfterUnassign(e.OccurredAt())
	if err := d.repo.SetPurgeAt(ctx, e.AggregateID(), &at); err != nil {
		return fmt.Errorf("schedule document deletion: %w", err)
	}
	return nil
}

// OnAccountAssigned cancels a scheduled deletion when the account is bound
// again before it happens.
func (d *Documents) OnAccountAssigned(ctx context.Context, e shared.Event) error {
	if err := d.repo.SetPurgeAt(ctx, e.AggregateID(), nil); err != nil {
		return fmt.Errorf("cancel document deletion: %w", err)
	}
	return nil
}

// PurgeDue deletes documents whose time has come: the file in storage first,
// then the row and an audit event per account. It is safe to run repeatedly;
// a file already gone counts as deleted, and a failure leaves the row for the
// next run.
func (d *Documents) PurgeDue(ctx context.Context) (int, error) {
	ctx = shared.WithActor(ctx, shared.Actor{Type: shared.ActorSystem})
	due, err := d.repo.DocumentsDueForPurge(ctx, d.now())
	if err != nil {
		return 0, fmt.Errorf("documents due for deletion: %w", err)
	}
	byAccount := map[string][]domain.TenantDocument{}
	var order []string
	for _, doc := range due {
		if _, seen := byAccount[doc.AccountID]; !seen {
			order = append(order, doc.AccountID)
		}
		byAccount[doc.AccountID] = append(byAccount[doc.AccountID], doc)
	}

	purged := 0
	var errs []error
	for _, accountID := range order {
		docs := byAccount[accountID]
		if err := d.purgeAccount(ctx, accountID, docs); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", accountID, err))
			continue
		}
		purged += len(docs)
	}
	return purged, errors.Join(errs...)
}

func (d *Documents) purgeAccount(ctx context.Context, accountID string, docs []domain.TenantDocument) error {
	for _, doc := range docs {
		if err := d.files.Delete(ctx, doc.FileKey); err != nil {
			return fmt.Errorf("delete file %s: %w", doc.FileKey, err)
		}
	}
	return d.tx.WithinTx(ctx, func(ctx context.Context) error {
		ev := domain.TenantDocumentsPurged{EventBase: shared.NewEventBase(accountID, d.now())}
		for _, doc := range docs {
			if err := d.repo.DeleteDocument(ctx, doc.ID); err != nil {
				return err
			}
			ev.DocumentIDs = append(ev.DocumentIDs, doc.ID)
			ev.Types = append(ev.Types, doc.Type)
		}
		return d.events.Dispatch(ctx, ev)
	})
}

// RunRetention runs PurgeDue now and then every interval until ctx ends. It
// is the in-process daily job of §6.6 (no external scheduler).
func (d *Documents) RunRetention(ctx context.Context, interval time.Duration, log *slog.Logger) {
	run := func() {
		n, err := d.PurgeDue(ctx)
		if err != nil {
			log.Error("document retention failed", "error", err, "purged", n)
			return
		}
		if n > 0 {
			log.Info("document retention", "purged", n)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func documentPrefix(accountID string) string { return "documents/" + accountID }
