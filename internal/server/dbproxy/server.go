package dbproxy

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
	"jianmen/internal/util"
)

type Gateway struct {
	cfg               config.DatabaseGatewayConfig
	store             databaseAccountResolver
	db                *gorm.DB
	replayDir         string
	logger            *slog.Logger
	permissionChecker permissionChecker
	superAdminIDs     map[string]bool
}

type databaseAccountResolver interface {
	DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error)
	AuthenticateDirect(ctx context.Context, username, password string) (model.User, error)
}

type permissionChecker interface {
	HasPermission(userID, action, resourceType, resourceID string) (bool, error)
}

func NewGateway(cfg config.DatabaseGatewayConfig, store databaseAccountResolver, replayDir string, logger *slog.Logger, db *gorm.DB, superAdminIDs map[string]bool) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	var checker permissionChecker
	if db != nil {
		checker = rbaccheck.NewChecker(db)
	}
	return &Gateway{cfg: cfg, store: store, db: db, replayDir: replayDir, logger: logger, permissionChecker: checker, superAdminIDs: superAdminIDs}
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
	protocol     string
	accountID    string
	accountName  string
	upstream     net.Conn
	upstreamAddr string
	userID       string
}

func (g *Gateway) handleConn(ctx context.Context, client net.Conn) {
	defer client.Close()

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
			conn = g.handlePG(ctx, client)
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

	recorder, _ := g.newRecorder(conn)
	if recorder != nil {
		defer recorder.Close()
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
// 1. Extract username from PG StartupMessage
// 2. Request cleartext password from client (bastion user auth)
// 3. Authenticate via StaticStore.AuthenticateDirect
// 4. RBAC check
// 5. Check account disabled/expiry
// 6. Connect to upstream and relay auth with UpstreamPassword
func (g *Gateway) handlePG(ctx context.Context, client net.Conn) *gatewayConn {
	buf := make([]byte, 8*1024)
	totalRead := 0

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
	password := string(pwdBuf[5 : 5+pwdLen])

	// Validate bastion user password
	var userID string
	if resolved.isCompact && resolved.user != nil {
		// 通过 user session 验证堡垒机用户密码
		if err := g.validateUserPassword(resolved.user, []byte(password)); err != nil {
			g.logger.Warn("db gateway auth failed", "user", resolved.rawName, "error", err)
			return nil
		}
		userID = resolved.user.ID
	} else {
		// 回退到 AuthenticateDirect (非 compact username 路径)
		user, err := g.store.AuthenticateDirect(ctx, resolved.rawName, password)
		if err != nil {
			g.logger.Warn("db gateway auth failed", "user", resolved.rawName, "error", err)
			return nil
		}
		userID = user.ID
	}

	// RBAC check
	resourceID := dbAccountResourceID(acct)
	if err := g.authorizeConnect(userID, resolved.rawName, resourceID); err != nil {
		g.logger.Warn("db gateway rbac denied", "user", userID, "resource", resourceID, "error", err)
		return nil
	}

	// Check account disabled and expiry
	if acct.Disabled {
		g.logger.Warn("db gateway account disabled", "account", resolved.rawName)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("db gateway account expired", "account", resolved.rawName, "expires_at", acct.ExpiresAt)
		return nil
	}
	if acct.Instance.Disabled {
		g.logger.Warn("db gateway instance disabled", "account", resolved.rawName, "instance", acct.InstanceID)
		return nil
	}

	// Connect to upstream
	upstream, err := net.DialTimeout("tcp", acct.Instance.Address, 10*time.Second)
	if err != nil {
		g.logger.Warn("db gateway upstream connect failed", "upstream", acct.Instance.Address, "error", err)
		return nil
	}

	// Forward a new StartupMessage to upstream with UpstreamUsername
	upUsername := acct.UpstreamUsername
	var sb strings.Builder
	sb.WriteString("user")
	sb.WriteByte(0)
	sb.WriteString(upUsername)
	sb.WriteByte(0)

	startupPayload := sb.String()
	startupLen := 4 + 4 + len(startupPayload) + 1 // length field + protocol + params + trailing \0
	startupMsg := make([]byte, startupLen)
	binary.BigEndian.PutUint32(startupMsg[0:4], uint32(startupLen))
	binary.BigEndian.PutUint32(startupMsg[4:8], 196608) // PG 3.0
	copy(startupMsg[8:], startupPayload)
	startupMsg[startupLen-1] = 0

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

		// Forward the server message to client
		if _, err := client.Write(respBuf[:nr]); err != nil {
			upstream.Close()
			return nil
		}

		if nr >= 6 && respBuf[0] == 'R' {
			authType := binary.BigEndian.Uint32(respBuf[5:9])
			if authType == 0 {
				// AuthenticationOk -- break out of auth loop
				break
			}

			if authType == 3 {
				// CleartextPassword: send upstream password back
				plainPwd := acct.UpstreamPassword.GetPlaintext()
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
				// MD5Password: for v1, try sending cleartext anyway
				plainPwd := acct.UpstreamPassword.GetPlaintext()
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
				continue
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
		upstream: upstream, upstreamAddr: acct.Instance.Address, userID: userID,
	}
}

// NOTE: Bastion user authentication via AuthenticateDirect is deferred for MySQL v1.
// The PG path validates bastion credentials via cleartext password challenge before RBAC.
// MySQL protocol does not support cleartext password, and implementing a full
// mysql_native_password challenge-response for bastion auth requires storing
// bastion user passwords or implementing AuthSwitchRequest. This is planned for v2.
// For now, the RBAC check uses the account's uniqueName as userID, which means
// any client that knows the compact username can attempt a connection.
// Access control relies on: (1) obscure compact username, (2) account disabled/expiry checks.
//
// handleMySQL implements MySQL proxy authentication:
// 1. Read client's initial login packet (already buffered after TCP connect)
// 2. Extract username using MySQLLoginParser
// 3. Look up DatabaseAccount by unique name
// 4. RBAC check
// 5. Check account disabled/expiry
// 6. Connect to upstream MySQL
// 7. Read upstream handshake, parse with ParseMySQLHandshake
// 8. Forward handshake to client
// 9. Read client's auth response
// 10. Build upstream login with upstream credentials (mysql_native_password)
// 11. Send to upstream, read auth result
// 12. Check auth OK (0x00) or ERR (0xff), forward result to client
// 13. Return gatewayConn for data relay
func (g *Gateway) handleMySQL(ctx context.Context, client net.Conn) *gatewayConn {
	// Read initial login packet from client
	clientLoginPkt, err := readMySQLPacket(client)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read initial packet", "error", err)
		return nil
	}

	// Parse username
	parser := &MySQLLoginParser{}
	fullPacket := make([]byte, 4+len(clientLoginPkt.payload))
	fullPacket[0] = byte(len(clientLoginPkt.payload))
	fullPacket[1] = byte(len(clientLoginPkt.payload) >> 8)
	fullPacket[2] = byte(len(clientLoginPkt.payload) >> 16)
	fullPacket[3] = clientLoginPkt.seq
	copy(fullPacket[4:], clientLoginPkt.payload)
	obs, _, err := parser.Observe(fullPacket)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse login", "error", err)
		return nil
	}
	if obs.User == "" {
		g.logger.Warn("mysql gateway empty username in login")
		return nil
	}

	resolved, err := g.resolveAccount(obs.User)
	if err != nil {
		g.logger.Warn("mysql gateway account resolution failed", "username", obs.User, "error", err)
		return nil
	}
	acct := resolved.account

	// RBAC check - 使用解析后的用户ID（compact）或 uniqueName（非 compact v1 路径）
	var rbacUserID string
	if resolved.isCompact && resolved.user != nil {
		rbacUserID = resolved.user.ID
	} else {
		// MySQL v1 回退：compact username 未识别时使用 unique name 作为 userID
		rbacUserID = resolved.rawName
	}
	resourceID := dbAccountResourceID(acct)
	if err := g.authorizeConnect(rbacUserID, resolved.rawName, resourceID); err != nil {
		g.logger.Warn("mysql gateway rbac denied", "resource", resourceID, "error", err)
		return nil
	}

	// Check account disabled and expiry
	if acct.Disabled {
		g.logger.Warn("mysql gateway account disabled", "account", resolved.rawName)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("mysql gateway account expired", "account", resolved.rawName, "expires_at", acct.ExpiresAt)
		return nil
	}

	// Connect to upstream
	upstream, err := net.DialTimeout("tcp", acct.Instance.Address, 10*time.Second)
	if err != nil {
		g.logger.Warn("mysql gateway upstream connect failed", "upstream", acct.Instance.Address, "error", err)
		return nil
	}

	// Read upstream handshake
	hsPkt, err := readMySQLPacket(upstream)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read upstream handshake", "error", err)
		upstream.Close()
		return nil
	}
	hs, err := ParseMySQLHandshake(hsPkt.payload)
	if err != nil {
		g.logger.Warn("mysql gateway failed to parse handshake", "error", err)
		upstream.Close()
		return nil
	}

	// Forward handshake to client
	if _, err := client.Write(hsPkt.raw); err != nil {
		upstream.Close()
		return nil
	}

	// Read client's auth response
	_, err = readMySQLPacket(client)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read client auth", "error", err)
		upstream.Close()
		return nil
	}

	// Build upstream login with upstream credentials
	upstreamLogin := BuildMySQLUpstreamLogin(hs, acct.UpstreamUsername, acct.UpstreamPassword.GetPlaintext(), hs.AuthPluginName)
	if _, err := upstream.Write(upstreamLogin); err != nil {
		g.logger.Warn("mysql gateway failed to send upstream login", "error", err)
		upstream.Close()
		return nil
	}

	// Read upstream auth result
	authPkt, err := readMySQLPacket(upstream)
	if err != nil {
		g.logger.Warn("mysql gateway failed to read upstream auth result", "error", err)
		upstream.Close()
		return nil
	}

	// Check auth result
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0xff {
		errMsg := ParseMySQLErrorMessage(authPkt.payload)
		g.logger.Warn("mysql gateway upstream auth failed", "error", errMsg)
		client.Write(authPkt.raw)
		upstream.Close()
		return nil
	}

	// Handle AuthSwitchRequest (0xfe) — MySQL 8.0 可能要求切换 auth plugin
	if len(authPkt.payload) > 1 && authPkt.payload[0] == 0xfe {
		switched, err := g.handleMySQLAuthSwitch(upstream, acct, hs, authPkt.payload[1:])
		if err != nil {
			g.logger.Warn("mysql gateway auth switch failed", "error", err)
			upstream.Close()
			return nil
		}
		authPkt = switched
	}

	// Handle caching_sha2_password full auth: 0x01 (more data) + 0x03 (fast auth success)
	if len(authPkt.payload) > 0 && authPkt.payload[0] == 0x01 {
		extraPkt, err := readMySQLPacket(upstream)
		if err != nil {
			g.logger.Warn("mysql gateway failed to read auth extra", "error", err)
			upstream.Close()
			return nil
		}
		if len(extraPkt.payload) > 0 && extraPkt.payload[0] == 0x03 {
			// fast auth success — forward to client
			if _, err := client.Write(authPkt.raw); err != nil {
				upstream.Close()
				return nil
			}
			if _, err := client.Write(extraPkt.raw); err != nil {
				upstream.Close()
				return nil
			}
			return &gatewayConn{
				protocol:     "mysql",
				accountID:    acct.ID,
				accountName:  resolved.rawName,
				upstream:     upstream,
				upstreamAddr: acct.Instance.Address,
				userID:       rbacUserID,
			}
		}
		// 0x04 = full auth with public key — 暂不支持，回传错误
		if len(extraPkt.payload) > 0 && extraPkt.payload[0] == 0x04 {
			g.logger.Warn("mysql gateway caching_sha2_password full auth (0x04) not supported, need SSL or native_password")
			client.Write(authPkt.raw)
			client.Write(extraPkt.raw)
			upstream.Close()
			return nil
		}
	}

	// Forward OK to client
	if _, err := client.Write(authPkt.raw); err != nil {
		upstream.Close()
		return nil
	}

	return &gatewayConn{
		protocol:     "mysql",
		accountID:    acct.ID,
		accountName:  resolved.rawName,
		upstream:     upstream,
		upstreamAddr: acct.Instance.Address,
		userID:       rbacUserID,
	}
}

// handleMySQLAuthSwitch 处理 MySQL AuthSwitchRequest (0xfe)
// payload 是 0xfe 之后的部分：plugin name (null-terminated) + auth data
func (g *Gateway) handleMySQLAuthSwitch(upstream net.Conn, acct *model.DatabaseAccount, hs *MySQLHandshake, payload []byte) (*mysqlPacket, error) {
	// 解析新 auth plugin name
	nullPos := 0
	for nullPos < len(payload) && payload[nullPos] != 0 {
		nullPos++
	}
	newPlugin := string(payload[:nullPos])
	authData := payload[nullPos+1:]

	// 更新 salt 用于新 plugin
	if len(authData) > 0 {
		hs.AuthData = authData
	}

	// 构建新 auth plugin 的响应
	switchLogin := BuildMySQLUpstreamLogin(hs, acct.UpstreamUsername, acct.UpstreamPassword.GetPlaintext(), newPlugin)
	if _, err := upstream.Write(switchLogin); err != nil {
		return nil, fmt.Errorf("write auth switch: %w", err)
	}

	return readMySQLPacket(upstream)
}

func (g *Gateway) authorizeConnect(userID, uniqueName, resourceID string) error {
	if g.permissionChecker == nil || g.superAdminIDs[userID] {
		return nil
	}
	allowed, err := g.permissionChecker.HasPermission(userID, rbaccheck.ActionDBConnect, model.ResourceTypeDatabaseAccount, resourceID)
	if err != nil {
		return fmt.Errorf("rbac check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("user %q not permitted to connect to %s", userID, resourceID)
	}
	return nil
}

// resolvedDBAccount 解析连接用户名后的数据库账号及关联用户信息
type resolvedDBAccount struct {
	account  *model.DatabaseAccount
	user     *model.User   // compact username 认证后的堡垒机用户
	isCompact bool          // 是否通过 compact username 解析
	rawName  string        // 原始用户名（用于日志）
}

// resolveAccount 解析连接用户名：优先尝试 compact username (D/H前缀10位)，失败则回退到 unique_name 查找
func (g *Gateway) resolveAccount(rawUsername string) (*resolvedDBAccount, error) {
	rawUsername = strings.TrimSpace(rawUsername)
	if rawUsername == "" {
		return nil, errors.New("empty username")
	}

	// 尝试 compact username 解析
	if len(rawUsername) == 10 {
		prefix, _, _, err := util.ParseCompactUsername(rawUsername)
		if err == nil && prefix == util.PrefixDatabase {
			return g.resolveCompactAccount(rawUsername)
		}
		// 也支持 H 前缀（SSH 主机账号的 compact username 同样格式可用于 DB）
		if err == nil && prefix == util.PrefixHost {
			return g.resolveCompactAccount(rawUsername)
		}
	}

	// 回退到 unique_name 查找
	acct, err := g.store.DatabaseAccountByUniqueName(rawUsername)
	if err != nil {
		return nil, fmt.Errorf("account not found by unique_name %q: %w", rawUsername, err)
	}
	return &resolvedDBAccount{
		account:  acct,
		isCompact: false,
		rawName:  rawUsername,
	}, nil
}

// resolveCompactAccount 从 compact username 解析并查找数据库账号和用户会话
func (g *Gateway) resolveCompactAccount(username string) (*resolvedDBAccount, error) {
	resourceID := username[1:5]
	sessionID := username[5:10]

	// 查找数据库账号（按 resource_id）
	var acct model.DatabaseAccount
	if err := g.db.Preload("Instance").Where("resource_id = ?", resourceID).First(&acct).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("database account not found for resource_id %q", resourceID)
		}
		return nil, fmt.Errorf("lookup database account: %w", err)
	}

	// 查找用户会话
	var sess model.UserSession
	if err := g.db.Where("session_id = ? AND status = ?", sessionID, "active").First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("invalid session %q", sessionID)
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	// 检查会话过期
	if sess.ExpiresAt != nil && time.Now().UTC().After(*sess.ExpiresAt) {
		g.db.Model(&sess).Update("status", "expired")
		return nil, fmt.Errorf("session %q expired", sessionID)
	}

	// 查找用户
	var user model.User
	if err := g.db.Where("id = ? AND status = ?", sess.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user for session %q is disabled or not found", sessionID)
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}

	return &resolvedDBAccount{
		account:  &acct,
		user:     &user,
		isCompact: true,
		rawName:  username,
	}, nil
}

// validateUserPassword 验证堡垒机用户密码（仅 compact username 路径使用）
func (g *Gateway) validateUserPassword(user *model.User, password []byte) error {
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), password); err != nil {
		return errors.New("authentication failed")
	}
	return nil
}

// dbAccountResourceID 获取数据库账号的 RBAC 资源ID
func dbAccountResourceID(acct *model.DatabaseAccount) string {
	return rbaccheck.DatabaseAccountResourceID(acct.UniqueName)
}

func copyClientToUpstream(client net.Conn, upstream net.Conn, observer queryObserver) {
	buf := make([]byte, 32*1024)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if decision := observer.ObserveClientBytes(data); decision != nil && !decision.Allowed {
				return
			}
			if _, werr := upstream.Write(data); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func copyUpstreamToClient(client net.Conn, upstream net.Conn, observer queryObserver) {
	buf := make([]byte, 32*1024)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			observer.ObserveServerBytes(data)
			if _, werr := client.Write(data); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

type connectionRecorder struct {
	mu       sync.Mutex
	id       string
	protocol string
	metaPath string
	meta     DBConnectionMeta
	file     *os.File
	seq      int64
}

func (g *Gateway) newRecorder(conn *gatewayConn) (*connectionRecorder, error) {
	id := model.NewID()
	dir := filepath.Join(g.replayDir, "db", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	meta := DBConnectionMeta{
		ID:           id,
		Name:         conn.accountName,
		Protocol:     conn.protocol,
		ClientAddr:   "",
		UpstreamAddr: conn.upstreamAddr,
		StartedAt:    startedAt.Format(time.RFC3339Nano),
	}
	file, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	recorder := &connectionRecorder{
		id:       id,
		protocol: conn.protocol,
		metaPath: filepath.Join(dir, "meta.json"),
		meta:     meta,
		file:     file,
	}
	if err := recorder.writeMetaLocked(); err != nil {
		file.Close()
		return nil, err
	}
	return recorder, nil
}

func (r *connectionRecorder) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	if r == nil || strings.TrimSpace(sql) == "" {
		return queryRecord{}, allowQuery()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	startedAt := time.Now().UTC()
	queryKind := classifyQueryKind(sql)
	record := queryRecord{
		seq:       r.seq,
		protocol:  r.protocol,
		sql:       sql,
		queryKind: queryKind,
		detail:    detail,
		startedAt: startedAt,
	}
	decision := allowQuery()
	startDetail := mergeDetails(detail, map[string]any{"query_kind": queryKind})
	r.writeQueryEventLocked(DBQueryEvent{
		Type:         queryEventTypeStarted,
		ConnectionID: r.id,
		Seq:          record.seq,
		Protocol:     r.protocol,
		SQL:          sql,
		QueryKind:    queryKind,
		Detail:       startDetail,
		StartedAt:    startedAt.UnixMilli(),
		Status:       queryStatusUnknown,
	})
	if !decision.Allowed {
		r.writeFinishLocked(record, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: decision.ErrorMessage,
			Detail:       decision.Detail,
		})
	}
	return record, decision
}

func (r *connectionRecorder) FinishQuery(record queryRecord, finish queryFinish) {
	if r == nil || record.seq == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeFinishLocked(record, finish)
}

func (r *connectionRecorder) writeFinishLocked(record queryRecord, finish queryFinish) {
	if finish.Status == "" {
		finish.Status = queryStatusUnknown
	}
	completedAt := time.Now().UTC()
	r.writeQueryEventLocked(DBQueryEvent{
		Type:         queryEventTypeFinished,
		ConnectionID: r.id,
		Seq:          record.seq,
		Protocol:     record.protocol,
		SQL:          record.sql,
		QueryKind:    record.queryKind,
		Detail:       mergeDetails(record.detail, finish.Detail),
		StartedAt:    record.startedAt.UnixMilli(),
		CompletedAt:  completedAt.UnixMilli(),
		DurationMs:   completedAt.Sub(record.startedAt).Milliseconds(),
		Status:       finish.Status,
		ErrorCode:    finish.ErrorCode,
		ErrorMessage: finish.ErrorMessage,
		RowsAffected: finish.RowsAffected,
		Rows:         finish.Rows,
	})
}

func (r *connectionRecorder) writeQueryEventLocked(event DBQueryEvent) {
	if r.file == nil {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = r.file.Write(append(raw, '\n'))
}

func (r *connectionRecorder) writeMetaLocked() error {
	if r.metaPath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(r.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.metaPath, raw, 0o644)
}

func (r *connectionRecorder) RecordQuery(sql string, detail map[string]any) {
	record, decision := r.StartQuery(sql, detail)
	if decision.Allowed {
		r.FinishQuery(record, queryFinish{Status: queryStatusUnknown})
	}
}

func (r *connectionRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

// mysqlPacket represents a parsed MySQL packet
type mysqlPacket struct {
	raw     []byte
	payload []byte
	seq     byte
}

// readMySQLPacket reads a single MySQL packet (4-byte header + payload) from conn
func readMySQLPacket(conn net.Conn) (*mysqlPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	payloadLen := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if payloadLen == 0 || payloadLen > 128*1024*1024 {
		return nil, fmt.Errorf("invalid mysql packet length %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	raw := make([]byte, 4+payloadLen)
	copy(raw, header)
	copy(raw[4:], payload)
	return &mysqlPacket{raw: raw, payload: payload, seq: header[3]}, nil
}

// BuildMySQLUpstreamLogin builds a MySQL login packet for the upstream server.
// Exported for use by test connection in admin package.
func BuildMySQLUpstreamLogin(hs *MySQLHandshake, username, password, authPlugin string) []byte {
	var authResp []byte
	switch authPlugin {
	case "mysql_native_password":
		authResp = BuildMySQLNativePassword(password, hs.AuthData)
	case "caching_sha2_password":
		authResp = BuildMySQLCachingSha2Password(password, hs.AuthData)
	}

	capFlags := uint32(mysqlClientProtocol41 | mysqlClientSecureConnection | mysqlClientPluginAuth)

	var payload []byte
	capBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(capBytes, capFlags)
	payload = append(payload, capBytes...)
	maxPkt := make([]byte, 4)
	binary.LittleEndian.PutUint32(maxPkt, 16777215)
	payload = append(payload, maxPkt...)
	payload = append(payload, hs.CharacterSet)
	reserved := make([]byte, 23)
	payload = append(payload, reserved...)
	payload = append(payload, []byte(username)...)
	payload = append(payload, 0)
	payload = append(payload, byte(len(authResp)))
	payload = append(payload, authResp...)
	payload = append(payload, 0) // empty database
	payload = append(payload, []byte(authPlugin)...)
	payload = append(payload, 0)

	pkt := make([]byte, 4+len(payload))
	pkt[0] = byte(len(payload))
	pkt[1] = byte(len(payload) >> 8)
	pkt[2] = byte(len(payload) >> 16)
	pkt[3] = 1
	copy(pkt[4:], payload)
	return pkt
}
