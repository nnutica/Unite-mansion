package httpadapter

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"unite-mansion/api"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/tenancy/app"
	"unite-mansion/internal/tenancy/domain"
)

func DocumentErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrConsentRequired, Status: http.StatusConflict, Code: "CONSENT_REQUIRED"},
		{Err: domain.ErrConsentVersionOutdated, Status: http.StatusConflict, Code: "CONSENT_VERSION_OUTDATED"},
		{Err: domain.ErrInvalidDocumentType, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
	}
}

// DocumentsHandler serves consent and tenant documents (§6.6, M-10, P-T12).
type DocumentsHandler struct {
	docs *app.Documents
}

func NewDocumentsHandler(docs *app.Documents) *DocumentsHandler { return &DocumentsHandler{docs: docs} }

func (h *DocumentsHandler) GetTenantConsent(ctx context.Context, _ api.GetTenantConsentRequestObject) (api.GetTenantConsentResponseObject, error) {
	p, err := activeTenant(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.docs.ConsentStatus(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	return api.GetTenantConsent200JSONResponse{ConsentStatusJSONResponse: toConsentStatus(st)}, nil
}

func (h *DocumentsHandler) AcceptConsent(ctx context.Context, req api.AcceptConsentRequestObject) (api.AcceptConsentResponseObject, error) {
	p, err := activeTenant(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.docs.AcceptConsent(ctx, p.AccountID, req.Body.Version)
	if err != nil {
		return nil, err
	}
	return api.AcceptConsent200JSONResponse{ConsentStatusJSONResponse: toConsentStatus(st)}, nil
}

func (h *DocumentsHandler) GetConsentText(ctx context.Context, _ api.GetConsentTextRequestObject) (api.GetConsentTextResponseObject, error) {
	c, err := h.docs.ConsentText(ctx)
	if err != nil {
		return nil, err
	}
	return api.GetConsentText200JSONResponse{ConsentTextJSONResponse: toConsentText(c)}, nil
}

func (h *DocumentsHandler) UpdateConsentText(ctx context.Context, req api.UpdateConsentTextRequestObject) (api.UpdateConsentTextResponseObject, error) {
	c, err := h.docs.UpdateConsentText(ctx, req.Body.Text)
	if err != nil {
		return nil, err
	}
	return api.UpdateConsentText200JSONResponse{ConsentTextJSONResponse: toConsentText(c)}, nil
}

func (h *DocumentsHandler) CreateDocumentUploadUrl(ctx context.Context, req api.CreateDocumentUploadUrlRequestObject) (api.CreateDocumentUploadUrlResponseObject, error) {
	p, err := activeTenant(ctx)
	if err != nil {
		return nil, err
	}
	presigned, key, err := h.docs.CreateUpload(ctx, p.AccountID, domain.DocumentType(req.Body.Type), string(req.Body.ContentType), req.Body.SizeBytes)
	if err != nil {
		return nil, err
	}
	return api.CreateDocumentUploadUrl200JSONResponse{UploadUrlJSONResponse: api.UploadUrlJSONResponse{
		UploadUrl: presigned.URL, Method: presigned.Method, Headers: presigned.Headers, FileKey: key, ExpiresAt: presigned.ExpiresAt,
	}}, nil
}

func (h *DocumentsHandler) SubmitDocument(ctx context.Context, req api.SubmitDocumentRequestObject) (api.SubmitDocumentResponseObject, error) {
	p, err := activeTenant(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.docs.Submit(ctx, p.AccountID, p.RoomID, domain.DocumentType(req.Body.Type), req.Body.FileKey)
	if err != nil {
		return nil, err
	}
	return api.SubmitDocument201JSONResponse(toDocument(v)), nil
}

func (h *DocumentsHandler) ListTenantDocuments(ctx context.Context, _ api.ListTenantDocumentsRequestObject) (api.ListTenantDocumentsResponseObject, error) {
	p, err := activeTenant(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.docs.TenantDocuments(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	return api.ListTenantDocuments200JSONResponse(toDocuments(list)), nil
}

func (h *DocumentsHandler) ListRoomDocuments(ctx context.Context, req api.ListRoomDocumentsRequestObject) (api.ListRoomDocumentsResponseObject, error) {
	list, err := h.docs.RoomDocuments(ctx, req.RoomNumber)
	if err != nil {
		return nil, err
	}
	return api.ListRoomDocuments200JSONResponse(toDocuments(list)), nil
}

// activeTenant returns the session's ACTIVE tenant; documents always belong to
// the caller's own account and room (§6.5).
func activeTenant(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.FromContext(ctx)
	if !ok || !p.IsActiveTenant() {
		return auth.Principal{}, auth.ErrAccountNotActive
	}
	return p, nil
}

func toConsentStatus(st app.ConsentStatus) api.ConsentStatusJSONResponse {
	return api.ConsentStatusJSONResponse{
		Version: st.Text.Version, Text: st.Text.Body, Accepted: st.AcceptedAt != nil, AcceptedAt: st.AcceptedAt,
	}
}

func toConsentText(c domain.ConsentText) api.ConsentTextJSONResponse {
	return api.ConsentTextJSONResponse{Version: c.Version, Text: c.Body, CreatedAt: c.CreatedAt}
}

func toDocuments(list []app.DocumentView) []api.TenantDocument {
	out := make([]api.TenantDocument, 0, len(list))
	for _, v := range list {
		out = append(out, toDocument(v))
	}
	return out
}

func toDocument(v app.DocumentView) api.TenantDocument {
	return api.TenantDocument{
		Id: uuid.MustParse(v.ID), Type: api.DocumentType(v.Type), ContentType: v.ContentType,
		Url: v.URL, UploadedAt: v.UploadedAt, Username: v.Username, PurgeAt: v.PurgeAt,
	}
}
