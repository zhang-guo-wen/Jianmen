package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type Server struct {
	cfg              *config.Config
	store            store.Store
	db               *gorm.DB
	logger           *slog.Logger
	dataDir          string
	loginLimiter     *loginLimiter
	loginCaptcha     loginCaptchaVerifier
	appProxy         *appproxy.Server
	onlineSessions   *online.Registry
	containerService *service.ContainerService
	identity         *service.IdentityService
	authorization    authorizationService
	resourceGrants   *service.ResourceGrantService
	resourceGroups   *service.ResourceGroupService
	userManagement   *service.UserService
	userGroups       *service.UserGroupService
	roleManagement   *service.RoleService
	temporaryAccess  *service.TemporaryAccessService
	browserSessions  *service.BrowserSessionService
	setupOnce        sync.Once
	setupSlot        chan struct{}
}

type loginCaptchaVerifier interface {
	CreateChallenge() (service.LoginCaptchaChallenge, error)
	Verify(payload string) error
}

type authorizationService interface {
	AuthorizeConnection(
		ctx context.Context,
		userID string,
		actions []string,
		resourceType string,
		resourceID string,
	) (bool, error)
}

func New(
	cfg *config.Config,
	repository store.Store,
	db *gorm.DB,
	identity *service.IdentityService,
	browserSessions *service.BrowserSessionService,
	authorization authorizationService,
	resourceGrants *service.ResourceGrantService,
	resourceGroups *service.ResourceGroupService,
	logger *slog.Logger,
	dataDir string,
	appProxy *appproxy.Server,
	onlineSessions *online.Registry,
) (*Server, error) {
	switch {
	case cfg == nil:
		return nil, errors.New("admin config is required")
	case repository == nil:
		return nil, errors.New("admin store is required")
	case db == nil:
		return nil, errors.New("admin metadata database is required")
	case identity == nil:
		return nil, errors.New("admin identity service is required")
	case browserSessions == nil:
		return nil, errors.New("admin browser session service is required")
	case authorization == nil:
		return nil, errors.New("admin authorization service is required")
	case resourceGrants == nil:
		return nil, errors.New("admin resource grant service is required")
	case resourceGroups == nil:
		return nil, errors.New("admin resource group service is required")
	case logger == nil:
		return nil, errors.New("admin logger is required")
	case onlineSessions == nil:
		return nil, errors.New("admin online session registry is required")
	}
	loginCaptcha, err := service.NewLoginCaptcha()
	if err != nil {
		return nil, fmt.Errorf("initialize login captcha: %w", err)
	}
	temporaryRepository, ok := repository.(service.TemporaryAccessRepository)
	if !ok {
		return nil, errors.New("admin store does not support temporary access")
	}
	temporaryAccess, err := service.NewTemporaryAccessService(temporaryRepository)
	if err != nil {
		return nil, fmt.Errorf("initialize temporary access service: %w", err)
	}
	userRepository, ok := repository.(service.UserRepository)
	if !ok {
		return nil, errors.New("admin store does not support user management")
	}
	userManagement, err := service.NewUserService(userRepository)
	if err != nil {
		return nil, fmt.Errorf("initialize user service: %w", err)
	}
	userGroupRepository, ok := repository.(service.UserGroupRepository)
	if !ok {
		return nil, errors.New("admin store does not support user group management")
	}
	userGroups, err := service.NewUserGroupService(userGroupRepository)
	if err != nil {
		return nil, fmt.Errorf("initialize user group service: %w", err)
	}
	roleManagement, err := newRoleManagementService(repository)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg: cfg, store: repository, db: db, logger: logger,
		dataDir:      dataDir,
		loginLimiter: newDefaultLoginLimiter(), loginCaptcha: loginCaptcha, appProxy: appProxy,
		onlineSessions: onlineSessions, containerService: service.NewContainerService(),
		identity: identity, authorization: authorization,
		resourceGrants: resourceGrants, resourceGroups: resourceGroups, userManagement: userManagement, userGroups: userGroups, roleManagement: roleManagement, temporaryAccess: temporaryAccess,
		browserSessions: browserSessions,
	}, nil
}
