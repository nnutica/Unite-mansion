// Package httpadapter implements the auth and tenant-account endpoints of the
// generated OpenAPI server.
package httpadapter

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"unite-mansion/api"
	"unite-mansion/internal/identity/app"
	"unite-mansion/internal/identity/domain"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
)

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrInvalidCredentials, Status: http.StatusUnauthorized, Code: "INVALID_CREDENTIALS"},
		{Err: domain.ErrUsernameTaken, Status: http.StatusConflict, Code: "USERNAME_TAKEN"},
		{Err: domain.ErrAccountNotFound, Status: http.StatusNotFound, Code: "ACCOUNT_NOT_FOUND"},
		{Err: domain.ErrInvalidAccountStatus, Status: http.StatusConflict, Code: "INVALID_ACCOUNT_STATUS"},
		{Err: domain.ErrRoomAlreadyAssigned, Status: http.StatusConflict, Code: "ROOM_ALREADY_ASSIGNED"},
		{Err: domain.ErrInvalidInput, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
		{Err: app.ErrRoomNotFound, Status: http.StatusNotFound, Code: "ROOM_NOT_FOUND"},
		{Err: app.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"},
	}
}

type Handler struct {
	svc          *app.Service
	secureCookie bool
}

// NewHandler: secureCookie adds the Secure attribute (everything but local).
func NewHandler(svc *app.Service, secureCookie bool) *Handler {
	return &Handler{svc: svc, secureCookie: secureCookie}
}

func (h *Handler) AdminLogin(ctx context.Context, req api.AdminLoginRequestObject) (api.AdminLoginResponseObject, error) {
	res, err := h.svc.AdminLogin(ctx, req.Body.Username, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return api.AdminLogin200JSONResponse{LoggedInJSONResponse: h.loggedIn(res)}, nil
}

func (h *Handler) TenantLogin(ctx context.Context, req api.TenantLoginRequestObject) (api.TenantLoginResponseObject, error) {
	res, err := h.svc.TenantLogin(ctx, req.Body.Username, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return api.TenantLogin200JSONResponse{LoggedInJSONResponse: h.loggedIn(res)}, nil
}

func (h *Handler) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	if err := h.svc.Logout(ctx, p.SessionHash); err != nil {
		return nil, err
	}
	cookie := auth.ClearCookie(h.secureCookie)
	return api.Logout204Response{Headers: api.Logout204ResponseHeaders{SetCookie: &cookie}}, nil
}

func (h *Handler) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	me, err := h.svc.Current(ctx, domain.SubjectType(p.Role), p.AccountID)
	if err != nil {
		return nil, err
	}
	return api.GetMe200JSONResponse(toMe(me)), nil
}

func (h *Handler) RegisterTenant(ctx context.Context, req api.RegisterTenantRequestObject) (api.RegisterTenantResponseObject, error) {
	acc, err := h.svc.Register(ctx, req.Body.Username, req.Body.Password, req.Body.FullName, req.Body.Phone)
	if err != nil {
		return nil, err
	}
	return api.RegisterTenant201JSONResponse(toAccount(acc)), nil
}

func (h *Handler) ListTenantAccounts(ctx context.Context, req api.ListTenantAccountsRequestObject) (api.ListTenantAccountsResponseObject, error) {
	var status *domain.AccountStatus
	if req.Params.Status != nil {
		s := domain.AccountStatus(*req.Params.Status)
		status = &s
	}
	accounts, err := h.svc.ListTenantAccounts(ctx, status)
	if err != nil {
		return nil, err
	}
	out := make(api.ListTenantAccounts200JSONResponse, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toAccount(a))
	}
	return out, nil
}

func (h *Handler) AssignRoom(ctx context.Context, req api.AssignRoomRequestObject) (api.AssignRoomResponseObject, error) {
	acc, err := h.svc.AssignRoom(ctx, req.AccountId.String(), req.Body.RoomNumber)
	if err != nil {
		return nil, err
	}
	return api.AssignRoom200JSONResponse(toAccount(acc)), nil
}

func (h *Handler) UnassignRoom(ctx context.Context, req api.UnassignRoomRequestObject) (api.UnassignRoomResponseObject, error) {
	acc, err := h.svc.Unassign(ctx, req.AccountId.String())
	if err != nil {
		return nil, err
	}
	return api.UnassignRoom200JSONResponse(toAccount(acc)), nil
}

func (h *Handler) ResetTenantPassword(ctx context.Context, req api.ResetTenantPasswordRequestObject) (api.ResetTenantPasswordResponseObject, error) {
	if err := h.svc.ResetPassword(ctx, req.AccountId.String(), req.Body.Password); err != nil {
		return nil, err
	}
	return api.ResetTenantPassword204Response{}, nil
}

func (h *Handler) loggedIn(res app.LoginResult) api.LoggedInJSONResponse {
	cookie := auth.SessionCookie(res.Token, res.ExpiresAt, h.secureCookie)
	return api.LoggedInJSONResponse{Body: toMe(res.Me), Headers: api.LoggedInResponseHeaders{SetCookie: &cookie}}
}

// Authenticator adapts the identity service to platform/auth.
type Authenticator struct {
	Svc *app.Service
}

func (a Authenticator) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
	me, hash, err := a.Svc.Authenticate(ctx, token)
	if errors.Is(err, app.ErrUnauthenticated) {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	if err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{
		Role:         auth.Role(me.Role),
		AccountID:    me.ID,
		Username:     me.Username,
		TenantStatus: string(me.Status),
		RoomID:       me.RoomID,
		SessionHash:  hash,
	}, nil
}

func toMe(me app.Me) api.Me {
	out := api.Me{Id: uuid.MustParse(me.ID), Role: api.Role(me.Role), Username: me.Username}
	if me.Role == domain.SubjectTenant {
		status := api.AccountStatus(me.Status)
		fullName := me.FullName
		out.Status, out.FullName = &status, &fullName
		if me.RoomNumber != "" {
			room := me.RoomNumber
			out.RoomNumber = &room
		}
	}
	return out
}

func toAccount(a *domain.TenantAccount) api.TenantAccount {
	return api.TenantAccount{
		Id:           uuid.MustParse(a.ID),
		Username:     a.Username,
		FullName:     a.FullName,
		Phone:        a.Phone,
		Status:       api.AccountStatus(a.Status),
		RoomNumber:   a.RoomNumber,
		RegisteredAt: a.CreatedAt,
		UnassignedAt: a.UnassignedAt,
	}
}
