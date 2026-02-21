package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/perbu/science-newsletter/internal/agent"
	"github.com/perbu/science-newsletter/internal/auth"
	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/database"
	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/email"
	"github.com/perbu/science-newsletter/internal/openalex"
	"github.com/perbu/science-newsletter/internal/scanner"
	syncpkg "github.com/perbu/science-newsletter/internal/sync"
	"github.com/perbu/science-newsletter/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Load config first (this also reads .env)
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Configure structured logging (after .env is loaded)
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))
	slog.Debug("logging initialized", "level", logLevel.String())

	// Open database
	sqlDB, err := sql.Open("sqlite", cfg.Server.DatabasePath+"?_pragma=journal_mode(wal)&_pragma=foreign_keys(on)")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close()

	// Run migrations
	if err := database.RunMigrations(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	slog.Info("migrations complete")

	// Initialize components
	queries := db.New(sqlDB)
	oaClient := openalex.NewClient(cfg.OpenAlex.Email, cfg.OpenAlex.APIKey)
	syncer := syncpkg.New(queries, oaClient)
	scn := scanner.New(queries, oaClient, cfg.Scanner.MaxTopics, cfg.Scanner.MaxCitedAuthors, cfg.Scanner.LookbackDays, cfg.Scanner.ImpactWeight)

	// Initialize enricher (optional — works without API key, just gives default summaries)
	var enricher *agent.Enricher
	if cfg.Gemini.APIKey != "" {
		enricher, err = agent.NewEnricher(cfg.Gemini.APIKey, cfg.Gemini.Model, cfg.Agent)
		if err != nil {
			slog.Warn("failed to create enricher, summaries will be generic", "err", err)
		}
	}
	if enricher == nil {
		slog.Info("no Gemini API key configured, using placeholder summaries")
		enricher = agent.NewNoopEnricher()
	}

	// Initialize mailer (optional — works without API key)
	var mailer *email.Mailer
	if cfg.Resend.APIKey != "" {
		mailer = email.NewMailer(cfg.Resend.APIKey, cfg.Resend.From)
		slog.Info("email sending enabled via Resend")
	} else {
		slog.Info("no Resend API key configured, email sending disabled")
	}

	// Setup HTTP
	handler, err := web.NewHandler(queries, oaClient, syncer, scn, enricher, mailer, cfg)
	if err != nil {
		return fmt.Errorf("create handler: %w", err)
	}

	mux := http.NewServeMux()
	web.SetupRoutes(mux, handler)

	// Wrap with auth middleware
	var root http.Handler = mux
	if len(cfg.Auth.AllowedEmails) > 0 {
		root = auth.Middleware(queries, cfg.Auth, mux)
		slog.Info("auth enabled", "allowed_emails", len(cfg.Auth.AllowedEmails))
	} else {
		slog.Info("auth disabled (no allowed_emails configured)")
	}

	// Start periodic cleanup of expired tokens and sessions
	auth.StartCleanup(context.Background(), queries, 1*time.Hour)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	slog.Info("starting server", "addr", addr)
	return http.ListenAndServe(addr, root)
}
