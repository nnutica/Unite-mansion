// Package httpadapter implements the utility-rate and invoice endpoints of
// the generated OpenAPI server.
package httpadapter

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"unite-mansion/api"
	"unite-mansion/internal/billing/app"
	"unite-mansion/internal/billing/domain"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/shared"
)

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: domain.ErrInvoiceNotFound, Status: http.StatusNotFound, Code: "INVOICE_NOT_FOUND"},
		{Err: domain.ErrInvoiceAlreadyExists, Status: http.StatusConflict, Code: "INVOICE_ALREADY_EXISTS"},
		{Err: domain.ErrInvoiceNotDraft, Status: http.StatusConflict, Code: "INVOICE_NOT_DRAFT"},
		{Err: domain.ErrInvalidStatusTransition, Status: http.StatusConflict, Code: "INVALID_STATUS_TRANSITION"},
		{Err: domain.ErrRoomHasNoLease, Status: http.StatusUnprocessableEntity, Code: "ROOM_HAS_NO_LEASE"},
		{Err: domain.ErrMeterDigitsNotSet, Status: http.StatusUnprocessableEntity, Code: "METER_DIGITS_NOT_SET"},
		{Err: domain.ErrRateNotFound, Status: http.StatusUnprocessableEntity, Code: "RATE_NOT_FOUND"},
		{Err: domain.ErrMeterReadingOutOfRange, Status: http.StatusUnprocessableEntity, Code: "METER_READING_OUT_OF_RANGE"},
		{Err: domain.ErrMeterRolloverNotConfirmed, Status: http.StatusUnprocessableEntity, Code: "METER_ROLLOVER_NOT_CONFIRMED"},
		{Err: domain.ErrPreviousReadingRequired, Status: http.StatusUnprocessableEntity, Code: "PREVIOUS_READING_REQUIRED"},
		{Err: domain.ErrMetersIncomplete, Status: http.StatusUnprocessableEntity, Code: "METERS_INCOMPLETE"},
		{Err: domain.ErrDiscountExceedsRent, Status: http.StatusUnprocessableEntity, Code: "DISCOUNT_EXCEEDS_RENT"},
		{Err: domain.ErrInvalidLine, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
		{Err: domain.ErrLineNotFound, Status: http.StatusNotFound, Code: "LINE_NOT_FOUND"},
		{Err: domain.ErrInvalidRate, Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"},
		{Err: domain.ErrRateNotAfterLatest, Status: http.StatusConflict, Code: "RATE_NOT_AFTER_LATEST"},
		{Err: app.ErrRoomNotFound, Status: http.StatusNotFound, Code: "ROOM_NOT_FOUND"},
	}
}

type Handler struct {
	svc *app.Service
}

func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// --- utility rates ---

func (h *Handler) ListUtilityRates(ctx context.Context, _ api.ListUtilityRatesRequestObject) (api.ListUtilityRatesResponseObject, error) {
	v, err := h.svc.ListRates(ctx)
	if err != nil {
		return nil, err
	}
	out := api.UtilityRates{
		Electricity: toRate(v.Electricity),
		Water:       toRate(v.Water),
		History:     make([]api.UtilityRate, 0, len(v.History)),
	}
	for _, r := range v.History {
		out.History = append(out.History, toRate(r))
	}
	return api.ListUtilityRates200JSONResponse(out), nil
}

func (h *Handler) CreateUtilityRate(ctx context.Context, req api.CreateUtilityRateRequestObject) (api.CreateUtilityRateResponseObject, error) {
	perUnit, err := shared.ParseMoney(req.Body.PerUnit)
	if err != nil {
		return nil, err
	}
	r, err := h.svc.CreateRate(ctx, domain.UtilityType(req.Body.UtilityType), perUnit, req.Body.EffectiveFrom.Time)
	if err != nil {
		return nil, err
	}
	return api.CreateUtilityRate201JSONResponse(toRate(r)), nil
}

// --- invoices ---

func (h *Handler) CreateInvoiceDraft(ctx context.Context, req api.CreateInvoiceDraftRequestObject) (api.CreateInvoiceDraftResponseObject, error) {
	period, err := shared.ParseBillingPeriod(req.Period)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.CreateDraft(ctx, period, req.RoomNumber)
	if err != nil {
		return nil, err
	}
	return api.CreateInvoiceDraft201JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) GetRoomInvoice(ctx context.Context, req api.GetRoomInvoiceRequestObject) (api.GetRoomInvoiceResponseObject, error) {
	period, err := shared.ParseBillingPeriod(req.Period)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.GetRoomInvoice(ctx, period, req.RoomNumber)
	if err != nil {
		return nil, err
	}
	return api.GetRoomInvoice200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) GetInvoice(ctx context.Context, req api.GetInvoiceRequestObject) (api.GetInvoiceResponseObject, error) {
	v, err := h.svc.GetInvoice(ctx, req.InvoiceId.String())
	if err != nil {
		return nil, err
	}
	return api.GetInvoice200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) SetInvoiceMeters(ctx context.Context, req api.SetInvoiceMetersRequestObject) (api.SetInvoiceMetersResponseObject, error) {
	v, err := h.svc.SetMeters(ctx, req.InvoiceId.String(), app.MeterUpdate{
		Electricity: toMeterInput(req.Body.Electricity),
		Water:       toMeterInput(req.Body.Water),
	})
	if err != nil {
		return nil, err
	}
	return api.SetInvoiceMeters200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) AddInvoiceLine(ctx context.Context, req api.AddInvoiceLineRequestObject) (api.AddInvoiceLineResponseObject, error) {
	amount, err := shared.ParseMoney(req.Body.Amount)
	if err != nil {
		return nil, err
	}
	v, err := h.svc.AddLine(ctx, req.InvoiceId.String(), domain.LineType(req.Body.Type), req.Body.Description, amount)
	if err != nil {
		return nil, err
	}
	return api.AddInvoiceLine201JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) RemoveInvoiceLine(ctx context.Context, req api.RemoveInvoiceLineRequestObject) (api.RemoveInvoiceLineResponseObject, error) {
	v, err := h.svc.RemoveLine(ctx, req.InvoiceId.String(), req.LineId.String())
	if err != nil {
		return nil, err
	}
	return api.RemoveInvoiceLine200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func (h *Handler) IssueInvoice(ctx context.Context, req api.IssueInvoiceRequestObject) (api.IssueInvoiceResponseObject, error) {
	v, err := h.svc.Issue(ctx, req.InvoiceId.String())
	if err != nil {
		return nil, err
	}
	return api.IssueInvoice200JSONResponse{InvoiceJSONResponse: toInvoice(v)}, nil
}

func toMeterInput(in *api.MeterInput) *domain.MeterInput {
	if in == nil {
		return nil
	}
	out := &domain.MeterInput{Previous: in.PreviousReading, Current: in.CurrentReading}
	if in.RolloverConfirmed != nil {
		out.RolloverConfirmed = *in.RolloverConfirmed
	}
	return out
}

func toInvoice(v app.InvoiceView) api.InvoiceJSONResponse {
	inv := v.Invoice
	snap := inv.Snapshot()
	out := api.InvoiceJSONResponse{
		Id:            uuid.MustParse(inv.ID()),
		InvoiceNumber: inv.InvoiceNumber(),
		RoomNumber:    inv.RoomNumber(),
		BillingPeriod: inv.Period().String(),
		DueDate:       openapi_types.Date{Time: v.DueDate},
		Status:        api.BillDisplayStatus(v.DisplayStatus),
		Electricity:   toMeterDetail(inv, domain.Electricity, snap.ElectricRate),
		Water:         toMeterDetail(inv, domain.Water, snap.WaterRate),
		Total:         inv.Total().String(),
		AbnormalUsage: api.AbnormalUsage{Electricity: toUsage(v.Electricity), Water: toUsage(v.Water)},
		IssuedAt:      inv.IssuedAt(),
		PaidAt:        inv.PaidAt(),
		CreatedAt:     inv.CreatedAt(),
	}
	for _, l := range inv.Lines() {
		line := api.InvoiceLine{Type: api.InvoiceLineType(l.Type), Description: l.Description, Amount: l.Amount.String()}
		if l.ID != "" {
			id := uuid.MustParse(l.ID)
			line.Id = &id
		}
		out.Lines = append(out.Lines, line)
	}
	return out
}

func toMeterDetail(inv *domain.Invoice, typ domain.UtilityType, rate shared.Money) api.MeterDetail {
	m := inv.Meter(typ)
	return api.MeterDetail{
		PreviousReading:   m.Previous,
		CurrentReading:    m.Current,
		Units:             inv.Units(typ),
		RatePerUnit:       rate.String(),
		MeterDigits:       m.Digits,
		RolloverConfirmed: m.RolloverConfirmed,
	}
}

func toUsage(u domain.UsageCheck) api.UsageCheck {
	return api.UsageCheck{Abnormal: u.Abnormal, PercentOfAverage: u.PercentOfAverage}
}

func toRate(r domain.UtilityRate) api.UtilityRate {
	return api.UtilityRate{
		Id:            uuid.MustParse(r.ID),
		UtilityType:   api.UtilityType(r.Type),
		PerUnit:       r.PerUnit.String(),
		EffectiveFrom: openapi_types.Date{Time: r.EffectiveFrom},
		CreatedAt:     r.CreatedAt,
	}
}
