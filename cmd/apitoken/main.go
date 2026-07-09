// Command apitoken mints a REST API bearer token for an existing user and prints
// the PLAINTEXT exactly once. It is the operator/MCP path to obtain a token:
//
//	go run ./cmd/apitoken -email admin@example.com -name "mcp-server"
//
// The plaintext is never stored — copy it immediately. An optional -days flag
// sets an expiry (0 = never expires). It loads config and connects to the pool
// exactly like cmd/seed, then resolves the user by email and generates a token.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/apitoken"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
)

func main() {
	if err := run(); err != nil {
		slog.Error("apitoken failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "email of the user to mint a token for (required)")
	name := flag.String("name", "api-token", "human label for the token")
	days := flag.Int("days", 0, "days until the token expires (0 = never)")
	flag.Parse()

	if *email == "" {
		flag.Usage()
		return fmt.Errorf("-email is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg)
	slog.SetDefault(logger)

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := sqlcgen.New(pool)
	userRepo := accounts.NewUserRepoPG(queries)

	user, err := userRepo.GetByEmail(ctx, *email)
	if err != nil {
		return fmt.Errorf("resolve user by email %q: %w", *email, err)
	}

	var expiresAt *time.Time
	if *days > 0 {
		exp := time.Now().Add(time.Duration(*days) * 24 * time.Hour)
		expiresAt = &exp
	}

	svc := apitoken.NewService(apitoken.NewRepoPG(queries))
	plaintext, tok, err := svc.Generate(ctx, user.ID, *name, expiresAt)
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	// The plaintext is shown ONCE and never persisted — print it to stdout so it
	// can be captured, and log the non-secret metadata separately.
	fmt.Println(plaintext)
	logger.Info("api token minted",
		"id", tok.ID,
		"user_email", user.Email,
		"name", tok.Name,
		"last_four", tok.LastFour,
		"expires_at", tok.ExpiresAt,
	)
	return nil
}
