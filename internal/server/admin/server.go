package admin

import (
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
	appProxy         *appproxy.Server
	onlineSessions   *online.Registry
	containerService *service.ContainerService
}

func New(cfg *config.Config, store store.Store, logger *slog.Logger, dataDir string, appProxy *appproxy.Server, onlineSessions *online.Registry, dbs ...*gorm.DB) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	var db *gorm.DB
	var checker *rbac.Checker
	if len(dbs) > 0 {
		db = dbs[0]
		checker = rbac.NewChecker(db)
	}
	superAdminIDs := LoadSuperAdminIDs(cfg, dataDir)
	return &Server{
		cfg: cfg, store: store, db: db, rbacChecker: checker, logger: logger,
		dataDir: dataDir, superAdminIDs: superAdminIDs,
		loginLimiter: newDefaultLoginLimiter(), appProxy: appProxy,
		onlineSessions: onlineSessions, containerService: service.NewContainerService(),
	}
}
