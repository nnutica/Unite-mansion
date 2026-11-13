// Command seed-admin creates the single admin account (context.md §6.5), or
// with -reset changes its password. Credentials come from the environment
// (ADMIN_USERNAME, ADMIN_PASSWORD), never from code or flags, so they do not
// end up in shell history.
//
//	go run ./cmd/seed-admin          # create
//	go run ./cmd/seed-admin -reset   # change password
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	"github.com/joho/godotenv"

	identitypg "unite-mansion/internal/identity/adapter/postgres"
	"unite-mansion/internal/identity/domain"
	"unite-mansion/internal/platform/password"
	"unite-mansion/internal/platform/postgres"
)

func main() {
	reset := flag.Bool("reset", false, "change the password of the existing admin")
	flag.Parse()
	if err := run(*reset); err != nil {
		fmt.Fprintln(os.Stderr, "seed-admin:", err)
		os.Exit(1)
	}
}

func run(reset bool) error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	username := domain.NormalizeUsername(os.Getenv("ADMIN_USERNAME"))
	pw := os.Getenv("ADMIN_PASSWORD")
	if dsn == "" || username == "" || pw == "" {
		return errors.New("DATABASE_URL, ADMIN_USERNAME and ADMIN_PASSWORD must be set")
	}
	if err := domain.ValidateUsername(username); err != nil {
		return err
	}
	if err := domain.ValidatePassword(pw); err != nil {
		return err
	}

	hash, err := password.Hasher{}.Hash(pw)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	created, err := identitypg.NewRepository(pool).UpsertAdmin(ctx, username, hash, reset)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("admin %q created\n", username)
	} else {
		fmt.Printf("password of admin %q changed\n", username)
	}
	return nil
}
