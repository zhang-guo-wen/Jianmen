package dbproxy

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/online"
)

type Gateway struct {
	cfg               config.DatabaseGatewayConfig
	store             databaseAccountResolver
	db                *gorm.DB
	replayDir         string
	logger            *slog.Logger
	authorizer        connectionAuthorizer
	audit             auditWriter
	auditRequired     bool
	onlineSessions    *online.Registry
	postgresCancels   postgresCancelRegistry
	pendingHandshakes *pendingHandshakeLimiter
}

type databaseAccountResolver interface {
	AuthenticateConnectionPassword(ctx context.Context, userID, resourceType, resourceID, password string) error
	AuthenticateMySQLConnectionPassword(ctx context.Context, userID, resourceID string, salt, response []byte) error
}

type auditWriter interface {
	CreateAuditSession(session *model.AuditSession) error
	EndAuditSession(id string) error
	CreateAuditDBQuery(query *model.AuditDBQuery) error
}

type connectionAuthorizer interface {
	AuthorizeConnection(ctx context.Context, userID string, actions []string, resourceType, resourceID string) (bool, error)
}

func NewGateway(cfg config.DatabaseGatewayConfig, store databaseAccountResolver, replayDir string, logger *slog.Logger, db *gorm.DB, authorizer connectionAuthorizer, onlineSessions *online.Registry, auditStore auditWriter) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		cfg:               cfg,
		store:             store,
		db:                db,
		replayDir:         replayDir,
		logger:            logger,
		authorizer:        authorizer,
		audit:             auditStore,
		auditRequired:     true,
		onlineSessions:    onlineSessions,
		pendingHandshakes: newPendingHandshakeLimiter(defaultPendingHandshakeLimit),
	}
}

func (g *Gateway) Enabled() bool {
	return g.cfg.Enabled
}

func (g *Gateway) ListenAndServe(ctx context.Context) error {
	if !g.cfg.Enabled {
		return nil
	}
	return g.listenAndServeProtocolListeners(ctx)
}

type gatewayConn struct {
	client                net.Conn
	protocol              string
	accountID             string
	instanceID            string
	accountName           string
	upstream              net.Conn
	upstreamAddr          string
	userID                string
	accountUser           string // 上游数据库登录名
	instanceName          string // 数据库实例名称
	userSessionID         string
	postgresCancelCleanup func()
}

func upstreamAddress(inst model.DatabaseInstance) string {
	port := inst.Port
	if port == 0 {
		switch inst.Protocol {
		case "postgres":
			port = 5432
		case "redis":
			port = 6379
		default:
			port = 3306
		}
	}
	return net.JoinHostPort(inst.Address, strconv.Itoa(port))
}

func (g *Gateway) handleGatewayConn(client net.Conn, conn *gatewayConn) {
	if conn.client != nil {
		client = conn.client
	}
	defer conn.releasePostgresCancel()
	defer client.Close()
	defer conn.upstream.Close()

	g.logger.Info("db gateway connection started",
		"protocol", conn.protocol, "account", conn.accountName,
		"client", client.RemoteAddr().String(), "upstream", conn.upstreamAddr)

	// 查找操作者用户名
	var authUser string
	if g.db != nil {
		var u model.User
		if err := g.db.First(&u, "id = ?", conn.userID).Error; err == nil {
			authUser = u.Username
		}
	}

	auditSession := &model.AuditSession{
		UserID:      conn.userID,
		Username:    authUser,
		Protocol:    conn.protocol,
		TargetName:  conn.instanceName,
		AccountName: conn.accountUser,
		ClientIP:    "",
		StartedAt:   time.Now().UTC(),
		State:       "started",
	}
	if conn.userSessionID != "" {
		auditSession.UserSessionID = conn.userSessionID
	}
	auditSession.BeforeCreate(nil)
	auditSession.ReplayDir = filepath.Join(g.replayDir, "db", auditSession.ID)
	if g.auditRequired && g.audit == nil {
		g.logger.Warn("db gateway audit writer unavailable")
		g.writeAuditUnavailableResponse(client, conn.protocol)
		return
	}
	if g.audit != nil {
		if err := g.audit.CreateAuditSession(auditSession); err != nil {
			g.logger.Warn("db gateway audit session creation failed", "error", err)
			g.writeAuditUnavailableResponse(client, conn.protocol)
			return
		}
		defer g.audit.EndAuditSession(auditSession.ID)
	}

	recorder, recErr := g.newRecorder(conn, auditSession.ID, func(error) {
		_ = client.Close()
		_ = conn.upstream.Close()
	})
	if recErr != nil {
		g.logger.Warn("db gateway recorder init failed", "error", recErr)
		g.writeAuditUnavailableResponse(client, conn.protocol)
		return
	}
	defer recorder.Close()

	unregisterOnline := g.onlineSessions.Register(online.Session{
		ID:             auditSession.ID,
		AuditSessionID: auditSession.ID,
		ResourceType:   model.ResourceTypeDatabaseInstance,
		ResourceID:     conn.instanceID,
		AccountID:      conn.accountID,
		Instance:       conn.instanceName,
		Protocol:       conn.protocol,
		Account:        conn.accountUser,
		Operator:       authUser,
		StartedAt:      auditSession.StartedAt,
	}, func() {
		_ = client.Close()
		_ = conn.upstream.Close()
	})
	defer unregisterOnline()

	observer := newQueryObserver(conn.protocol, recorder)
	relayGatewayConnection(client, conn.upstream, observer)
}

func (connection *gatewayConn) releasePostgresCancel() {
	if connection == nil || connection.postgresCancelCleanup == nil {
		return
	}
	cleanup := connection.postgresCancelCleanup
	connection.postgresCancelCleanup = nil
	cleanup()
}

func (g *Gateway) writeAuditUnavailableResponse(client net.Conn, protocol string) {
	decision := newObserverFatalDecision(observerErrorAuditFailure, "database gateway audit unavailable")
	response := newQueryObserver(protocol, nil).ErrorResponse(*decision)
	if len(response) > 0 {
		if _, err := client.Write(response); err != nil {
			g.logger.Warn("db gateway failed to write audit unavailable response", "error", err)
		}
	}
}
