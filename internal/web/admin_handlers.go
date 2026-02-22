package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/perbu/science-newsletter/internal/auth"
)

// requireAdmin checks that the current user is an admin. Returns true if access is denied.
func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	email := auth.EmailFromContext(r.Context())
	if email == "" || !auth.IsAdmin(email, h.cfg.Auth.AdminEmails) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return true
	}
	return false
}

func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

	researchers, err := h.queries.ListResearchersAdmin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "admin_dashboard.html.tmpl", map[string]any{
		"Researchers":    researchers,
		"HasGeminiKey":   h.cfg.Gemini.APIKey != "",
		"HasOpenAlexKey": h.cfg.OpenAlex.APIKey != "",
		"HasResendKey":   h.cfg.Resend.APIKey != "",
		"MailerEnabled":  h.mailer != nil,
	})
}

func (h *Handler) AdminNewsletterRuns(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

	runs, err := h.queries.ListAllNewsletterRuns(r.Context(), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "admin_newsletter_runs.html.tmpl", map[string]any{
		"Runs": runs,
	})
}

func (h *Handler) AdminSessions(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

	sessions, err := h.queries.ListActiveSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "admin_sessions.html.tmpl", map[string]any{
		"Sessions": sessions,
	})
}

func (h *Handler) AdminRevokeSession(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

	sessionID, err := strconv.ParseInt(r.PathValue("sessionID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid session ID", http.StatusBadRequest)
		return
	}

	if err := h.queries.DeleteSessionByID(r.Context(), sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("admin revoked session", "session_id", sessionID, "admin", auth.EmailFromContext(r.Context()))

	// Re-render the sessions table fragment
	sessions, err := h.queries.ListActiveSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFragment(w, "admin_sessions_table.html.tmpl", map[string]any{
		"Sessions": sessions,
	})
}

func (h *Handler) AdminTriggerPipeline(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

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
	if researcher.ResearchInterests == "" {
		fmt.Fprint(w, `<span class="text-red-600">Researcher has no research interests configured.</span>`)
		return
	}

	jobKey := "admin-pipeline:" + id
	adminEmail := auth.EmailFromContext(r.Context())
	mailer := h.mailer
	researcherEmail := researcher.Email

	started := h.jobs.Start(jobKey, func(ctx context.Context) (string, error) {
		slog.Info("admin triggered pipeline", "researcher_id", id, "admin", adminEmail)
		result, err := h.pipeline.FetchAndAnalyze(ctx, id)
		if err != nil {
			return "", err
		}

		// Send email if mailer is configured and researcher has an email
		if mailer != nil && researcherEmail.Valid && researcherEmail.String != "" && result.HTML != "" {
			if err := mailer.Send(ctx, []string{researcherEmail.String}, "Your Science Newsletter", result.HTML); err != nil {
				slog.Error("admin pipeline: failed to send email", "researcher_id", id, "err", err)
				return fmt.Sprintf("Pipeline complete (%d papers included) but email failed: %s", result.PapersIncluded, err), nil
			}
			return fmt.Sprintf("Pipeline complete: %d papers included, email sent to %s.", result.PapersIncluded, researcherEmail.String), nil
		}

		return fmt.Sprintf(`Pipeline complete: %d papers included. <a href="/newsletters/%d" class="underline">View newsletter</a>`, result.PapersIncluded, result.RunID), nil
	})

	if !started {
		fmt.Fprint(w, `<span class="text-yellow-600">Pipeline already running for this researcher...</span>`)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span hx-get="/admin/jobs/%s" hx-trigger="every 2s" hx-swap="innerHTML" hx-target="this" class="text-blue-600">Pipeline in progress... ⏳</span>`, id)
}

func (h *Handler) AdminJobStatus(w http.ResponseWriter, r *http.Request) {
	if h.requireAdmin(w, r) {
		return
	}

	id, err := parseID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobKey := "admin-pipeline:" + id
	status := h.jobs.Status(jobKey)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if status == nil {
		return
	}

	switch status.State {
	case JobRunning:
		elapsed := time.Since(status.StartedAt).Truncate(time.Second)
		fmt.Fprintf(w, `<span hx-get="/admin/jobs/%s" hx-trigger="every 2s" hx-swap="innerHTML" hx-target="this" class="text-blue-600">Pipeline in progress (%s)... ⏳</span>`, id, elapsed)
	case JobCompleted:
		fmt.Fprintf(w, `<span class="text-green-600">%s</span>`, status.Message)
	case JobFailed:
		fmt.Fprintf(w, `<span class="text-red-600">Pipeline failed: %s</span>`, status.Message)
	}
}
