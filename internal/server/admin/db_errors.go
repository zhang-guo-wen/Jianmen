package admin

import (
	"fmt"
	"log/slog"
	"net/http"

	"jianmen/internal/pkg/apiresp"
)

func (s *Server) writeDatabaseOperationError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	publicMessage string,
	err error,
) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn(
		"database operation failed",
		"request_id",
		apiresp.RequestID(r.Context()),
		"path",
		r.URL.Path,
		"error_type",
		fmt.Sprintf("%T", err),
	)
	s.writeErrorText(w, r, status, publicMessage)
}
