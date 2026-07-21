package admin

import (
	"net/http"
	"strings"

	"jianmen/internal/handler/sqlconsole"
)

func (s *Server) handleSQLConsoleSessions(w http.ResponseWriter, r *http.Request) {
	if s.sqlConsole == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "SQL console is unavailable")
		return
	}
	s.sqlConsole.HandleCreateSession(w, r, sqlConsoleActor(r))
}

func (s *Server) handleSQLConsoleSession(w http.ResponseWriter, r *http.Request) {
	if s.sqlConsole == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "SQL console is unavailable")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sql-console/sessions/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] != "" {
		s.sqlConsole.HandleCloseSession(w, r, sqlConsoleActor(r), parts[0])
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "execute" {
		s.sqlConsole.HandleExecute(w, r, sqlConsoleActor(r), parts[0])
		return
	}
	s.writeErrorText(w, r, http.StatusNotFound, "SQL console session endpoint not found")
}

func sqlConsoleActor(r *http.Request) sqlconsole.Actor {
	return sqlconsole.Actor{
		UserID:   userIDFromRequest(r),
		Username: usernameFromRequest(r),
		ClientIP: requestClientIP(r),
	}
}
