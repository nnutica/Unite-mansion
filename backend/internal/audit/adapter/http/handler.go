// Package httpadapter implements the audit log endpoint of the generated
// OpenAPI server.
package httpadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"unite-mansion/api"
	"unite-mansion/internal/audit/app"
	"unite-mansion/internal/platform/httpx"
)

type Handler struct {
	logs app.LogReader
}

func NewHandler(logs app.LogReader) *Handler { return &Handler{logs: logs} }

func (h *Handler) ListAuditLogs(ctx context.Context, req api.ListAuditLogsRequestObject) (api.ListAuditLogsResponseObject, error) {
	limit := 0
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	page, err := app.ListPage(ctx, h.logs, req.Params.AggregateId, req.Params.Cursor, limit)
	if err != nil {
		return nil, err
	}
	out := api.AuditLogPage{Items: make([]api.AuditLogEntry, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, e := range page.Items {
		var payload map[string]any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, fmt.Errorf("audit %s payload: %w", e.ID, err)
		}
		out.Items = append(out.Items, api.AuditLogEntry{
			Id: uuid.MustParse(e.ID), EventName: e.EventName, AggregateId: e.AggregateID,
			ActorType: api.AuditLogEntryActorType(e.ActorType), ActorId: e.ActorID,
			Payload: payload, OccurredAt: e.OccurredAt,
		})
	}
	return api.ListAuditLogs200JSONResponse(out), nil
}

func ErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: app.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "INVALID_CURSOR"},
	}
}
