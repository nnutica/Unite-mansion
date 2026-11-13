package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"unite-mansion/api"
	audithttp "unite-mansion/internal/audit/adapter/http"
	auditpg "unite-mansion/internal/audit/adapter/postgres"
	auditapp "unite-mansion/internal/audit/app"
	billinghttp "unite-mansion/internal/billing/adapter/http"
	billingpg "unite-mansion/internal/billing/adapter/postgres"
	billingapp "unite-mansion/internal/billing/app"
	commhttp "unite-mansion/internal/communication/adapter/http"
	commpg "unite-mansion/internal/communication/adapter/postgres"
	commapp "unite-mansion/internal/communication/app"
	identityhttp "unite-mansion/internal/identity/adapter/http"
	identitypg "unite-mansion/internal/identity/adapter/postgres"
	identityapp "unite-mansion/internal/identity/app"
	maintenancehttp "unite-mansion/internal/maintenance/adapter/http"
	maintenancepg "unite-mansion/internal/maintenance/adapter/postgres"
	maintenanceapp "unite-mansion/internal/maintenance/app"
	"unite-mansion/internal/platform/auth"
	"unite-mansion/internal/platform/health"
	"unite-mansion/internal/platform/httpx"
	"unite-mansion/internal/platform/password"
	"unite-mansion/internal/platform/postgres"
	"unite-mansion/internal/platform/storage"
	"unite-mansion/internal/shared"
	tenancyhttp "unite-mansion/internal/tenancy/adapter/http"
	tenancypg "unite-mansion/internal/tenancy/adapter/postgres"
	tenancyapp "unite-mansion/internal/tenancy/app"
)

const basePath = "/api/v1"

// Aliases give each embedded handler a distinct field name.
type (
	healthHandler        = health.Handler
	identityHandler      = identityhttp.Handler
	tenancyHandler       = tenancyhttp.Handler
	documentsHandler     = tenancyhttp.DocumentsHandler
	billingHandler       = billinghttp.Handler
	maintenanceHandler   = maintenancehttp.Handler
	communicationHandler = commhttp.Handler
	auditHandler         = audithttp.Handler
)

// server composes each context's handler into the generated StrictServerInterface.
type server struct {
	*healthHandler
	*identityHandler
	*tenancyHandler
	*documentsHandler
	*billingHandler
	*maintenanceHandler
	*communicationHandler
	*auditHandler
	*dashboardHandler
}

var _ api.StrictServerInterface = (*server)(nil)

type deps struct {
	pool         *pgxpool.Pool
	log          *slog.Logger
	files        storage.FileStorage
	secureCookie bool
	now          func() time.Time
}

// application is the wired API plus the background work main must start.
type application struct {
	handler   http.Handler
	documents *tenancyapp.Documents // runs the PDPA retention job
}

// newApplication wires every bounded context; it is the only place that
// knows all of them (context.md §4).
func newApplication(d deps) (*application, error) {
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	txm := postgres.NewTxManager(d.pool)
	dispatcher := shared.NewDispatcher()

	tenancyRepo := tenancypg.NewRepository(d.pool)
	tenancy := tenancyapp.NewService(tenancyRepo, txm, d.now, newID)
	identity, err := identityapp.NewService(identitypg.NewRepository(d.pool), identityRooms{tenancy},
		password.Hasher{}, txm, dispatcher, d.now, newID)
	if err != nil {
		return nil, err
	}
	billingRepo := billingpg.NewRepository(d.pool)
	billing := billingapp.NewService(billingRepo, billingRepo, billingTenancy{tenancy}, d.files, txm, dispatcher, d.now, newID)
	maintenance := maintenanceapp.NewService(maintenancepg.NewRepository(d.pool), tenancy, d.files, txm, dispatcher, d.now, newID)
	communication := commapp.NewService(commpg.NewRepository(d.pool), communicationLeases{tenancy}, d.files, d.now, newID)
	documents := tenancyapp.NewDocuments(tenancyRepo, tenancyRepo, tenancyUsernames{identity}, d.files, txm, dispatcher, d.now, newID)
	auditRepo := auditpg.NewRepository(d.pool)

	// Domain events (§6.7), handled in the transaction that raised them.
	dispatcher.SubscribeAll(auditapp.NewRecorder(auditRepo).Handle)
	dispatcher.Subscribe("UtilityRateChanged", communication.HandleUtilityRateChanged) // §6.3 announcement
	dispatcher.Subscribe("AccountUnassigned", documents.OnAccountUnassigned)           // §6.6 schedule deletion
	dispatcher.Subscribe("AccountAssignedToRoom", documents.OnAccountAssigned)         // §6.6 cancel deletion

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
	mappings = append(mappings, tenancyhttp.DocumentErrorMappings()...)
	mappings = append(mappings, billinghttp.ErrorMappings()...)
	mappings = append(mappings, billinghttp.UploadErrorMappings()...)
	mappings = append(mappings, maintenancehttp.ErrorMappings()...)
	mappings = append(mappings, commhttp.ErrorMappings()...)
	mappings = append(mappings, audithttp.ErrorMappings()...)

	router.Use(
		httpx.RequestIDMiddleware(),
		httpx.Errors(d.log, mappings...),
		httpx.Recovery(d.log),
		auth.Middleware(identityhttp.Authenticator{Svc: identity}),
		auth.Guard(basePath),
	)
	router.NoRoute(httpx.NotFound)

	srv := &server{
		healthHandler:        health.NewHandler(d.pool),
		identityHandler:      identityhttp.NewHandler(identity, d.secureCookie),
		tenancyHandler:       tenancyhttp.NewHandler(tenancy, tenancyAccounts{identity}, communication),
		documentsHandler:     tenancyhttp.NewDocumentsHandler(documents),
		billingHandler:       billinghttp.NewHandler(billing),
		maintenanceHandler:   maintenancehttp.NewHandler(maintenance),
		communicationHandler: commhttp.NewHandler(communication),
		auditHandler:         audithttp.NewHandler(auditRepo),
		dashboardHandler: &dashboardHandler{
			billing: billing, maintenance: maintenance, communication: communication, identity: identity, now: d.now,
		},
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
	return &application{handler: router, documents: documents}, nil
}

// Invalid money or billing-period strings in a request are client errors.
func sharedErrorMappings() []httpx.ErrorMapping {
	return []httpx.ErrorMapping{
		{Err: shared.ErrInvalidMoney, Status: http.StatusBadRequest, Code: "INVALID_MONEY"},
		{Err: shared.ErrInvalidBillingPeriod, Status: http.StatusBadRequest, Code: "INVALID_BILLING_PERIOD"},
	}
}
