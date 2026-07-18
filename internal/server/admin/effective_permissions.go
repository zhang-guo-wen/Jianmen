package admin

import "context"

func (s *Server) effectiveGlobalActions(ctx context.Context, userID string) ([]string, error) {
	roles, err := s.roleManagementService()
	if err != nil {
		return nil, err
	}
	return roles.EffectiveGlobalActions(ctx, userID)
}

func globalActionSet(actions []string) map[string]struct{} {
	set := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		set[action] = struct{}{}
	}
	return set
}
