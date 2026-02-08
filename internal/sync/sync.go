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

// SyncResearcher fetches the researcher's profile, works, cited authors, and topics from OpenAlex.
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

	// Fetch and sync works + cited authors
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
	// Only fetch last 5 years of publications for cited author discovery
	fiveYearsAgo := time.Now().AddDate(-5, 0, 0)
	works, err := s.client.GetAuthorWorks(ctx, authorOpenAlexID, fiveYearsAgo)
	if err != nil {
		return fmt.Errorf("fetch works: %w", err)
	}

	slog.Info("syncing works", "works_count", len(works), "since", fiveYearsAgo.Format("2006-01-02"))

	// Collect all referenced work IDs across all publications
	refWorkIDSet := make(map[string]bool)
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

		for _, ref := range w.ReferencedWorks {
			refWorkIDSet[ref] = true
		}
	}

	slog.Info("publications synced, collecting cited authors",
		"publications", len(works),
		"unique_referenced_works", len(refWorkIDSet),
	)

	// Strip URL prefixes and batch-fetch referenced works
	refIDs := make([]string, 0, len(refWorkIDSet))
	for ref := range refWorkIDSet {
		// OpenAlex referenced_works are full URLs like "https://openalex.org/W123"
		id := ref
		if idx := strings.LastIndex(ref, "/"); idx >= 0 {
			id = ref[idx+1:]
		}
		refIDs = append(refIDs, id)
	}

	refWorks, err := s.client.GetWorksByIDs(ctx, refIDs)
	if err != nil {
		return fmt.Errorf("fetch referenced works: %w", err)
	}

	// Count citation frequency per author
	type authorInfo struct {
		name        string
		affiliation string
		count       int
	}
	authorCounts := make(map[string]*authorInfo) // keyed by author OpenAlex ID
	for _, rw := range refWorks {
		for _, a := range rw.Authorships {
			if a.Author.ID == authorOpenAlexID {
				continue // exclude the researcher themselves
			}
			info, ok := authorCounts[a.Author.ID]
			if !ok {
				aff := ""
				if len(a.Institutions) > 0 {
					names := make([]string, len(a.Institutions))
					for i, inst := range a.Institutions {
						names[i] = inst.DisplayName
					}
					aff = strings.Join(names, "; ")
				}
				info = &authorInfo{
					name:        a.Author.DisplayName,
					affiliation: aff,
				}
				authorCounts[a.Author.ID] = info
			}
			info.count++
		}
	}

	slog.Info("cited author counts computed", "unique_authors", len(authorCounts))

	// Merge-safe sync: upsert fresh authors, then remove stale synced ones.
	// This preserves manual authors and active/inactive state.
	freshIDs := make(map[string]bool)
	upserted := 0
	for authorID, info := range authorCounts {
		if authorID == "" {
			slog.Debug("skipping cited author with empty ID", "name", info.name)
			continue
		}
		freshIDs[authorID] = true
		err := s.queries.UpsertSyncedCitedAuthor(ctx, db.UpsertSyncedCitedAuthorParams{
			ResearcherID:  researcherID,
			OpenalexID:    authorID,
			Name:          info.name,
			Affiliation:   info.affiliation,
			CitationCount: int64(info.count),
		})
		if err != nil {
			slog.Warn("upsert cited author failed", "name", info.name, "err", err)
		} else {
			upserted++
		}
	}

	// Delete stale synced authors (those no longer in the fresh set)
	existingSynced, err := s.queries.ListSyncedCitedAuthorIDs(ctx, researcherID)
	if err != nil {
		return fmt.Errorf("list synced cited author IDs: %w", err)
	}
	staleDeleted := 0
	for _, existingID := range existingSynced {
		if !freshIDs[existingID] {
			if err := s.queries.DeleteSyncedCitedAuthor(ctx, db.DeleteSyncedCitedAuthorParams{
				ResearcherID: researcherID,
				OpenalexID:   existingID,
			}); err != nil {
				slog.Warn("delete stale cited author failed", "openalex_id", existingID, "err", err)
			} else {
				staleDeleted++
			}
		}
	}

	slog.Info("works sync complete",
		"publications", len(works),
		"cited_authors_upserted", upserted,
		"stale_deleted", staleDeleted,
	)
	return nil
}
