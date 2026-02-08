package agent

import (
	"context"
	"encoding/json"
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
	if paper.IsCitedAuthor {
		systemPrompt = strings.ReplaceAll(e.cfg.CitedAuthorPrompt, "{researcher}", researcherName)
	}

	slog.Debug("enriching paper", "title", paper.Work.Title, "is_cited_author", paper.IsCitedAuthor)
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

// PaperEvaluation is the structured JSON response from the LLM filter.
type PaperEvaluation struct {
	Score   int    `json:"score"`   // 1-10
	Include bool   `json:"include"` // LLM's verdict
	Summary string `json:"summary"` // one-sentence explanation
}

// FilterResult is a paper that passed the LLM filter.
type FilterResult struct {
	Paper   scanner.CandidatePaper
	Summary string
}

// FilterAndEnrich evaluates each paper against the researcher's interests using Gemini,
// returning only papers the LLM deems relevant (score >= 6) along with summaries.
func (e *Enricher) FilterAndEnrich(ctx context.Context, papers []scanner.CandidatePaper, researcherName, researchInterests string) ([]FilterResult, error) {
	slog.Info("starting LLM filter+enrich batch", "papers", len(papers), "model", e.model)
	start := time.Now()

	var results []FilterResult
	var totalPrompt, totalResponse, totalAll int32
	included, excluded := 0, 0

	for i, p := range papers {
		slog.Debug("evaluating paper", "index", i+1, "of", len(papers), "title", p.Work.Title)

		eval, tokens, err := e.evaluatePaper(ctx, p, researcherName, researchInterests)
		if err != nil {
			return nil, fmt.Errorf("evaluate paper %q: %w", p.Work.Title, err)
		}

		totalPrompt += tokens.promptTokens
		totalResponse += tokens.responseTokens
		totalAll += tokens.totalTokens

		p.RelevancyScore = float64(eval.Score) / 10.0

		slog.Info("paper evaluated",
			"title", p.Work.Title,
			"score", eval.Score,
			"include", eval.Include,
			"is_cited_author", p.IsCitedAuthor,
		)

		if eval.Include {
			results = append(results, FilterResult{
				Paper:   p,
				Summary: eval.Summary,
			})
			included++
		} else {
			excluded++
		}
	}

	slog.Info("LLM filter+enrich complete",
		"papers", len(papers),
		"included", included,
		"excluded", excluded,
		"total_prompt_tokens", totalPrompt,
		"total_response_tokens", totalResponse,
		"total_tokens", totalAll,
		"duration", time.Since(start),
	)

	return results, nil
}

func (e *Enricher) evaluatePaper(ctx context.Context, paper scanner.CandidatePaper, researcherName, researchInterests string) (PaperEvaluation, enrichResult, error) {
	if e.client == nil {
		// Noop enricher: include everything
		return PaperEvaluation{Score: 7, Include: true, Summary: "Relevant to the researcher's area of study."}, enrichResult{}, nil
	}

	prompt := e.buildFilterPrompt(paper, researcherName, researchInterests)

	slog.Debug("evaluating paper with LLM", "title", paper.Work.Title, "is_cited_author", paper.IsCitedAuthor)
	start := time.Now()

	resp, err := e.client.Models.GenerateContent(ctx, e.model,
		genai.Text(prompt),
		&genai.GenerateContentConfig{
			SystemInstruction: &genai.Content{
				Parts: []*genai.Part{{Text: e.cfg.FilterPrompt}},
			},
			ResponseMIMEType: "application/json",
			ResponseSchema: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"score":   {Type: genai.TypeInteger, Description: "Relevancy score from 1 to 10"},
					"include": {Type: genai.TypeBoolean, Description: "Whether to include this paper"},
					"summary": {Type: genai.TypeString, Description: "One-sentence summary of relevance"},
				},
				Required: []string{"score", "include", "summary"},
			},
		},
	)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("gemini filter request failed", "title", paper.Work.Title, "duration", elapsed, "err", err)
		return PaperEvaluation{}, enrichResult{}, fmt.Errorf("generate content: %w", err)
	}

	var tokens enrichResult
	if resp.UsageMetadata != nil {
		tokens.promptTokens = resp.UsageMetadata.PromptTokenCount
		tokens.responseTokens = resp.UsageMetadata.CandidatesTokenCount
		tokens.totalTokens = resp.UsageMetadata.TotalTokenCount
	}
	slog.Debug("gemini filter response",
		"title", paper.Work.Title,
		"duration", elapsed,
		"prompt_tokens", tokens.promptTokens,
		"response_tokens", tokens.responseTokens,
	)

	var eval PaperEvaluation
	if err := json.Unmarshal([]byte(resp.Text()), &eval); err != nil {
		return PaperEvaluation{}, tokens, fmt.Errorf("unmarshal evaluation: %w", err)
	}

	return eval, tokens, nil
}

func (e *Enricher) buildFilterPrompt(paper scanner.CandidatePaper, researcherName, researchInterests string) string {
	authors := scanner.AuthorNames(paper.Work)
	abstract := paper.Abstract

	// Collect paper topic names
	var topicNames []string
	for _, t := range paper.Work.Topics {
		topicNames = append(topicNames, t.DisplayName)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Paper: %s\n", paper.Work.Title)
	fmt.Fprintf(&sb, "Authors: %s\n", authors)
	if abstract != "" {
		fmt.Fprintf(&sb, "Abstract: %s\n", abstract)
	}
	if len(topicNames) > 0 {
		fmt.Fprintf(&sb, "Paper topics: %s\n", strings.Join(topicNames, ", "))
	}
	fmt.Fprintf(&sb, "\nResearcher: %s\n", researcherName)
	fmt.Fprintf(&sb, "Research interests: %s\n", researchInterests)

	if paper.IsCitedAuthor {
		fmt.Fprintf(&sb, "\nNote: This paper includes a frequently cited author (%s).\n", paper.CitedAuthorName)
	}

	return sb.String()
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

	if paper.IsCitedAuthor {
		fmt.Fprintf(&sb, "\nNote: This paper includes a frequently cited author (%s).\n", paper.CitedAuthorName)
	}

	sb.WriteString("\nProvide a one-sentence summary of what this paper found or contributes.")
	return sb.String()
}
