// Package httpadapter implements the room, lease and recurring-charge
// endpoints of the generated OpenAPI server.
package httpadapter

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"unite-mansion/api"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/shared"
	"unite-mansion/internal/tenancy/app"
	"unite-mansion/internal/tenancy/domain"
)

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrRoomNotFound, Status: http.StatusNotFound, Code: "ROOM_NOT_FOUND"},
		{Err: domain.ErrLeaseNotFound, Status: http.StatusNotFound, Code: "LEASE_NOT_FOUND"},
		{Err: domain.ErrRecurringChargeNotFound, Status: http.StatusNotFound, Code: "RECURRING_CHARGE_NOT_FOUND"},
		{Err: domain.ErrLeaseOverlap, Status: http.StatusConflict, Code: "LEASE_OVERLAP"},
		{Err: domain.ErrInvalidInput, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
	}
}

// RoomAccount is the account bound to a room, as shown in the room list.
type RoomAccount struct {
	ID           string
	Username     string
	FullName     string
	Phone        string
	RegisteredAt time.Time
}

// AccountDirectory is implemented by identity and wired in cmd/api.
type AccountDirectory interface {
	ActiveAccountsByRoom(ctx context.Context) (map[string]RoomAccount, error) // keyed by room ID
}

// ExpiryDirectory is implemented by communication (it owns the alert window).
type ExpiryDirectory interface {
	ExpiringRoomNumbers(ctx context.Context) (map[string]bool, error)
}

type Handler struct {
	svc      *app.Service
	accounts AccountDirectory
	expiry   ExpiryDirectory
}

func NewHandler(svc *app.Service, accounts AccountDirectory, expiry ExpiryDirectory) *Handler {
	return &Handler{svc: svc, accounts: accounts, expiry: expiry}
}

func (h *Handler) ListRooms(ctx context.Context, req api.ListRoomsRequestObject) (api.ListRoomsResponseObject, error) {
	rooms, err := h.svc.ListRooms(ctx, req.Params.Floor)
	if err != nil {
		return nil, err
	}
	var expiring map[string]bool
	if req.Params.LeaseExpiring != nil && *req.Params.LeaseExpiring {
		if expiring, err = h.expiry.ExpiringRoomNumbers(ctx); err != nil {
			return nil, err
		}
	}
	accounts, err := h.accounts.ActiveAccountsByRoom(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListRooms200JSONResponse, 0, len(rooms))
	for _, r := range rooms {
		if expiring != nil && !expiring[r.Room.Number] {
			continue
		}
		out = append(out, toRoom(r, accounts))
	}
	return out, nil
}

func (h *Handler) GetRoom(ctx context.Context, req api.GetRoomRequestObject) (api.GetRoomResponseObject, error) {
	d, err := h.svc.GetRoom(ctx, req.RoomNumber)
	if err != nil {
		return nil, err
	}
	detail, err := h.roomDetail(ctx, d)
	if err != nil {
		return nil, err
	}
	return api.GetRoom200JSONResponse(detail), nil
}

func (h *Handler) UpdateRoom(ctx context.Context, req api.UpdateRoomRequestObject) (api.UpdateRoomResponseObject, error) {
	changes := domain.RoomChanges{ElectricMeterDigits: req.Body.ElectricMeterDigits, WaterMeterDigits: req.Body.WaterMeterDigits}
	if req.Body.BasePrice != nil {
		price, err := shared.ParseMoney(*req.Body.BasePrice)
		if err != nil {
			return nil, err
		}
		changes.BasePrice = &price
	}
	d, err := h.svc.UpdateRoom(ctx, req.RoomNumber, changes)
	if err != nil {
		return nil, err
	}
	detail, err := h.roomDetail(ctx, d)
	if err != nil {
		return nil, err
	}
	return api.UpdateRoom200JSONResponse(detail), nil
}

func (h *Handler) CreateLease(ctx context.Context, req api.CreateLeaseRequestObject) (api.CreateLeaseResponseObject, error) {
	l, err := h.svc.CreateLease(ctx, req.RoomNumber, req.Body.TenantName, req.Body.StartDate.Time, req.Body.EndDate.Time)
	if err != nil {
		return nil, err
	}
	return api.CreateLease201JSONResponse(toLease(l)), nil
}

func (h *Handler) UpdateLease(ctx context.Context, req api.UpdateLeaseRequestObject) (api.UpdateLeaseResponseObject, error) {
	l, err := h.svc.UpdateLease(ctx, req.LeaseId.String(), req.Body.TenantName, req.Body.StartDate.Time, req.Body.EndDate.Time)
	if err != nil {
		return nil, err
	}
	return api.UpdateLease200JSONResponse(toLease(l)), nil
}

func (h *Handler) CreateRecurringCharge(ctx context.Context, req api.CreateRecurringChargeRequestObject) (api.CreateRecurringChargeResponseObject, error) {
	amount, err := shared.ParseMoney(req.Body.Amount)
	if err != nil {
		return nil, err
	}
	active := true
	if req.Body.Active != nil {
		active = *req.Body.Active
	}
	c, err := h.svc.CreateCharge(ctx, req.RoomNumber, req.Body.Name, amount, active)
	if err != nil {
		return nil, err
	}
	return api.CreateRecurringCharge201JSONResponse(toCharge(c)), nil
}

func (h *Handler) UpdateRecurringCharge(ctx context.Context, req api.UpdateRecurringChargeRequestObject) (api.UpdateRecurringChargeResponseObject, error) {
	var amount *shared.Money
	if req.Body.Amount != nil {
		m, err := shared.ParseMoney(*req.Body.Amount)
		if err != nil {
			return nil, err
		}
		amount = &m
	}
	c, err := h.svc.UpdateCharge(ctx, req.ChargeId.String(), req.Body.Name, amount, req.Body.Active)
	if err != nil {
		return nil, err
	}
	return api.UpdateRecurringCharge200JSONResponse(toCharge(c)), nil
}

// GetTenantRoom returns only the room bound to the caller's account; the room
// ID comes from the server-side session, never from the request (§6.5).
func (h *Handler) GetTenantRoom(ctx context.Context, _ api.GetTenantRoomRequestObject) (api.GetTenantRoomResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.IsActiveTenant() {
		return nil, auth.ErrAccountNotActive
	}
	r, err := h.svc.TenantRoom(ctx, p.RoomID)
	if err != nil {
		return nil, err
	}
	out := api.TenantRoom{RoomNumber: r.Room.Number, Floor: r.Room.Floor}
	if r.ActiveLease != nil {
		l := toLease(*r.ActiveLease)
		out.ActiveLease = &l
	}
	return api.GetTenantRoom200JSONResponse(out), nil
}

func (h *Handler) roomDetail(ctx context.Context, d app.RoomDetail) (api.RoomDetail, error) {
	accounts, err := h.accounts.ActiveAccountsByRoom(ctx)
	if err != nil {
		return api.RoomDetail{}, err
	}
	r := toRoom(d.RoomSummary, accounts)
	out := api.RoomDetail{
		RoomNumber: r.RoomNumber, Floor: r.Floor, BasePrice: r.BasePrice,
		ElectricMeterDigits: r.ElectricMeterDigits, WaterMeterDigits: r.WaterMeterDigits,
		Status: r.Status, Account: r.Account, ActiveLease: r.ActiveLease,
		Leases:           make([]api.Lease, 0, len(d.Leases)),
		RecurringCharges: make([]api.RecurringCharge, 0, len(d.Charges)),
	}
	for _, l := range d.Leases {
		out.Leases = append(out.Leases, toLease(l))
	}
	for _, c := range d.Charges {
		out.RecurringCharges = append(out.RecurringCharges, toCharge(c))
	}
	return out, nil
}

func toRoom(s app.RoomSummary, accounts map[string]RoomAccount) api.Room {
	r := api.Room{
		RoomNumber:          s.Room.Number,
		Floor:               s.Room.Floor,
		BasePrice:           s.Room.BasePrice.String(),
		ElectricMeterDigits: s.Room.ElectricMeterDigits,
		WaterMeterDigits:    s.Room.WaterMeterDigits,
		Status:              api.RoomStatusVACANT,
	}
	if s.ActiveLease != nil {
		l := toLease(*s.ActiveLease)
		r.ActiveLease = &l
		r.Status = api.RoomStatusOCCUPIED
	}
	if a, ok := accounts[s.Room.ID]; ok {
		r.Account = &api.RoomAccount{
			Id: uuid.MustParse(a.ID), Username: a.Username, FullName: a.FullName, Phone: a.Phone, RegisteredAt: a.RegisteredAt,
		}
	}
	return r
}

func toLease(l domain.Lease) api.Lease {
	return api.Lease{
		Id:         uuid.MustParse(l.ID),
		TenantName: l.TenantName,
		StartDate:  openapi_types.Date{Time: l.StartDate},
		EndDate:    openapi_types.Date{Time: l.EndDate},
	}
}

func toCharge(c domain.RecurringCharge) api.RecurringCharge {
	return api.RecurringCharge{Id: uuid.MustParse(c.ID), Name: c.Name, Amount: c.Amount.String(), Active: c.Active}
}
