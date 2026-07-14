package admin

import (
	"sort"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func (s *Server) effectiveGlobalActions(userID string) ([]string, error) {
	var permissions []model.Permission
	err := s.db.Table("permissions").
		Select("permissions.*").
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ?", userID).
		Where("user_roles.expires_at IS NULL OR user_roles.expires_at > ?", time.Now().UTC()).
		Where("roles.status = '' OR roles.status = ?", "active").
		Where("permissions.resource_type = '' AND permissions.resource_id = ''").
		Where("permissions.action <> ''").
		Find(&permissions).Error
	if err != nil {
		return nil, err
	}

	allows := make(map[string]struct{})
	denies := make(map[string]struct{})
	for _, permission := range permissions {
		action := strings.TrimSpace(permission.Action)
		if strings.EqualFold(permission.Effect, model.PermissionEffectDeny) {
			denies[action] = struct{}{}
		} else {
			allows[action] = struct{}{}
		}
	}
	if _, denied := denies["*"]; denied {
		return []string{}, nil
	}
	if _, wildcard := allows["*"]; wildcard && len(denies) == 0 {
		return []string{"*"}, nil
	}

	effective := make(map[string]struct{})
	if _, wildcard := allows["*"]; wildcard {
		for _, definition := range rbac.PermissionCatalog() {
			if _, denied := denies[definition.Action]; !denied {
				effective[definition.Action] = struct{}{}
			}
		}
	}
	for action := range allows {
		if action == "*" {
			continue
		}
		if _, denied := denies[action]; !denied {
			effective[action] = struct{}{}
		}
	}

	actions := make([]string, 0, len(effective))
	for action := range effective {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions, nil
}

func globalActionSet(actions []string) map[string]struct{} {
	set := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		set[action] = struct{}{}
	}
	return set
}
