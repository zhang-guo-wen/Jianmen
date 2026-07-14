package admin

import (
	"net/http"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
)

var menuOrder = []string{
	"hosts",
	"databases",
	"platformAccounts",
	"rbac",
	"audit",
	"applications",
	"quickConnect",
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

func (s *Server) handleMePermissions(w http.ResponseWriter, r *http.Request) {
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
	if s.isSuperAdmin(userID) {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": []string{"*"}})
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": []string{}})
		return
	}
	actions, err := s.effectiveGlobalActions(userID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) handleMeMenus(w http.ResponseWriter, r *http.Request) {
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
	if s.isSuperAdmin(userID) {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": append([]string(nil), menuOrder...)})
		return
	}
	if s.db == nil || s.rbacChecker == nil {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": append([]string(nil), menuOrder...)})
		return
	}
	actions, err := s.effectiveGlobalActions(userID)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	actionSet := globalActionSet(actions)
	if _, hasWildcard := actionSet["*"]; hasWildcard {
		s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": append([]string(nil), menuOrder...)})
		return
	}
	seen := make(map[string]struct{})
	menus := make([]string, 0, len(menuOrder))
	for _, menuKey := range menuOrder {
		definition, ok := rbac.FindMenuPermissionDefinition(menuKey)
		if !ok {
			continue
		}
		if _, allowed := actionSet[definition.Action]; !allowed {
			continue
		}
		if _, exists := seen[menuKey]; !exists {
			seen[menuKey] = struct{}{}
			menus = append(menus, menuKey)
		}
	}
	if _, ok := seen["dashboard"]; !ok {
		menus = append([]string{"dashboard"}, menus...)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"menus": menus})
}
