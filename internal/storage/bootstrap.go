package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

const (
	builtinAdminRoleID       = "builtin-admin"
	builtinSSHOperatorRoleID = "builtin-ssh-operator"
	builtinDBOperatorRoleID  = "builtin-db-operator"
	builtinDBAuditorRoleID   = "builtin-db-auditor"

	builtinAdminActionPermissionID   = "builtin-admin-action-all"
	builtinAdminResourcePermissionID = "builtin-admin-resource-all"
	builtinDBConnectAction           = "db:connect"
)

func BootstrapMetadata(db *gorm.DB, cfg *config.Config) error {
	if db == nil {
		return nil
	}
	if cfg == nil {
		return fmt.Errorf("bootstrap metadata: nil config")
	}
	if err := bootstrapConfigUsers(db, cfg.Users); err != nil {
		return err
	}
	if err := bootstrapBuiltinRoles(db); err != nil {
		return err
	}
	if err := bootstrapBuiltinPermissions(db); err != nil {
		return err
	}
	if err := bootstrapBuiltinRolePermissions(db); err != nil {
		return err
	}
	if adminUserID := firstConfigUserID(cfg.Users); adminUserID != "" {
		if err := bootstrapAdminUserRole(db, adminUserID); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapConfigUsers(db *gorm.DB, users []config.User) error {
	for _, cfgUser := range users {
		userID := configUserID(cfgUser)
		username := strings.TrimSpace(cfgUser.Username)
		if userID == "" || username == "" {
			continue
		}
		user := model.User{
			ID:       userID,
			Username: username,
			Status:   "active",
		}
		if token := strings.TrimSpace(cfgUser.ApiToken); token != "" {
			hash := sha256.Sum256([]byte(token))
			user.TokenHash = hex.EncodeToString(hash[:])
		}
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"username":   user.Username,
				"status":     user.Status,
				"token_hash": user.TokenHash,
			}),
		}).Create(&user).Error; err != nil {
			return fmt.Errorf("bootstrap metadata user %q: %w", userID, err)
		}
	}
	return nil
}

func bootstrapBuiltinRoles(db *gorm.DB) error {
	roles := []model.Role{
		{
			ID:          builtinAdminRoleID,
			Name:        "builtin-admin",
			Description: "Full administrative access bootstrapped from static configuration.",
			Builtin:     true,
			Status:      "active",
		},
		{
			ID:          builtinSSHOperatorRoleID,
			Name:        "builtin-ssh-operator",
			Description: "Operational SSH/SFTP access role template.",
			Builtin:     true,
			Status:      "active",
		},
		{
			ID:          builtinDBOperatorRoleID,
			Name:        "builtin-db-operator",
			Description: "Operational database proxy access role template.",
			Builtin:     true,
			Status:      "active",
		},
		{
			ID:          builtinDBAuditorRoleID,
			Name:        "builtin-db-auditor",
			Description: "Database proxy audit viewer role template.",
			Builtin:     true,
			Status:      "active",
		},
	}
	for _, role := range roles {
		if err := upsertRole(db, role); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapBuiltinPermissions(db *gorm.DB) error {
	permissions := []model.Permission{
		{
			ID:          builtinAdminActionPermissionID,
			Name:        "builtin-admin-action-all",
			Action:      "*",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows all actions.",
		},
		{
			ID:           builtinAdminResourcePermissionID,
			Name:         "builtin-admin-resource-all",
			Action:       "*",
			ResourceType: "*",
			ResourceID:   "*",
			Effect:       model.PermissionEffectAllow,
			Description:  "Allows all resource scopes.",
		},
		{
			ID:          "builtin-dashboard-view",
			Name:        "builtin-dashboard-view",
			Action:      "dashboard:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows viewing the dashboard.",
		},
		{
			ID:          "builtin-ssh-connect",
			Name:        "builtin-ssh-connect",
			Action:      "session:connect",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows SSH session connection when paired with a resource grant.",
		},
		{
			ID:           "builtin-ssh-any-host-account",
			Name:         "builtin-ssh-any-host-account",
			ResourceType: model.ResourceTypeHostAccount,
			ResourceID:   "*",
			Effect:       model.PermissionEffectAllow,
			Description:  "Grants all host account resources.",
		},
		{
			ID:          "builtin-sftp-read",
			Name:        "builtin-sftp-read",
			Action:      "sftp:read",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows SFTP read operations when paired with a host grant.",
		},
		{
			ID:          "builtin-sftp-write",
			Name:        "builtin-sftp-write",
			Action:      "sftp:write",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows SFTP write operations when paired with a host grant.",
		},
		{
			ID:          "builtin-audit-view",
			Name:        "builtin-audit-view",
			Action:      "audit:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows audit list and detail views.",
		},
		{
			ID:          "builtin-db-audit-view",
			Name:        "builtin-db-audit-view",
			Action:      "db:audit:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows database proxy audit views.",
		},
		{
			ID:          "builtin-db-connect",
			Name:        "builtin-db-connect",
			Action:      builtinDBConnectAction,
			Effect:      model.PermissionEffectAllow,
			Description: "Allows database proxy connections when paired with a database account grant.",
		},
		{
			ID:           "builtin-db-any-database-account",
			Name:         "builtin-db-any-database-account",
			ResourceType: model.ResourceTypeDatabaseAccount,
			ResourceID:   "*",
			Effect:       model.PermissionEffectAllow,
			Description:  "Grants all database account resources.",
		},
		{
			ID:           "builtin-db-instance-manage",
			Name:         "builtin-db-instance-manage",
			Action:       "db:instance:manage",
			ResourceType: model.ResourceTypeDatabaseInstance,
			ResourceID:   "*",
			Effect:       model.PermissionEffectAllow,
			Description:  "Allows manage all database instances.",
		},
		{
			ID:           "builtin-db-account-manage",
			Name:         "builtin-db-account-manage",
			Action:       "db:account:manage",
			ResourceType: model.ResourceTypeDatabaseAccount,
			ResourceID:   "*",
			Effect:       model.PermissionEffectAllow,
			Description:  "Allows manage all database accounts.",
		},
		{
			ID:          "builtin-host-view",
			Name:        "builtin-host-view",
			Action:      "host:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows viewing host resources.",
		},
		{
			ID:          "builtin-host-create",
			Name:        "builtin-host-create",
			Action:      "host:create",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows creating host resources.",
		},
		{
			ID:          "builtin-host-update",
			Name:        "builtin-host-update",
			Action:      "host:update",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows updating host resources.",
		},
		{
			ID:          "builtin-host-delete",
			Name:        "builtin-host-delete",
			Action:      "host:delete",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows deleting host resources.",
		},
		{
			ID:          "builtin-target-view",
			Name:        "builtin-target-view",
			Action:      "target:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows viewing target resources.",
		},
		{
			ID:          "builtin-target-create",
			Name:        "builtin-target-create",
			Action:      "target:create",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows creating target resources.",
		},
		{
			ID:          "builtin-target-update",
			Name:        "builtin-target-update",
			Action:      "target:update",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows updating target resources.",
		},
		{
			ID:          "builtin-target-delete",
			Name:        "builtin-target-delete",
			Action:      "target:delete",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows deleting target resources.",
		},
		{
			ID:          "builtin-dbproxy-view",
			Name:        "builtin-dbproxy-view",
			Action:      "dbproxy:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows viewing database proxy resources.",
		},
		{
			ID:          "builtin-dbproxy-create",
			Name:        "builtin-dbproxy-create",
			Action:      "dbproxy:create",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows creating database proxy resources.",
		},
		{
			ID:          "builtin-dbproxy-update",
			Name:        "builtin-dbproxy-update",
			Action:      "dbproxy:update",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows updating database proxy resources.",
		},
		{
			ID:          "builtin-dbproxy-delete",
			Name:        "builtin-dbproxy-delete",
			Action:      "dbproxy:delete",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows deleting database proxy resources.",
		},
		{
			ID:          "builtin-rbac-manage",
			Name:        "builtin-rbac-manage",
			Action:      "rbac:manage",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows managing RBAC roles, permissions, and bindings.",
		},
		{
			ID:          "builtin-session-view",
			Name:        "builtin-session-view",
			Action:      "session:view",
			Effect:      model.PermissionEffectAllow,
			Description: "Allows viewing session records.",
		},
	}
	for _, permission := range permissions {
		if err := upsertPermission(db, permission); err != nil {
			return err
		}
	}
	return nil
}

func bootstrapBuiltinRolePermissions(db *gorm.DB) error {
	assignments := []struct {
		roleID       string
		permissionID string
	}{
		{builtinAdminRoleID, builtinAdminActionPermissionID},
		{builtinAdminRoleID, builtinAdminResourcePermissionID},
		{builtinSSHOperatorRoleID, "builtin-ssh-connect"},
		{builtinSSHOperatorRoleID, "builtin-ssh-any-host-account"},
		{builtinSSHOperatorRoleID, "builtin-sftp-read"},
		{builtinSSHOperatorRoleID, "builtin-sftp-write"},
		{builtinDBOperatorRoleID, "builtin-db-connect"},
		{builtinDBOperatorRoleID, "builtin-db-any-database-account"},
		{builtinDBAuditorRoleID, "builtin-audit-view"},
		{builtinDBAuditorRoleID, "builtin-db-audit-view"},
		{builtinDBOperatorRoleID, "builtin-db-instance-manage"},
		{builtinDBOperatorRoleID, "builtin-db-account-manage"},
		{builtinAdminRoleID, "builtin-db-instance-manage"},
		{builtinAdminRoleID, "builtin-db-account-manage"},
		{builtinAdminRoleID, "builtin-host-view"},
		{builtinAdminRoleID, "builtin-host-create"},
		{builtinAdminRoleID, "builtin-host-update"},
		{builtinAdminRoleID, "builtin-host-delete"},
		{builtinAdminRoleID, "builtin-target-view"},
		{builtinAdminRoleID, "builtin-target-create"},
		{builtinAdminRoleID, "builtin-target-update"},
		{builtinAdminRoleID, "builtin-target-delete"},
		{builtinAdminRoleID, "builtin-dbproxy-view"},
		{builtinAdminRoleID, "builtin-dbproxy-create"},
		{builtinAdminRoleID, "builtin-dbproxy-update"},
		{builtinAdminRoleID, "builtin-dbproxy-delete"},
		{builtinAdminRoleID, "builtin-rbac-manage"},
		{builtinAdminRoleID, "builtin-session-view"},
		{builtinAdminRoleID, "builtin-dashboard-view"},
		{builtinSSHOperatorRoleID, "builtin-dashboard-view"},
		{builtinDBOperatorRoleID, "builtin-dashboard-view"},
		{builtinDBAuditorRoleID, "builtin-dashboard-view"},
	}
	for _, assignment := range assignments {
		binding := model.RolePermission{
			ID:           stableID("brp", assignment.roleID, assignment.permissionID),
			RoleID:       assignment.roleID,
			PermissionID: assignment.permissionID,
		}
		if err := db.Where("role_id = ? AND permission_id = ?", binding.RoleID, binding.PermissionID).
			FirstOrCreate(&binding).Error; err != nil {
			return fmt.Errorf("bootstrap role permission %s/%s: %w", binding.RoleID, binding.PermissionID, err)
		}
	}
	return nil
}

func bootstrapAdminUserRole(db *gorm.DB, userID string) error {
	binding := model.UserRole{
		ID:     stableID("bur", userID, builtinAdminRoleID),
		UserID: userID,
		RoleID: builtinAdminRoleID,
	}
	if err := db.Where("user_id = ? AND role_id = ?", binding.UserID, binding.RoleID).
		FirstOrCreate(&binding).Error; err != nil {
		return fmt.Errorf("bootstrap admin user role %s: %w", userID, err)
	}
	return nil
}

func upsertRole(db *gorm.DB, role model.Role) error {
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":        role.Name,
			"description": role.Description,
			"builtin":     role.Builtin,
			"status":      role.Status,
		}),
	}).Create(&role).Error; err != nil {
		return fmt.Errorf("bootstrap role %q: %w", role.ID, err)
	}
	return nil
}

func upsertPermission(db *gorm.DB, permission model.Permission) error {
	if err := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"name":          permission.Name,
			"action":        permission.Action,
			"resource_type": permission.ResourceType,
			"resource_id":   permission.ResourceID,
			"effect":        permission.Effect,
			"description":   permission.Description,
		}),
	}).Create(&permission).Error; err != nil {
		return fmt.Errorf("bootstrap permission %q: %w", permission.ID, err)
	}
	return nil
}

func firstConfigUserID(users []config.User) string {
	for _, user := range users {
		userID := configUserID(user)
		if userID != "" {
			return userID
		}
	}
	return ""
}

func configUserID(user config.User) string {
	if id := strings.TrimSpace(user.ID); id != "" {
		return id
	}
	return strings.TrimSpace(user.Username)
}

func stableID(parts ...string) string {
	value := strings.Join(parts, "-")
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, value)
	value = strings.Trim(value, "-")
	if len(value) <= 64 {
		return value
	}
	return value[:64]
}
