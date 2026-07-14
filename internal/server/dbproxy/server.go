package dbproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
)

type Gateway struct {
	cfg               config.DatabaseGatewayConfig
	store             databaseAccountResolver
	db                *gorm.DB
	replayDir         string
	logger            *slog.Logger
	permissionChecker permissionChecker
	resourceChecker   resourceGrantChecker
	superAdminIDs     map[string]bool
	audit             auditWriter
}

type databaseAccountResolver interface {
	// 仅保留以满足 store.Store 接口；网关不再使用
}

type auditWriter interface {
	CreateAuditSession(session *model.AuditSession) error
	EndAuditSession(id string) error
	CreateAuditDBQuery(query *model.AuditDBQuery) error
}

type permissionChecker interface {
	HasPermission(userID, action, resourceType, resourceID string) (bool, error)
}

type resourceGrantChecker interface {
	HasGrant(userID, resourceType, resourceID string) (bool, error)
}

func NewGateway(cfg config.DatabaseGatewayConfig, store databaseAccountResolver, replayDir string, logger *slog.Logger, db *gorm.DB, superAdminIDs map[string]bool, auditStore auditWriter) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	var checker permissionChecker
	var resourceChecker resourceGrantChecker
	if db != nil {
		checker = rbaccheck.NewChecker(db)
		resourceChecker = rbaccheck.NewResourceGrantChecker(db)
	}
	return &Gateway{cfg: cfg, store: store, db: db, replayDir: replayDir, logger: logger, permissionChecker: checker, resourceChecker: resourceChecker, superAdminIDs: superAdminIDs, audit: auditStore}
}

func (g *Gateway) Enabled() bool {
	return g.cfg.Enabled
}

func (g *Gateway) ListenAndServe(ctx context.Context) error {
	if !g.cfg.Enabled {
		return nil
	}
	listener, err := net.Listen("tcp", g.cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer listener.Close()
	g.logger.Info("database gateway listening", "addr", g.cfg.ListenAddr)

	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "closed") {
				wg.Wait()
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.handleConn(ctx, conn)
		}()
	}
}

type gatewayConn struct {
	protocol      string
	accountID     string
	accountName   string
	upstream      net.Conn
	upstreamAddr  string
	userID        string
	accountUser   string // 上游数据库登录名
	instanceName  string // 数据库实例名称
	userSessionID string
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

func (g *Gateway) handleConn(ctx context.Context, client net.Conn) {
	defer client.Close()

	// 协议检测：MySQL 客户端会等待服务器先发握手包，不主动发数据，
	// 因此用短超时区分协议（PG/Redis 客户端会立刻发数据）。
	// 原来 3s 导致每个 MySQL 连接固定 3s 延迟，DBeaver 打开多连接时延迟叠加。
	client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	firstByte := make([]byte, 1)
	_, err := io.ReadFull(client, firstByte)
	client.SetReadDeadline(time.Time{})

	var conn *gatewayConn
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			conn = g.handleMySQL(ctx, client)
		} else {
			g.logger.Warn("db gateway protocol detection read error", "error", err)
			return
		}
	} else {
		switch {
		case firstByte[0] == 0x00:
			conn = g.handlePG(ctx, client, firstByte[0])
		case firstByte[0] == '*':
			conn = g.handleRedis(ctx, client, firstByte[0])
		default:
			g.logger.Warn("db gateway unsupported protocol", "first_byte", firstByte[0])
			return
		}
	}
	if conn == nil {
		return
	}
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
		ReplayDir:   filepath.Join(g.replayDir, "db", model.NewID()),
	}
	if conn.userSessionID != "" {
		auditSession.UserSessionID = conn.userSessionID
	}
	auditSession.BeforeCreate(nil)
	if g.audit != nil {
		g.audit.CreateAuditSession(auditSession)
	}

	recorder, recErr := g.newRecorder(conn, auditSession.ID)
	if recErr != nil {
		g.logger.Warn("db gateway recorder init failed, audit db queries may be incomplete", "error", recErr)
	}
	if recorder != nil {
		defer func() {
			recorder.Close()
			if g.audit != nil {
				g.audit.EndAuditSession(auditSession.ID)
			}
		}()
	}

	observer := newQueryObserver(conn.protocol, recorder)
	done := make(chan struct{}, 2)
	go func() {
		copyClientToUpstream(client, conn.upstream, observer)
		done <- struct{}{}
	}()
	go func() {
		copyUpstreamToClient(client, conn.upstream, observer)
		done <- struct{}{}
	}()
	<-done
}

// handlePG implements two-layer PostgreSQL authentication:
// 1. Extract compact username from PG StartupMessage
// 2. Request cleartext password from client (bastion user auth)
// 3. Validate via user session password
// 4. RBAC check
// 5. Check account disabled/expiry
// 6. Connect to upstream and relay auth with Password
func (g *Gateway) handlePG(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn {
	buf := make([]byte, 8*1024)
	// 协议检测已读了第1字节，放回缓冲区头部
	buf[0] = firstByte
	totalRead := 1

	// Read StartupMessage header (4 bytes len + 4 bytes protocol)
	for totalRead < 8 {
		n, err := client.Read(buf[totalRead:])
		if err != nil {
			return nil
		}
		totalRead += n
	}

	msgLen := int(binary.BigEndian.Uint32(buf[0:4]))
	protocol := int(binary.BigEndian.Uint32(buf[4:8]))

	// Handle SSLRequest (protocol 80877103)
	if protocol == 80877103 {
		// Refuse SSL
		if _, err := client.Write([]byte{'N'}); err != nil {
			return nil
		}
		totalRead = 0
		for totalRead < 8 {
			n, err := client.Read(buf[totalRead:])
			if err != nil {
				return nil
			}
			totalRead += n
		}
		msgLen = int(binary.BigEndian.Uint32(buf[0:4]))
	}

	// Read the rest of the StartupMessage
	for totalRead < msgLen && totalRead < len(buf) {
		n, err := client.Read(buf[totalRead:])
		if err != nil {
			return nil
		}
		totalRead += n
	}

	// Parse StartupMessage key-value pairs
	username := ""
	database := ""
	pos := 8
	for pos < msgLen-1 {
		keyEnd := pos
		for keyEnd < msgLen && buf[keyEnd] != 0 {
			keyEnd++
		}
		if keyEnd >= msgLen {
			break
		}
		valEnd := keyEnd + 1
		for valEnd < msgLen && buf[valEnd] != 0 {
			valEnd++
		}
		if valEnd >= msgLen {
			break
		}
		key := string(buf[pos:keyEnd])
		value := string(buf[keyEnd+1 : valEnd])
		if key == "user" {
			username = value
		}
		if key == "database" {
			database = value
		}
		pos = valEnd + 1
	}
	if username == "" {
		return nil
	}

	resolved, err := g.resolveAccount(username)
	if err != nil {
		g.logger.Warn("db gateway account resolution failed", "username", username, "error", err)
		return nil
	}
	acct := resolved.account

	// Request cleartext password from client
	// PG AuthenticationCleartextPassword: 'R' type, int32(8) len, int32(3) auth type
	authReq := []byte{'R', 0, 0, 0, 8, 0, 0, 0, 3}
	if _, err := client.Write(authReq); err != nil {
		return nil
	}

	// Read password response: type 'p', int32(len), password (null-terminated)
	pwdBuf := make([]byte, 1024)
	n, err := client.Read(pwdBuf)
	if err != nil || n < 6 {
		return nil
	}
	pwdLen := int(binary.BigEndian.Uint32(pwdBuf[1:5])) - 4
	if pwdLen <= 0 {
		return nil
	}
	// PG 密码消息以 \x00 结尾，需截断
	password := strings.TrimRight(string(pwdBuf[5:5+pwdLen]), "\x00")

	// 验证堡垒机用户密码
	if err := g.validateUserPassword(resolved.user, []byte(password)); err != nil {
		g.logger.Warn("db gateway auth failed", "user", resolved.rawName, "error", err)
		return nil
	}
	userID := resolved.user.ID

	// RBAC check
	resourceID := acct.ID
	if err := g.authorizeConnect(userID, resolved.rawName, resourceID); err != nil {
		g.logger.Warn("db gateway rbac denied", "user", userID, "resource", resourceID, "error", err)
		return nil
	}

	// Check account disabled and expiry
	if acct.Status == "disabled" {
		g.logger.Warn("db gateway account disabled", "account", resolved.rawName)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("db gateway account expired", "account", resolved.rawName, "expires_at", acct.ExpiresAt)
		return nil
	}
	if acct.Instance.Status == "disabled" {
		g.logger.Warn("db gateway instance disabled", "account", resolved.rawName, "instance", acct.InstanceID)
		return nil
	}

	// Connect to upstream
	upstream, err := net.DialTimeout("tcp", upstreamAddress(acct.Instance), 10*time.Second)
	if err != nil {
		g.logger.Warn("db gateway upstream connect failed", "upstream", upstreamAddress(acct.Instance), "error", err)
		return nil
	}

	// Forward a new StartupMessage to upstream with the upstream username and target database.
	startupMsg := BuildPostgresUpstreamStartupMessage(acct.Username, database)

	if _, err := upstream.Write(startupMsg); err != nil {
		upstream.Close()
		return nil
	}

	// PG auth relay: handle upstream's authentication challenge
	respBuf := make([]byte, 4096)
	for {
		nr, err := upstream.Read(respBuf)
		if err != nil || nr < 1 {
			upstream.Close()
			return nil
		}

		// Forward only messages the client should see. Upstream authentication
		// challenges are answered by the proxy using stored credentials; forwarding
		// them would make the client send an extra PasswordMessage that PostgreSQL
		// later rejects after authentication completes.
		if shouldForwardPostgresAuthMessage(respBuf[:nr]) {
			if _, err := client.Write(respBuf[:nr]); err != nil {
				upstream.Close()
				return nil
			}
		}

		if nr >= 6 && respBuf[0] == 'R' {
			authType := binary.BigEndian.Uint32(respBuf[5:9])
			if authType == 0 {
				// AuthenticationOk -- break out of auth loop
				break
			}

			if authType == 3 {
				// CleartextPassword: send upstream password back
				plainPwd := acct.Password.GetPlaintext()
				pwdMsg := make([]byte, 0, 5+len(plainPwd)+1)
				pwdMsg = append(pwdMsg, 'p')
				pwdLenBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(pwdLenBytes, uint32(4+len(plainPwd)+1))
				pwdMsg = append(pwdMsg, pwdLenBytes...)
				pwdMsg = append(pwdMsg, []byte(plainPwd)...)
				pwdMsg = append(pwdMsg, 0)
				if _, err := upstream.Write(pwdMsg); err != nil {
					upstream.Close()
					return nil
				}
			} else if authType == 5 {
				plainPwd := acct.Password.GetPlaintext()
				if nr < 13 {
					upstream.Close()
					return nil
				}
				md5Pwd := BuildPostgresPasswordResponse(authType, acct.Username, plainPwd, respBuf[9:13])
				pwdMsg := make([]byte, 0, 5+len(md5Pwd)+1)
				pwdMsg = append(pwdMsg, 'p')
				pwdLenBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(pwdLenBytes, uint32(4+len(md5Pwd)+1))
				pwdMsg = append(pwdMsg, pwdLenBytes...)
				pwdMsg = append(pwdMsg, []byte(md5Pwd)...)
				pwdMsg = append(pwdMsg, 0)
				if _, err := upstream.Write(pwdMsg); err != nil {
					upstream.Close()
					return nil
				}
				continue
			} else if authType == 10 {
				// SASL/SCRAM-SHA-256：用存储密码完成 SCRAM 交换
				if err := g.pgSCRAMExchange(upstream, acct.Username, acct.Password.GetPlaintext(), respBuf[:nr]); err != nil {
					g.logger.Warn("pg scram auth failed", "error", err)
					upstream.Close()
					return nil
				}
				// SCRAM 成功后发送 AuthenticationOk 给客户端
				okMsg := []byte{'R', 0, 0, 0, 8, 0, 0, 0, 0}
				if _, err := client.Write(okMsg); err != nil {
					upstream.Close()
					return nil
				}
				break
			}
		}

		// ErrorResponse from upstream -- auth failed
		if nr >= 1 && respBuf[0] == 'E' {
			upstream.Close()
			return nil
		}

		// ReadyForQuery -- auth succeeded
		if nr >= 1 && respBuf[0] == 'Z' {
			break
		}
	}

	return &gatewayConn{
		protocol: "postgres", accountID: acct.ID, accountName: resolved.rawName,
		upstream: upstream, upstreamAddr: upstreamAddress(acct.Instance), userID: userID,
		accountUser: acct.Username, instanceName: acct.Instance.Name,
		userSessionID: resolved.userSessionID,
	}
}
