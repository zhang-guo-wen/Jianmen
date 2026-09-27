package admin

import (
	"net/http"
	"time"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"status":   "api-only",
		"message":  "legacy HTML admin console is disabled; use the API or Vue frontend",
		"frontend": "http://127.0.0.1:47101",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}
