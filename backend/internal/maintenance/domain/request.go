// Package domain holds the maintenance aggregate: MaintenanceRequest
// (context.md §6.6).
// Imports: standard library and internal/shared only.
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
	ErrRequestNotFound         = errors.New("maintenance request not found")
	ErrInvalidInput            = errors.New("invalid input")
	ErrTooManyImages           = errors.New("too many images")
	ErrInvalidStatusTransition = errors.New("maintenance status can only move forward")
)

// MaxImages per request (§6.6).
const MaxImages = 3

const maxDescription = 1000

type Category string

const (
	CategoryWater          Category = "WATER"
	CategoryElectricity    Category = "ELECTRICITY"
	CategoryAirConditioner Category = "AIR_CONDITIONER"
	CategoryFurniture      Category = "FURNITURE"
)

func (c Category) Valid() bool {
	switch c {
	case CategoryWater, CategoryElectricity, CategoryAirConditioner, CategoryFurniture:
		return true
	}
	return false
}

type Status string

const (
	StatusNew                  Status = "NEW"
	StatusCoordinating         Status = "COORDINATING"
	StatusTechnicianDispatched Status = "TECHNICIAN_DISPATCHED"
	StatusCompleted            Status = "COMPLETED"
)

// order defines the forward direction NEW → COORDINATING →
// TECHNICIAN_DISPATCHED → COMPLETED.
var order = map[Status]int{
	StatusNew:                  0,
	StatusCoordinating:         1,
	StatusTechnicianDispatched: 2,
	StatusCompleted:            3,
}

// Request is a repair request reported by a tenant for their room.
type Request struct {
	shared.EventRecorder

	ID          string
	RoomID      string
	Category    Category
	Description string
	Status      Status
	ImageKeys   []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewRequest(id, roomID string, category Category, description string, imageKeys []string, now time.Time) (*Request, error) {
	if !category.Valid() {
		return nil, fmt.Errorf("%w: category %q", ErrInvalidInput, category)
	}
	description = strings.TrimSpace(description)
	if n := utf8.RuneCountInString(description); n < 1 || n > maxDescription {
		return nil, fmt.Errorf("%w: description must be 1–%d characters", ErrInvalidInput, maxDescription)
	}
	if len(imageKeys) > MaxImages {
		return nil, fmt.Errorf("%w: at most %d images", ErrTooManyImages, MaxImages)
	}
	seen := map[string]bool{}
	for _, k := range imageKeys {
		if k == "" || seen[k] {
			return nil, fmt.Errorf("%w: image keys must be unique and non-empty", ErrInvalidInput)
		}
		seen[k] = true
	}
	at := now.UTC()
	return &Request{
		ID: id, RoomID: roomID, Category: category, Description: description, Status: StatusNew,
		ImageKeys: append([]string(nil), imageKeys...), CreatedAt: at, UpdatedAt: at,
	}, nil
}

// ChangeStatus moves the request forward; steps may be skipped (e.g. NEW →
// COMPLETED) but it never goes back or stays put (§6.6).
func (r *Request) ChangeStatus(to Status, now time.Time) error {
	toOrder, ok := order[to]
	if !ok {
		return fmt.Errorf("%w: status %q", ErrInvalidInput, to)
	}
	if toOrder <= order[r.Status] {
		return fmt.Errorf("%w: %s → %s", ErrInvalidStatusTransition, r.Status, to)
	}
	from := r.Status
	r.Status = to
	r.UpdatedAt = now.UTC()
	r.Record(MaintenanceStatusChanged{
		EventBase: shared.NewEventBase(r.ID, now), RoomID: r.RoomID, From: from, To: to,
	})
	return nil
}

type MaintenanceStatusChanged struct {
	shared.EventBase
	RoomID string `json:"roomId"`
	From   Status `json:"from"`
	To     Status `json:"to"`
}

func (MaintenanceStatusChanged) EventName() string { return "MaintenanceStatusChanged" }
