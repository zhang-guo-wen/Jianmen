package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/handler/systemsettings"
)

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	if s.systemSettings == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "system settings are unavailable")
		return
	}
	s.systemSettings.Collection(w, r, systemsettings.Subject{
		UserID: userIDFromRequest(r), Username: usernameFromRequest(r),
	})
}

func (s *Server) handleSystemSettingRevisions(w http.ResponseWriter, r *http.Request) {
	if s.systemSettings == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "system settings are unavailable")
		return
	}
	s.systemSettings.Revisions(w, r)
}

func (s *Server) handleSystemSettingsDiagnostic(w http.ResponseWriter, r *http.Request) {
	if s.systemSettings == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "system settings are unavailable")
		return
	}
	name := strings.Trim(strings.TrimPrefix(
		r.URL.Path,
		"/api/system-settings/diagnostics/",
	), "/")
	s.systemSettings.Diagnostic(w, r, name)
}
