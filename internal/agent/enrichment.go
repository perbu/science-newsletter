package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/scanner"
)

// Enricher uses Gemini to generate one-line summaries for candidate papers.
type Enricher struct {
	client *genai.Client
	model  string
	cfg    config.AgentConfig
}

type enrichResult struct {
	summary        string
	promptTokens   int32
	responseTokens int32
	totalTokens    int32
}

func NewEnricher(apiKey, model string, cfg config.AgentConfig) (*Enricher, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	slog.Info("gemini enricher initialized", "model", model)
	return &Enricher{
		client: client,
		model:  model,
		cfg:    cfg,
	}, nil
}

// NewNoopEnricher returns an enricher that gives placeholder summaries (no API key needed).
func NewNoopEnricher() *Enricher {
	return &Enricher{}
}

func (e *Enricher) enrichPaper(ctx context.Context, paper scanner.CandidatePaper, researcherName string, topicNames []string) (enrichResult, error) {
	if e.client == nil {
		return enrichResult{summary: "Relevant to the researcher's area of study."}, nil
	}

	prompt := e.buildPrompt(paper, researcherName, topicNames)

	systemPrompt := e.cfg.EnrichmentPrompt
	if paper.IsCoauthor {
		systemPrompt = strings.ReplaceAll(e.cfg.CoauthorPrompt, "{researcher}", researcherName)
	}

	slog.Debug("enriching paper", "title", paper.Work.Title, "is_coauthor", paper.IsCoauthor)
	start := time.Now()

	resp, err := e.client.Models.GenerateContent(ctx, e.model,
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: systemPrompt}},
			},
		},
	)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("gemini request failed", "title", paper.Work.Title, "duration", elapsed, "err", err)
		return enrichResult{}, fmt.Errorf("generate content: %w", err)
	}

	var res enrichResult
	if resp.UsageMetadata != nil {
		res.promptTokens = resp.UsageMetadata.PromptTokenCount
		res.responseTokens = resp.UsageMetadata.CandidatesTokenCount
		res.totalTokens = resp.UsageMetadata.TotalTokenCount
	}
	slog.Debug("gemini response",
		"title", paper.Work.Title,
		"duration", elapsed,
		"prompt_tokens", res.promptTokens,
		"response_tokens", res.responseTokens,
		"total_tokens", res.totalTokens,
	)

	res.summary = strings.TrimSpace(resp.Text())
	return res, nil
}

// EnrichAll enriches all candidate papers sequentially and logs a token summary.
func (e *Enricher) EnrichAll(ctx context.Context, papers []scanner.CandidatePaper, researcherName string, topicNames []string) map[string]string {
	slog.Info("starting enrichment batch", "papers", len(papers), "model", e.model)
	start := time.Now()
	summaries := make(map[string]string, len(papers))

	var totalPrompt, totalResponse, totalAll int32
	successes, failures := 0, 0

	for i, p := range papers {
		slog.Debug("enriching paper", "index", i+1, "of", len(papers), "title", p.Work.Title)
		res, err := e.enrichPaper(ctx, p, researcherName, topicNames)
		if err != nil {
			slog.Warn("enrichment failed", "paper", p.Work.Title, "err", err)
			res.summary = "Relevant to the researcher's area of study."
			failures++
		} else {
			successes++
		}
		summaries[p.Work.ID] = res.summary
		totalPrompt += res.promptTokens
		totalResponse += res.responseTokens
		totalAll += res.totalTokens
	}

	slog.Info("enrichment batch complete",
		"papers", len(papers),
		"successes", successes,
		"failures", failures,
		"total_prompt_tokens", totalPrompt,
		"total_response_tokens", totalResponse,
		"total_tokens", totalAll,
		"duration", time.Since(start),
	)
	return summaries
}

func (e *Enricher) buildPrompt(paper scanner.CandidatePaper, researcherName string, topicNames []string) string {
	authors := scanner.AuthorNames(paper.Work)
	abstract := paper.Work.AbstractText()
	if len(abstract) > 500 {
		abstract = abstract[:500] + "..."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Paper: %s\n", paper.Work.Title)
	fmt.Fprintf(&sb, "Authors: %s\n", authors)
	if abstract != "" {
		fmt.Fprintf(&sb, "Abstract: %s\n", abstract)
	}
	fmt.Fprintf(&sb, "\nResearcher: %s\n", researcherName)
	fmt.Fprintf(&sb, "Research topics: %s\n", strings.Join(topicNames, ", "))

	if paper.IsCoauthor {
		fmt.Fprintf(&sb, "\nNote: %s is a former co-author of %s.\n", paper.CoauthorName, researcherName)
	}

	sb.WriteString("\nProvide a one-sentence summary of why this paper is relevant.")
	return sb.String()
}
