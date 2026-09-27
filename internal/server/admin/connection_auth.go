package admin

import (
	"context"
	"errors"
	"strings"
)

// authorizeConnection requires a unified action and resource decision.
func (s *Server) authorizeConnection(ctx context.Context, userID, action, resourceType, resourceID string) (bool, error) {
	return s.authorizeAnyConnection(ctx, userID, []string{action}, resourceType, resourceID)
}

func (s *Server) authorizeAnyConnection(ctx context.Context, userID string, actions []string, resourceType, resourceID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	if s.authorization == nil {
		return false, errors.New("authorization service unavailable")
	}
	return s.authorization.AuthorizeConnection(ctx, userID, actions, resourceType, resourceID)
}
