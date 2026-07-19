package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"jianmen/internal/config"
	"jianmen/internal/handler/accessrequest"
	"jianmen/internal/handler/systemsettings"
	"jianmen/internal/handler/webrdp"
	"jianmen/internal/online"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type Server struct {
	cfg                    *config.Config
	adminAuth              *service.AdminAuthService
	aiAccessTokens         *service.AIAccessTokenService
	aiResources            *service.AIResourceService
	hostTargets            adminHostTargetRepository
	hostManagement         *service.HostManagementService
	databases              adminDatabaseRepository
	databaseManagement     *service.DatabaseManagementService
	applicationService     *service.ApplicationService
	containerManagement    *service.ContainerManagementService
	platformAccountService *service.PlatformAccountService
	userSessionCreation    *service.UserSessionCreationService
	audit                  adminAuditRepository
	auditQuery             *service.AuditQueryService
	connectionPassword     *service.ConnectionPasswordService
	preferences            *service.UserPreferenceService
	temporaryRepository    service.TemporaryAccessRepository
	userRepository         service.UserRepository
	userGroupRepository    service.UserGroupRepository
	roleRepository         service.RoleManagementRepository
	db                     *gorm.DB
	logger                 *slog.Logger
	dataDir                string
	loginLimiter           *loginLimiter
	loginCaptcha           loginCaptchaVerifier
	onlineSessions         *online.Registry
	identity               *service.IdentityService
	authorization          authorizationService
	resourceAccess         resourceAccessRepository
	resourceGrants         *service.ResourceGrantService
	resourceGroups         *service.ResourceGroupService
	userManagement         *service.UserService
	userGroups             *service.UserGroupService
	roleManagement         *service.RoleService
	databaseProvisioning   databaseProvisioningService
	temporaryAccess        *service.TemporaryAccessService
	browserSessions        *service.BrowserSessionService
	webRDP                 *webrdp.Handler
	accessRequests         *accessrequest.Handler
	systemSettings         *systemsettings.Handler
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
	systemSettings *systemsettings.Handler,
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
	case isNilAdminAuthorization(authorization):
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
	case systemSettings == nil:
		return nil, errors.New("admin system settings handler is required")
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
	aiAccessTokens, err := service.NewAIAccessTokenService(dependencies.aiTokens)
	if err != nil {
		return nil, fmt.Errorf("initialize AI access token service: %w", err)
	}
	keyReader, err := store.NewFileAdminEncryptionKeyReader(dataDir)
	if err != nil {
		return nil, fmt.Errorf("initialize admin encryption key reader: %w", err)
	}
	adminAuth, err := service.NewAdminAuthService(dependencies.adminAuth, browserSessions, keyReader)
	if err != nil {
		return nil, fmt.Errorf("initialize admin auth service: %w", err)
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
	aiResources, err := service.NewAIResourceService(
		dependencies.aiResources,
		aiResourceAuthorizerAdapter{authorization: authorization},
		aiResourceSessionCreatorAdapter{sessions: userSessionCreation},
	)
	if err != nil {
		return nil, fmt.Errorf("initialize AI resource service: %w", err)
	}
	connectionPassword, err := service.NewConnectionPasswordService(
		dependencies.connectionPassword,
		authorization,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize connection password service: %w", err)
	}
	hostManagement, err := service.NewHostManagementService(hostManagementRepositoryAdapter{repository: dependencies.hostTargets}, authorization)
	if err != nil {
		return nil, fmt.Errorf("initialize host management service: %w", err)
	}
	userPreferences, err := service.NewUserPreferenceService(dependencies.userPreferences)
	if err != nil {
		return nil, fmt.Errorf("initialize user preference service: %w", err)
	}
	databaseManagement, err := service.NewDatabaseManagementService(databaseManagementRepositoryAdapter{repository: dependencies.databases}, authorization, databaseProvisioning)
	if err != nil {
		return nil, fmt.Errorf("initialize database management service: %w", err)
	}
	var applicationProxy service.ApplicationProxy
	if appProxy != nil {
		applicationProxy = appProxy
	}
	applicationService, err := service.NewApplicationService(
		dependencies.applications,
		authorization,
		applicationProxy,
		cfg.ApplicationGateway.PortStart,
		cfg.ApplicationGateway.PortEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("initialize application service: %w", err)
	}
	platformAccountService, err := service.NewPlatformAccountService(dependencies.platformAccounts, authorization)
	if err != nil {
		return nil, fmt.Errorf("initialize platform account service: %w", err)
	}
	containerManagement, err := service.NewContainerManagementService(
		dependencies.containers,
		authorization,
		service.NewContainerService(),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize container management service: %w", err)
	}
	auditQuery, err := service.NewAuditQueryService(
		adminAuditQueryRepository{repository: dependencies.audit},
		adminAuditQueryAuthorizer{authorization: authorization},
	)
	if err != nil {
		return nil, fmt.Errorf("initialize audit query service: %w", err)
	}
	return &Server{
		cfg: cfg, db: db, logger: logger,
		adminAuth: adminAuth, aiAccessTokens: aiAccessTokens, aiResources: aiResources,
		hostTargets: dependencies.hostTargets, hostManagement: hostManagement, databases: dependencies.databases,
		databaseManagement: databaseManagement, applicationService: applicationService,
		containerManagement: containerManagement, platformAccountService: platformAccountService,
		userSessionCreation: userSessionCreation, audit: dependencies.audit, auditQuery: auditQuery, connectionPassword: connectionPassword,
		preferences: userPreferences, temporaryRepository: dependencies.temporaryAccess,
		userRepository: dependencies.users, userGroupRepository: dependencies.userGroups, roleRepository: dependencies.roles,
		dataDir:      dataDir,
		loginLimiter: newDefaultLoginLimiter(), loginCaptcha: loginCaptcha,
		onlineSessions: onlineSessions,
		identity:       identity, authorization: authorization, resourceAccess: dependencies.resourceAccess,
		resourceGrants: resourceGrants, resourceGroups: resourceGroups, userManagement: userManagement, userGroups: userGroups, roleManagement: roleManagement, databaseProvisioning: databaseProvisioning, temporaryAccess: temporaryAccess,
		browserSessions: browserSessions,
		webRDP:          webRDP, accessRequests: accessRequests,
		systemSettings: systemSettings,
	}, nil
}

func isNilAdminAuthorization(authorization authorizationService) bool {
	if authorization == nil {
		return true
	}
	value := reflect.ValueOf(authorization)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
