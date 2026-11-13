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
	ErrConsentRequired        = errors.New("consent to the latest version is required before uploading documents")
	ErrConsentVersionOutdated = errors.New("consent version is not the latest")
	ErrInvalidDocumentType    = errors.New("invalid document type")
)

// RetentionPeriod: documents are deleted 30 days after the account is
// unbound from its room (§6.6, PDPA).
const RetentionPeriod = 30 * 24 * time.Hour

const maxConsentText = 10000

type DocumentType string

const (
	DocumentIDCardCopy    DocumentType = "ID_CARD_COPY"
	DocumentLeaseContract DocumentType = "LEASE_CONTRACT"
)

func (t DocumentType) Valid() bool { return t == DocumentIDCardCopy || t == DocumentLeaseContract }

// ConsentText is one published version of the PDPA consent text. Every edit
// is a new version that tenants must accept again.
type ConsentText struct {
	Version   int
	Body      string
	CreatedAt time.Time
}

// NextConsentText builds the version after latest.
func NextConsentText(latest ConsentText, body string, now time.Time) (ConsentText, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxConsentText {
		return ConsentText{}, fmt.Errorf("%w: consent text must be 1–%d characters", ErrInvalidInput, maxConsentText)
	}
	return ConsentText{Version: latest.Version + 1, Body: body, CreatedAt: now.UTC()}, nil
}

// TenantDocument is an ID card copy or lease contract uploaded by a tenant.
// It is private and opened only through short-lived signed URLs.
type TenantDocument struct {
	ID          string
	AccountID   string
	RoomID      string
	Type        DocumentType
	FileKey     string
	ContentType string
	SizeBytes   int64
	UploadedAt  time.Time
	PurgeAt     *time.Time
}

// PurgeTimeAfterUnassign is when documents of an account unbound at t are deleted.
func PurgeTimeAfterUnassign(t time.Time) time.Time { return t.Add(RetentionPeriod).UTC() }

// TenantDocumentsPurged records what was deleted, never the file contents (§6.6).
type TenantDocumentsPurged struct {
	shared.EventBase
	DocumentIDs []string       `json:"documentIds"`
	Types       []DocumentType `json:"types"`
}

func (TenantDocumentsPurged) EventName() string { return "TenantDocumentsPurged" }
