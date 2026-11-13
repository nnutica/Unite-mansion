package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/api"
	auditpg "unite-mansion/internal/audit/adapter/postgres"
	auditapp "unite-mansion/internal/audit/app"
	billinghttp "unite-mansion/internal/billing/adapter/http"
	billingpg "unite-mansion/internal/billing/adapter/postgres"
	billingapp "unite-mansion/internal/billing/app"
	identityhttp "unite-mansion/internal/identity/adapter/http"
	identitypg "unite-mansion/internal/identity/adapter/postgres"
	identityapp "unite-mansion/internal/identity/app"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/health"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/platform/password"
	"unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/shared"
	tenancyhttp "unite-mansion/internal/tenancy/adapter/http"
	tenancypg "unite-mansion/internal/tenancy/adapter/postgres"
	tenancyapp "unite-mansion/internal/tenancy/app"
)

const basePath = "/api/v1"

// Aliases give each embedded handler a distinct field name.
type (
	healthHandler   = health.Handler
	identityHandler = identityhttp.Handler
	tenancyHandler  = tenancyhttp.Handler
	billingHandler  = billinghttp.Handler
)

// server composes each context's handler into the generated StrictServerInterface.
type server struct {
	*healthHandler
	*identityHandler
	*tenancyHandler
	*billingHandler
}

var _ api.StrictServerInterface = (*server)(nil)

type deps struct {
	pool         *pgxpool.Pool
	log          *slog.Logger
	secureCookie bool
	now          func() time.Time
}

// newRouter wires every bounded context; it is the only place that knows
// all of them (context.md §4).
func newRouter(d deps) (http.Handler, error) {
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	txm := postgres.NewTxManager(d.pool)

	// Domain events: every event is written to the audit log in the same tx.
	dispatcher := shared.NewDispatcher()
	dispatcher.SubscribeAll(auditapp.NewRecorder(auditpg.NewRepository(d.pool)).Handle)

	tenancy := tenancyapp.NewService(tenancypg.NewRepository(d.pool), txm, d.now, newID)
	identity, err := identityapp.NewService(identitypg.NewRepository(d.pool), identityRooms{tenancy},
		password.Hasher{}, txm, dispatcher, d.now, newID)
	if err != nil {
		return nil, err
	}
	billing := billingapp.NewService(billingpg.NewRepository(d.pool), billingTenancy{tenancy}, txm, dispatcher, d.now, newID)

	router := gin.New()
	// Handlers receive *gin.Context as context.Context; fall back to the
	// request context so values set by middleware (principal, actor, tx) are visible.
	router.ContextWithFallback = true
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	var mappings []httpx.ErrorMapping
	mappings = append(mappings, sharedErrorMappings()...)
	mappings = append(mappings, auth.ErrorMappings()...)
	mappings = append(mappings, identityhttp.ErrorMappings()...)
	mappings = append(mappings, tenancyhttp.ErrorMappings()...)
	mappings = append(mappings, billinghttp.ErrorMappings()...)

	router.Use(
		httpx.RequestIDMiddleware(),
		httpx.Errors(d.log, mappings...),
		httpx.Recovery(d.log),
		auth.Middleware(identityhttp.Authenticator{Svc: identity}),
		auth.Guard(basePath),
	)
	router.NoRoute(httpx.NotFound)

	srv := &server{
		healthHandler:   health.NewHandler(d.pool),
		identityHandler: identityhttp.NewHandler(identity, d.secureCookie),
		tenancyHandler:  tenancyhttp.NewHandler(tenancy, tenancyAccounts{identity}),
		billingHandler:  billinghttp.NewHandler(billing),
	}
	strict := api.NewStrictHandlerWithOptions(srv, nil, api.StrictGinServerOptions{
		RequestErrorHandlerFunc:  httpx.RequestError,
		HandlerErrorFunc:         httpx.HandlerError,
		ResponseErrorHandlerFunc: httpx.HandlerError,
	})
	api.RegisterHandlersWithOptions(router, strict, api.GinServerOptions{
		BaseURL:      basePath,
		ErrorHandler: httpx.ParamError,
	})
	return router, nil
}

// Invalid money or billing-period strings in a request are client errors.
func sharedErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: shared.ErrInvalidMoney, Status: http.StatusBadRequest, Code: "INVALID_MONEY"},
		{Err: shared.ErrInvalidBillingPeriod, Status: http.StatusBadRequest, Code: "INVALID_BILLING_PERIOD"},
	}
}
