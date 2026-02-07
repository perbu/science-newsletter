package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

// TopicForMapping is the minimal topic representation sent to the LLM.
type TopicForMapping struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

// TopicMapping is a single topic mapping returned by the LLM.
type TopicMapping struct {
	OpenAlexID string `json:"openalex_id"`
	Score      int    `json:"score"`
}

// MapTopics uses Gemini to map a researcher's interests to specific OpenAlex topic IDs.
func (e *Enricher) MapTopics(ctx context.Context, interests string, abstracts []string, topics []TopicForMapping) ([]TopicMapping, error) {
	if e.client == nil {
		return nil, fmt.Errorf("no Gemini client configured")
	}

	prompt := buildMappingPrompt(interests, abstracts, topics)
	systemPrompt := e.cfg.TopicMappingPrompt
	if systemPrompt == "" {
		systemPrompt = defaultTopicMappingPrompt
	}

	slog.Info("mapping topics with LLM", "interests_len", len(interests), "topics_count", len(topics), "abstracts_count", len(abstracts))
	start := time.Now()

	resp, err := e.client.Models.GenerateContent(ctx, e.model,
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: systemPrompt}},
			},
			ResponseMIMEType: "application/json",
		},
	)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("topic mapping LLM request failed", "duration", elapsed, "err", err)
		return nil, fmt.Errorf("generate content: %w", err)
	}

	if resp.UsageMetadata != nil {
		slog.Info("topic mapping LLM response",
			"duration", elapsed,
			"prompt_tokens", resp.UsageMetadata.PromptTokenCount,
			"response_tokens", resp.UsageMetadata.CandidatesTokenCount,
			"total_tokens", resp.UsageMetadata.TotalTokenCount,
		)
	}

	text := strings.TrimSpace(resp.Text())
	var mappings []TopicMapping
	if err := json.Unmarshal([]byte(text), &mappings); err != nil {
		slog.Error("failed to parse LLM topic mapping response", "response", text, "err", err)
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Filter out low scores
	var filtered []TopicMapping
	for _, m := range mappings {
		if m.Score >= 20 {
			filtered = append(filtered, m)
		}
	}

	slog.Info("topic mapping complete", "raw_mappings", len(mappings), "filtered", len(filtered))
	return filtered, nil
}

func buildMappingPrompt(interests string, abstracts []string, topics []TopicForMapping) string {
	var sb strings.Builder
	sb.WriteString("## Researcher's Research Interests\n\n")
	sb.WriteString(interests)
	sb.WriteString("\n\n")

	if len(abstracts) > 0 {
		sb.WriteString("## Recent Publication Abstracts\n\n")
		for i, a := range abstracts {
			fmt.Fprintf(&sb, "%d. %s\n\n", i+1, a)
		}
	}

	sb.WriteString("## Available OpenAlex Topics\n\n")
	// Send topics as compact JSON to save tokens
	topicsJSON, _ := json.Marshal(topics)
	sb.Write(topicsJSON)
	sb.WriteString("\n\n")

	sb.WriteString("Select the most relevant topics and return a JSON array of objects with openalex_id and score (0-100).")
	return sb.String()
}

const defaultTopicMappingPrompt = `You are an expert academic topic mapper. Given a researcher's stated research interests, some of their recent publication abstracts, and a list of OpenAlex topics (each with id, name, description, and keywords), select the topics most relevant to this researcher's work.

Return a JSON array of objects, each with:
- "openalex_id": the topic's ID string
- "score": relevance score from 0 to 100 (100 = perfectly matches their core interests)

Rules:
- Return at most 30 topics
- Only include topics with score >= 20
- Focus on topics that genuinely match the researcher's described interests and publication patterns
- Consider both direct matches and closely related sub-areas
- Score topics that match core interests higher (70-100) than peripheral ones (20-50)`
