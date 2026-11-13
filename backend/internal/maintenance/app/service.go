// Package app holds the maintenance use cases: tenants report repairs with
// up to 3 photos, the admin moves them forward (context.md §6.6).
package app

import (
	"context"
	"fmt"
	"time"

	"unite-mansion/internal/maintenance/domain"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
)

type Repository interface {
	Insert(ctx context.Context, r *domain.Request) error
	UpdateStatus(ctx context.Context, r *domain.Request) error
	Request(ctx context.Context, id string, forUpdate bool) (*domain.Request, error) // ErrRequestNotFound
	ListByRoom(ctx context.Context, roomID string) ([]*domain.Request, error)
	List(ctx context.Context, status *domain.Status) ([]*domain.Request, error)
}

// RoomDirectory is implemented by tenancy and wired in cmd/api.
type RoomDirectory interface {
	RoomNumbers(ctx context.Context) (map[string]string, error) // room ID → number
}

type EventDispatcher interface {
	Dispatch(ctx context.Context, events ...shared.Event) error
}

type Service struct {
	repo   Repository
	rooms  RoomDirectory
	files  storage.FileStorage
	tx     shared.TxManager
	events EventDispatcher
	now    func() time.Time
	newID  func() string
}

func NewService(repo Repository, rooms RoomDirectory, files storage.FileStorage, tx shared.TxManager,
	events EventDispatcher, now func() time.Time, newID func() string) *Service {
	return &Service{repo: repo, rooms: rooms, files: files, tx: tx, events: events, now: now, newID: newID}
}

// View is a request for display with its room number and signed image URLs.
type View struct {
	Request    *domain.Request
	RoomNumber string
	ImageURLs  []string
}

// CreateUpload presigns one photo upload for the tenant's room.
func (s *Service) CreateUpload(ctx context.Context, roomID, contentType string, size int64) (storage.PresignedRequest, string, error) {
	if err := storage.CheckImage(contentType, size); err != nil {
		return storage.PresignedRequest{}, "", err
	}
	key := storage.ImageKey(imagePrefix(roomID), s.newID(), contentType)
	req, err := s.files.PresignPut(ctx, storage.PutObject{Key: key, ContentType: contentType, ContentLength: size}, storage.UploadURLTTL)
	if err != nil {
		return storage.PresignedRequest{}, "", fmt.Errorf("presign repair photo: %w", err)
	}
	return req, key, nil
}

// Create records a repair request for the tenant's room. Every photo must
// have been uploaded under that room's prefix.
func (s *Service) Create(ctx context.Context, roomID string, category domain.Category, description string, imageKeys []string) (View, error) {
	req, err := domain.NewRequest(s.newID(), roomID, category, description, imageKeys, s.now())
	if err != nil {
		return View{}, err
	}
	for _, key := range imageKeys {
		if _, err := storage.VerifyImageUpload(ctx, s.files, imagePrefix(roomID), key); err != nil {
			return View{}, err
		}
	}
	if err := s.repo.Insert(ctx, req); err != nil {
		return View{}, fmt.Errorf("create maintenance request: %w", err)
	}
	return s.view(ctx, req)
}

func (s *Service) TenantList(ctx context.Context, roomID string) ([]View, error) {
	list, err := s.repo.ListByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("list maintenance requests: %w", err)
	}
	return s.views(ctx, list)
}

// TenantGet returns a request of the tenant's room; another room's request
// is reported as not found.
func (s *Service) TenantGet(ctx context.Context, roomID, id string) (View, error) {
	req, err := s.repo.Request(ctx, id, false)
	if err != nil {
		return View{}, err
	}
	if req.RoomID != roomID {
		return View{}, domain.ErrRequestNotFound
	}
	return s.view(ctx, req)
}

func (s *Service) List(ctx context.Context, status *domain.Status) ([]View, error) {
	list, err := s.repo.List(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("list maintenance requests: %w", err)
	}
	return s.views(ctx, list)
}

func (s *Service) Get(ctx context.Context, id string) (View, error) {
	req, err := s.repo.Request(ctx, id, false)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, req)
}

func (s *Service) ChangeStatus(ctx context.Context, id string, to domain.Status) (View, error) {
	var req *domain.Request
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if req, err = s.repo.Request(ctx, id, true); err != nil {
			return err
		}
		if err := req.ChangeStatus(to, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateStatus(ctx, req); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, req.PullEvents()...)
	})
	if err != nil {
		return View{}, fmt.Errorf("change status of request %s: %w", id, err)
	}
	return s.view(ctx, req)
}

func (s *Service) view(ctx context.Context, r *domain.Request) (View, error) {
	views, err := s.views(ctx, []*domain.Request{r})
	if err != nil {
		return View{}, err
	}
	return views[0], nil
}

func (s *Service) views(ctx context.Context, list []*domain.Request) ([]View, error) {
	numbers, err := s.rooms.RoomNumbers(ctx)
	if err != nil {
		return nil, fmt.Errorf("room numbers: %w", err)
	}
	out := make([]View, 0, len(list))
	for _, r := range list {
		v := View{Request: r, RoomNumber: numbers[r.RoomID], ImageURLs: make([]string, 0, len(r.ImageKeys))}
		for _, key := range r.ImageKeys {
			req, err := s.files.PresignGet(ctx, key, storage.ReadURLTTL)
			if err != nil {
				return nil, fmt.Errorf("sign repair photo: %w", err)
			}
			v.ImageURLs = append(v.ImageURLs, req.URL)
		}
		out = append(out, v)
	}
	return out, nil
}

func imagePrefix(roomID string) string { return "maintenance/" + roomID }
