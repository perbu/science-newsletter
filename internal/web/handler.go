package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/perbu/science-newsletter/internal/agent"
	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/email"
	"github.com/perbu/science-newsletter/internal/newsletter"
	"github.com/perbu/science-newsletter/internal/openalex"
	"github.com/perbu/science-newsletter/internal/scanner"
	"github.com/perbu/science-newsletter/internal/sync"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

type Handler struct {
	queries  *db.Queries
	client   *openalex.Client
	syncer   *sync.Syncer
	scanner  *scanner.Scanner
	enricher *agent.Enricher
	mailer   *email.Mailer
	cfg      *config.Config
	pages    map[string]*template.Template
}

func NewHandler(
	queries *db.Queries,
	client *openalex.Client,
	syncer *sync.Syncer,
	scn *scanner.Scanner,
	enricher *agent.Enricher,
	mailer *email.Mailer,
	cfg *config.Config,
) (*Handler, error) {
	// Parse each page template together with the layout
	pageFiles := []string{
		"index.html.tmpl",
		"researcher_new.html.tmpl",
		"researcher_detail.html.tmpl",
		"newsletter_view.html.tmpl",
	}
	pages := make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		t, err := template.ParseFS(templateFS, "templates/layout.html.tmpl", "templates/"+pf)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", pf, err)
		}
		pages[pf] = t
	}

	return &Handler{
		queries:  queries,
		client:   client,
		syncer:   syncer,
		scanner:  scn,
		enricher: enricher,
		mailer:   mailer,
		cfg:      cfg,
		pages:    pages,
	}, nil
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	researchers, err := h.queries.ListResearchers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderPage(w, "index.html.tmpl", map[string]any{"Researchers": researchers})
}

func (h *Handler) NewResearcher(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, "researcher_new.html.tmpl", nil)
}

func (h *Handler) SearchResearchers(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}
	results, err := h.client.SearchAuthors(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(results) > 10 {
		results = results[:10]
	}
	h.renderFragment(w, "search_results.html.tmpl", map[string]any{"Results": results})
}

func (h *Handler) CreateResearcher(w http.ResponseWriter, r *http.Request) {
	hIndex, _ := strconv.ParseInt(r.FormValue("h_index"), 10, 64)
	worksCount, _ := strconv.ParseInt(r.FormValue("works_count"), 10, 64)
	citedByCount, _ := strconv.ParseInt(r.FormValue("cited_by_count"), 10, 64)

	name := r.FormValue("name")
	openalexID := r.FormValue("openalex_id")
	_, err := h.queries.CreateResearcher(r.Context(), db.CreateResearcherParams{
		OpenalexID:         openalexID,
		Name:               name,
		Affiliation:        r.FormValue("affiliation"),
		HIndex:             hIndex,
		WorksCount:         worksCount,
		CitedByCount:       citedByCount,
		RelevancyThreshold: h.cfg.Scanner.DefaultThreshold,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("researcher created", "name", name, "openalex_id", openalexID)
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) DeleteResearcher(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.queries.DeleteResearcher(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("researcher deleted", "id", id)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ResearcherDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	researcher, err := h.queries.GetResearcher(r.Context(), id)
	if err != nil {
		http.Error(w, "Researcher not found", http.StatusNotFound)
		return
	}
	topics, _ := h.queries.ListTopicsByResearcher(r.Context(), id)
	coauthors, _ := h.queries.ListCoAuthorsByResearcher(r.Context(), id)
	if len(coauthors) > 20 {
		coauthors = coauthors[:20]
	}
	newsletters, _ := h.queries.ListNewsletterRunsByResearcher(r.Context(), db.ListNewsletterRunsByResearcherParams{
		ResearcherID: id,
		Limit:        20,
	})

	h.renderPage(w, "researcher_detail.html.tmpl", map[string]any{
		"Researcher":  researcher,
		"Topics":      topics,
		"CoAuthors":   coauthors,
		"Newsletters": newsletters,
	})
}

func (h *Handler) UpdateThreshold(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	threshold, err := strconv.ParseFloat(r.FormValue("threshold"), 64)
	if err != nil {
		http.Error(w, "invalid threshold", http.StatusBadRequest)
		return
	}
	if err := h.queries.UpdateResearcherThreshold(r.Context(), db.UpdateResearcherThresholdParams{
		ID:                 id,
		RelevancyThreshold: threshold,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("threshold updated", "researcher_id", id, "threshold", threshold)
	fmt.Fprintf(w, "%.2f", threshold)
}

func (h *Handler) SyncResearcher(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.syncer.SyncResearcher(r.Context(), id); err != nil {
		slog.Error("sync failed", "researcher_id", id, "err", err)
		fmt.Fprintf(w, `<span class="text-red-600">Sync failed: %s</span>`, err)
		return
	}
	slog.Info("researcher synced", "researcher_id", id)
	fmt.Fprint(w, `<span class="text-green-600">Sync complete! Reload page to see updated data.</span>`)
}

func (h *Handler) FetchWorks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	count, err := h.scanner.FetchWorks(r.Context(), id)
	if err != nil {
		slog.Error("fetch failed", "researcher_id", id, "err", err)
		fmt.Fprintf(w, `<span class="text-red-600">Fetch failed: %s</span>`, err)
		return
	}

	weekStr := scanner.WeekStart(time.Now()).Format("2006-01-02")
	slog.Info("works fetched", "researcher_id", id, "count", count, "week", weekStr)
	fmt.Fprintf(w, `<span class="text-green-600">Fetched %d papers for week %s. Ready to analyze.</span>`, count, weekStr)
}

func (h *Handler) AnalyzeScan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	researcher, err := h.queries.GetResearcher(ctx, id)
	if err != nil {
		http.Error(w, "Researcher not found", http.StatusNotFound)
		return
	}

	run, err := h.queries.CreateNewsletterRun(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	weekStart := scanner.WeekStart(time.Now())
	candidates, err := h.scanner.AnalyzeWorks(ctx, id, weekStart)
	if err != nil {
		h.failRun(ctx, run.ID, err)
		fmt.Fprintf(w, `<span class="text-red-600">Analysis failed: %s</span>`, err)
		return
	}

	if len(candidates) == 0 {
		_ = h.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
			ID:     run.ID,
			Status: "completed",
		})
		fmt.Fprint(w, `<span class="text-yellow-600">No papers above threshold. Try fetching first or lowering the threshold.</span>`)
		return
	}

	topicNames := h.getTopicNames(ctx, id)
	summaries := h.enricher.EnrichAll(ctx, candidates, researcher.Name, topicNames)

	for _, c := range candidates {
		isCoauthor := int64(0)
		if c.IsCoauthor {
			isCoauthor = 1
		}
		_ = h.queries.CreateNewsletterItem(ctx, db.CreateNewsletterItemParams{
			NewsletterRunID: run.ID,
			OpenalexID:      c.Work.ID,
			Title:           c.Work.Title,
			Authors:         scanner.AuthorNames(c.Work),
			PublicationDate: c.Work.PublicationDate,
			Doi:             c.Work.DOI,
			RelevancyScore:  c.RelevancyScore,
			Summary:         summaries[c.Work.ID],
			IsCoauthorPaper: isCoauthor,
			CoauthorName:    c.CoauthorName,
		})
	}

	items, _ := h.queries.ListNewsletterItems(ctx, run.ID)
	html, err := newsletter.Generate(researcher, items, h.cfg.Scanner.LookbackDays)
	if err != nil {
		h.failRun(ctx, run.ID, err)
		fmt.Fprintf(w, `<span class="text-red-600">Newsletter generation failed: %s</span>`, err)
		return
	}

	_ = h.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
		ID:             run.ID,
		Status:         "completed",
		PapersFound:    int64(len(candidates)),
		PapersIncluded: int64(len(items)),
		HtmlContent:    html,
	})

	fmt.Fprintf(w, `<span class="text-green-600">Newsletter ready! %d papers included. <a href="/newsletters/%d" class="underline">View newsletter</a></span>`,
		len(items), run.ID)
}

func (h *Handler) ViewNewsletter(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	run, err := h.queries.GetNewsletterRun(r.Context(), id)
	if err != nil {
		http.Error(w, "Newsletter not found", http.StatusNotFound)
		return
	}
	h.renderPage(w, "newsletter_view.html.tmpl", map[string]any{
		"ResearcherID": run.ResearcherID,
		"HTMLContent":  template.HTML(run.HtmlContent),
	})
}

func (h *Handler) renderPage(w http.ResponseWriter, page string, data any) {
	t, ok := h.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("render page", "page", page, "err", err)
	}
}

func (h *Handler) renderFragment(w http.ResponseWriter, name string, data any) {
	tmpl, err := template.ParseFS(templateFS, "templates/"+name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("render fragment", "name", name, "err", err)
	}
}

func (h *Handler) failRun(ctx context.Context, runID int64, runErr error) {
	_ = h.queries.UpdateNewsletterRun(ctx, db.UpdateNewsletterRunParams{
		ID:     runID,
		Status: "failed",
	})
	slog.Error("scan run failed", "run_id", runID, "err", runErr)
}

func (h *Handler) getTopicNames(ctx context.Context, researcherID int64) []string {
	topics, err := h.queries.ListTopTopicsByResearcher(ctx, db.ListTopTopicsByResearcherParams{
		ResearcherID: researcherID,
		Limit:        int64(h.cfg.Scanner.MaxTopics),
	})
	if err != nil {
		return nil
	}
	names := make([]string, len(topics))
	for i, t := range topics {
		names[i] = t.Name
	}
	return names
}

func (h *Handler) DeleteTopic(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	topicID, err := strconv.ParseInt(r.PathValue("topicId"), 10, 64)
	if err != nil {
		http.Error(w, "invalid topic id", http.StatusBadRequest)
		return
	}
	if err := h.queries.DeleteTopic(r.Context(), db.DeleteTopicParams{
		ID:           topicID,
		ResearcherID: researcherID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("topic deleted", "researcher_id", researcherID, "topic_id", topicID)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) UpdateTopicScore(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	topicID, err := strconv.ParseInt(r.PathValue("topicId"), 10, 64)
	if err != nil {
		http.Error(w, "invalid topic id", http.StatusBadRequest)
		return
	}
	score, err := strconv.ParseFloat(r.FormValue("score"), 64)
	if err != nil {
		http.Error(w, "invalid score", http.StatusBadRequest)
		return
	}
	if err := h.queries.UpdateTopicScore(r.Context(), db.UpdateTopicScoreParams{
		Score:        score,
		ID:           topicID,
		ResearcherID: researcherID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("topic score updated", "researcher_id", researcherID, "topic_id", topicID, "score", score)
	fmt.Fprintf(w, "%.3f", score)
}

func (h *Handler) SearchTopics(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		http.Error(w, "query required", http.StatusBadRequest)
		return
	}
	results, err := h.client.SearchTopics(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	researcherID, _ := parseID(r)
	h.renderFragment(w, "topic_search_results.html.tmpl", map[string]any{
		"Results":      results,
		"ResearcherID": researcherID,
	})
}

func (h *Handler) AddTopic(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	openalexID := r.FormValue("openalex_id")
	name := r.FormValue("name")
	subfield := r.FormValue("subfield")
	field := r.FormValue("field")
	domain := r.FormValue("domain")
	score, err := strconv.ParseFloat(r.FormValue("score"), 64)
	if err != nil {
		score = 50.0 // reasonable default for manual topics
	}

	if err := h.queries.UpsertTopic(r.Context(), db.UpsertTopicParams{
		ResearcherID: researcherID,
		OpenalexID:   openalexID,
		Name:         name,
		Subfield:     subfield,
		Field:        field,
		Domain:       domain,
		Score:        score,
		Source:       "manual",
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("topic added", "researcher_id", researcherID, "name", name, "openalex_id", openalexID)
	// Return updated topic list
	topics, _ := h.queries.ListTopicsByResearcher(r.Context(), researcherID)
	h.renderFragment(w, "topics_table.html.tmpl", map[string]any{
		"Topics":     topics,
		"Researcher": map[string]any{"ID": researcherID},
	})
}

func (h *Handler) UpdateResearchInterests(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	interests := r.FormValue("research_interests")
	if err := h.queries.UpdateResearcherInterests(r.Context(), db.UpdateResearcherInterestsParams{
		ResearchInterests: interests,
		ID:                id,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("research interests updated", "researcher_id", id, "length", len(interests))
	fmt.Fprint(w, `<span class="text-green-600">Interests saved.</span>`)
}

func (h *Handler) MirrorTopics(w http.ResponseWriter, r *http.Request) {
	count, err := h.syncer.MirrorTopics(r.Context())
	if err != nil {
		slog.Error("topic mirror failed", "err", err)
		fmt.Fprintf(w, `<span class="text-red-600">Mirror failed: %s</span>`, err)
		return
	}
	slog.Info("topic mirror complete", "count", count)
	fmt.Fprintf(w, `<span class="text-green-600">Mirrored %d topics from OpenAlex.</span>`, count)
}

type SubfieldItem struct {
	SubfieldID   string
	SubfieldName string
	Selected     bool
}

type FieldItem struct {
	FieldID   string
	FieldName string
	Selected  bool
	Subfields []SubfieldItem
}

type DomainGroup struct {
	DomainID   string
	DomainName string
	Fields     []FieldItem
}

func (h *Handler) ListFields(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	fields, err := h.queries.ListDistinctFields(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	selections, err := h.queries.ListFieldSelectionsByResearcher(ctx, researcherID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	selectedSet := make(map[string]bool)
	for _, s := range selections {
		selectedSet[s.Level+":"+s.OpenalexID] = true
	}

	domainMap := make(map[string]*DomainGroup)
	var domainOrder []string
	for _, f := range fields {
		dg, ok := domainMap[f.DomainID]
		if !ok {
			dg = &DomainGroup{DomainID: f.DomainID, DomainName: f.DomainName}
			domainMap[f.DomainID] = dg
			domainOrder = append(domainOrder, f.DomainID)
		}

		fieldSelected := selectedSet["field:"+f.FieldID]

		subfields, _ := h.queries.ListDistinctSubfieldsByField(ctx, f.FieldID)
		var sfItems []SubfieldItem
		for _, sf := range subfields {
			sfItems = append(sfItems, SubfieldItem{
				SubfieldID:   sf.SubfieldID,
				SubfieldName: sf.SubfieldName,
				Selected:     selectedSet["subfield:"+sf.SubfieldID],
			})
		}

		dg.Fields = append(dg.Fields, FieldItem{
			FieldID:   f.FieldID,
			FieldName: f.FieldName,
			Selected:  fieldSelected,
			Subfields: sfItems,
		})
	}

	var domains []DomainGroup
	for _, did := range domainOrder {
		domains = append(domains, *domainMap[did])
	}

	topicCount, _ := h.queries.CountOpenAlexTopics(ctx)

	h.renderFragment(w, "field_selection.html.tmpl", map[string]any{
		"ResearcherID": researcherID,
		"Domains":      domains,
		"Selections":   selections,
		"TopicCount":   topicCount,
	})
}

func (h *Handler) ToggleFieldSelection(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	action := r.FormValue("action") // "add" or "remove"
	level := r.FormValue("level")   // "field" or "subfield"
	openalexID := r.FormValue("openalex_id")
	displayName := r.FormValue("display_name")

	ctx := r.Context()

	if action == "remove" {
		err = h.queries.DeleteFieldSelection(ctx, db.DeleteFieldSelectionParams{
			ResearcherID: researcherID,
			Level:        level,
			OpenalexID:   openalexID,
		})
	} else {
		err = h.queries.UpsertFieldSelection(ctx, db.UpsertFieldSelectionParams{
			ResearcherID: researcherID,
			Level:        level,
			OpenalexID:   openalexID,
			DisplayName:  displayName,
		})
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("field selection toggled", "researcher_id", researcherID, "action", action, "level", level, "id", openalexID)

	// Re-render the field selection panel
	h.ListFields(w, r)
}

func (h *Handler) MapTopicsLLM(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	researcher, err := h.queries.GetResearcher(ctx, researcherID)
	if err != nil {
		http.Error(w, "Researcher not found", http.StatusNotFound)
		return
	}

	if researcher.ResearchInterests == "" {
		fmt.Fprint(w, `<span class="text-red-600">Please set research interests first.</span>`)
		return
	}

	selections, err := h.queries.ListFieldSelectionsByResearcher(ctx, researcherID)
	if err != nil || len(selections) == 0 {
		fmt.Fprint(w, `<span class="text-red-600">Please select at least one field or subfield first.</span>`)
		return
	}

	// Collect subfield IDs from selections
	subfieldIDs := make(map[string]bool)
	for _, s := range selections {
		if s.Level == "subfield" {
			subfieldIDs[s.OpenalexID] = true
		} else if s.Level == "field" {
			sfs, _ := h.queries.ListDistinctSubfieldsByField(ctx, s.OpenalexID)
			for _, sf := range sfs {
				subfieldIDs[sf.SubfieldID] = true
			}
		}
	}

	// Load topics from selected subfields
	var topicsForMapping []agent.TopicForMapping
	for sfID := range subfieldIDs {
		rows, err := h.queries.ListTopicsBySubfield(ctx, sfID)
		if err != nil {
			continue
		}
		for _, row := range rows {
			var keywords []string
			_ = json.Unmarshal([]byte(row.Keywords), &keywords)
			topicsForMapping = append(topicsForMapping, agent.TopicForMapping{
				ID:          row.OpenalexID,
				Name:        row.DisplayName,
				Description: row.Description,
				Keywords:    keywords,
			})
		}
	}

	if len(topicsForMapping) == 0 {
		fmt.Fprint(w, `<span class="text-red-600">No topics found in selected fields. Try refreshing the topic database first.</span>`)
		return
	}

	// Fetch recent publication abstracts
	pubs, _ := h.queries.ListPublicationsByResearcher(ctx, db.ListPublicationsByResearcherParams{
		ResearcherID: researcherID,
		Limit:        5,
	})
	var abstracts []string
	for _, pub := range pubs {
		work, err := h.client.GetWork(ctx, pub.OpenalexID)
		if err != nil {
			continue
		}
		abstract := work.AbstractText()
		if abstract != "" {
			abstracts = append(abstracts, abstract)
		}
	}

	// Call LLM
	mappings, err := h.enricher.MapTopics(ctx, researcher.ResearchInterests, abstracts, topicsForMapping)
	if err != nil {
		slog.Error("LLM topic mapping failed", "researcher_id", researcherID, "err", err)
		fmt.Fprintf(w, `<span class="text-red-600">Topic mapping failed: %s</span>`, err)
		return
	}

	// Delete old LLM topics and upsert new ones
	_ = h.queries.DeleteLLMTopicsByResearcher(ctx, researcherID)

	for _, m := range mappings {
		// Look up full topic metadata from openalex_topics table
		oaTopic, err := h.queries.GetOpenAlexTopicByOpenAlexID(ctx, m.OpenAlexID)
		if err != nil {
			slog.Warn("mapped topic not found in mirror", "openalex_id", m.OpenAlexID)
			continue
		}

		score := float64(m.Score) / 100.0
		_ = h.queries.UpsertTopic(ctx, db.UpsertTopicParams{
			ResearcherID: researcherID,
			OpenalexID:   m.OpenAlexID,
			Name:         oaTopic.DisplayName,
			Subfield:     oaTopic.SubfieldName,
			Field:        oaTopic.FieldName,
			Domain:       oaTopic.DomainName,
			Score:        score,
			Source:       "llm",
		})
	}

	slog.Info("LLM topic mapping saved", "researcher_id", researcherID, "topics", len(mappings))

	// Re-render topics table
	topics, _ := h.queries.ListTopicsByResearcher(ctx, researcherID)
	h.renderFragment(w, "topics_table.html.tmpl", map[string]any{
		"Topics":     topics,
		"Researcher": map[string]any{"ID": researcherID},
	})
}

func parseID(r *http.Request) (int64, error) {
	idStr := r.PathValue("id")
	return strconv.ParseInt(idStr, 10, 64)
}
