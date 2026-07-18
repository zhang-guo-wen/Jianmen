package admin

import (
	"context"
	"net/http"
	"strings"
)

func (s *Server) withAuthAndUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token == auth {
			s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		if s.identity == nil {
			s.writeErrorText(w, r, http.StatusServiceUnavailable, "authentication service unavailable")
			return
		}

		subject, found, err := s.identity.FindIdentitySubjectByTokenHash(r.Context(), hashToken(token))
		if err != nil {
			s.logger.Error("admin authentication failed", "error", err)
			s.writeErrorText(w, r, http.StatusInternalServerError, "authentication failed")
			return
		}
		if !found {
			s.writeErrorText(w, r, http.StatusUnauthorized, "invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserID, subject.ID)
		ctx = context.WithValue(ctx, ctxKeyUsername, subject.Username)
		ctx = context.WithValue(ctx, ctxKeySuperAdmin, subject.SuperAdmin)
		authenticatedRequest := r.WithContext(ctx)
		if isAuditableMutation(authenticatedRequest) {
			aw := &auditResponseWriter{ResponseWriter: w}
			next(aw, authenticatedRequest)
			s.recordOperation(authenticatedRequest, aw.statusCode())
			return
		}
		next(w, authenticatedRequest)
	}
}

func userIDFromRequest(r *http.Request) string {
	if id, ok := r.Context().Value(ctxKeyUserID).(string); ok {
		return id
	}
	return ""
}

func usernameFromRequest(r *http.Request) string {
	if name, ok := r.Context().Value(ctxKeyUsername).(string); ok {
		return name
	}
	return ""
}

func isSuperAdminRequest(r *http.Request) bool {
	superAdmin, _ := r.Context().Value(ctxKeySuperAdmin).(bool)
	return superAdmin
}

func (s *Server) requirePermission(r *http.Request, action string) bool {
	return s.requireAnyPermission(r, action)
}

func (s *Server) requireAnyPermission(r *http.Request, actions ...string) bool {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.logger.Warn("permission denied: missing authenticated user", "actions", actions)
		return false
	}
	if s.authorization == nil {
		s.logger.Warn("permission denied: authorization service unavailable", "user_id", userID, "actions", actions)
		return false
	}
	allowed, err := s.authorization.AuthorizeConnection(r.Context(), userID, actions, "", "")
	if err != nil {
		s.logger.Warn("authorization failed", "user_id", userID, "actions", actions, "error", err)
		return false
	}
	return allowed
}

func (s *Server) withAnyPermission(actions []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAnyPermission(r, actions...) {
			s.forbidden(w, r)
			return
		}
		next(w, r)
	}
}

func (s *Server) withPermission(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requirePermission(r, action) {
			s.forbidden(w, r)
			return
		}
		next(w, r)
	}
}
