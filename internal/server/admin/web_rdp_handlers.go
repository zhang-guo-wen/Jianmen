package admin

import (
	"net/http"

	"jianmen/internal/handler/webrdp"
)

func (s *Server) handleWebRDPTicket(w http.ResponseWriter, r *http.Request) {
	if s.webRDP == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "Web RDP is unavailable")
		return
	}
	browser, ok := browserSessionFromRequest(r)
	if !ok {
		s.writeErrorText(w, r, http.StatusUnauthorized, "missing browser session")
		return
	}
	s.webRDP.CreateTicket(w, r, webrdp.AuthenticatedSubject{
		UserID: userIDFromRequest(r), Username: usernameFromRequest(r),
	}, browser)
}

func (s *Server) handleWebRDP(w http.ResponseWriter, r *http.Request) {
	if s.webRDP == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "Web RDP is unavailable")
		return
	}
	s.webRDP.Connect(w, r)
}

func (s *Server) handleRDPAudit(w http.ResponseWriter, r *http.Request) {
	if s.webRDP == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "Web RDP is unavailable")
		return
	}
	s.webRDP.AuditList(w, r, webrdp.AuthenticatedSubject{
		UserID: userIDFromRequest(r), Username: usernameFromRequest(r),
	})
}

func (s *Server) handleRDPAuditItem(w http.ResponseWriter, r *http.Request) {
	if s.webRDP == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "Web RDP is unavailable")
		return
	}
	s.webRDP.AuditItem(w, r, webrdp.AuthenticatedSubject{
		UserID: userIDFromRequest(r), Username: usernameFromRequest(r),
	})
}
