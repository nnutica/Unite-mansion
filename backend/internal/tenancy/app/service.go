// Package app holds the tenancy use cases: rooms, leases and recurring charges.
package app

import (
	"context"
	"fmt"
	"time"

	"unite-mansion/internal/shared"
	"unite-mansion/internal/tenancy/domain"
)

type Repository interface {
	ListRooms(ctx context.Context) ([]domain.Room, error)
	RoomByNumber(ctx context.Context, number string, forUpdate bool) (domain.Room, error) // ErrRoomNotFound
	RoomByID(ctx context.Context, id string) (domain.Room, error)
	UpdateRoom(ctx context.Context, r domain.Room) error

	LeasesByRoom(ctx context.Context, roomID string) ([]domain.Lease, error)
	LeasesActiveOn(ctx context.Context, date time.Time) ([]domain.Lease, error)
	LeasesEndingBetween(ctx context.Context, from, to time.Time) ([]LeaseWithRoom, error)
	LeaseForUpdate(ctx context.Context, id string) (domain.Lease, error) // ErrLeaseNotFound
	InsertLease(ctx context.Context, l domain.Lease) error               // ErrLeaseOverlap
	UpdateLease(ctx context.Context, l domain.Lease) error               // ErrLeaseOverlap

	ChargesByRoom(ctx context.Context, roomID string) ([]domain.RecurringCharge, error)
	ChargeForUpdate(ctx context.Context, id string) (domain.RecurringCharge, error) // ErrRecurringChargeNotFound
	InsertCharge(ctx context.Context, c domain.RecurringCharge) error
	UpdateCharge(ctx context.Context, c domain.RecurringCharge) error
}

type Service struct {
	repo  Repository
	tx    shared.TxManager
	now   func() time.Time
	newID func() string
}

func NewService(repo Repository, tx shared.TxManager, now func() time.Time, newID func() string) *Service {
	return &Service{repo: repo, tx: tx, now: now, newID: newID}
}

// RoomSummary is a room with the lease in effect today (nil = VACANT).
type RoomSummary struct {
	Room        domain.Room
	ActiveLease *domain.Lease
}

type RoomDetail struct {
	RoomSummary
	Leases  []domain.Lease
	Charges []domain.RecurringCharge
}

func (s *Service) ListRooms(ctx context.Context, floor *int) ([]RoomSummary, error) {
	rooms, err := s.repo.ListRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	active, err := s.repo.LeasesActiveOn(ctx, shared.DateOf(s.now()))
	if err != nil {
		return nil, fmt.Errorf("list active leases: %w", err)
	}
	byRoom := make(map[string]domain.Lease, len(active))
	for _, l := range active {
		byRoom[l.RoomID] = l
	}

	out := make([]RoomSummary, 0, len(rooms))
	for _, r := range rooms {
		if floor != nil && r.Floor != *floor {
			continue
		}
		sum := RoomSummary{Room: r}
		if l, ok := byRoom[r.ID]; ok {
			sum.ActiveLease = &l
		}
		out = append(out, sum)
	}
	return out, nil
}

func (s *Service) GetRoom(ctx context.Context, number string) (RoomDetail, error) {
	room, err := s.repo.RoomByNumber(ctx, number, false)
	if err != nil {
		return RoomDetail{}, fmt.Errorf("get room %s: %w", number, err)
	}
	return s.detail(ctx, room)
}

func (s *Service) UpdateRoom(ctx context.Context, number string, c domain.RoomChanges) (RoomDetail, error) {
	var room domain.Room
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if room, err = s.repo.RoomByNumber(ctx, number, true); err != nil {
			return err
		}
		if err := room.Update(c); err != nil {
			return err
		}
		return s.repo.UpdateRoom(ctx, room)
	})
	if err != nil {
		return RoomDetail{}, fmt.Errorf("update room %s: %w", number, err)
	}
	return s.detail(ctx, room)
}

// CreateLease adds a lease. The room row is locked so two concurrent requests
// cannot both pass the overlap check; the DB exclusion constraint backs it up.
func (s *Service) CreateLease(ctx context.Context, roomNumber, tenantName string, start, end time.Time) (domain.Lease, error) {
	var lease domain.Lease
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		room, err := s.repo.RoomByNumber(ctx, roomNumber, true)
		if err != nil {
			return err
		}
		if lease, err = domain.NewLease(s.newID(), room.ID, tenantName, start, end); err != nil {
			return err
		}
		existing, err := s.repo.LeasesByRoom(ctx, room.ID)
		if err != nil {
			return err
		}
		if err := domain.CheckNoOverlap(lease, existing); err != nil {
			return err
		}
		return s.repo.InsertLease(ctx, lease)
	})
	if err != nil {
		return domain.Lease{}, fmt.Errorf("create lease for room %s: %w", roomNumber, err)
	}
	return lease, nil
}

func (s *Service) UpdateLease(ctx context.Context, id, tenantName string, start, end time.Time) (domain.Lease, error) {
	var lease domain.Lease
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if lease, err = s.repo.LeaseForUpdate(ctx, id); err != nil {
			return err
		}
		if err := lease.Change(tenantName, start, end); err != nil {
			return err
		}
		existing, err := s.repo.LeasesByRoom(ctx, lease.RoomID)
		if err != nil {
			return err
		}
		if err := domain.CheckNoOverlap(lease, existing); err != nil {
			return err
		}
		return s.repo.UpdateLease(ctx, lease)
	})
	if err != nil {
		return domain.Lease{}, fmt.Errorf("update lease %s: %w", id, err)
	}
	return lease, nil
}

func (s *Service) CreateCharge(ctx context.Context, roomNumber, name string, amount shared.Money, active bool) (domain.RecurringCharge, error) {
	room, err := s.repo.RoomByNumber(ctx, roomNumber, false)
	if err != nil {
		return domain.RecurringCharge{}, fmt.Errorf("create charge for room %s: %w", roomNumber, err)
	}
	c, err := domain.NewRecurringCharge(s.newID(), room.ID, name, amount, active)
	if err != nil {
		return domain.RecurringCharge{}, err
	}
	if err := s.repo.InsertCharge(ctx, c); err != nil {
		return domain.RecurringCharge{}, fmt.Errorf("create charge for room %s: %w", roomNumber, err)
	}
	return c, nil
}

func (s *Service) UpdateCharge(ctx context.Context, id string, name *string, amount *shared.Money, active *bool) (domain.RecurringCharge, error) {
	var c domain.RecurringCharge
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.repo.ChargeForUpdate(ctx, id); err != nil {
			return err
		}
		if err := c.Update(name, amount, active); err != nil {
			return err
		}
		return s.repo.UpdateCharge(ctx, c)
	})
	if err != nil {
		return domain.RecurringCharge{}, fmt.Errorf("update charge %s: %w", id, err)
	}
	return c, nil
}

// TenantRoom is the room bound to a tenant account.
func (s *Service) TenantRoom(ctx context.Context, roomID string) (RoomSummary, error) {
	room, err := s.repo.RoomByID(ctx, roomID)
	if err != nil {
		return RoomSummary{}, fmt.Errorf("tenant room: %w", err)
	}
	leases, err := s.repo.LeasesByRoom(ctx, room.ID)
	if err != nil {
		return RoomSummary{}, fmt.Errorf("tenant room leases: %w", err)
	}
	return RoomSummary{Room: room, ActiveLease: domain.ActiveLease(leases, shared.DateOf(s.now()))}, nil
}

// --- queries used by other contexts (wired in cmd/api) ---

func (s *Service) RoomIDByNumber(ctx context.Context, number string) (string, error) {
	room, err := s.repo.RoomByNumber(ctx, number, false)
	if err != nil {
		return "", err
	}
	return room.ID, nil
}

func (s *Service) RoomNumbers(ctx context.Context) (map[string]string, error) {
	rooms, err := s.repo.ListRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("room numbers: %w", err)
	}
	out := make(map[string]string, len(rooms))
	for _, r := range rooms {
		out[r.ID] = r.Number
	}
	return out, nil
}

// BillingInfo is what billing needs to create a draft for a room and period.
type BillingInfo struct {
	Room             domain.Room
	HasLeaseInPeriod bool
	ActiveCharges    []domain.RecurringCharge
}

func (s *Service) BillingInfo(ctx context.Context, roomNumber string, period shared.BillingPeriod) (BillingInfo, error) {
	room, err := s.repo.RoomByNumber(ctx, roomNumber, false)
	if err != nil {
		return BillingInfo{}, err
	}
	leases, err := s.repo.LeasesByRoom(ctx, room.ID)
	if err != nil {
		return BillingInfo{}, fmt.Errorf("leases: %w", err)
	}
	info := BillingInfo{Room: room}
	for _, l := range leases {
		info.HasLeaseInPeriod = info.HasLeaseInPeriod || l.CoversPeriod(period)
	}
	charges, err := s.repo.ChargesByRoom(ctx, room.ID)
	if err != nil {
		return BillingInfo{}, fmt.Errorf("charges: %w", err)
	}
	for _, c := range charges {
		if c.Active {
			info.ActiveCharges = append(info.ActiveCharges, c)
		}
	}
	return info, nil
}

func (s *Service) detail(ctx context.Context, room domain.Room) (RoomDetail, error) {
	leases, err := s.repo.LeasesByRoom(ctx, room.ID)
	if err != nil {
		return RoomDetail{}, fmt.Errorf("leases of room %s: %w", room.Number, err)
	}
	charges, err := s.repo.ChargesByRoom(ctx, room.ID)
	if err != nil {
		return RoomDetail{}, fmt.Errorf("charges of room %s: %w", room.Number, err)
	}
	return RoomDetail{
		RoomSummary: RoomSummary{Room: room, ActiveLease: domain.ActiveLease(leases, shared.DateOf(s.now()))},
		Leases:      leases,
		Charges:     charges,
	}, nil
}

// LeaseWithRoom is a lease with its room number, for alerts.
type LeaseWithRoom struct {
	domain.Lease
	RoomNumber string
}

// LeasesEndingBetween lists leases whose end date is within [from, to]
// (dates), soonest first. Used by communication for expiry alerts.
func (s *Service) LeasesEndingBetween(ctx context.Context, from, to time.Time) ([]LeaseWithRoom, error) {
	list, err := s.repo.LeasesEndingBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("leases ending between: %w", err)
	}
	return list, nil
}

// ActiveLeaseOfRoom returns the lease in effect today for roomID, or nil.
func (s *Service) ActiveLeaseOfRoom(ctx context.Context, roomID string) (*domain.Lease, error) {
	leases, err := s.repo.LeasesByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("active lease: %w", err)
	}
	return domain.ActiveLease(leases, shared.DateOf(s.now())), nil
}
