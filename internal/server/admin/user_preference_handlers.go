package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"jianmen/internal/model"
	"jianmen/internal/service"
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
		preference, err := s.preferences.Get(r.Context(), userID)
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
	preference, err := s.preferences.Update(r.Context(), userID, toUserPreferencePatch(request))
	if err != nil {
		if errors.Is(err, service.ErrInvalidUserPreference) {
			s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, userPreferenceView(preference))
}

func toUserPreferencePatch(request userPreferenceRequest) service.UserPreferencePatch {
	return service.UserPreferencePatch{
		Theme:              request.Theme,
		SSHClient:          request.SSHClient,
		SSHClientPath:      request.SSHClientPath,
		TerminalFontFamily: request.TerminalFontFamily,
		TerminalFontSize:   request.TerminalFontSize,
	}
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
