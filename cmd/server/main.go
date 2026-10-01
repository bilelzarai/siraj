// Command server runs the Sirāj quiz application.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	app "github.com/bilelzarai/siraj"
	"github.com/bilelzarai/siraj/internal/config"
	"github.com/bilelzarai/siraj/internal/database"
	"github.com/bilelzarai/siraj/internal/handlers"
	"github.com/bilelzarai/siraj/internal/i18n"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
)

// healthcheck makes the binary its own probe, so the container image needs no
// curl and no shell: `server -healthcheck` exits 0 when /healthz answers OK.
var healthcheck = flag.Bool("healthcheck", false,
	"probe the local /healthz endpoint and exit 0 if it is healthy")

func main() {
	flag.Parse()

	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

// probe asks the running process whether it is healthy. It reads APP_ADDR
// directly rather than going through config.Load, because a probe must not
// need DATABASE_URL or SESSION_SECRET to be present in its own environment.
func probe() error {
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz returned %s", resp.Status)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	setupLogging(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- storage
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("database connected")

	if err := database.Migrate(ctx, pool); err != nil {
		return err
	}
	if cfg.Seed {
		if err := database.SeedQuestions(ctx, pool); err != nil {
			return err
		}
	}

	// ---- translations
	bundle, err := i18n.New(cfg.DefaultLocale)
	if err != nil {
		return err
	}
	for _, loc := range i18n.Supported {
		if missing := bundle.MissingKeys(loc.Code); len(missing) > 0 {
			slog.Warn("incomplete translation catalog",
				"locale", loc.Code, "missing", len(missing), "sample", missing[0])
		}
	}

	// ---- wiring
	repo := repository.New(pool)
	hub := service.NewHub()

	auth := service.NewAuth(repo, cfg.SessionLifetime, cfg.SecureCookies)
	game := service.NewGame(repo, hub)
	social := service.NewSocial(repo, hub)
	presence := service.NewPresence(repo)
	importer := service.NewImporter(repo)
	reset := service.NewReset(repo)
	mailer := service.NewMailer(cfg.SMTP)
	translator := service.NewTranslator(cfg.Translate)
	service.LogTranslatorStatus(translator)

	h := handlers.New(cfg, repo, bundle, auth, game, social, hub, presence,
		importer, translator, reset, mailer)

	staticFS, err := resolveStaticFS()
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h.Routes(staticFS),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Long: the SSE stream holds a response open.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go janitor(ctx, repo)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("server stopped")
	return nil
}

// resolveStaticFS serves from disk when STATIC_DIR is set, so CSS and JS
// edits show up on reload during development, and from the embedded copy
// otherwise, which keeps the release a single binary.
func resolveStaticFS() (fs.FS, error) {
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return nil, err
		}
		slog.Info("serving assets from disk", "dir", dir)
		return os.DirFS(dir), nil
	}
	embedded := app.StaticFS()
	return fs.Sub(embedded, "static")
}

// auditRetention is how long the privileged-action trail is kept. Long enough
// to investigate something reported weeks later, short enough that the table
// does not grow without bound for the lifetime of the deployment.
const auditRetention = 365 * 24 * time.Hour

// notificationRetention is how far back the "what happened while you were away"
// history goes. The screen shows the most recent sixty and the badges cover
// what is still outstanding, so older than this is read by nobody.
const notificationRetention = 90 * 24 * time.Hour

// janitor sweeps what nothing else deletes: expired sessions, stale duels, and
// the old end of the audit trail, the notification history and the password
// reset table.
func janitor(ctx context.Context, repo *repository.Repo) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	sweep := func() {
		sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if n, err := repo.PurgeExpiredSessions(sweepCtx); err != nil {
			slog.Warn("session purge failed", "error", err)
		} else if n > 0 {
			slog.Info("expired sessions purged", "count", n)
		}
		if n, err := repo.ExpireStaleChallenges(sweepCtx); err != nil {
			slog.Warn("challenge expiry failed", "error", err)
		} else if n > 0 {
			slog.Info("challenges expired", "count", n)
		}
		if n, err := repo.PurgeAuditOlderThan(sweepCtx, auditRetention); err != nil {
			slog.Warn("audit purge failed", "error", err)
		} else if n > 0 {
			slog.Info("audit entries purged", "count", n, "older_than", auditRetention)
		}
		if n, err := repo.PurgeNotificationsOlderThan(sweepCtx, notificationRetention); err != nil {
			slog.Warn("notification purge failed", "error", err)
		} else if n > 0 {
			slog.Info("notifications purged", "count", n, "older_than", notificationRetention)
		}
		if n, err := repo.PurgeOrphanedNotifications(sweepCtx); err != nil {
			slog.Warn("orphaned notification purge failed", "error", err)
		} else if n > 0 {
			slog.Info("orphaned notifications purged", "count", n)
		}
		// Temporary players, and everything they did. This is what makes
		// "anonymous data is temporary" a fact rather than a promise: the
		// rounds, the matches and the sessions go with the row, by cascade.
		if n, err := repo.PurgeExpiredGuests(sweepCtx); err != nil {
			slog.ErrorContext(sweepCtx, "guest sweep failed", "error", err)
		} else if n > 0 {
			slog.InfoContext(sweepCtx, "temporary players swept", "count", n)
		}

		// The claims that make a resubmitted form harmless. Nothing reads one
		// after a few hours, and a table that only grows eventually matters.
		if n, err := repo.PurgeRequestKeys(sweepCtx); err != nil {
			slog.ErrorContext(sweepCtx, "request key sweep failed", "error", err)
		} else if n > 0 {
			slog.InfoContext(sweepCtx, "request keys swept", "count", n)
		}

		if n, err := repo.PurgeSpentPasswordResets(sweepCtx); err != nil {
			slog.Warn("password reset purge failed", "error", err)
		} else if n > 0 {
			slog.Info("spent password resets purged", "count", n)
		}
	}

	sweep()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

func setupLogging(cfg *config.Config) {
	level := slog.LevelDebug
	var handler slog.Handler

	if cfg.IsProduction() {
		level = slog.LevelInfo
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))
}
