// Command migrate runs goose migrations against DATABASE_URL.
//
// Usage:
//
//	migrate up
//	migrate down
//	migrate status
//
// The migration SQL lives in db/migrations.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/huseyn0w/cmstack-go/internal/platform/config"
)

const migrationsDir = "db/migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "up"
	if len(args) > 0 {
		command = args[0]
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	ctx := context.Background()
	if err := goose.RunContext(ctx, command, db, migrationsDir, args[min(1, len(args)):]...); err != nil {
		return fmt.Errorf("goose %s: %w", command, err)
	}
	return nil
}
