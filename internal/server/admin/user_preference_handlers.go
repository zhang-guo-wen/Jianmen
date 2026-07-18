package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"jianmen/internal/model"
)

type userPreferenceResponse struct {
	Theme              string `json:"theme"`
	SSHClient          string `json:"ssh_client"`
	SSHClientPath      string `json:"ssh_client_path"`
	TerminalFontFamily string `json:"terminal_font_family"`
	TerminalFontSize   int    `json:"terminal_font_size"`
}

type userPreferenceRequest struct {
	Theme              *string `json:"theme"`
	SSHClient          *string `json:"ssh_client"`
	SSHClientPath      *string `json:"ssh_client_path"`
	TerminalFontFamily *string `json:"terminal_font_family"`
	TerminalFontSize   *int    `json:"terminal_font_size"`
}

func (s *Server) handleMePreferences(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		preference, err := s.store.UserPreference(r.Context(), userID)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, userPreferenceView(preference))
	case http.MethodPut:
		s.updateMePreferences(w, r, userID)
	default:
		w.Header().Set("Allow", "GET, PUT")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) updateMePreferences(w http.ResponseWriter, r *http.Request, userID string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var request userPreferenceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	preference, err := s.store.UserPreference(r.Context(), userID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	applyUserPreferenceRequest(&preference, request)
	if message := validateUserPreference(preference); message != "" {
		s.writeErrorText(w, r, http.StatusBadRequest, message)
		return
	}
	preference, err = s.store.SaveUserPreference(r.Context(), preference)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, userPreferenceView(preference))
}

func applyUserPreferenceRequest(preference *model.UserPreference, request userPreferenceRequest) {
	if request.Theme != nil {
		preference.Theme = strings.ToLower(strings.TrimSpace(*request.Theme))
	}
	if request.SSHClient != nil {
		preference.SSHClient = strings.ToLower(strings.TrimSpace(*request.SSHClient))
	}
	if request.SSHClientPath != nil {
		preference.SSHClientPath = strings.TrimSpace(*request.SSHClientPath)
	}
	if request.TerminalFontFamily != nil {
		preference.TerminalFontFamily = strings.TrimSpace(*request.TerminalFontFamily)
	}
	if request.TerminalFontSize != nil {
		preference.TerminalFontSize = *request.TerminalFontSize
	}
}

func validateUserPreference(preference model.UserPreference) string {
	validThemes := map[string]bool{"system": true, "light": true, "dark": true}
	if !validThemes[preference.Theme] {
		return "theme must be system, light, or dark"
	}
	validClients := map[string]bool{"": true, "default": true, "xshell": true, "putty": true, "securecrt": true, "mobaxterm": true, "winterm": true, "system": true}
	if !validClients[preference.SSHClient] {
		return "unsupported ssh client"
	}
	if preference.TerminalFontSize < 10 || preference.TerminalFontSize > 30 {
		return "terminal_font_size must be between 10 and 30"
	}
	if len(preference.SSHClientPath) > 512 || len(preference.TerminalFontFamily) > 128 {
		return "preference value is too long"
	}
	return ""
}

func userPreferenceView(preference model.UserPreference) userPreferenceResponse {
	return userPreferenceResponse{
		Theme:              preference.Theme,
		SSHClient:          preference.SSHClient,
		SSHClientPath:      preference.SSHClientPath,
		TerminalFontFamily: preference.TerminalFontFamily,
		TerminalFontSize:   preference.TerminalFontSize,
	}
}
