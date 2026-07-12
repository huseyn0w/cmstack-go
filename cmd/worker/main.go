// Command worker is the async background process for Agentic CMS-Go. It hosts the
// outbox relay loop, honestly constructed over a real pgx pool, the sqlc
// querier, and the wired event bus. Each tick claims unprocessed outbox rows
// (FOR UPDATE SKIP LOCKED) inside a transaction, dispatches each to its
// registered async handler (e.g. the email listener), and marks delivered rows
// processed atomically.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/contact"
	"github.com/huseyn0w/agentic-cms-go/internal/content/comments"
	"github.com/huseyn0w/agentic-cms-go/internal/content/media"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db/sqlcgen"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/logging"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/mailer"
	sitesettings "github.com/huseyn0w/agentic-cms-go/internal/settings"
	"github.com/huseyn0w/agentic-cms-go/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
}

// buildMailer selects the transactional-email backend from config (M14) and logs
// the chosen driver. On an smtp construction error it falls back to the dev
// LogMailer and logs, so the worker still boots. The returned instance is shared
// by every listener the relay dispatches to (auth, comment, contact).
func buildMailer(cfg config.Config, logger *slog.Logger) mailer.Mailer {
	from := cfg.MailFrom
	if from == "" {
		from = cfg.AdminEmail
	}
	m, err := mailer.New(mailer.Config{
		Driver: cfg.MailDriver,
		SMTP: mailer.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     from,
			FromName: cfg.MailFromName,
			TLS:      cfg.SMTPTLS,
		},
	}, logger)
	if err != nil {
		logger.Error("mailer init failed; falling back to log mailer", "driver", cfg.MailDriver, "err", err)
		return mailer.NewLogMailer(logger)
	}
	logger.Info("mailer configured", "driver", cfg.MailDriver)
	return m
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

	// Honest relay wiring: real pool + sqlc querier + the bus as dispatcher. The
	// email listener is registered on the bus so the relay routes drained outbox
	// rows to it after commit. The bus needs no outbox enqueuer here (the worker
	// only dispatches; the server enqueues).
	bus := events.NewBus(nil)

	// Transactional email backend (M14). The worker drains the async outbox and
	// actually sends, so it MUST use the same selected mailer as the server. On an
	// smtp construction error it falls back to LogMailer so the worker still boots.
	appMailer := buildMailer(cfg, logger)

	emailListener := accounts.NewEmailListener(appMailer, cfg.BaseURL)
	emailListener.Register(bus)
	// The content publish listener must also be registered on the WORKER bus so
	// the relay can dispatch the async content.published events the server
	// enqueued (cache invalidation + search reindex seams).
	posts.NewPublishListener(logger, nil, nil).Register(bus)
	// The media upload listener must likewise be on the WORKER bus so the relay
	// dispatches the async media.uploaded events the server enqueued (M4 seams).
	media.NewUploadListener(logger).Register(bus)

	relay := events.NewRelay(pool, bus, 100, logger)

	// Scheduled-publishing scan: a periodic ticker calls PostService.PublishDue,
	// flipping due DRAFT-with-scheduled_at posts to PUBLISHED. This is the simple
	// reuse of the existing ticker pattern; river (durable jobs) is the documented
	// upgrade path if scheduling needs at-least-once durability across crashes.
	queries := sqlcgen.New(pool)
	postRepo := posts.NewRepoPG(queries)
	revisionRepo := posts.NewRevisionRepoPG(queries)
	userRepo := accounts.NewUserRepoPG(queries)
	roleRepo := accounts.NewRoleRepoPG(queries)
	authz := accounts.NewAuthorizer(userRepo, roleRepo)
	roleKeys := posts.NewRoleKeyResolver(userRepo, roleRepo)
	// The publish bus needs an outbox enqueuer so PublishDue can emit the async
	// content.published event inside its tx; reuse the sqlc-backed outbox repo.
	publishBus := events.NewBus(events.NewOutboxRepository())
	posts.NewPublishListener(logger, nil, nil).Register(publishBus)
	postSvc := posts.NewService(pool, postRepo, revisionRepo, authz, roleKeys, publishBus, nil)

	// The comment notification listener must be on the WORKER bus so the relay
	// dispatches the async comment.created events the server enqueued: it resolves
	// the recipients (post author + moderators) and sends the moderation email.
	commentAdapters := web.NewCommentAdapters(
		postSvc,
		postRepo,
		web.NewUserEmailRepo(userRepo, func(u accounts.User) string { return u.Email }),
	)
	comments.NewNotificationListener(
		logger,
		commentAdapters,
		web.NewCommentNotifierAdapter(appMailer),
		cfg.BaseURL,
	).Register(bus)

	// The contact notify listener (M12) must likewise be on the WORKER bus so the
	// relay dispatches the async contact.submitted events the server enqueued: it
	// resolves the recipient (settings `contact_recipient` → ContactRecipient →
	// AdminEmail) and sends the contact-notification email.
	settingsSvc := sitesettings.NewService(sitesettings.NewRepoPG(queries))
	contact.NewNotifyListener(
		logger,
		web.NewContactRecipientResolver(settingsSvc, cfg.ContactRecipient, cfg.AdminEmail),
		web.NewContactNotifierAdapter(appMailer),
	).Register(bus)

	logger.Info("worker started", "env", cfg.AppEnv)

	const interval = 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	const scheduleInterval = 30 * time.Second
	scheduleTicker := time.NewTicker(scheduleInterval)
	defer scheduleTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped cleanly")
			return nil
		case <-ticker.C:
			n, err := relay.Drain(ctx)
			if err != nil {
				logger.Error("outbox relay drain failed", "err", err)
				continue
			}
			logger.Debug("outbox relay tick", "observed", n)
		case <-scheduleTicker.C:
			n, err := postSvc.PublishDue(ctx)
			if err != nil {
				logger.Error("scheduled publish scan failed", "err", err)
				continue
			}
			if n > 0 {
				logger.Info("scheduled posts published", "count", n)
			}
		}
	}
}
