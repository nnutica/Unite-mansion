package main

import (
	"context"
	"errors"
	"time"

	billingapp "unite-mansion/internal/billing/app"
	billingdomain "unite-mansion/internal/billing/domain"
	commapp "unite-mansion/internal/communication/app"
	identityapp "unite-mansion/internal/identity/app"
	"unite-mansion/internal/shared"
	tenancyhttp "unite-mansion/internal/tenancy/adapter/http"
	tenancyapp "unite-mansion/internal/tenancy/app"
	tenancydomain "unite-mansion/internal/tenancy/domain"
)

// Cross-context ports (context.md §4): each consumer declares the interface it
// needs and these adapters implement it on top of the provider's service,
// translating types and errors so no context imports another's domain.

// identityRooms implements identityapp.RoomDirectory.
type identityRooms struct{ tenancy *tenancyapp.Service }

func (b identityRooms) RoomIDByNumber(ctx context.Context, number string) (string, error) {
	id, err := b.tenancy.RoomIDByNumber(ctx, number)
	if errors.Is(err, tenancydomain.ErrRoomNotFound) {
		return "", identityapp.ErrRoomNotFound
	}
	return id, err
}

func (b identityRooms) RoomNumbers(ctx context.Context) (map[string]string, error) {
	return b.tenancy.RoomNumbers(ctx)
}

// tenancyAccounts implements tenancyhttp.AccountDirectory.
type tenancyAccounts struct{ identity *identityapp.Service }

func (b tenancyAccounts) ActiveAccountsByRoom(ctx context.Context) (map[string]tenancyhttp.RoomAccount, error) {
	byRoom, err := b.identity.ActiveAccountsByRoom(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]tenancyhttp.RoomAccount, len(byRoom))
	for roomID, a := range byRoom {
		out[roomID] = tenancyhttp.RoomAccount{
			ID: a.ID, Username: a.Username, FullName: a.FullName, Phone: a.Phone, RegisteredAt: a.CreatedAt,
		}
	}
	return out, nil
}

// billingTenancy implements billingapp.TenancyReader.
type billingTenancy struct{ tenancy *tenancyapp.Service }

func (b billingTenancy) BillingInfo(ctx context.Context, roomNumber string, period shared.BillingPeriod) (billingapp.RoomBillingInfo, error) {
	info, err := b.tenancy.BillingInfo(ctx, roomNumber, period)
	if errors.Is(err, tenancydomain.ErrRoomNotFound) {
		return billingapp.RoomBillingInfo{}, billingapp.ErrRoomNotFound
	}
	if err != nil {
		return billingapp.RoomBillingInfo{}, err
	}
	out := billingapp.RoomBillingInfo{
		RoomID:           info.Room.ID,
		RoomNumber:       info.Room.Number,
		Rent:             info.Room.BasePrice,
		ElectricDigits:   info.Room.ElectricMeterDigits,
		WaterDigits:      info.Room.WaterMeterDigits,
		HasLeaseInPeriod: info.HasLeaseInPeriod,
	}
	for _, c := range info.ActiveCharges {
		out.Recurring = append(out.Recurring, billingdomain.RecurringItem{Name: c.Name, Amount: c.Amount})
	}
	return out, nil
}

func (b billingTenancy) RoomIDByNumber(ctx context.Context, roomNumber string) (string, error) {
	id, err := b.tenancy.RoomIDByNumber(ctx, roomNumber)
	if errors.Is(err, tenancydomain.ErrRoomNotFound) {
		return "", billingapp.ErrRoomNotFound
	}
	return id, err
}

// communicationLeases implements commapp.LeaseReader.
type communicationLeases struct{ tenancy *tenancyapp.Service }

func (b communicationLeases) LeasesEndingBetween(ctx context.Context, from, to time.Time) ([]commapp.Lease, error) {
	list, err := b.tenancy.LeasesEndingBetween(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]commapp.Lease, 0, len(list))
	for _, l := range list {
		out = append(out, commapp.Lease{ID: l.ID, RoomNumber: l.RoomNumber, TenantName: l.TenantName, StartDate: l.StartDate, EndDate: l.EndDate})
	}
	return out, nil
}

func (b communicationLeases) ActiveLeaseOfRoom(ctx context.Context, roomID string) (*commapp.Lease, error) {
	l, err := b.tenancy.ActiveLeaseOfRoom(ctx, roomID)
	if err != nil || l == nil {
		return nil, err
	}
	return &commapp.Lease{ID: l.ID, TenantName: l.TenantName, StartDate: l.StartDate, EndDate: l.EndDate}, nil
}

// tenancyUsernames implements tenancyapp.UsernameDirectory.
type tenancyUsernames struct{ identity *identityapp.Service }

func (b tenancyUsernames) Usernames(ctx context.Context) (map[string]string, error) {
	accounts, err := b.identity.ListTenantAccounts(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(accounts))
	for _, a := range accounts {
		out[a.ID] = a.Username
	}
	return out, nil
}
