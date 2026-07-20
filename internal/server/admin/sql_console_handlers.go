package admin

import (
	"net/http"

	"jianmen/internal/handler/sqlconsole"
)

func (s *Server) handleSQLConsoleExecute(w http.ResponseWriter, r *http.Request) {
	if s.sqlConsole == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "SQL console is unavailable")
		return
	}
	s.sqlConsole.HandleExecute(w, r, sqlconsole.Actor{
		UserID:   userIDFromRequest(r),
		Username: usernameFromRequest(r),
		ClientIP: requestClientIP(r),
	})
}
