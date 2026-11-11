// Sirāj worker: scheduled housekeeping (T-51). Every few minutes it deletes expired
// guests, closes challenges past their deadline and rooms nobody came back to, and
// refreshes the leaderboard views. Next: duplicate detection and reminders.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"siraj/internal/config"
	"siraj/internal/db"
	"siraj/internal/modules/catalog"
	"siraj/internal/modules/challenges"
	"siraj/internal/modules/games"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db", "err", err)
		os.Exit(1)
	}
	defer d.Close()

	// Challenges close themselves at their deadline: whoever never started leaves,
	// and the results open for the rest (CH-06).
	challengeSvc := challenges.NewService(d, games.NewService(d, catalog.NewService(d)), nil)

	run := func() {
		n, err := d.Q.DeleteExpiredGuests(ctx)
		if err != nil {
			slog.Error("delete expired guests", "err", err)
		} else if n > 0 {
			slog.Info("expired guests deleted", "count", n)
		}
		closed, err := challengeSvc.ExpireDue(ctx)
		if err != nil {
			slog.Error("close challenges past their deadline", "err", err)
		} else if closed > 0 {
			slog.Info("challenges closed at their deadline", "count", closed)
		}
		// A share link stops working after 24 hours; the rows go with it (CH-10).
		if n, err := d.Q.DeleteExpiredChallengeLinks(ctx); err != nil {
			slog.Error("delete expired challenge links", "err", err)
		} else if n > 0 {
			slog.Info("expired challenge links deleted", "count", n)
		}
		// A room nobody came back to stops holding its code and its players (RM-05).
		if err := d.Q.CloseStaleRooms(ctx); err != nil {
			slog.Error("close stale rooms", "err", err)
		}
		// Sign-in codes, a day after they expired. The query was written for this
		// sweep and migration 18 indexed auth_codes (expires_at) for it, but nothing
		// ever called it: every code ever sent was still in the table, and since
		// migration 19 a create-account code carries the name and the hashed
		// password of a signup that may never have been finished.
		if n, err := d.Q.DeleteOldAuthCodes(ctx); err != nil {
			slog.Error("delete expired sign-in codes", "err", err)
		} else if n > 0 {
			slog.Info("expired sign-in codes deleted", "count", n)
		}
		// The month and all-time boards are materialised views: the weekly table is
		// written as games finish, these two are caught up here (LB-02).
		if err := d.Q.RefreshLeaderboardViews(ctx); err != nil {
			slog.Error("refresh the monthly leaderboard", "err", err)
		}
		if err := d.Q.RefreshLeaderboardAlltime(ctx); err != nil {
			slog.Error("refresh the all-time leaderboard", "err", err)
		}
	}

	run()
	// A deadline is measured in hours, so the round runs every few minutes: a
	// challenge should not sit closed-but-unopened for an hour (CH-06).
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
