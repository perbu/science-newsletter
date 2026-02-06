package scanner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/openalex"
)

// CandidatePaper is a paper found during scanning with its relevancy metadata.
type CandidatePaper struct {
	Work            openalex.Work
	RelevancyScore  float64
	SourceCitedness float64
	SourceName      string
	IsCoauthor      bool
	CoauthorName    string
}

type Scanner struct {
	queries      *db.Queries
	client       *openalex.Client
	maxTopics    int
	maxCoauthors int
	lookbackDays int
	impactWeight float64
}

func New(queries *db.Queries, client *openalex.Client, maxTopics, maxCoauthors, lookbackDays int, impactWeight float64) *Scanner {
	return &Scanner{
		queries:      queries,
		client:       client,
		maxTopics:    maxTopics,
		maxCoauthors: maxCoauthors,
		lookbackDays: lookbackDays,
		impactWeight: impactWeight,
	}
}

// FetchWorks fetches recent papers from OpenAlex for the current ISO week and caches them in the DB.
// Returns the number of works stored.
func (s *Scanner) FetchWorks(ctx context.Context, researcherID int64) (int, error) {
	researcher, err := s.queries.GetResearcher(ctx, researcherID)
	if err != nil {
		return 0, fmt.Errorf("get researcher: %w", err)
	}

	topics, err := s.queries.ListTopTopicsByResearcher(ctx, db.ListTopTopicsByResearcherParams{
		ResearcherID: researcherID,
		Limit:        int64(s.maxTopics),
	})
	if err != nil {
		return 0, fmt.Errorf("list topics: %w", err)
	}
	if len(topics) == 0 {
		return 0, fmt.Errorf("researcher %s has no topics, sync first", researcher.Name)
	}

	for _, t := range topics {
		slog.Debug("search topic", "name", t.Name, "openalex_id", t.OpenalexID, "score", t.Score)
	}

	topicIDs := make([]string, len(topics))
	for i, t := range topics {
		topicIDs[i] = t.OpenalexID
	}

	weekStart := WeekStart(time.Now())
	scanWeek := weekStart.Format("2006-01-02")

	since := time.Now().AddDate(0, 0, -s.lookbackDays)
	slog.Info("fetching works from OpenAlex",
		"researcher", researcher.Name,
		"topic_count", len(topicIDs),
		"since", since.Format("2006-01-02"),
		"scan_week", scanWeek,
	)

	works, err := s.client.SearchRecentWorks(ctx, topicIDs, since)
	if err != nil {
		return 0, fmt.Errorf("search recent works: %w", err)
	}

	slog.Info("topic search returned works", "raw_count", len(works))

	// Co-author search
	if s.maxCoauthors > 0 {
		coauthors, err := s.queries.ListTopCoAuthorsByResearcher(ctx, db.ListTopCoAuthorsByResearcherParams{
			ResearcherID: researcherID,
			Limit:        int64(s.maxCoauthors),
		})
		if err != nil {
			return 0, fmt.Errorf("list co-authors: %w", err)
		}

		if len(coauthors) > 0 {
			authorIDs := make([]string, len(coauthors))
			for i, ca := range coauthors {
				authorIDs[i] = ca.OpenalexID
			}
			slog.Info("searching co-author works", "coauthor_count", len(authorIDs))

			coauthorWorks, err := s.client.SearchRecentWorksByAuthors(ctx, authorIDs, since)
			if err != nil {
				return 0, fmt.Errorf("search co-author works: %w", err)
			}
			slog.Info("co-author search returned works", "raw_count", len(coauthorWorks))
			works = append(works, coauthorWorks...)
		} else {
			slog.Info("no co-authors found, skipping co-author search")
		}
	}

	// Deduplicate by OpenAlex ID
	seen := make(map[string]bool)
	var unique []openalex.Work
	for _, w := range works {
		if !seen[w.ID] {
			seen[w.ID] = true
			unique = append(unique, w)
		}
	}
	slog.Info("deduplicated works", "unique", len(unique), "duplicates_removed", len(works)-len(unique))

	// Collect unique source IDs from works
	sourceIDSet := make(map[string]bool)
	for _, w := range unique {
		if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil && w.PrimaryLocation.Source.ID != "" {
			sourceIDSet[w.PrimaryLocation.Source.ID] = true
		}
	}

	// Batch-fetch source details for citedness
	sourceCitedness := make(map[string]float64)
	sourceNames := make(map[string]string)
	if len(sourceIDSet) > 0 {
		sourceIDs := make([]string, 0, len(sourceIDSet))
		for id := range sourceIDSet {
			sourceIDs = append(sourceIDs, id)
		}
		slog.Info("fetching source details for citedness", "source_count", len(sourceIDs))

		sources, err := s.client.GetSources(ctx, sourceIDs)
		if err != nil {
			slog.Warn("failed to fetch sources, continuing without citedness", "err", err)
		} else {
			for _, src := range sources {
				sourceCitedness[src.ID] = src.SummaryStats.TwoYrMeanCitedness
				sourceNames[src.ID] = src.DisplayName
			}
			slog.Info("fetched source citedness", "sources_found", len(sources))
		}
	}

	// Cache each work in the DB
	for _, w := range unique {
		authorshipsJSON, err := json.Marshal(w.Authorships)
		if err != nil {
			return 0, fmt.Errorf("marshal authorships for %s: %w", w.ID, err)
		}
		topicsJSON, err := json.Marshal(w.Topics)
		if err != nil {
			return 0, fmt.Errorf("marshal topics for %s: %w", w.ID, err)
		}

		abstract := w.AbstractText()

		var srcName string
		var srcCitedness float64
		if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
			srcID := w.PrimaryLocation.Source.ID
			srcName = sourceNames[srcID]
			if srcName == "" {
				srcName = w.PrimaryLocation.Source.DisplayName
			}
			srcCitedness = sourceCitedness[srcID]
		}

		err = s.queries.UpsertScannedWork(ctx, db.UpsertScannedWorkParams{
			ResearcherID:    researcherID,
			ScanWeek:        scanWeek,
			OpenalexID:      w.ID,
			Title:           w.Title,
			Doi:             sql.NullString{String: w.DOI, Valid: w.DOI != ""},
			PublicationDate: sql.NullString{String: w.PublicationDate, Valid: w.PublicationDate != ""},
			CitedByCount:    sql.NullInt64{Int64: int64(w.CitedByCount), Valid: true},
			Abstract:        sql.NullString{String: abstract, Valid: abstract != ""},
			Authorships:     string(authorshipsJSON),
			Topics:          string(topicsJSON),
			SourceName:      srcName,
			SourceCitedness: srcCitedness,
		})
		if err != nil {
			return 0, fmt.Errorf("upsert scanned work %s: %w", w.ID, err)
		}
	}

	slog.Info("cached works in DB", "count", len(unique), "scan_week", scanWeek)
	return len(unique), nil
}

// AnalyzeWorks scores cached works for a given scan week and returns candidates above the threshold.
func (s *Scanner) AnalyzeWorks(ctx context.Context, researcherID int64, scanWeek time.Time) ([]CandidatePaper, error) {
	researcher, err := s.queries.GetResearcher(ctx, researcherID)
	if err != nil {
		return nil, fmt.Errorf("get researcher: %w", err)
	}

	topics, err := s.queries.ListTopTopicsByResearcher(ctx, db.ListTopTopicsByResearcherParams{
		ResearcherID: researcherID,
		Limit:        int64(s.maxTopics),
	})
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("researcher %s has no topics, sync first", researcher.Name)
	}

	weekStr := scanWeek.Format("2006-01-02")
	cached, err := s.queries.ListScannedWorksByWeek(ctx, db.ListScannedWorksByWeekParams{
		ResearcherID: researcherID,
		ScanWeek:     weekStr,
	})
	if err != nil {
		return nil, fmt.Errorf("list cached works: %w", err)
	}

	slog.Info("analyzing cached works",
		"researcher", researcher.Name,
		"scan_week", weekStr,
		"cached_count", len(cached),
		"threshold", researcher.RelevancyThreshold,
	)

	var candidates []CandidatePaper
	belowThreshold := 0

	for _, sw := range cached {
		w, err := cachedWorkToOpenAlex(sw)
		if err != nil {
			slog.Warn("skipping malformed cached work", "id", sw.OpenalexID, "error", err)
			continue
		}

		score := ScorePaper(w, topics, sw.SourceCitedness, s.impactWeight)

		// Check if any author is a co-author
		isCoauthor := false
		coauthorName := ""
		for _, a := range w.Authorships {
			ok, err := s.queries.IsCoAuthor(ctx, db.IsCoAuthorParams{
				ResearcherID: researcherID,
				OpenalexID:   a.Author.ID,
			})
			if err != nil {
				continue
			}
			if ok {
				isCoauthor = true
				coauthorName = a.Author.DisplayName
				break
			}
		}

		if score >= researcher.RelevancyThreshold || isCoauthor {
			slog.Debug("paper included",
				"title", w.Title,
				"score", fmt.Sprintf("%.3f", score),
				"source_citedness", fmt.Sprintf("%.2f", sw.SourceCitedness),
				"source_name", sw.SourceName,
				"is_coauthor", isCoauthor,
				"coauthor", coauthorName,
			)
			candidates = append(candidates, CandidatePaper{
				Work:            w,
				RelevancyScore:  score,
				SourceCitedness: sw.SourceCitedness,
				SourceName:      sw.SourceName,
				IsCoauthor:      isCoauthor,
				CoauthorName:    coauthorName,
			})
		} else {
			belowThreshold++
			slog.Debug("paper excluded",
				"title", w.Title,
				"score", fmt.Sprintf("%.3f", score),
				"threshold", researcher.RelevancyThreshold,
			)
		}
	}

	slog.Info("analysis complete",
		"researcher", researcher.Name,
		"total_cached", len(cached),
		"included", len(candidates),
		"below_threshold", belowThreshold,
		"threshold", researcher.RelevancyThreshold,
	)

	return candidates, nil
}

// RunScan fetches recent works and analyzes them in one step.
func (s *Scanner) RunScan(ctx context.Context, researcherID int64) ([]CandidatePaper, error) {
	start := time.Now()

	_, err := s.FetchWorks(ctx, researcherID)
	if err != nil {
		return nil, err
	}

	weekStart := WeekStart(time.Now())
	candidates, err := s.AnalyzeWorks(ctx, researcherID, weekStart)
	if err != nil {
		return nil, err
	}

	slog.Info("scan complete", "duration", time.Since(start))
	return candidates, nil
}

// cachedWorkToOpenAlex reconstructs an openalex.Work from a cached ScannedWork row.
func cachedWorkToOpenAlex(sw db.ScannedWork) (openalex.Work, error) {
	var authorships []openalex.Authorship
	if err := json.Unmarshal([]byte(sw.Authorships), &authorships); err != nil {
		return openalex.Work{}, fmt.Errorf("unmarshal authorships: %w", err)
	}

	var topics []openalex.WorkTopic
	if err := json.Unmarshal([]byte(sw.Topics), &topics); err != nil {
		return openalex.Work{}, fmt.Errorf("unmarshal topics: %w", err)
	}

	return openalex.Work{
		ID:              sw.OpenalexID,
		Title:           sw.Title,
		DOI:             sw.Doi.String,
		PublicationDate: sw.PublicationDate.String,
		CitedByCount:    int(sw.CitedByCount.Int64),
		Authorships:     authorships,
		Topics:          topics,
		// AbstractInvertedIndex not needed — abstract was already reconstructed at fetch time.
		// Downstream code uses Work fields directly; AbstractText() won't work but isn't called post-cache.
	}, nil
}

// AuthorNames returns a comma-separated list of author display names for a work.
func AuthorNames(w openalex.Work) string {
	names := make([]string, len(w.Authorships))
	for i, a := range w.Authorships {
		names[i] = a.Author.DisplayName
	}
	return strings.Join(names, ", ")
}
