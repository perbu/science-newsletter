package scanner

import (
	"fmt"
	"log/slog"
	"math"

	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/openalex"
)

// ScorePaper computes the relevancy score for a paper against a researcher's topics,
// modified by the source's citation impact.
//
// topic_score = sum(matching researcher_topic.score) / sum(top N researcher_topic.score)  [0,1]
// impact_factor = log2(1 + citedness) / log2(1 + 10.0)  (normalized so citedness=10 → 1.0)
// final = topic_score * (1 - impactWeight + impactWeight * impact_factor)
//
// With impactWeight=0.3:
//   - Unknown source (citedness=0): score × 0.70
//   - Decent journal (citedness=10): score × 1.00
//   - Top journal (citedness=50): score × 1.18
func ScorePaper(paper openalex.Work, researcherTopics []db.Topic, sourceCitedness float64, impactWeight float64) float64 {
	if len(researcherTopics) == 0 {
		return 0
	}

	// Build a set of researcher topic IDs -> score
	topicScores := make(map[string]float64, len(researcherTopics))
	totalScore := 0.0
	for _, t := range researcherTopics {
		topicScores[t.OpenalexID] = t.Score
		totalScore += t.Score
	}
	if totalScore == 0 {
		return 0
	}

	// Sum scores for matching topics
	matchScore := 0.0
	matches := 0
	for _, pt := range paper.Topics {
		if s, ok := topicScores[pt.ID]; ok {
			matchScore += s
			matches++
			slog.Debug("topic match",
				"paper_topic", pt.DisplayName,
				"topic_id", pt.ID,
				"researcher_score", fmt.Sprintf("%.4f", s),
			)
		}
	}

	topicScore := matchScore / totalScore

	// Apply impact factor modifier
	impactFactor := math.Log2(1+sourceCitedness) / math.Log2(1+10.0)
	finalScore := topicScore * (1 - impactWeight + impactWeight*impactFactor)

	slog.Debug("paper scored",
		"title", paper.Title,
		"matches", matches,
		"paper_topics", len(paper.Topics),
		"researcher_topics", len(researcherTopics),
		"topic_score", fmt.Sprintf("%.4f", topicScore),
		"source_citedness", fmt.Sprintf("%.2f", sourceCitedness),
		"impact_factor", fmt.Sprintf("%.4f", impactFactor),
		"impact_weight", fmt.Sprintf("%.2f", impactWeight),
		"final_score", fmt.Sprintf("%.4f", finalScore),
	)

	return finalScore
}
