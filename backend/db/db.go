// Package db holds goose migrations and sqlc queries.
package db

import "embed"

//go:generate go tool -modfile=../tools/go.mod sqlc generate -f ../sqlc.yaml

// Migrations embeds the goose migrations so tests can apply them.
//
//go:embed migrations/*.sql
var Migrations embed.FS
