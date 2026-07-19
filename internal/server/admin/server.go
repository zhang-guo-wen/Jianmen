package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"jianmen/internal/config"
	"jianmen/internal/handler/accessrequest"
	"jianmen/internal/handler/webrdp"
	"jianmen/internal/online"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type Server struct {
	cfg                  *config.Config
	store                store.Store
	db                   *gorm.DB
	logger               *slog.Logger
	dataDir              string
	loginLimiter         *loginLimiter
	loginCaptcha         loginCaptchaVerifier
	appProxy             *appproxy.Server
	onlineSessions       *online.Registry
	containerService     *service.ContainerService
	identity             *service.IdentityService
	authorization        authorizationService
	resourceAccess       resourceAccessRepository
	resourceGrants       *service.ResourceGrantService
	resourceGroups       *service.ResourceGroupService
	userManagement       *service.UserService
	userGroups           *service.UserGroupService
	roleManagement       *service.RoleService
	databaseProvisioning databaseProvisioningService
	temporaryAccess      *service.TemporaryAccessService
	browserSessions      *service.BrowserSessionService
	webRDP               *webrdp.Handler
	accessRequests       *accessrequest.Handler
	setupOnce            sync.Once
	setupSlot            chan struct{}
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
	databaseProvisioning databaseProvisioningService,
	logger *slog.Logger,
	dataDir string,
	appProxy *appproxy.Server,
	onlineSessions *online.Registry,
	webRDP *webrdp.Handler,
	accessRequests *accessrequest.Handler,
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
	case databaseProvisioning == nil:
		return nil, errors.New("admin database provisioning service is required")
	case logger == nil:
		return nil, errors.New("admin logger is required")
	case onlineSessions == nil:
		return nil, errors.New("admin online session registry is required")
	case cfg.WebRDP.Enabled && (webRDP == nil || accessRequests == nil):
		return nil, errors.New("admin Web RDP handlers are required")
	}
	resourceAccess, ok := repository.(resourceAccessRepository)
	if !ok {
		return nil, errors.New("admin store does not support resource access")
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
		identity: identity, authorization: authorization, resourceAccess: resourceAccess,
		resourceGrants: resourceGrants, resourceGroups: resourceGroups, userManagement: userManagement, userGroups: userGroups, roleManagement: roleManagement, databaseProvisioning: databaseProvisioning, temporaryAccess: temporaryAccess,
		browserSessions: browserSessions,
		webRDP:          webRDP, accessRequests: accessRequests,
	}, nil
}
