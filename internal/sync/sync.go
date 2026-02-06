package sync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/openalex"
)

type Syncer struct {
	queries *db.Queries
	client  *openalex.Client
}

func New(queries *db.Queries, client *openalex.Client) *Syncer {
	return &Syncer{queries: queries, client: client}
}

// SyncResearcher fetches the researcher's profile, works, co-authors, and topics from OpenAlex.
func (s *Syncer) SyncResearcher(ctx context.Context, researcherID int64) error {
	start := time.Now()
	researcher, err := s.queries.GetResearcher(ctx, researcherID)
	if err != nil {
		return fmt.Errorf("get researcher: %w", err)
	}

	slog.Info("syncing researcher", "name", researcher.Name, "openalex_id", researcher.OpenalexID)

	// Fetch author profile
	author, err := s.client.GetAuthor(ctx, researcher.OpenalexID)
	if err != nil {
		return fmt.Errorf("fetch author: %w", err)
	}

	// Update researcher stats
	affiliation := ""
	if len(author.LastKnownInstitutions) > 0 {
		affiliation = author.LastKnownInstitutions[0].DisplayName
	}
	slog.Debug("updating researcher stats",
		"name", author.DisplayName,
		"affiliation", affiliation,
		"h_index", author.SummaryStats.HIndex,
		"works_count", author.WorksCount,
		"cited_by_count", author.CitedByCount,
	)
	err = s.queries.UpdateResearcherStats(ctx, db.UpdateResearcherStatsParams{
		ID:           researcherID,
		Name:         author.DisplayName,
		Affiliation:  affiliation,
		HIndex:       int64(author.SummaryStats.HIndex),
		WorksCount:   int64(author.WorksCount),
		CitedByCount: int64(author.CitedByCount),
	})
	if err != nil {
		return fmt.Errorf("update researcher stats: %w", err)
	}

	// Sync topics
	if err := s.syncTopics(ctx, researcherID, author.Topics); err != nil {
		return fmt.Errorf("sync topics: %w", err)
	}

	// Fetch and sync works + co-authors
	if err := s.syncWorks(ctx, researcherID, researcher.OpenalexID); err != nil {
		return fmt.Errorf("sync works: %w", err)
	}

	slog.Info("sync complete", "name", researcher.Name, "duration", time.Since(start))
	return nil
}

func (s *Syncer) syncTopics(ctx context.Context, researcherID int64, topics []openalex.AuthorTopic) error {
	slog.Debug("syncing topics", "count", len(topics))
	// Only delete OpenAlex-sourced topics; manual topics are preserved.
	if err := s.queries.DeleteOpenAlexTopicsByResearcher(ctx, researcherID); err != nil {
		return err
	}

	for _, t := range topics {
		slog.Debug("upserting topic",
			"name", t.DisplayName,
			"subfield", t.Subfield.DisplayName,
			"field", t.Field.DisplayName,
			"score", t.Score,
		)
		err := s.queries.UpsertTopic(ctx, db.UpsertTopicParams{
			ResearcherID: researcherID,
			OpenalexID:   t.ID,
			Name:         t.DisplayName,
			Subfield:     t.Subfield.DisplayName,
			Field:        t.Field.DisplayName,
			Domain:       t.Domain.DisplayName,
			Score:        t.Score,
			Source:       "openalex",
		})
		if err != nil {
			return fmt.Errorf("upsert topic %s: %w", t.DisplayName, err)
		}
	}
	slog.Info("synced topics", "count", len(topics))
	return nil
}

func (s *Syncer) syncWorks(ctx context.Context, researcherID int64, authorOpenAlexID string) error {
	works, err := s.client.GetAuthorWorks(ctx, authorOpenAlexID)
	if err != nil {
		return fmt.Errorf("fetch works: %w", err)
	}

	slog.Info("syncing works and co-authors", "works_count", len(works))
	coauthorsSeen := 0

	for _, w := range works {
		doi := w.DOI
		err := s.queries.UpsertPublication(ctx, db.UpsertPublicationParams{
			OpenalexID:      w.ID,
			ResearcherID:    researcherID,
			Title:           w.Title,
			PublicationDate: w.PublicationDate,
			Doi:             doi,
			CitedByCount:    int64(w.CitedByCount),
		})
		if err != nil {
			slog.Warn("upsert publication failed", "title", w.Title, "err", err)
			continue
		}

		// Extract co-authors (everyone except the researcher)
		for _, authorship := range w.Authorships {
			if authorship.Author.ID == authorOpenAlexID {
				continue
			}
			coaffiliation := ""
			if len(authorship.Institutions) > 0 {
				names := make([]string, len(authorship.Institutions))
				for i, inst := range authorship.Institutions {
					names[i] = inst.DisplayName
				}
				coaffiliation = strings.Join(names, "; ")
			}
			err := s.queries.UpsertCoAuthor(ctx, db.UpsertCoAuthorParams{
				ResearcherID:     researcherID,
				OpenalexID:       authorship.Author.ID,
				Name:             authorship.Author.DisplayName,
				Affiliation:      coaffiliation,
				LastCollaborated: w.PublicationDate,
			})
			if err != nil {
				slog.Warn("upsert co-author failed", "name", authorship.Author.DisplayName, "err", err)
			} else {
				coauthorsSeen++
			}
		}
	}
	slog.Info("works sync complete", "publications", len(works), "coauthor_upserts", coauthorsSeen)
	return nil
}
