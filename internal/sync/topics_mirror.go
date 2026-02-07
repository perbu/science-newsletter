package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/perbu/science-newsletter/internal/database/db"
)

// MirrorTopics downloads all OpenAlex topics and upserts them into the openalex_topics table.
func (s *Syncer) MirrorTopics(ctx context.Context) (int, error) {
	slog.Info("starting topic mirror from OpenAlex")
	start := time.Now()

	topics, err := s.client.ListAllTopics(ctx)
	if err != nil {
		return 0, fmt.Errorf("list all topics: %w", err)
	}

	for _, t := range topics {
		keywords, err := json.Marshal(t.Keywords)
		if err != nil {
			keywords = []byte("[]")
		}

		err = s.queries.UpsertOpenAlexTopic(ctx, db.UpsertOpenAlexTopicParams{
			OpenalexID:   t.ID,
			DisplayName:  t.DisplayName,
			Description:  t.Description,
			Keywords:     string(keywords),
			SubfieldID:   t.Subfield.ID,
			SubfieldName: t.Subfield.DisplayName,
			FieldID:      t.Field.ID,
			FieldName:    t.Field.DisplayName,
			DomainID:     t.Domain.ID,
			DomainName:   t.Domain.DisplayName,
			WorksCount:   int64(t.WorksCount),
		})
		if err != nil {
			slog.Warn("upsert topic failed", "topic", t.DisplayName, "err", err)
		}
	}

	slog.Info("topic mirror complete", "topics", len(topics), "duration", time.Since(start))
	return len(topics), nil
}
