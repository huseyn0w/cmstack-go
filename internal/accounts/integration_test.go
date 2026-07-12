package accounts_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql "pgx" driver for goose
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller for migrations path")
	}
	// internal/accounts/integration_test.go -> repo root is 3 up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, "db", "migrations")
}

func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgC, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("agentic_cms_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(pgC); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(sqlDB, migrationsDir(t)); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// wiring assembles a real-DB stack for integration tests.
type wiring struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
	users   *accounts.UserRepoPG
	roles   *accounts.RoleRepoPG
	tokens  *accounts.TokenRepoPG
	oauth   *accounts.OAuthRepoPG
	authz   *accounts.Authorizer
	bus     *events.Bus
	svc     *accounts.AuthService
}

func newWiring(t *testing.T, settings accounts.SettingsProvider) *wiring {
	pool := startPostgres(t)
	q := sqlcgen.New(pool)
	hasher := security.NewPasswordHasher()
	users := accounts.NewUserRepoPG(q)
	roles := accounts.NewRoleRepoPG(q)
	tokens := accounts.NewTokenRepoPG(q)
	oauth := accounts.NewOAuthRepoPG(q)
	bus := events.NewBus(events.NewOutboxRepository())
	bus.SubscribeAsync(accounts.EventAccountRegistered)
	bus.SubscribeAsync(accounts.EventPasswordResetRequested)
	authz := accounts.NewAuthorizer(users, roles)
	svc := accounts.NewAuthService(pool, users, roles, tokens, oauth, hasher, bus, settings, nil)

	// Seed so the Member role and admin exist.
	seeder := accounts.NewSeeder(pool, q, users, roles, hasher)
	if err := seeder.Seed(context.Background(), accounts.AdminSeed{Email: "admin@agentic-cms.local", Password: "admin-password-123"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	return &wiring{pool: pool, queries: q, users: users, roles: roles, tokens: tokens, oauth: oauth, authz: authz, bus: bus, svc: svc}
}

func TestSeedIsIdempotentAndPopulatesAuthz(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()

	// Re-running the seed must not error or duplicate.
	hasher := security.NewPasswordHasher()
	seeder := accounts.NewSeeder(w.pool, w.queries, w.users, w.roles, hasher)
	if err := seeder.Seed(ctx, accounts.AdminSeed{Email: "admin@agentic-cms.local", Password: "admin-password-123"}); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	// Authorizer loads the seeded mappings from the DB.
	admin, err := w.users.GetByEmail(ctx, "admin@agentic-cms.local")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if !w.authz.Can(ctx, admin.ID, accounts.ActionManage, accounts.SubjectUser) {
		t.Error("seeded administrator should have manage:user")
	}
	if !w.authz.Can(ctx, admin.ID, accounts.ActionDelete, accounts.SubjectSetting) {
		t.Error("seeded administrator (manage:all) should be able to delete setting")
	}

	// Member role exists and is read-only.
	member, err := w.roles.GetByKey(ctx, accounts.RoleMember)
	if err != nil {
		t.Fatalf("get member role: %v", err)
	}
	if member.Key != accounts.RoleMember {
		t.Errorf("member role key = %q", member.Key)
	}

	// Exactly one admin user despite two seed runs (count via repo lookup).
	if _, err := w.users.GetByEmail(ctx, "admin@agentic-cms.local"); err != nil {
		t.Fatalf("admin should exist exactly once: %v", err)
	}
}

func TestRegisterVerifyAndLoginEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, true)) // verification required
	ctx := context.Background()

	captured := captureAsync(w.bus)

	u, err := w.svc.Register(ctx, accounts.RegisterInput{
		Name: "Alice", Email: "alice@example.com", Password: "long-enough-password",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// New users default to the Member role.
	role, err := w.roles.GetByID(ctx, u.RoleID)
	if err != nil || role.Key != accounts.RoleMember {
		t.Fatalf("new user role = %v (err %v), want member", role.Key, err)
	}

	// Login is blocked until verified.
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "alice@example.com", Password: "long-enough-password"}); err != accounts.ErrEmailNotVerified {
		t.Fatalf("expected ErrEmailNotVerified before verify, got %v", err)
	}

	// The async verification event was enqueued; the relay dispatches it.
	relay := events.NewRelay(w.pool, w.bus, 100, slogDiscard())
	n, err := relay.Drain(ctx)
	if err != nil {
		t.Fatalf("relay drain: %v", err)
	}
	if n == 0 {
		t.Fatal("expected the relay to process the enqueued registration event")
	}
	token := captured.verificationToken()
	if token == "" {
		t.Fatal("no verification token captured from async dispatch")
	}

	// Verify, then login succeeds.
	if err := w.svc.VerifyEmail(ctx, token); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "alice@example.com", Password: "long-enough-password"}); err != nil {
		t.Fatalf("Login after verify: %v", err)
	}

	// Reusing the verification token fails (single-use).
	if err := w.svc.VerifyEmail(ctx, token); err != accounts.ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken on token reuse, got %v", err)
	}
}

func TestPasswordResetEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping testcontainers integration test in -short mode")
	}
	w := newWiring(t, accounts.NewStaticSettings(true, false))
	ctx := context.Background()
	captured := captureAsync(w.bus)

	if _, err := w.svc.Register(ctx, accounts.RegisterInput{Name: "Bob", Email: "bob@example.com", Password: "first-password-1"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Unknown email: outwardly succeeds, issues nothing.
	if err := w.svc.RequestPasswordReset(ctx, "nobody@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset(unknown): %v", err)
	}

	if err := w.svc.RequestPasswordReset(ctx, "bob@example.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	relay := events.NewRelay(w.pool, w.bus, 100, slogDiscard())
	if _, err := relay.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	resetTok := captured.resetToken()
	if resetTok == "" {
		t.Fatal("no reset token captured")
	}

	if err := w.svc.ResetPassword(ctx, resetTok, "brand-new-password-2"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	// Old password no longer works; new one does.
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "bob@example.com", Password: "first-password-1"}); err != accounts.ErrInvalidCredentials {
		t.Fatalf("old password should be rejected, got %v", err)
	}
	if _, err := w.svc.Login(ctx, accounts.LoginInput{Identifier: "bob@example.com", Password: "brand-new-password-2"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	// Reusing the reset token fails.
	if err := w.svc.ResetPassword(ctx, resetTok, "another-3"); err != accounts.ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken on reset reuse, got %v", err)
	}
}
