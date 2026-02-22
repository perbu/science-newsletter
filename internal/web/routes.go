package web

import "net/http"

func SetupRoutes(mux *http.ServeMux, h *Handler) {
	// Auth routes (public, not behind middleware)
	mux.HandleFunc("GET /signup", h.LoginPage)
	mux.HandleFunc("POST /signup", h.LoginSubmit)
	mux.HandleFunc("GET /auth/verify", h.VerifyTokenPage)
	mux.HandleFunc("POST /auth/verify", h.VerifyTokenSubmit)
	mux.HandleFunc("POST /logout", h.Logout)

	mux.HandleFunc("GET /{$}", h.Index)
	mux.HandleFunc("GET /researchers/new", h.NewResearcher)
	mux.HandleFunc("POST /researchers/search", h.SearchResearchers)
	mux.HandleFunc("POST /researchers", h.CreateResearcher)
	mux.HandleFunc("DELETE /researchers/{id}", h.DeleteResearcher)
	mux.HandleFunc("GET /researchers/{id}", h.ResearcherDetail)
	mux.HandleFunc("PUT /researchers/{id}/threshold", h.UpdateThreshold)
	mux.HandleFunc("POST /researchers/{id}/sync", h.SyncResearcher)
	mux.HandleFunc("POST /researchers/{id}/fetch", h.FetchWorks)
	mux.HandleFunc("POST /researchers/{id}/analyze", h.AnalyzeScan)
	mux.HandleFunc("GET /researchers/{id}/jobs/{type}", h.JobStatusHandler)
	mux.HandleFunc("GET /newsletters/{id}", h.ViewNewsletter)
	mux.HandleFunc("PUT /researchers/{id}/interests", h.UpdateResearchInterests)
	mux.HandleFunc("POST /researchers/{id}/cited-authors/search", h.SearchCitedAuthors)
	mux.HandleFunc("POST /researchers/{id}/cited-authors", h.AddCitedAuthor)
	mux.HandleFunc("PUT /researchers/{id}/cited-authors/{authorID}/toggle", h.ToggleCitedAuthor)
	mux.HandleFunc("DELETE /researchers/{id}/cited-authors/{authorID}", h.DeleteCitedAuthor)

	// Admin routes
	mux.HandleFunc("GET /admin", h.AdminDashboard)
	mux.HandleFunc("GET /admin/newsletter-runs", h.AdminNewsletterRuns)
	mux.HandleFunc("GET /admin/sessions", h.AdminSessions)
	mux.HandleFunc("DELETE /admin/sessions/{sessionID}", h.AdminRevokeSession)
	mux.HandleFunc("POST /admin/trigger/{id}", h.AdminTriggerPipeline)
	mux.HandleFunc("GET /admin/jobs/{id}", h.AdminJobStatus)
}
