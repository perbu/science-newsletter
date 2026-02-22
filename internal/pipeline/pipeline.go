package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/perbu/science-newsletter/internal/agent"
	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/newsletter"
	"github.com/perbu/science-newsletter/internal/scanner"
)

// Pipeline orchestrates the newsletter creation process:
// load cached works, enrich via LLM, create run + items, generate HTML.
type Pipeline struct {
	queries  *db.Queries
	scanner  *scanner.Scanner
	enricher *agent.Enricher
	cfg      *config.Config
}

// RunResult contains the outcome of a pipeline run.
type RunResult struct {
	RunID          int64
	PapersFound    int
	PapersIncluded int
	HTML           string
}

func New(queries *db.Queries, scn *scanner.Scanner, enricher *agent.Enricher, cfg *config.Config) *Pipeline {
	return &Pipeline{
		queries:  queries,
		scanner:  scn,
		enricher: enricher,
		cfg:      cfg,
	}
}

// Analyze loads cached works for the previous month, enriches them via the LLM,
// and produces a newsletter run. The caller is responsible for validating that the
// researcher has research_interests set.
func (p *Pipeline) Analyze(ctx context.Context, researcherID string) (RunResult, error) {
	researcher, err := p.queries.GetResearcher(ctx, researcherID)
	if err != nil {
		return RunResult{}, fmt.Errorf("get researcher: %w", err)
	}

	run, err := p.queries.CreateNewsletterRun(ctx, researcherID)
	if err != nil {
		return RunResult{}, fmt.Errorf("creating newsletter run: %w", err)
	}

	papers, err := p.scanner.LoadCachedWorks(ctx, researcherID)
	if err != nil {
		p.failRun(ctx, run.ID, err)
		return RunResult{}, fmt.Errorf("loading cached works: %w", err)
	}

	if len(papers) == 0 {
		_ = p.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
			ID:     run.ID,
			Status: "completed",
		})
		return RunResult{RunID: run.ID}, nil
	}

	results, err := p.enricher.FilterAndEnrich(ctx, papers, researcher.Name, researcher.ResearchInterests)
	if err != nil {
		p.failRun(ctx, run.ID, err)
		return RunResult{}, fmt.Errorf("analysis failed: %w", err)
	}

	if len(results) == 0 {
		_ = p.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
			ID:     run.ID,
			Status: "completed",
		})
		return RunResult{RunID: run.ID, PapersFound: len(papers)}, nil
	}

	for _, r := range results {
		isCitedAuthor := int64(0)
		if r.Paper.IsCitedAuthor {
			isCitedAuthor = 1
		}
		_ = p.queries.CreateNewsletterItem(ctx, db.CreateNewsletterItemParams{
			NewsletterRunID:    run.ID,
			OpenalexID:         r.Paper.Work.ID,
			Title:              r.Paper.Work.Title,
			Authors:            scanner.AuthorNames(r.Paper.Work),
			PublicationDate:    r.Paper.Work.PublicationDate,
			Doi:                r.Paper.Work.DOI,
			RelevancyScore:     r.Paper.RelevancyScore,
			Summary:            r.Summary,
			IsCitedAuthorPaper: isCitedAuthor,
			CitedAuthorName:    r.Paper.CitedAuthorName,
		})
	}

	items, _ := p.queries.ListNewsletterItems(ctx, run.ID)
	html, err := newsletter.Generate(researcher, items, p.cfg.Scanner.LookbackDays)
	if err != nil {
		p.failRun(ctx, run.ID, err)
		return RunResult{}, fmt.Errorf("newsletter generation failed: %w", err)
	}

	_ = p.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
		ID:             run.ID,
		Status:         "completed",
		PapersFound:    int64(len(papers)),
		PapersIncluded: int64(len(items)),
		HtmlContent:    html,
	})

	return RunResult{
		RunID:          run.ID,
		PapersFound:    len(papers),
		PapersIncluded: len(items),
		HTML:           html,
	}, nil
}

// FetchAndAnalyze fetches works from OpenAlex first, then runs the analysis pipeline.
// Used by the scheduler for fully automated runs.
func (p *Pipeline) FetchAndAnalyze(ctx context.Context, researcherID string) (RunResult, error) {
	count, err := p.scanner.FetchWorks(ctx, researcherID)
	if err != nil {
		return RunResult{}, fmt.Errorf("fetch works: %w", err)
	}
	slog.Info("fetched works for pipeline", "researcher_id", researcherID, "count", count)

	return p.Analyze(ctx, researcherID)
}

func (p *Pipeline) failRun(ctx context.Context, runID int64, runErr error) {
	_ = p.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
		ID:     runID,
		Status: "failed",
	})
	slog.Error("pipeline run failed", "run_id", runID, "err", runErr)
}
