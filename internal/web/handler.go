package web

import (
	"context"
	"embed"
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
	citedAuthors, _ := h.queries.ListCitedAuthorsByResearcher(r.Context(), id)
	newsletters, _ := h.queries.ListNewsletterRunsByResearcher(r.Context(), db.ListNewsletterRunsByResearcherParams{
		ResearcherID: id,
		Limit:        20,
	})

	h.renderPage(w, "researcher_detail.html.tmpl", map[string]any{
		"Researcher":   researcher,
		"CitedAuthors": citedAuthors,
		"Newsletters":  newsletters,
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
		isCitedAuthor := int64(0)
		if c.IsCitedAuthor {
			isCitedAuthor = 1
		}
		_ = h.queries.CreateNewsletterItem(ctx, db.CreateNewsletterItemParams{
			NewsletterRunID:    run.ID,
			OpenalexID:         c.Work.ID,
			Title:              c.Work.Title,
			Authors:            scanner.AuthorNames(c.Work),
			PublicationDate:    c.Work.PublicationDate,
			Doi:                c.Work.DOI,
			RelevancyScore:     c.RelevancyScore,
			Summary:            summaries[c.Work.ID],
			IsCitedAuthorPaper: isCitedAuthor,
			CitedAuthorName:    c.CitedAuthorName,
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

func (h *Handler) SearchSubfields(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	query := r.FormValue("query")
	if len(query) < 2 {
		w.WriteHeader(http.StatusOK)
		return
	}

	results, err := h.queries.SearchSubfieldsByName(r.Context(), "%"+query+"%")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check which are already selected
	selections, _ := h.queries.ListFieldSelectionsByResearcher(r.Context(), researcherID)
	selectedSet := make(map[string]bool)
	for _, s := range selections {
		selectedSet[s.Level+":"+s.OpenalexID] = true
	}

	type result struct {
		SubfieldID   string
		SubfieldName string
		FieldName    string
		DomainName   string
		Selected     bool
	}
	var items []result
	for _, r := range results {
		items = append(items, result{
			SubfieldID:   r.SubfieldID,
			SubfieldName: r.SubfieldName,
			FieldName:    r.FieldName,
			DomainName:   r.DomainName,
			Selected:     selectedSet["subfield:"+r.SubfieldID],
		})
	}

	h.renderFragment(w, "subfield_search_results.html.tmpl", map[string]any{
		"Results":      items,
		"ResearcherID": researcherID,
	})
}

func (h *Handler) SearchCitedAuthors(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	query := r.FormValue("query")
	if len(query) < 2 {
		w.WriteHeader(http.StatusOK)
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
	h.renderFragment(w, "cited_author_search_results.html.tmpl", map[string]any{
		"Results":      results,
		"ResearcherID": researcherID,
	})
}

func (h *Handler) AddCitedAuthor(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	citationCount, _ := strconv.ParseInt(r.FormValue("citation_count"), 10, 64)
	err = h.queries.InsertCitedAuthor(r.Context(), db.InsertCitedAuthorParams{
		ResearcherID:  researcherID,
		OpenalexID:    r.FormValue("openalex_id"),
		Name:          r.FormValue("name"),
		Affiliation:   r.FormValue("affiliation"),
		CitationCount: citationCount,
		Source:        "manual",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("cited author added manually", "researcher_id", researcherID, "name", r.FormValue("name"))
	h.renderCitedAuthorsSection(w, r.Context(), researcherID)
}

func (h *Handler) ToggleCitedAuthor(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authorID, err := strconv.ParseInt(r.PathValue("authorID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid author ID", http.StatusBadRequest)
		return
	}
	if err := h.queries.ToggleCitedAuthorActive(r.Context(), db.ToggleCitedAuthorActiveParams{
		ID:           authorID,
		ResearcherID: researcherID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderCitedAuthorsSection(w, r.Context(), researcherID)
}

func (h *Handler) DeleteCitedAuthor(w http.ResponseWriter, r *http.Request) {
	researcherID, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authorID, err := strconv.ParseInt(r.PathValue("authorID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid author ID", http.StatusBadRequest)
		return
	}
	if err := h.queries.DeleteCitedAuthor(r.Context(), db.DeleteCitedAuthorParams{
		ID:           authorID,
		ResearcherID: researcherID,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("cited author deleted", "researcher_id", researcherID, "author_id", authorID)
	h.renderCitedAuthorsSection(w, r.Context(), researcherID)
}

func (h *Handler) renderCitedAuthorsSection(w http.ResponseWriter, ctx context.Context, researcherID int64) {
	citedAuthors, err := h.queries.ListCitedAuthorsByResearcher(ctx, researcherID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFragment(w, "cited_authors_table.html.tmpl", map[string]any{
		"CitedAuthors": citedAuthors,
		"ResearcherID": researcherID,
	})
}

func parseID(r *http.Request) (int64, error) {
	idStr := r.PathValue("id")
	return strconv.ParseInt(idStr, 10, 64)
}
