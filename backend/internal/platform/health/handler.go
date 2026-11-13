// Package health implements the system endpoints of the API.
package health

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/api"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

func (h *Handler) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	if err := h.pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("health: ping database: %w", err)
	}
	return api.GetHealth200JSONResponse{Status: api.HealthStatusOK}, nil
}
