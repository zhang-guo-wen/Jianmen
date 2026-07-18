package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
	"jianmen/internal/util"
)

var errDatabaseAuthentication = errors.New("database connection authentication failed")

func (g *Gateway) authorizeConnect(ctx context.Context, userID, resourceID string) error {
	if g.authorizer == nil {
		return errors.New("database authorization unavailable")
	}
	allowed, err := g.authorizer.AuthorizeConnection(
		ctx,
		userID,
		[]string{rbaccheck.ActionDBConnect},
		model.ResourceTypeDatabaseAccount,
		resourceID,
	)
	if err != nil {
		return fmt.Errorf("authorize database connection: %w", err)
	}
	if !allowed {
		return fmt.Errorf("user %q lacks %s", userID, rbaccheck.ActionDBConnect)
	}
	return nil
}

func (g *Gateway) authenticateAndAuthorizeConnection(
	ctx context.Context,
	userID string,
	resourceID string,
	authenticate func(context.Context) error,
) error {
	if authenticate == nil {
		return errDatabaseAuthentication
	}
	if err := authenticate(ctx); err != nil {
		return fmt.Errorf("%w: %w", errDatabaseAuthentication, err)
	}
	return g.authorizeConnect(ctx, userID, resourceID)
}

func (g *Gateway) authenticateMySQLConnection(
	ctx context.Context,
	resolved *resolvedDBAccount,
	salt []byte,
	response []byte,
) error {
	return g.authenticateAndAuthorizeConnection(ctx, resolved.user.ID, resolved.account.ID, func(ctx context.Context) error {
		if g.store == nil {
			return errors.New("authentication unavailable")
		}
		if err := g.store.AuthenticateMySQLConnectionPassword(ctx, resolved.user.ID, resolved.account.ID, salt, response); err != nil {
			return fmt.Errorf("authenticate mysql connection password: %w", err)
		}
		return nil
	})
}

func (g *Gateway) authenticatePostgresConnection(ctx context.Context, resolved *resolvedDBAccount, password string) error {
	return g.authenticateAndAuthorizeConnection(ctx, resolved.user.ID, resolved.account.ID, func(ctx context.Context) error {
		return g.validateUserPassword(ctx, resolved.user, resolved.account.ID, password)
	})
}

func (g *Gateway) authenticateRedisConnection(ctx context.Context, resolved *resolvedDBAccount, password string) error {
	return g.authenticateAndAuthorizeConnection(ctx, resolved.user.ID, resolved.account.ID, func(ctx context.Context) error {
		return g.validateUserPassword(ctx, resolved.user, resolved.account.ID, password)
	})
}

// resolvedDBAccount 解析连接用户名后的数据库账号及关联用户信息
type resolvedDBAccount struct {
	account       *model.DatabaseAccount
	user          *model.User // compact username 认证后的堡垒机用户
	isCompact     bool        // 是否通过 compact username 解析
	rawName       string      // 原始用户名（用于日志）
	userSessionID string
}

// resolveAccount 解析连接用户名：优先尝试 compact username (D/H前缀10位)，失败则回退到 unique_name 查找
func (g *Gateway) resolveAccount(ctx context.Context, rawUsername string) (*resolvedDBAccount, error) {
	rawUsername = strings.TrimSpace(rawUsername)
	if rawUsername == "" {
		return nil, errors.New("empty username")
	}

	// 仅支持 compact username 登录（10位，D或R前缀）
	if len(rawUsername) != 10 {
		return nil, fmt.Errorf("invalid username format: must be 10-character compact username (D/R + resource_id + session_id)")
	}
	prefix, _, _, err := util.ParseCompactUsername(rawUsername)
	if err != nil {
		return nil, fmt.Errorf("invalid compact username %q: %w", rawUsername, err)
	}
	if prefix != util.PrefixDatabase && prefix != util.PrefixRedis {
		return nil, fmt.Errorf("unsupported prefix %q in username %q, expected D or R", prefix, rawUsername)
	}
	return g.resolveCompactAccount(ctx, rawUsername)
}

// resolveCompactAccount 从 compact username 解析并查找数据库账号和用户会话
func (g *Gateway) resolveCompactAccount(ctx context.Context, username string) (*resolvedDBAccount, error) {
	resourceID := username[1:5]
	sessionID := username[5:10]
	db := g.db.WithContext(ctx)

	// 查找数据库账号（按 resource_id）
	var acct model.DatabaseAccount
	if err := db.Preload("Instance").Where("resource_id = ? AND status = ?", resourceID, "active").First(&acct).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("database account not found for resource_id %q", resourceID)
		}
		return nil, fmt.Errorf("lookup database account: %w", err)
	}
	if acct.Instance.Status == "disabled" {
		return nil, fmt.Errorf("database instance %q is disabled", acct.InstanceID)
	}

	// 查找用户会话
	var sess model.UserSession
	if err := db.Where("session_id = ? AND status = ?", sessionID, "active").First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("invalid session %q", sessionID)
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	// 检查会话过期
	if sess.ExpiresAt != nil && time.Now().UTC().After(*sess.ExpiresAt) {
		db.Model(&sess).Update("status", "expired")
		return nil, fmt.Errorf("session %q expired", sessionID)
	}

	// 查找用户
	var user model.User
	if err := db.Where("id = ? AND status = ?", sess.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user for session %q is disabled or not found", sessionID)
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}

	return &resolvedDBAccount{
		account:       &acct,
		user:          &user,
		isCompact:     true,
		rawName:       username,
		userSessionID: sess.ID,
	}, nil
}

// validateUserPassword 验证堡垒机用户密码（仅 compact username 路径使用）
func (g *Gateway) validateUserPassword(ctx context.Context, user *model.User, accountID, password string) error {
	if g.store == nil {
		return errors.New("authentication unavailable")
	}
	if err := g.store.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeDatabaseAccount, accountID, password); err != nil {
		return fmt.Errorf("authenticate connection password: %w", err)
	}
	return nil
}

// dbAccountResourceID 获取数据库账号的 RBAC 资源ID
func dbAccountResourceID(acct *model.DatabaseAccount) string {
	return rbaccheck.DatabaseAccountResourceID(acct.UniqueName)
}
