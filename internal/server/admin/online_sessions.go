package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"jianmen/internal/online"
	"jianmen/internal/rbac"
)

func (s *Server) handleOnlineSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionSessionView) {
		s.forbidden(w, r)
		return
	}

	items := s.onlineSessions.List()
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	resourceID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	if resourceType != "" || resourceID != "" {
		filtered := make([]online.Session, 0, len(items))
		for _, item := range items {
			if resourceType != "" && item.ResourceType != resourceType {
				continue
			}
			if resourceID != "" && item.ResourceID != resourceID {
				continue
			}
			filtered = append(filtered, item)
		}
		items = filtered
	}

	// 补充 UserSession 的 SessionID
	s.enrichOnlineSessionsWithUserSession(r.Context(), items)

	resp := paginateSlice(items, r, func(item online.Session, q string) bool {
		return strings.Contains(strings.ToLower(item.Instance), q) ||
			strings.Contains(strings.ToLower(item.Protocol), q) ||
			strings.Contains(strings.ToLower(item.ProtocolSubtype), q) ||
			strings.Contains(strings.ToLower(item.Account), q) ||
			strings.Contains(strings.ToLower(item.Operator), q)
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleOnlineSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionSessionDisconnect) {
		s.forbidden(w, r)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/online-sessions/"), "/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid online session id")
		return
	}
	if err := s.onlineSessions.Disconnect(id); err != nil {
		if errors.Is(err, online.ErrSessionNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "online session not found")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// enrichOnlineSessionsWithUserSession 批量补充在线会话的 UserSession.SessionID。
func (s *Server) enrichOnlineSessionsWithUserSession(ctx context.Context, sessions []online.Session) {
	if len(sessions) == 0 {
		return
	}
	// 收集所有 AuditSessionID
	auditIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess.AuditSessionID != "" {
			auditIDs = append(auditIDs, sess.AuditSessionID)
		}
	}
	if len(auditIDs) == 0 {
		return
	}
	// 批量查询 audit_sessions 获取 UserSessionID
	type auditRow struct {
		ID            string `gorm:"column:id"`
		UserSessionID string `gorm:"column:user_session_id"`
	}
	var auditRows []auditRow
	if err := s.db.WithContext(ctx).
		Table("audit_sessions").
		Select("id, user_session_id").
		Where("id IN ?", auditIDs).
		Find(&auditRows).Error; err != nil {
		s.logger.Warn("批量查询 audit_sessions 失败", slog.String("error", err.Error()))
	}

	auditToUserSession := map[string]string{}
	userSessionIDs := make([]string, 0, len(auditRows))
	for _, r := range auditRows {
		auditToUserSession[r.ID] = r.UserSessionID
		if r.UserSessionID != "" {
			userSessionIDs = append(userSessionIDs, r.UserSessionID)
		}
	}
	if len(userSessionIDs) == 0 {
		return
	}
	// 批量查询 user_sessions 获取 5 位 SessionID
	type userSessionRow struct {
		ID        string `gorm:"column:id"`
		SessionID string `gorm:"column:session_id"`
	}
	var usRows []userSessionRow
	if err := s.db.WithContext(ctx).
		Table("user_sessions").
		Select("id, session_id").
		Where("id IN ?", userSessionIDs).
		Find(&usRows).Error; err != nil {
		s.logger.Warn("批量查询 user_sessions 失败", slog.String("error", err.Error()))
	}

	userSessionIDToShort := map[string]string{}
	for _, r := range usRows {
		userSessionIDToShort[r.ID] = r.SessionID
	}

	for i := range sessions {
		usID := auditToUserSession[sessions[i].AuditSessionID]
		if usID == "" {
			continue
		}
		sessions[i].UserSessionID = usID
		sessions[i].SessionID = userSessionIDToShort[usID]
	}
}
