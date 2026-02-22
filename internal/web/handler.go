package web

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/perbu/science-newsletter/internal/auth"
	"github.com/perbu/science-newsletter/internal/config"
	"github.com/perbu/science-newsletter/internal/database/db"
	"github.com/perbu/science-newsletter/internal/email"
	"github.com/perbu/science-newsletter/internal/openalex"
	"github.com/perbu/science-newsletter/internal/pipeline"
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
	pipeline *pipeline.Pipeline
	mailer   *email.Mailer
	cfg      *config.Config
	pages    map[string]*template.Template
	jobs     *JobRunner
}

func NewHandler(
	queries *db.Queries,
	client *openalex.Client,
	syncer *sync.Syncer,
	scn *scanner.Scanner,
	pip *pipeline.Pipeline,
	mailer *email.Mailer,
	cfg *config.Config,
) (*Handler, error) {
	// Parse each page template together with the layout
	pageFiles := []string{
		"landing.html.tmpl",
		"index.html.tmpl",
		"researcher_new.html.tmpl",
		"researcher_detail.html.tmpl",
		"newsletter_view.html.tmpl",
		"login.html.tmpl",
		"check_email.html.tmpl",
		"verify_confirm.html.tmpl",
		"admin_dashboard.html.tmpl",
		"admin_newsletter_runs.html.tmpl",
		"admin_sessions.html.tmpl",
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
		pipeline: pip,
		mailer:   mailer,
		cfg:      cfg,
		pages:    pages,
		jobs:     NewJobRunner(),
	}, nil
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	// If the user is authenticated, redirect to their researcher or setup
	if email := auth.EmailFromContext(r.Context()); email != "" {
		researcher, err := h.queries.GetResearcherByEmail(r.Context(), sql.NullString{String: email, Valid: true})
		if err == nil {
			http.Redirect(w, r, "/researchers/"+researcher.ID, http.StatusSeeOther)
			return
		}
		// No linked researcher — redirect to setup
		http.Redirect(w, r, "/researchers/new", http.StatusSeeOther)
		return
	}

	// Unauthenticated visitors see the public landing page
	h.renderPage(w, r, "landing.html.tmpl", nil)
}

func (h *Handler) NewResearcher(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{}
	if email := auth.EmailFromContext(r.Context()); email != "" {
		data["SetupMode"] = true
	}
	h.renderPage(w, r, "researcher_new.html.tmpl", data)
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
	newID := uuid.New().String()
	_, err := h.queries.CreateResearcher(r.Context(), db.CreateResearcherParams{
		ID:                 newID,
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

	// Auto-link authenticated user's email to the new researcher
	if email := auth.EmailFromContext(r.Context()); email != "" {
		if err := h.queries.LinkResearcherEmail(r.Context(), db.LinkResearcherEmailParams{
			Email: sql.NullString{String: email, Valid: true},
			ID:    newID,
		}); err != nil {
			slog.Error("failed to link email to researcher", "email", email, "researcher_id", newID, "err", err)
		} else {
			slog.Info("researcher linked to email", "email", email, "researcher_id", newID)
		}
	}

	w.Header().Set("HX-Redirect", "/researchers/"+newID)
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

	h.renderPage(w, r, "researcher_detail.html.tmpl", map[string]any{
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
	jobKey := "sync:" + id
	started := h.jobs.Start(jobKey, func(ctx context.Context) (string, error) {
		if err := h.syncer.SyncResearcher(ctx, id); err != nil {
			slog.Error("sync failed", "researcher_id", id, "err", err)
			return "", err
		}
		slog.Info("researcher synced", "researcher_id", id)
		return "Sync complete! Reload page to see updated data.", nil
	})
	if !started {
		fmt.Fprint(w, `<span class="text-yellow-600">Sync already running...</span>`)
		return
	}
	h.writePollingFragment(w, id, "sync")
}

func (h *Handler) FetchWorks(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jobKey := "fetch:" + id
	started := h.jobs.Start(jobKey, func(ctx context.Context) (string, error) {
		count, err := h.scanner.FetchWorks(ctx, id)
		if err != nil {
			slog.Error("fetch failed", "researcher_id", id, "err", err)
			return "", err
		}
		monthStr := scanner.MonthStart(time.Now()).AddDate(0, -1, 0).Format("2006-01")
		slog.Info("works fetched", "researcher_id", id, "count", count, "month", monthStr)
		return fmt.Sprintf("Fetched %d papers for %s. Ready to analyze.", count, monthStr), nil
	})
	if !started {
		fmt.Fprint(w, `<span class="text-yellow-600">Fetch already running...</span>`)
		return
	}
	h.writePollingFragment(w, id, "fetch")
}

func (h *Handler) AnalyzeScan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate synchronously before launching background job
	researcher, err := h.queries.GetResearcher(r.Context(), id)
	if err != nil {
		http.Error(w, "Researcher not found", http.StatusNotFound)
		return
	}
	if researcher.ResearchInterests == "" {
		fmt.Fprint(w, `<span class="text-red-600">Please set research interests before analyzing.</span>`)
		return
	}

	jobKey := "analyze:" + id
	started := h.jobs.Start(jobKey, func(ctx context.Context) (string, error) {
		result, err := h.pipeline.Analyze(ctx, id)
		if err != nil {
			return "", err
		}

		if result.PapersFound == 0 && result.PapersIncluded == 0 {
			return "No cached papers found. Fetch papers first.", nil
		}
		if result.PapersIncluded == 0 {
			return "No papers deemed relevant by LLM filter. Try adjusting research interests.", nil
		}

		return fmt.Sprintf(`Newsletter ready! %d papers included (from %d candidates). <a href="/newsletters/%d" class="underline">View newsletter</a>`,
			result.PapersIncluded, result.PapersFound, result.RunID), nil
	})
	if !started {
		fmt.Fprint(w, `<span class="text-yellow-600">Analysis already running...</span>`)
		return
	}
	h.writePollingFragment(w, id, "analyze")
}

func (h *Handler) ViewNewsletter(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	run, err := h.queries.GetNewsletterRun(r.Context(), id)
	if err != nil {
		http.Error(w, "Newsletter not found", http.StatusNotFound)
		return
	}
	h.renderPage(w, r, "newsletter_view.html.tmpl", map[string]any{
		"ResearcherID": run.ResearcherID,
		"HTMLContent":  template.HTML(run.HtmlContent),
	})
}

func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, page string, data any) {
	t, ok := h.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}

	// Inject AuthEmail and IsAdmin from context into template data
	m, _ := data.(map[string]any)
	if m == nil {
		m = make(map[string]any)
	}
	if email := auth.EmailFromContext(r.Context()); email != "" {
		m["AuthEmail"] = email
		m["IsAdmin"] = auth.IsAdmin(email, h.cfg.Auth.AdminEmails)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", m); err != nil {
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

func (h *Handler) renderCitedAuthorsSection(w http.ResponseWriter, ctx context.Context, researcherID string) {
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

func (h *Handler) writePollingFragment(w http.ResponseWriter, researcherID, jobType string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span hx-get="/researchers/%s/jobs/%s" hx-trigger="every 2s" hx-swap="innerHTML" hx-target="this" class="text-blue-600">%s in progress... ⏳</span>`,
		researcherID, jobType, jobType)
}

func (h *Handler) JobStatusHandler(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jobType := r.PathValue("type")
	jobKey := jobType + ":" + id

	status := h.jobs.Status(jobKey)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if status == nil {
		return
	}

	switch status.State {
	case JobRunning:
		elapsed := time.Since(status.StartedAt).Truncate(time.Second)
		fmt.Fprintf(w, `<span hx-get="/researchers/%s/jobs/%s" hx-trigger="every 2s" hx-swap="innerHTML" hx-target="this" class="text-blue-600">%s in progress (%s)... ⏳</span>`,
			id, jobType, jobType, elapsed)
	case JobCompleted:
		fmt.Fprintf(w, `<span class="text-green-600">%s</span>`, status.Message)
	case JobFailed:
		fmt.Fprintf(w, `<span class="text-red-600">%s failed: %s</span>`, jobType, status.Message)
	}
}

func parseID(r *http.Request) (string, error) {
	idStr := r.PathValue("id")
	if _, err := uuid.Parse(idStr); err != nil {
		return "", fmt.Errorf("invalid ID: %w", err)
	}
	return idStr, nil
}
