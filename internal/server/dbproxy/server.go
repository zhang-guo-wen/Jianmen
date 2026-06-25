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

	"gorm.io/gorm"
	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
)

type Gateway struct {
	cfg               config.DatabaseGatewayConfig
	store             *access.StaticStore
	db                *gorm.DB
	replayDir         string
	logger            *slog.Logger
	permissionChecker permissionChecker
}

type permissionChecker interface {
	HasPermission(userID, action, resourceType, resourceID string) (bool, error)
}

func NewGateway(cfg config.DatabaseGatewayConfig, store *access.StaticStore, replayDir string, logger *slog.Logger, db *gorm.DB) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	var checker permissionChecker
	if db != nil {
		checker = rbaccheck.NewChecker(db)
	}
	return &Gateway{cfg: cfg, store: store, db: db, replayDir: replayDir, logger: logger, permissionChecker: checker}
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

	firstByte := make([]byte, 1)
	if _, err := io.ReadFull(client, firstByte); err != nil {
		g.logger.Warn("db gateway failed to read protocol byte", "client", client.RemoteAddr().String(), "error", err)
		return
	}

	var conn *gatewayConn
	switch {
	case firstByte[0] == 0x00:
		conn = g.handlePG(ctx, client)
	case firstByte[0] == 0x4a:
		conn = g.handleMySQL(ctx, client)
	default:
		g.logger.Warn("db gateway unsupported protocol", "first_byte", firstByte[0])
		return
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

	uniqueName := strings.TrimSpace(username)
	acct, err := g.store.DatabaseAccountByUniqueName(uniqueName)
	if err != nil {
		g.logger.Warn("db gateway account not found", "unique_name", uniqueName, "error", err)
		return nil
	}

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

	// Validate bastion user password via StaticStore
	user, err := g.store.AuthenticateDirect(ctx, uniqueName, password)
	if err != nil {
		g.logger.Warn("db gateway auth failed", "user", uniqueName, "error", err)
		return nil
	}

	// RBAC check
	resourceID := rbaccheck.DatabaseAccountResourceID(uniqueName)
	if err := g.authorizeConnect(user.ID, uniqueName, resourceID); err != nil {
		g.logger.Warn("db gateway rbac denied", "user", user.ID, "resource", resourceID, "error", err)
		return nil
	}

	// Check account disabled and expiry
	if acct.Disabled {
		g.logger.Warn("db gateway account disabled", "account", uniqueName)
		return nil
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		g.logger.Warn("db gateway account expired", "account", uniqueName, "expires_at", acct.ExpiresAt)
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
				pwdMsg := make([]byte, 0, 5+len(acct.UpstreamPassword)+1)
				pwdMsg = append(pwdMsg, 'p')
				pwdLenBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(pwdLenBytes, uint32(4+len(acct.UpstreamPassword)+1))
				pwdMsg = append(pwdMsg, pwdLenBytes...)
				pwdMsg = append(pwdMsg, []byte(acct.UpstreamPassword)...)
				pwdMsg = append(pwdMsg, 0)
				if _, err := upstream.Write(pwdMsg); err != nil {
					upstream.Close()
					return nil
				}
			} else if authType == 5 {
				// MD5Password: for v1, try sending cleartext anyway
				pwdMsg := make([]byte, 0, 5+len(acct.UpstreamPassword)+1)
				pwdMsg = append(pwdMsg, 'p')
				pwdLenBytes := make([]byte, 4)
				binary.BigEndian.PutUint32(pwdLenBytes, uint32(4+len(acct.UpstreamPassword)+1))
				pwdMsg = append(pwdMsg, pwdLenBytes...)
				pwdMsg = append(pwdMsg, []byte(acct.UpstreamPassword)...)
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
		protocol: "postgres", accountID: acct.ID, accountName: uniqueName,
		upstream: upstream, upstreamAddr: acct.Instance.Address, userID: user.ID,
	}
}

// handleMySQL is a v1 stub. Returns nil with a warning log.
func (g *Gateway) handleMySQL(ctx context.Context, client net.Conn) *gatewayConn {
	// MySQL v1 is deferred. The first byte (0x4a) was already consumed by handleConn.
	// A full implementation would need to: connect upstream, relay handshake,
	// extract username from client login, RBAC check, then proxy.
	g.logger.Warn("MySQL proxy not yet implemented in gateway v1 - use PG protocol")
	return nil
}

func (g *Gateway) authorizeConnect(userID, uniqueName, resourceID string) error {
	if g.permissionChecker == nil {
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
