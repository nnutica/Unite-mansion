// Package httpadapter implements the announcement, settings and lease-alert
// endpoints of the generated OpenAPI server.
package httpadapter

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"unite-mansion/api"
	"unite-mansion/internal/communication/app"
	"unite-mansion/internal/communication/domain"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
)

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrPaymentQRNotSet, Status: http.StatusNotFound, Code: "PAYMENT_QR_NOT_SET"},
		{Err: domain.ErrInvalidInput, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
	}
}

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) ListAnnouncements(ctx context.Context, _ api.ListAnnouncementsRequestObject) (api.ListAnnouncementsResponseObject, error) {
	list, err := h.announcements(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListAnnouncements200JSONResponse(list), nil
}

func (h *Handler) ListTenantAnnouncements(ctx context.Context, _ api.ListTenantAnnouncementsRequestObject) (api.ListTenantAnnouncementsResponseObject, error) {
	list, err := h.announcements(ctx)
	if err != nil {
		return nil, err
	}
	return api.ListTenantAnnouncements200JSONResponse(list), nil
}

func (h *Handler) GetSettings(ctx context.Context, _ api.GetSettingsRequestObject) (api.GetSettingsResponseObject, error) {
	v, err := h.svc.Settings(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetSettings200JSONResponse{SettingsJSONResponse: toSettings(v)}, nil
}

func (h *Handler) UpdateSettings(ctx context.Context, req api.UpdateSettingsRequestObject) (api.UpdateSettingsResponseObject, error) {
	v, err := h.svc.UpdateAlertDays(ctx, req.Body.LeaseExpiryAlertDays)
	if err != nil {
		return nil, err
	}
	return api.UpdateSettings200JSONResponse{SettingsJSONResponse: toSettings(v)}, nil
}

func (h *Handler) CreatePaymentQrUploadUrl(ctx context.Context, req api.CreatePaymentQrUploadUrlRequestObject) (api.CreatePaymentQrUploadUrlResponseObject, error) {
	p, key, err := h.svc.CreatePaymentQRUpload(ctx, string(req.Body.ContentType), req.Body.SizeBytes)
	if err != nil {
		return nil, err
	}
	return api.CreatePaymentQrUploadUrl200JSONResponse{UploadUrlJSONResponse: api.UploadUrlJSONResponse{
		UploadUrl: p.URL, Method: p.Method, Headers: p.Headers, FileKey: key, ExpiresAt: p.ExpiresAt,
	}}, nil
}

func (h *Handler) SetPaymentQr(ctx context.Context, req api.SetPaymentQrRequestObject) (api.SetPaymentQrResponseObject, error) {
	v, err := h.svc.SetPaymentQR(ctx, req.Body.FileKey)
	if err != nil {
		return nil, err
	}
	return api.SetPaymentQr200JSONResponse{SettingsJSONResponse: toSettings(v)}, nil
}

func (h *Handler) GetPaymentQr(ctx context.Context, _ api.GetPaymentQrRequestObject) (api.GetPaymentQrResponseObject, error) {
	url, err := h.svc.PaymentQRURL(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetPaymentQr200JSONResponse{ImageUrl: url}, nil
}

func (h *Handler) ListExpiringLeases(ctx context.Context, _ api.ListExpiringLeasesRequestObject) (api.ListExpiringLeasesResponseObject, error) {
	list, err := h.svc.ExpiringLeases(ctx)
	if err != nil {
		return nil, err
	}
	out := make(api.ListExpiringLeases200JSONResponse, 0, len(list))
	for _, l := range list {
		out = append(out, api.ExpiringLease{
			RoomNumber: l.RoomNumber,
			DaysLeft:   l.DaysLeft,
			Lease: api.Lease{
				Id: uuid.MustParse(l.ID), TenantName: l.TenantName,
				StartDate: openapi_types.Date{Time: l.StartDate}, EndDate: openapi_types.Date{Time: l.EndDate},
			},
		})
	}
	return out, nil
}

func (h *Handler) GetTenantLeaseAlert(ctx context.Context, _ api.GetTenantLeaseAlertRequestObject) (api.GetTenantLeaseAlertResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.IsActiveTenant() {
		return nil, auth.ErrAccountNotActive
	}
	a, err := h.svc.TenantLeaseAlert(ctx, p.RoomID)
	if err != nil {
		return nil, err
	}
	out := api.LeaseAlert{Expiring: a.Expiring, DaysLeft: a.DaysLeft}
	if a.EndDate != nil {
		out.EndDate = &openapi_types.Date{Time: *a.EndDate}
	}
	return api.GetTenantLeaseAlert200JSONResponse(out), nil
}

func (h *Handler) announcements(ctx context.Context) ([]api.Announcement, error) {
	list, err := h.svc.Announcements(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.Announcement, 0, len(list))
	for _, a := range list {
		out = append(out, api.Announcement{Id: uuid.MustParse(a.ID), Title: a.Title, Message: a.Message, CreatedAt: a.CreatedAt})
	}
	return out, nil
}

func toSettings(v app.SettingsView) api.SettingsJSONResponse {
	return api.SettingsJSONResponse{LeaseExpiryAlertDays: v.LeaseExpiryAlertDays, PaymentQrUrl: v.PaymentQRURL}
}
