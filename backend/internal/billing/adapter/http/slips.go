package httpadapter

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"unite-mansion/api"
	"unite-mansion/internal/billing/app"
	"unite-mansion/internal/billing/domain"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/platform/storage"
)

// UploadErrorMappings are shared by every endpoint that accepts uploads.
func UploadErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: storage.ErrFileTypeNotAllowed, Status: http.StatusUnprocessableEntity, Code: "FILE_TYPE_NOT_ALLOWED"},
		{Err: storage.ErrFileTooLarge, Status: http.StatusUnprocessableEntity, Code: "FILE_TOO_LARGE"},
		{Err: storage.ErrInvalidFileKey, Status: http.StatusUnprocessableEntity, Code: "INVALID_FILE_KEY"},
		{Err: storage.ErrUploadNotFound, Status: http.StatusUnprocessableEntity, Code: "UPLOAD_NOT_FOUND"},
	}
}

func slipErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrSlipNotFound, Status: http.StatusNotFound, Code: "SLIP_NOT_FOUND"},
		{Err: domain.ErrSlipNotAllowed, Status: http.StatusConflict, Code: "SLIP_NOT_ALLOWED"},
		{Err: domain.ErrSlipAlreadyReviewed, Status: http.StatusConflict, Code: "SLIP_ALREADY_REVIEWED"},
		{Err: domain.ErrSlipAlreadyExists, Status: http.StatusConflict, Code: "SLIP_ALREADY_SUBMITTED"},
		{Err: domain.ErrInvalidRejectReason, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
	}
}

// --- tenant: the room always comes from the session, never the request ---

func (h *Handler) ListTenantInvoices(ctx context.Context, _ api.ListTenantInvoicesRequestObject) (api.ListTenantInvoicesResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.svc.TenantInvoices(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make(api.ListTenantInvoices200JSONResponse, 0, len(list))
	for _, s := range list {
		out = append(out, toSummary(s))
	}
	return out, nil
}

func (h *Handler) GetTenantInvoice(ctx context.Context, req api.GetTenantInvoiceRequestObject) (api.GetTenantInvoiceResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.TenantInvoice(ctx, roomID, req.InvoiceId.String())
	if err != nil {
		return nil, err
	}
	return api.GetTenantInvoice200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) CreateSlipUploadUrl(ctx context.Context, req api.CreateSlipUploadUrlRequestObject) (api.CreateSlipUploadUrlResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	presigned, key, err := h.svc.CreateSlipUpload(ctx, roomID, req.InvoiceId.String(), string(req.Body.ContentType), req.Body.SizeBytes)
	if err != nil {
		return nil, err
	}
	return api.CreateSlipUploadUrl200JSONResponse{UploadUrlJSONResponse: toUploadURL(presigned, key)}, nil
}

func (h *Handler) SubmitSlip(ctx context.Context, req api.SubmitSlipRequestObject) (api.SubmitSlipResponseObject, error) {
	roomID, err := tenantRoom(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.SubmitSlip(ctx, roomID, req.InvoiceId.String(), req.Body.FileKey)
	if err != nil {
		return nil, err
	}
	return api.SubmitSlip201JSONResponse(toSlip(v)), nil
}

// --- admin ---

func (h *Handler) ListSlips(ctx context.Context, req api.ListSlipsRequestObject) (api.ListSlipsResponseObject, error) {
	var status *domain.SlipStatus
	if req.Params.Status != nil {
		s := domain.SlipStatus(*req.Params.Status)
		status = &s
	}
	items, err := h.svc.SlipQueue(ctx, status)
	if err != nil {
		return nil, err
	}
	out := make(api.ListSlips200JSONResponse, 0, len(items))
	for _, it := range items {
		out = append(out, api.SlipQueueItem(toQueueItem(it)))
	}
	return out, nil
}

func (h *Handler) ApproveSlip(ctx context.Context, req api.ApproveSlipRequestObject) (api.ApproveSlipResponseObject, error) {
	item, err := h.svc.ApproveSlip(ctx, req.SlipId.String())
	if err != nil {
		return nil, err
	}
	return api.ApproveSlip200JSONResponse{SlipQueueItemJSONResponse: toQueueItem(item)}, nil
}

func (h *Handler) RejectSlip(ctx context.Context, req api.RejectSlipRequestObject) (api.RejectSlipResponseObject, error) {
	note := ""
	if req.Body.Note != nil {
		note = *req.Body.Note
	}
	item, err := h.svc.RejectSlip(ctx, req.SlipId.String(), domain.RejectReason(req.Body.Reason), note)
	if err != nil {
		return nil, err
	}
	return api.RejectSlip200JSONResponse{SlipQueueItemJSONResponse: toQueueItem(item)}, nil
}

// tenantRoom returns the room of the logged-in ACTIVE tenant.
func tenantRoom(ctx context.Context) (string, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.IsActiveTenant() {
		return "", auth.ErrAccountNotActive
	}
	return p.RoomID, nil
}

// toUploadURL converts a presigned PUT for the API.
func toUploadURL(p storage.PresignedRequest, key string) api.UploadUrlJSONResponse {
	return api.UploadUrlJSONResponse{UploadUrl: p.URL, Method: p.Method, Headers: p.Headers, FileKey: key, ExpiresAt: p.ExpiresAt}
}

func toSlip(v app.SlipView) api.PaymentSlip {
	s := v.Slip
	out := api.PaymentSlip{
		Id:                uuid.MustParse(s.ID),
		ImageUrl:          v.ImageURL,
		SubmittedAt:       s.SubmittedAt,
		Status:            api.SlipStatus(s.Status),
		PossibleDuplicate: v.PossibleDuplicate,
		ReviewedAt:        s.ReviewedAt,
	}
	if s.RejectReason != nil {
		r := api.SlipRejectReason(*s.RejectReason)
		out.RejectReason = &r
	}
	if s.RejectNote != "" {
		note := s.RejectNote
		out.RejectNote = &note
	}
	return out
}

func toSummary(s app.InvoiceSummary) api.InvoiceSummary {
	return api.InvoiceSummary{
		Id:            uuid.MustParse(s.ID),
		InvoiceNumber: s.InvoiceNumber,
		BillingPeriod: s.Period.String(),
		DueDate:       openapi_types.Date{Time: s.DueDate},
		Total:         s.Total.String(),
		Status:        api.BillDisplayStatus(s.DisplayStatus),
	}
}

func toQueueItem(it app.SlipQueueItem) api.SlipQueueItemJSONResponse {
	return api.SlipQueueItemJSONResponse{Slip: toSlip(it.Slip), RoomNumber: it.Invoice.RoomNumber, Invoice: toSummary(it.Invoice)}
}
