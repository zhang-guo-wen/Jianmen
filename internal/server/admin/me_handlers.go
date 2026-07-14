package admin

import (
	"net/http"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
)

type meAccessContextResponse struct {
	Actions []string          `json:"actions"`
	Pages   []rbac.PageAccess `json:"pages"`
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{
		"user_id":  userID,
		"username": usernameFromRequest(r),
	})
}

func (s *Server) handleMeAccessContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access, ok := s.currentUserAccessContext(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, access)
}

func (s *Server) handleMePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access, ok := s.currentUserAccessContext(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": access.Actions})
}

func (s *Server) handleMeMenus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	access, ok := s.currentUserAccessContext(w, r)
	if !ok {
		return
	}
	menus := make([]string, 0, len(access.Pages))
	for _, page := range access.Pages {
		menus = append(menus, page.Key)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": menus})
}

func (s *Server) currentUserAccessContext(w http.ResponseWriter, r *http.Request) (meAccessContextResponse, bool) {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "user not found")
		return meAccessContextResponse{}, false
	}
	if s.isSuperAdmin(userID) {
		actions := []string{"*"}
		return meAccessContextResponse{Actions: actions, Pages: appendSettingsPage(rbac.AccessiblePages(actions))}, true
	}
	if s.db == nil || s.rbacChecker == nil {
		return meAccessContextResponse{Actions: []string{}, Pages: appendSettingsPage(nil)}, true
	}
	actions, err := s.effectiveGlobalActions(userID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return meAccessContextResponse{}, false
	}
	return meAccessContextResponse{Actions: actions, Pages: appendSettingsPage(rbac.AccessiblePages(actions))}, true
}

func appendSettingsPage(pages []rbac.PageAccess) []rbac.PageAccess {
	return append(pages, rbac.PageAccess{Key: "settings", Path: "/settings", Order: 90})
}
