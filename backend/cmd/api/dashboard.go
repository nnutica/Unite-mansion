package main

import (
	"context"
	"time"

	"unite-mansion/api"
	billingapp "unite-mansion/internal/billing/app"
	commapp "unite-mansion/internal/communication/app"
	identityapp "unite-mansion/internal/identity/app"
	identitydomain "unite-mansion/internal/identity/domain"
	maintenanceapp "unite-mansion/internal/maintenance/app"
	maintenancedomain "unite-mansion/internal/maintenance/domain"
	"unite-mansion/internal/shared"
)

// dashboardHandler builds the P-A02 cards from each context's own counts, so
// no context reads another's tables.
type dashboardHandler struct {
	billing       *billingapp.Service
	maintenance   *maintenanceapp.Service
	communication *commapp.Service
	identity      *identityapp.Service
	now           func() time.Time
}

func (h *dashboardHandler) GetDashboard(ctx context.Context, _ api.GetDashboardRequestObject) (api.GetDashboardResponseObject, error) {
	period := shared.BillingPeriodOf(h.now())
	counts, err := h.billing.PeriodCounts(ctx, period)
	if err != nil {
		return nil, err
	}
	slips, err := h.billing.PendingSlipCount(ctx)
	if err != nil {
		return nil, err
	}
	newStatus := maintenancedomain.StatusNew
	repairs, err := h.maintenance.List(ctx, &newStatus)
	if err != nil {
		return nil, err
	}
	expiring, err := h.communication.ExpiringLeases(ctx)
	if err != nil {
		return nil, err
	}
	pendingStatus := identitydomain.StatusPending
	pending, err := h.identity.ListTenantAccounts(ctx, &pendingStatus)
	if err != nil {
		return nil, err
	}
	return api.GetDashboard200JSONResponse{
		BillingPeriod:          period.String(),
		PendingSlips:           slips,
		UnpaidRooms:            counts.Unpaid,
		UnbilledRooms:          counts.Unbilled,
		NewMaintenanceRequests: len(repairs),
		ExpiringLeases:         len(expiring),
		PendingAccounts:        len(pending),
	}, nil
}
