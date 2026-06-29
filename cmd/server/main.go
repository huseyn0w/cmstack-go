// Command server is the CMStack-Go HTTP entrypoint. It loads config, builds the
// pgx pool, wires services explicitly, and runs an http.Server with graceful
// shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/media"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/content/taxonomy"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/logging"
	"github.com/huseyn0w/cmstack-go/internal/platform/mailer"
	"github.com/huseyn0w/cmstack-go/internal/platform/oauth"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
	"github.com/huseyn0w/cmstack-go/internal/web"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Explicit dependency wiring — assembled here, nowhere else.
	healthSvc := health.NewService(pool)
	healthHandler := health.NewHandler(healthSvc)
	sess := session.NewManager(cfg.IsProduction())

	// Event bus + outbox. The outbox enqueue path is the sqlc-backed repository.
	// The async email listener is registered so account events are marked async
	// (enqueued in-tx); the worker process drains and dispatches them.
	outbox := events.NewOutboxRepository()
	bus := events.NewBus(outbox)
	emailListener := accounts.NewEmailListener(mailer.NewLogMailer(logger), cfg.BaseURL)
	emailListener.Register(bus)

	// Content publish listener (async content.published -> cache invalidation +
	// search reindex seams). Registered on the server bus so the event is marked
	// async and enqueued in-tx; the worker drains and dispatches it.
	postPublishListener := posts.NewPublishListener(logger, nil, nil)
	postPublishListener.Register(bus)

	// Accounts (auth) wiring.
	queries := sqlcgen.New(pool)
	hasher := security.NewPasswordHasher()
	userRepo := accounts.NewUserRepoPG(queries)
	roleRepo := accounts.NewRoleRepoPG(queries)
	tokenRepo := accounts.NewTokenRepoPG(queries)
	oauthRepo := accounts.NewOAuthRepoPG(queries)
	settings := accounts.NewStaticSettings(cfg.SignupEnabled, cfg.EmailVerificationRequired)
	authz := accounts.NewAuthorizer(userRepo, roleRepo)
	authSvc := accounts.NewAuthService(pool, userRepo, roleRepo, tokenRepo, oauthRepo, hasher, bus, settings, nil)

	// Blob storage (M4): one backend, selected by STORAGE_DRIVER, shared by
	// avatars and the media library. Local serves /uploads via uploadsHandler;
	// S3 serves objects directly (uploadsHandler is nil). The profile + media
	// services store blobs through the same Storage interface.
	blobStore, uploadsHandler, uploadsPrefix, err := storage.New(ctx, storage.DriverConfig{
		Driver:            cfg.StorageDriver,
		LocalBaseDir:      cfg.UploadDir,
		LocalPublicPrefix: "/uploads",
		S3: storage.S3Config{
			Bucket:          cfg.S3Bucket,
			Region:          cfg.S3Region,
			Endpoint:        cfg.S3Endpoint,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretKey,
			UsePathStyle:    cfg.S3UsePathStyle,
			PublicBaseURL:   cfg.S3PublicBaseURL,
		},
	})
	if err != nil {
		return err
	}
	profileSvc := accounts.NewProfileService(pool, userRepo, roleRepo, blobStore)

	// Idempotent seed: roles, permissions, mappings, default administrator.
	seeder := accounts.NewSeeder(pool, queries, userRepo, roleRepo, hasher)
	if err := seeder.Seed(ctx, accounts.AdminSeed{Email: cfg.AdminEmail, Password: cfg.AdminPassword}); err != nil {
		return err
	}

	authMW := web.NewAuthMiddleware(sess, userRepo, authz)

	// Social login (OAuth). Providers are registered ONLY when their credentials
	// are present; with none configured, enabled is empty and no buttons/routes
	// are offered. The callback base defaults to BaseURL.
	callbackBase := cfg.OAuthCallbackBase
	if callbackBase == "" {
		callbackBase = cfg.BaseURL
	}
	enabledProviders := oauth.Setup(oauth.Config{
		CallbackBase:       callbackBase,
		SessionKey:         cfg.SessionKey,
		Production:         cfg.IsProduction(),
		GoogleClientID:     cfg.GoogleClientID,
		GoogleClientSecret: cfg.GoogleClientSecret,
		GitHubClientID:     cfg.GitHubClientID,
		GitHubClientSecret: cfg.GitHubClientSecret,
	})
	providerButtons := make([]webtempl.OAuthProviderButton, 0, len(enabledProviders))
	for _, p := range enabledProviders {
		providerButtons = append(providerButtons, webtempl.OAuthProviderButton{Name: p.Name, Label: p.Label})
	}

	authHandler := accounts.NewHandler(authSvc, authMW, security.Token, accounts.NewValidator(), providerButtons...)

	// The OAuth HTTP handler is wired only when at least one provider is enabled.
	var oauthHandler *accounts.OAuthHandler
	if len(enabledProviders) > 0 {
		oauthHandler = accounts.NewOAuthHandler(authSvc, authMW, func(r *http.Request) string {
			return chi.URLParam(r, "provider")
		})
	}

	accountHandler := web.NewAccountHandler(profileSvc, authSvc, roleRepo, authz, security.Token, cfg.BaseURL)

	// Posts (M2a) wiring: repos over the shared querier, the role-key adapter for
	// the ownership gate, and the post service.
	postRepo := posts.NewRepoPG(queries)
	revisionRepo := posts.NewRevisionRepoPG(queries)
	roleKeys := posts.NewRoleKeyResolver(userRepo, roleRepo)
	postSvc := posts.NewService(pool, postRepo, revisionRepo, authz, roleKeys, bus, nil)

	// Taxonomies (M3): category + tag repos/services, and the combined assigner
	// that persists a post's category/tag M2M inside the post write tx. The post
	// service is given the assigner so Create/Update commit the post and its
	// associations atomically.
	categoryRepo := categories.NewRepoPG(queries)
	categorySvc := categories.NewService(pool, categoryRepo, authz)
	tagRepo := tags.NewRepoPG(queries)
	tagSvc := tags.NewService(pool, tagRepo, authz)
	postSvc.WithTaxonomy(taxonomy.NewAssigner(categorySvc, tagSvc))

	// Pages (M2b) wiring: repos over the shared querier + the page service. Pages
	// have no per-author ownership, so the service needs no role resolver.
	pageRepo := pages.NewRepoPG(queries)
	pageRevisionRepo := pages.NewRevisionRepoPG(queries)
	pageSvc := pages.NewService(pool, pageRepo, pageRevisionRepo, authz, bus, nil)

	// Services (M2b) wiring: repos over the shared querier + the service Manager.
	serviceRepo := services.NewRepoPG(queries)
	serviceRevisionRepo := services.NewRevisionRepoPG(queries)
	serviceMgr := services.NewManager(pool, serviceRepo, serviceRevisionRepo, authz, bus, nil)

	// Media (M4) wiring: the configured blob store + magic-byte validator +
	// thumbnailer, behind the media service. The async media.uploaded listener is
	// registered on the server bus so the event is enqueued in-tx.
	mediaRepo := media.NewRepoPG(queries)
	mediaValidator := storage.NewValidator(cfg.MediaMaxBytes, 0)
	mediaSvc := media.NewService(pool, mediaRepo, blobStore, mediaValidator, media.NewThumbnailer(), authz, bus, nil)
	media.NewUploadListener(logger).Register(bus)

	authorHandler := web.NewAuthorHandler(profileSvc, postSvc, "CMStack", cfg.BaseURL)

	handler := web.Router(web.Deps{
		Config:        cfg,
		Health:        healthHandler,
		Bus:           bus,
		Session:       sess,
		StaticDir:     web.StaticDirDefault(),
		LoggerHandler: logging.RequestLogger(logger),
		Auth:          authHandler,
		AuthMW:        authMW,
		CSRFFunc:      security.Token,
		Authz:         authz,
		Roles:         roleRepo,
		OAuth:         oauthHandler,
		Account:       accountHandler,
		Author:        authorHandler,
		Uploads:       uploadsHandler,
		UploadsPrefix: uploadsPrefix,
		PostAdminSvc:  postSvc,
		PostPublicSvc: postSvc,
		Authors:       userRepo,
		SiteName:      "CMStack",

		PageAdminSvc:  pageSvc,
		PagePublicSvc: pageSvc,

		ServiceAdminSvc:  serviceMgr,
		ServicePublicSvc: serviceMgr,

		// Taxonomies (M3).
		CategoryAdminSvc:  categorySvc,
		CategoryReadSvc:   categorySvc,
		CategoryPublicSvc: categorySvc,
		CategoryPostSvc:   categorySvc,
		TagAdminSvc:       tagSvc,
		TagReadSvc:        tagSvc,
		TagPublicSvc:      tagSvc,
		TagPostSvc:        tagSvc,
		PostHydrateSvc:    postSvc,

		// Media (M4).
		MediaAdminSvc: mediaSvc,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}
