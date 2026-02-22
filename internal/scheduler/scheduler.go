package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/email"
	"github.com/perbu/science-newsletter/internal/pipeline"
	"github.com/perbu/science-newsletter/internal/scanner"
)

// Scheduler runs the newsletter pipeline monthly for all eligible researchers.
type Scheduler struct {
	queries  *db.Queries
	pipeline *pipeline.Pipeline
	mailer   *email.Mailer
}

func New(queries *db.Queries, pip *pipeline.Pipeline, mailer *email.Mailer) *Scheduler {
	return &Scheduler{
		queries:  queries,
		pipeline: pip,
		mailer:   mailer,
	}
}

// Start launches a background goroutine that checks hourly whether it's the 1st
// of the month and runs the newsletter pipeline for all eligible researchers.
// Hourly checks avoid the drift problem where a daily ticker could miss the 1st.
func (s *Scheduler) Start(ctx context.Context) {
	go func() {
		slog.Info("monthly newsletter scheduler started")
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("scheduler stopped")
				return
			case <-ticker.C:
				if time.Now().Day() != 1 {
					continue
				}
				s.runAll(ctx)
			}
		}
	}()
}

func (s *Scheduler) runAll(ctx context.Context) {
	slog.Info("scheduler: starting monthly newsletter run")

	researchers, err := s.queries.ListResearchersWithEmail(ctx)
	if err != nil {
		slog.Error("scheduler: failed to list researchers", "err", err)
		return
	}

	var processed, skipped, failed int
	for _, r := range researchers {
		if r.ResearchInterests == "" {
			slog.Info("scheduler: skipping researcher without research interests", "name", r.Name, "id", r.ID)
			skipped++
			continue
		}

		// Idempotency: skip if a completed run already exists this month
		monthStart := scanner.MonthStart(time.Now())
		alreadyRun, err := s.queries.HasCompletedRunSince(ctx, db.HasCompletedRunSinceParams{
			ResearcherID: r.ID,
			CreatedAt:    monthStart,
		})
		if err != nil {
			slog.Error("scheduler: failed to check existing runs", "researcher", r.Name, "err", err)
			failed++
			continue
		}
		if alreadyRun {
			slog.Info("scheduler: already completed this month, skipping", "researcher", r.Name)
			skipped++
			continue
		}

		if err := s.processResearcher(ctx, r); err != nil {
			slog.Error("scheduler: failed to process researcher", "name", r.Name, "err", err)
			failed++
			continue
		}
		processed++
	}

	slog.Info("scheduler: monthly run complete",
		"processed", processed,
		"skipped", skipped,
		"failed", failed,
		"total", len(researchers),
	)
}

func (s *Scheduler) processResearcher(ctx context.Context, researcher db.Researcher) error {
	slog.Info("scheduler: processing researcher", "name", researcher.Name, "id", researcher.ID)

	result, err := s.pipeline.FetchAndAnalyze(ctx, researcher.ID)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}

	if result.PapersIncluded == 0 {
		slog.Info("scheduler: no papers included, skipping email", "researcher", researcher.Name)
		return nil
	}

	// Format subject with previous month name (the month we scanned)
	prevMonth := scanner.MonthStart(time.Now()).AddDate(0, -1, 0)
	subject := fmt.Sprintf("Science Newsletter - %s", prevMonth.Format("January 2006"))

	if err := s.mailer.Send(ctx, []string{researcher.Email.String}, subject, result.HTML); err != nil {
		return fmt.Errorf("send email to %s: %w", researcher.Email.String, err)
	}

	slog.Info("scheduler: newsletter emailed",
		"researcher", researcher.Name,
		"email", researcher.Email.String,
		"papers", result.PapersIncluded,
		"run_id", result.RunID,
	)
	return nil
}
