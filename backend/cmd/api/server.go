package main

import (
	"unite-mansion/api"
	"unite-mansion/internal/platform/health"
)

// server composes each context's handler into the generated StrictServerInterface.
// Add one embedded handler per bounded context as endpoints are implemented.
type server struct {
	*health.Handler
}

var _ api.StrictServerInterface = (*server)(nil)
