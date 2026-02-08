package newsletter

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"time"

	"github.com/perbu/science-newsletter/internal/database/db"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

type PaperView struct {
	Title           string
	Authors         string
	PublicationDate string
	DOI             string
	Summary         string
	CitedAuthorName string
	ScorePercent    float64
}

type NewsletterData struct {
	ResearcherName   string
	Date             string
	LookbackDays     int
	CitedAuthorPapers []PaperView
	RelevantPapers   []PaperView
}

// Generate renders the newsletter HTML from newsletter items.
func Generate(researcher db.Researcher, items []db.NewsletterItem, lookbackDays int) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/newsletter.html.tmpl")
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	data := NewsletterData{
		ResearcherName: researcher.Name,
		Date:           time.Now().Format("January 2, 2006"),
		LookbackDays:   lookbackDays,
	}

	for _, item := range items {
		pv := PaperView{
			Title:           item.Title,
			Authors:         item.Authors,
			PublicationDate: item.PublicationDate,
			DOI:             item.Doi,
			Summary:         item.Summary,
			ScorePercent:    item.RelevancyScore * 100,
			CitedAuthorName: item.CitedAuthorName,
		}
		if item.IsCitedAuthorPaper != 0 {
			data.CitedAuthorPapers = append(data.CitedAuthorPapers, pv)
		} else {
			data.RelevantPapers = append(data.RelevantPapers, pv)
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
