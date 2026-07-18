package admin

import (
	"errors"
	"fmt"
	"log/slog"

	"jianmen/internal/config"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/server/appproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type Server struct {
	cfg              *config.Config
	store            store.Store
	db               *gorm.DB
	rbacChecker      *rbac.Checker
	logger           *slog.Logger
	dataDir          string
	superAdminIDs    map[string]bool
	loginLimiter     *loginLimiter
	loginCaptcha     loginCaptchaVerifier
	appProxy         *appproxy.Server
	onlineSessions   *online.Registry
	containerService *service.ContainerService
	adminAuth        *service.AdminAuthService
	resourceGrants   *service.ResourceGrantService
	resourceGroups   *service.ResourceGroupService
	temporaryAccess  *service.TemporaryAccessService
}

type loginCaptchaVerifier interface {
	CreateChallenge() (service.LoginCaptchaChallenge, error)
	Verify(payload string) error
}

func New(
	cfg *config.Config,
	repository store.Store,
	db *gorm.DB,
	adminAuth *service.AdminAuthService,
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
	case adminAuth == nil:
		return nil, errors.New("admin auth service is required")
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
	superAdminIDs := LoadSuperAdminIDs(cfg, dataDir)
	return &Server{
		cfg: cfg, store: repository, db: db, rbacChecker: rbac.NewChecker(db), logger: logger,
		dataDir: dataDir, superAdminIDs: superAdminIDs,
		loginLimiter: newDefaultLoginLimiter(), loginCaptcha: loginCaptcha, appProxy: appProxy,
		onlineSessions: onlineSessions, containerService: service.NewContainerService(),
		adminAuth: adminAuth, resourceGrants: resourceGrants, resourceGroups: resourceGroups, temporaryAccess: temporaryAccess,
	}, nil
}
