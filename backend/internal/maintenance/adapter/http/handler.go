// Package httpadapter implements the maintenance endpoints of the generated
// OpenAPI server.
package httpadapter

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"unite-mansion/api"
	"unite-mansion/internal/maintenance/app"
	"unite-mansion/internal/maintenance/domain"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/platform/storage"
)

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrRequestNotFound, Status: http.StatusNotFound, Code: "MAINTENANCE_REQUEST_NOT_FOUND"},
		{Err: domain.ErrTooManyImages, Status: http.StatusUnprocessableEntity, Code: "TOO_MANY_IMAGES"},
		{Err: domain.ErrInvalidStatusTransition, Status: http.StatusConflict, Code: "INVALID_STATUS_TRANSITION"},
		{Err: domain.ErrInvalidInput, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
	}
}

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) CreateMaintenanceUploadUrl(ctx context.Context, req api.CreateMaintenanceUploadUrlRequestObject) (api.CreateMaintenanceUploadUrlResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	p, key, err := h.svc.CreateUpload(ctx, roomID, string(req.Body.ContentType), req.Body.SizeBytes)
	if err != nil {
		return nil, err
	}
	return api.CreateMaintenanceUploadUrl200JSONResponse{UploadUrlJSONResponse: toUploadURL(p, key)}, nil
}

func (h *Handler) CreateMaintenanceRequest(ctx context.Context, req api.CreateMaintenanceRequestRequestObject) (api.CreateMaintenanceRequestResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	if req.Body.ImageKeys != nil {
		keys = *req.Body.ImageKeys
	}
	v, err := h.svc.Create(ctx, roomID, domain.Category(req.Body.Category), req.Body.Description, keys)
	if err != nil {
		return nil, err
	}
	return api.CreateMaintenanceRequest201JSONResponse{MaintenanceRequestJSONResponse: toRequest(v)}, nil
}

func (h *Handler) ListTenantMaintenanceRequests(ctx context.Context, _ api.ListTenantMaintenanceRequestsRequestObject) (api.ListTenantMaintenanceRequestsResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.svc.TenantList(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make(api.ListTenantMaintenanceRequests200JSONResponse, 0, len(list))
	for _, v := range list {
		out = append(out, api.MaintenanceRequest(toRequest(v)))
	}
	return out, nil
}

func (h *Handler) GetTenantMaintenanceRequest(ctx context.Context, req api.GetTenantMaintenanceRequestRequestObject) (api.GetTenantMaintenanceRequestResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.TenantGet(ctx, roomID, req.RequestId.String())
	if err != nil {
		return nil, err
	}
	return api.GetTenantMaintenanceRequest200JSONResponse{MaintenanceRequestJSONResponse: toRequest(v)}, nil
}

func (h *Handler) ListMaintenanceRequests(ctx context.Context, req api.ListMaintenanceRequestsRequestObject) (api.ListMaintenanceRequestsResponseObject, error) {
	var status *domain.Status
	if req.Params.Status != nil {
		s := domain.Status(*req.Params.Status)
		status = &s
	}
	list, err := h.svc.List(ctx, status)
	if err != nil {
		return nil, err
	}
	out := make(api.ListMaintenanceRequests200JSONResponse, 0, len(list))
	for _, v := range list {
		out = append(out, api.MaintenanceRequest(toRequest(v)))
	}
	return out, nil
}

func (h *Handler) GetMaintenanceRequest(ctx context.Context, req api.GetMaintenanceRequestRequestObject) (api.GetMaintenanceRequestResponseObject, error) {
	v, err := h.svc.Get(ctx, req.RequestId.String())
	if err != nil {
		return nil, err
	}
	return api.GetMaintenanceRequest200JSONResponse{MaintenanceRequestJSONResponse: toRequest(v)}, nil
}

func (h *Handler) UpdateMaintenanceStatus(ctx context.Context, req api.UpdateMaintenanceStatusRequestObject) (api.UpdateMaintenanceStatusResponseObject, error) {
	v, err := h.svc.ChangeStatus(ctx, req.RequestId.String(), domain.Status(req.Body.Status))
	if err != nil {
		return nil, err
	}
	return api.UpdateMaintenanceStatus200JSONResponse{MaintenanceRequestJSONResponse: toRequest(v)}, nil
}

// tenantRoom returns the room of the logged-in ACTIVE tenant (§6.5).
func tenantRoom(ctx context.Context) (string, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.IsActiveTenant() {
		return "", auth.ErrAccountNotActive
	}
	return p.RoomID, nil
}

func toUploadURL(p storage.PresignedRequest, key string) api.UploadUrlJSONResponse {
	return api.UploadUrlJSONResponse{UploadUrl: p.URL, Method: p.Method, Headers: p.Headers, FileKey: key, ExpiresAt: p.ExpiresAt}
}

func toRequest(v app.View) api.MaintenanceRequestJSONResponse {
	r := v.Request
	return api.MaintenanceRequestJSONResponse{
		Id:          uuid.MustParse(r.ID),
		RoomNumber:  v.RoomNumber,
		Category:    api.MaintenanceCategory(r.Category),
		Description: r.Description,
		Images:      v.ImageURLs,
		Status:      api.MaintenanceStatus(r.Status),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
