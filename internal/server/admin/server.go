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

	"gorm.io/gorm"
)

type Server struct {
	cfg                  *config.Config
	aiTokens             adminAIAccessTokenRepository
	hostTargets          adminHostTargetRepository
	databases            adminDatabaseRepository
	applications         adminApplicationRepository
	containers           adminContainerRepository
	platformAccounts     adminPlatformAccountRepository
	userSessions         adminUserSessionRepository
	userSessionCreation  *service.UserSessionCreationService
	audit                adminAuditRepository
	connectionPassword   adminConnectionPasswordRepository
	preferences          adminUserPreferenceRepository
	temporaryRepository  service.TemporaryAccessRepository
	userRepository       service.UserRepository
	userGroupRepository  service.UserGroupRepository
	roleRepository       service.RoleManagementRepository
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
	repository adminRepository,
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
	case webRDP == nil || accessRequests == nil:
		return nil, errors.New("admin Web RDP audit and approval handlers are required")
	}
	dependencies, err := resolveAdminDependencies(repository)
	if err != nil {
		return nil, err
	}
	loginCaptcha, err := service.NewLoginCaptcha()
	if err != nil {
		return nil, fmt.Errorf("initialize login captcha: %w", err)
	}
	temporaryAccess, err := service.NewTemporaryAccessService(dependencies.temporaryAccess)
	if err != nil {
		return nil, fmt.Errorf("initialize temporary access service: %w", err)
	}
	userManagement, err := service.NewUserService(dependencies.users)
	if err != nil {
		return nil, fmt.Errorf("initialize user service: %w", err)
	}
	userGroups, err := service.NewUserGroupService(dependencies.userGroups)
	if err != nil {
		return nil, fmt.Errorf("initialize user group service: %w", err)
	}
	roleManagement, err := newRoleManagementService(dependencies.roles)
	if err != nil {
		return nil, err
	}
	userSessionCreation, err := service.NewUserSessionCreationService(dependencies.userSessionCreation, authorization)
	if err != nil {
		return nil, fmt.Errorf("initialize user session creation service: %w", err)
	}
	return &Server{
		cfg: cfg, db: db, logger: logger,
		aiTokens: dependencies.aiTokens, hostTargets: dependencies.hostTargets, databases: dependencies.databases,
		applications: dependencies.applications, containers: dependencies.containers, platformAccounts: dependencies.platformAccounts,
		userSessions: dependencies.userSessions, userSessionCreation: userSessionCreation, audit: dependencies.audit, connectionPassword: dependencies.connectionPassword,
		preferences: dependencies.preferences, temporaryRepository: dependencies.temporaryAccess,
		userRepository: dependencies.users, userGroupRepository: dependencies.userGroups, roleRepository: dependencies.roles,
		dataDir:      dataDir,
		loginLimiter: newDefaultLoginLimiter(), loginCaptcha: loginCaptcha, appProxy: appProxy,
		onlineSessions: onlineSessions, containerService: service.NewContainerService(),
		identity: identity, authorization: authorization, resourceAccess: dependencies.resourceAccess,
		resourceGrants: resourceGrants, resourceGroups: resourceGroups, userManagement: userManagement, userGroups: userGroups, roleManagement: roleManagement, databaseProvisioning: databaseProvisioning, temporaryAccess: temporaryAccess,
		browserSessions: browserSessions,
		webRDP:          webRDP, accessRequests: accessRequests,
	}, nil
}
