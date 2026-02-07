package web

import "net/http"

func SetupRoutes(mux *http.ServeMux, h *Handler) {
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
	mux.HandleFunc("POST /researchers/{id}/topics/search", h.SearchTopics)
	mux.HandleFunc("POST /researchers/{id}/topics", h.AddTopic)
	mux.HandleFunc("DELETE /researchers/{id}/topics/{topicId}", h.DeleteTopic)
	mux.HandleFunc("PUT /researchers/{id}/topics/{topicId}/score", h.UpdateTopicScore)
	mux.HandleFunc("GET /newsletters/{id}", h.ViewNewsletter)
	mux.HandleFunc("POST /admin/mirror-topics", h.MirrorTopics)
	mux.HandleFunc("PUT /researchers/{id}/interests", h.UpdateResearchInterests)
	mux.HandleFunc("GET /researchers/{id}/fields", h.ListFields)
	mux.HandleFunc("POST /researchers/{id}/field-selection", h.ToggleFieldSelection)
	mux.HandleFunc("POST /researchers/{id}/map-topics", h.MapTopicsLLM)
}
