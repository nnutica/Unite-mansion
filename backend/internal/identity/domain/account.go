// Package domain holds the identity aggregates: Admin, TenantAccount and the
// session policy (context.md §6.5).
// Imports: standard library and internal/shared only.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"unite-mansion/internal/shared"
)

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrInvalidCredentials   = errors.New("invalid username or password")
	ErrUsernameTaken        = errors.New("username is already taken")
	ErrAccountNotFound      = errors.New("tenant account not found")
	ErrInvalidAccountStatus = errors.New("operation not allowed in the account's current status")
	ErrRoomAlreadyAssigned  = errors.New("room already has an active account")
)

type AccountStatus string

const (
	StatusPending  AccountStatus = "PENDING"
	StatusActive   AccountStatus = "ACTIVE"
	StatusInactive AccountStatus = "INACTIVE"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)
var phonePattern = regexp.MustCompile(`^[0-9]{9,10}$`)

// NormalizeUsername lower-cases and trims; usernames are case-insensitive.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func ValidateUsername(s string) error {
	if !usernamePattern.MatchString(s) {
		return fmt.Errorf("%w: username must be 3–32 characters of a–z 0–9 . _ -", ErrInvalidInput)
	}
	return nil
}

func ValidatePassword(s string) error {
	if n := utf8.RuneCountInString(s); n < 8 || n > 128 {
		return fmt.Errorf("%w: password must be 8–128 characters", ErrInvalidInput)
	}
	return nil
}

// TenantAccount is one account per room, shared by roommates (§6.5).
type TenantAccount struct {
	shared.EventRecorder

	ID           string
	Username     string
	PasswordHash string
	FullName     string
	Phone        string
	Status       AccountStatus
	RoomID       *string
	RoomNumber   *string // read-side convenience, not persisted here
	UnassignedAt *time.Time
	CreatedAt    time.Time
}

// NewTenantAccount registers a PENDING account. username must already be
// normalized and validated; the password hash is computed by the caller.
func NewTenantAccount(id, username, passwordHash, fullName, phone string, now time.Time) (*TenantAccount, error) {
	fullName = strings.TrimSpace(fullName)
	phone = strings.TrimSpace(phone)
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if n := utf8.RuneCountInString(fullName); n < 1 || n > 100 {
		return nil, fmt.Errorf("%w: full name must be 1–100 characters", ErrInvalidInput)
	}
	if !phonePattern.MatchString(phone) {
		return nil, fmt.Errorf("%w: phone must be 9–10 digits", ErrInvalidInput)
	}
	return &TenantAccount{
		ID: id, Username: username, PasswordHash: passwordHash,
		FullName: fullName, Phone: phone, Status: StatusPending, CreatedAt: now.UTC(),
	}, nil
}

// AssignRoom binds the account to a room: PENDING or INACTIVE → ACTIVE.
// Re-binding an INACTIVE account cancels its document deletion (§6.6).
func (a *TenantAccount) AssignRoom(roomID, roomNumber string, now time.Time) error {
	if a.Status != StatusPending && a.Status != StatusInactive {
		return fmt.Errorf("%w: cannot assign a room to a %s account", ErrInvalidAccountStatus, a.Status)
	}
	a.Status = StatusActive
	a.RoomID = &roomID
	a.RoomNumber = &roomNumber
	a.UnassignedAt = nil
	a.Record(AccountAssignedToRoom{
		EventBase:  shared.NewEventBase(a.ID, now),
		Username:   a.Username,
		RoomID:     roomID,
		RoomNumber: roomNumber,
	})
	return nil
}

// Unassign unbinds the account when the tenant moves out: ACTIVE → INACTIVE.
func (a *TenantAccount) Unassign(now time.Time) error {
	if a.Status != StatusActive || a.RoomID == nil {
		return fmt.Errorf("%w: cannot unassign a %s account", ErrInvalidAccountStatus, a.Status)
	}
	ev := AccountUnassigned{
		EventBase: shared.NewEventBase(a.ID, now),
		Username:  a.Username,
		RoomID:    *a.RoomID,
	}
	if a.RoomNumber != nil {
		ev.RoomNumber = *a.RoomNumber
	}
	at := now.UTC()
	a.Status = StatusInactive
	a.RoomID = nil
	a.RoomNumber = nil
	a.UnassignedAt = &at
	a.Record(ev)
	return nil
}

type AccountAssignedToRoom struct {
	shared.EventBase
	Username   string `json:"username"`
	RoomID     string `json:"roomId"`
	RoomNumber string `json:"roomNumber"`
}

func (AccountAssignedToRoom) EventName() string { return "AccountAssignedToRoom" }

type AccountUnassigned struct {
	shared.EventBase
	Username   string `json:"username"`
	RoomID     string `json:"roomId"`
	RoomNumber string `json:"roomNumber,omitempty"`
}

func (AccountUnassigned) EventName() string { return "AccountUnassigned" }

// Admin is the single owner account (§6.5).
type Admin struct {
	ID           string
	Username     string
	PasswordHash string
}
