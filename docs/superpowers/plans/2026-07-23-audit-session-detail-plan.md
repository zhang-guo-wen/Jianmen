# 审计会话 SessionID 与授权详情 — 实施计划

> **对执行代理的要求：** 推荐使用 superpowers:subagent-driven-development 或 superpowers:executing-plans 逐任务实施。步骤使用 checkbox (`- [ ]`) 跟踪。

**目标：** 在 SSH 审计、DB 审计和在线会话中增加独立可点击 SessionID 列，点击后弹窗展示 UserSession 授权详情（授权类型、授权用户、有效期、开始时间、备注、状态）。

**架构：** 后端新增按 5 位 SessionID 查询 UserSession 详情的 store 方法和 REST API，在线会话列表补充 SessionID 返回。前端新增可复用 `UserSessionDetailDialog.vue` 弹窗组件，`AuditView.vue` 三个列表统一增加独立 SessionID 列，删除操作用户旁内联显示。

**技术栈：** Go 1.23+ / GORM / SQLite (测试), Vue 3 Composition API / TypeScript / Element Plus / Vitest

## 全局约束

- 备注复用 `TemporaryAccount.Remark`，不修改 `UserSession` 表结构。
- `TemporaryAccount.SessionID` = `UserSession.SessionID`（5 位短 ID），通过此字段关联临时授权记录。
- 不修改登录审计、操作审计和 RDP 审计。
- 不修改 `AdminSession`，不建立 `AdminSession` → `UserSession` 新关联。
- 不把完整授权详情冗余写入列表响应。
- SSH/DB 审计现有 session_id 数据链路不变。
- 所有新增 UI 文案须同时补充中文 i18n key。
- 注释和提交使用中文。

---

### Task 1: 后端 Store — UserSession 授权详情查询

**文件：**
- 修改：`internal/model/user_session.go`（不修改 struct，在同一文件或新文件定义详情视图类型）
- 修改：`internal/store/dbstore_sessions.go`（新增查询方法）
- 新增（可选）：`internal/store/dbstore_sessions_test.go`（新增测试）或直接扩展现有测试文件

**接口：**
- 产出：`model.UserSessionAuthDetail` 结构体
- 产出：`(*DBStore).GetUserSessionAuthDetail(ctx context.Context, sessionID string) (model.UserSessionAuthDetail, error)`

#### 步骤 1: 定义 UserSessionAuthDetail 视图结构体

在 `internal/model/user_session.go` 末尾追加：

```go
// UserSessionAuthDetail 用户会话授权详情，用于审计弹窗展示。
type UserSessionAuthDetail struct {
	ID                string     `json:"id"`
	SessionID         string     `json:"session_id"`
	SessionType       string     `json:"session_type"`
	AuthorizationType string     `json:"authorization_type"`
	UserID            string     `json:"user_id"`
	Username          string     `json:"username"`
	AuthorizedBy      string     `json:"authorized_by,omitempty"`
	StartsAt          time.Time  `json:"starts_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	Remark            string     `json:"remark,omitempty"`
	Status            string     `json:"status"`
	EffectiveStatus   string     `json:"effective_status"`
}
```

#### 步骤 2: 在 store 层新增查询方法

在 `internal/store/dbstore_sessions.go` 末尾追加：

```go
// GetUserSessionAuthDetail 通过 5 位 session_id 查询用户会话授权详情。
// 对于临时会话，同时查询 TemporaryAccount 获取授权类型和备注。
func (s *DBStore) GetUserSessionAuthDetail(ctx context.Context, sessionID string) (model.UserSessionAuthDetail, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 5 {
		return model.UserSessionAuthDetail{}, fmt.Errorf("invalid session_id: %q", sessionID)
	}

	var sess model.UserSession
	if err := s.db.WithContext(ctx).Preload("User").
		Where("session_id = ?", sessionID).First(&sess).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.UserSessionAuthDetail{}, fmt.Errorf("user session %q: %w", sessionID, ErrNotFound)
		}
		return model.UserSessionAuthDetail{}, fmt.Errorf("find user session %q: %w", sessionID, err)
	}

	detail := model.UserSessionAuthDetail{
		ID:            sess.ID,
		SessionID:     sess.SessionID,
		SessionType:   sess.Type,
		UserID:        sess.UserID,
		Username:      sess.User.Username,
		AuthorizedBy:  sess.CreatedBy,
		StartsAt:      sess.CreatedAt,
		ExpiresAt:     sess.ExpiresAt,
		Status:        sess.Status,
	}

	// 计算授权类型
	detail.AuthorizationType = authorizationTypeFromUserSession(sess)

	// 对于临时会话，查询 TemporaryAccount 获取备注和更精确的授权类型
	if sess.Type == "temporary" {
		var ta model.TemporaryAccount
		// TemporaryAccount.SessionID 存储的是 UserSession.SessionID（5位短ID）
		err := s.db.WithContext(ctx).Where("session_id = ?", sess.SessionID).First(&ta).Error
		if err == nil {
			detail.Remark = ta.Remark
			detail.AuthorizationType = authorizationTypeFromTemporaryAccount(ta)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return model.UserSessionAuthDetail{}, fmt.Errorf("find temporary account: %w", err)
		}
		// TemporaryAccount 不存在时，保持基础信息，类型由 authorizationTypeFromUserSession 决定
	}

	// 计算有效状态
	detail.EffectiveStatus = computeEffectiveStatus(detail.Status, detail.ExpiresAt)

	return detail, nil
}

// authorizationTypeFromUserSession 根据 UserSession 类型返回基础授权类型。
func authorizationTypeFromUserSession(sess model.UserSession) string {
	if sess.Type == "permanent" {
		return "normal"
	}
	return "unknown" // 临时会话但未找到 TemporaryAccount 时使用
}

// authorizationTypeFromTemporaryAccount 根据 TemporaryAccount 类型返回授权类型。
func authorizationTypeFromTemporaryAccount(ta model.TemporaryAccount) string {
	switch ta.Type {
	case model.TemporaryAccountTypeAI:
		return "ai"
	case model.TemporaryAccountTypeUser:
		return "temporary"
	default:
		return "unknown"
	}
}

// computeEffectiveStatus 综合原始状态和有效期计算有效状态。
func computeEffectiveStatus(status string, expiresAt *time.Time) string {
	if status != "active" {
		return "disabled"
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return "expired"
	}
	return "active"
}
```

注意：`ErrNotFound` 需在 store 包中已定义。检查 `internal/store/store.go` 或 `dbstore.go`；若不存在，添加：

```go
var ErrNotFound = errors.New("record not found")
```

#### 步骤 3: 编写 store 层测试

在 `internal/store/dbstore_sessions_test.go` 中新增（若文件不存在则创建，参考 `auth_test.go` 的测试模式）：

```go
func TestGetUserSessionAuthDetail_Permanent(t *testing.T) {
	db := storage.Open(t.TempDir())
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "testuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us1", UserID: "u1", SessionSeq: 1, SessionID: "00001",
		Type: "permanent", Status: "active", CreatedBy: "",
	}
	require.NoError(t, db.Create(&sess).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00001")
	require.NoError(t, err)
	assert.Equal(t, "normal", detail.AuthorizationType)
	assert.Equal(t, "testuser", detail.Username)
	assert.Equal(t, "active", detail.EffectiveStatus)
}

func TestGetUserSessionAuthDetail_Temporary(t *testing.T) {
	db := storage.Open(t.TempDir())
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "tempuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us2", UserID: "u1", SessionSeq: 2, SessionID: "00002",
		Type: "temporary", Status: "active", CreatedBy: "admin",
		ExpiresAt: timePtr(time.Now().Add(1 * time.Hour)),
	}
	require.NoError(t, db.Create(&sess).Error)

	ta := model.TemporaryAccount{
		ID: "ta1", SessionID: "00002", Type: model.TemporaryAccountTypeUser,
		AuthorizedUserID: "u1", Status: "active", Remark: "临时排查",
		StartsAt: time.Now(),
	}
	require.NoError(t, db.Create(&ta).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00002")
	require.NoError(t, err)
	assert.Equal(t, "temporary", detail.AuthorizationType)
	assert.Equal(t, "临时排查", detail.Remark)
	assert.Equal(t, "admin", detail.AuthorizedBy)
}

func TestGetUserSessionAuthDetail_AI(t *testing.T) {
	db := storage.Open(t.TempDir())
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "aiuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us3", UserID: "u1", SessionSeq: 3, SessionID: "00003",
		Type: "temporary", Status: "active", CreatedBy: "system",
	}
	require.NoError(t, db.Create(&sess).Error)

	ta := model.TemporaryAccount{
		ID: "ta2", SessionID: "00003", Type: model.TemporaryAccountTypeAI,
		AuthorizedUserID: "u1", Status: "active", Remark: "AI自动操作",
		StartsAt: time.Now(),
	}
	require.NoError(t, db.Create(&ta).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00003")
	require.NoError(t, err)
	assert.Equal(t, "ai", detail.AuthorizationType)
	assert.Equal(t, "AI自动操作", detail.Remark)
}

func TestGetUserSessionAuthDetail_NotFound(t *testing.T) {
	db := storage.Open(t.TempDir())
	storage.AutoMigrate(db)
	store := &DBStore{db: db}
	_, err := store.GetUserSessionAuthDetail(context.Background(), "99999")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetUserSessionAuthDetail_Expired(t *testing.T) {
	db := storage.Open(t.TempDir())
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "expireduser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us4", UserID: "u1", SessionSeq: 4, SessionID: "00004",
		Type: "permanent", Status: "active",
		ExpiresAt: timePtr(time.Now().Add(-1 * time.Hour)),
	}
	require.NoError(t, db.Create(&sess).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00004")
	require.NoError(t, err)
	assert.Equal(t, "expired", detail.EffectiveStatus)
	assert.Equal(t, "active", detail.Status) // 原始状态不变
}

func timePtr(t time.Time) *time.Time { return &t }
```

- [ ] **步骤 4: 运行 store 测试**

```bash
cd C:/02-codespace/Jianmen/.claude/worktrees/audit-session-detail
go test ./internal/store/ -run TestGetUserSessionAuthDetail -v -count=1
```

预期：全部 PASS。

- [ ] **步骤 5: 提交**

```bash
git add internal/model/user_session.go internal/store/dbstore_sessions.go internal/store/dbstore_sessions_test.go
git commit -m "feat: 新增 UserSession 授权详情存储查询方法"
```

---

### Task 2: 后端 Handler — UserSession 详情 REST API

**文件：**
- 修改：`internal/server/admin/session_handlers.go`（新增 handler）
- 修改：`internal/server/admin/routes.go`（注册路由）
- 修改：`internal/server/admin/session_handlers_test.go`（新增 handler 测试）

**接口：**
- 消费：`(*DBStore).GetUserSessionAuthDetail(ctx, sessionID) (model.UserSessionAuthDetail, error)`
- 产出：`GET /api/user-sessions/by-session-id/{sessionID}` → JSON body

#### 步骤 1: 新增 handler 函数

在 `internal/server/admin/session_handlers.go` 末尾追加：

```go
// handleUserSessionBySessionID 通过 5 位 session_id 查询用户会话授权详情。
// 用于审计页面 SessionID 点击后弹窗展示。
func (s *Server) handleUserSessionBySessionID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 权限检查：需要具备 SSH 审计、DB 审计或在线会话查看任一权限
	if !s.requireAnyPermission(r, rbac.ActionAuditView, rbac.ActionDBAuditView, rbac.ActionSessionView) {
		s.forbidden(w, r)
		return
	}

	// 解析路径中的 sessionID
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/user-sessions/by-session-id/")
	sessionID = strings.Trim(sessionID, "/")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid session id")
		return
	}

	detail, err := s.dbstore.GetUserSessionAuthDetail(r.Context(), sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "授权会话信息不存在")
			return
		}
		s.logger.Warn("查询用户会话授权详情失败", "session_id", sessionID, "error", err)
		s.writeErrorText(w, r, http.StatusInternalServerError, "查询授权详情失败")
		return
	}

	s.writeJSON(w, r, http.StatusOK, detail)
}
```

#### 步骤 2: 注册路由

在 `internal/server/admin/routes.go` 中，紧接 `"/api/user-sessions"` 行之后添加：

```go
s.muxHandle(mux, "/api/user-sessions/by-session-id/", s.withAuthAndUser(s.handleUserSessionBySessionID))
```

#### 步骤 3: 编写 handler 测试

在 `internal/server/admin/session_handlers_test.go` 中追加：

```go
func TestHandleUserSessionBySessionID_Success(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "admin-user")

	user := model.User{ID: "u1", Username: "testuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)
	sess := model.UserSession{
		ID: "us1", UserID: "u1", SessionSeq: 1, SessionID: "00001",
		Type: "permanent", Status: "active",
	}
	require.NoError(t, db.Create(&sess).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/00001", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data model.UserSessionAuthDetail `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "00001", resp.Data.SessionID)
	assert.Equal(t, "normal", resp.Data.AuthorizationType)
}

func TestHandleUserSessionBySessionID_NotFound(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "admin-user")

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/99999", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleUserSessionBySessionID_InvalidFormat(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "admin-user")

	req := httptest.NewRequest(http.MethodGet, "/api/user-sessions/by-session-id/too/long/path", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
```

注意：需要确认 `Server` 结构体中 `dbstore` 字段名。实际上 `Server` 可能没有直接引用 `DBStore`。检查 Server 结构体；可能已有 `auditQuery`、`userSessionCreation` 等 service 字段。若 `dbstore` 未直接暴露，可通过 `s.userSessionCreation` 或新增 service 方法封装。

如果 Server 未直接持有 `DBStore`，在 `session_handlers.go` 中需要将 handler 改为通过现有 service 调用。检查 Server 结构体确认是否有 `s.dbstore` 字段。若没有，可行方案：

- 方案 A：在 Server 新增 `dbstore *store.DBStore` 字段并在构造函数中注入。
- 方案 B：在 `userSessionCreation` service 接口中新增 `GetAuthDetail(ctx, sessionID)` 方法。

**推荐方案 A**（改动最小，可立即使用 store 方法）。若 Server 需注入，修改 `internal/server/admin/server.go`：

```go
// 在 Server 结构体中新增
dbstore *store.DBStore
```

构造函数中注入。然后 handler 用 `s.dbstore.GetUserSessionAuthDetail(...)`。

- [ ] **步骤 4: 运行 handler 测试**

```bash
go test ./internal/server/admin/ -run TestHandleUserSessionBySessionID -v -count=1
```

预期：全部 PASS。

- [ ] **步骤 5: 提交**

```bash
git add internal/server/admin/session_handlers.go internal/server/admin/routes.go internal/server/admin/session_handlers_test.go
git commit -m "feat: 新增 UserSession 授权详情 REST API"
```

---

### Task 3: 后端 — 在线会话 SessionID 补充

**文件：**
- 修改：`internal/online/registry.go`（Session 结构体增加字段）
- 修改：`internal/server/admin/online_sessions.go`（handler 中补充 SessionID）
- 修改：`internal/server/admin/online_sessions_test.go`（新增验证）
- 若 handler 需要访问 store 做批量查询，确认已有 `s.dbstore` 引用（参见 Task 2 步骤 3 注意事项）。

**接口：**
- 消费：`(*DBStore).GetUserSessionAuthDetail` 的批量变体或直接复用 AuditSession 查询
- 产出：在线会话 JSON 响应中新增 `user_session_id` 和 `session_id` 字段

#### 步骤 1: 扩展 online.Session 结构体

在 `internal/online/registry.go` 的 `Session` struct 中追加两个字段：

```go
type Session struct {
	// ... 现有字段保持不变 ...
	UserSessionID string `json:"user_session_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
}
```

#### 步骤 2: 在线会话 handler 中补充 SessionID

在 `internal/server/admin/online_sessions.go` 的 `handleOnlineSessions` 中，在取得 items 之后、paginateSlice 之前，调用批量查询逻辑：

```go
func (s *Server) handleOnlineSessions(w http.ResponseWriter, r *http.Request) {
	// ... 现有方法校验和权限检查代码不变 ...

	items := s.onlineSessions.List()
	// 现有过滤逻辑保持不变 ...

	// 补充 UserSession 的 SessionID
	s.enrichOnlineSessionsWithUserSession(r.Context(), items)

	resp := paginateSlice(items, r, func(item online.Session, q string) bool {
		// ... 现有搜索逻辑保持不变 ...
	})
	s.writeJSON(w, r, http.StatusOK, resp)
}

// enrichOnlineSessionsWithUserSession 批量补充在线会话的 UserSession.SessionID。
func (s *Server) enrichOnlineSessionsWithUserSession(ctx context.Context, sessions []online.Session) {
	if len(sessions) == 0 {
		return
	}
	// 收集所有 AuditSessionID
	auditIDs := make([]string, 0, len(sessions))
	for _, sess := range sessions {
		if sess.AuditSessionID != "" {
			auditIDs = append(auditIDs, sess.AuditSessionID)
		}
	}
	if len(auditIDs) == 0 {
		return
	}
	// 批量查询 audit_sessions 获取 UserSessionID
	type auditRow struct {
		ID            string `gorm:"column:id"`
		UserSessionID string `gorm:"column:user_session_id"`
	}
	var auditRows []auditRow
	_ = s.dbstore.DB().WithContext(ctx).
		Table("audit_sessions").
		Select("id, user_session_id").
		Where("id IN ?", auditIDs).
		Find(&auditRows) // 使用 GORM 模型查询

	auditToUserSession := map[string]string{}
	userSessionIDs := make([]string, 0, len(auditRows))
	for _, r := range auditRows {
		auditToUserSession[r.ID] = r.UserSessionID
		if r.UserSessionID != "" {
			userSessionIDs = append(userSessionIDs, r.UserSessionID)
		}
	}
	if len(userSessionIDs) == 0 {
		return
	}
	// 批量查询 user_sessions 获取 5 位 SessionID
	type userSessionRow struct {
		ID        string `gorm:"column:id"`
		SessionID string `gorm:"column:session_id"`
	}
	var usRows []userSessionRow
	_ = s.dbstore.DB().WithContext(ctx).
		Table("user_sessions").
		Select("id, session_id").
		Where("id IN ?", userSessionIDs).
		Find(&usRows)

	userSessionIDToShort := map[string]string{}
	for _, r := range usRows {
		userSessionIDToShort[r.ID] = r.SessionID
	}

	for i := range sessions {
		usID := auditToUserSession[sessions[i].AuditSessionID]
		if usID == "" {
			continue
		}
		sessions[i].UserSessionID = usID
		sessions[i].SessionID = userSessionIDToShort[usID]
	}
}
```

**重要：** 如果 `DBStore` 没有暴露 `DB()` 方法，请在 `internal/store/dbstore.go` 中添加：

```go
func (s *DBStore) DB() *gorm.DB { return s.db }
```

若 handler 中通过其他方式已有 `*gorm.DB` 引用（如 service 层），可改用该引用。

#### 步骤 3: 扩展在线会话测试

在 `internal/server/admin/online_sessions_test.go` 中追加测试，验证 SessionID 被正确返回：

```go
func TestHandleOnlineSessions_IncludesUserSessionID(t *testing.T) {
	srv, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "admin-user")

	// 创建 UserSession
	user := model.User{ID: "op1", Username: "operator1", Status: "active"}
	require.NoError(t, db.Create(&user).Error)
	us := model.UserSession{
		ID: "us-online-1", UserID: "op1", SessionSeq: 10,
		SessionID: "0000A", Type: "permanent", Status: "active",
	}
	require.NoError(t, db.Create(&us).Error)

	// 创建 AuditSession
	as := model.AuditSession{
		ID: "audit-online-1", UserSessionID: "us-online-1",
		UserID: "op1", Username: "operator1", Protocol: "ssh",
		State: "started", Outcome: "active",
	}
	require.NoError(t, db.Create(&as).Error)

	// 注册在线会话
	srv.onlineSessions = online.NewRegistry()
	srv.onlineSessions.Register(online.Session{
		ID: "online-1", AuditSessionID: "audit-online-1",
		ResourceType: "host", Instance: "web01", Protocol: "ssh",
		Operator: "operator1", Account: "root", StartedAt: time.Now(),
	}, func() {})

	req := httptest.NewRequest(http.MethodGet, "/api/online-sessions", nil)
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			SessionID string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Greater(t, len(resp.Data), 0)
	assert.Equal(t, "0000A", resp.Data[0].SessionID)
}
```

- [ ] **步骤 4: 运行在线会话相关测试**

```bash
go test ./internal/server/admin/ -run TestHandleOnlineSessions -v -count=1
go test ./internal/online/ -v -count=1
```

预期：全部 PASS。

- [ ] **步骤 5: 提交**

```bash
git add internal/online/registry.go internal/server/admin/online_sessions.go internal/server/admin/online_sessions_test.go
git commit -m "feat: 在线会话列表补充 UserSession 的 SessionID"
```

---

### Task 4: 前端 — API 类型与客户端方法

**文件：**
- 修改：`web/src/api/client.ts`

**接口：**
- 产出：`UserSessionDetail` 类型
- 产出：`getUserSessionBySessionID(sessionID: string): Promise<UserSessionDetail>`
- 产出：`OnlineSessionRecord` 新增 `user_session_id` 和 `session_id` 字段

#### 步骤 1: 新增 UserSessionDetail 类型

在 `web/src/api/client.ts` 中 `UserSessionRecord` 之后追加：

```typescript
/** UserSession 授权详情（点击 SessionID 弹窗展示） */
export interface UserSessionDetail {
  id: string;
  session_id: string;
  session_type: string;
  authorization_type: 'normal' | 'temporary' | 'ai' | 'unknown';
  user_id: string;
  username: string;
  authorized_by?: string;
  starts_at: string;
  expires_at?: string | null;
  remark?: string;
  status: string;
  effective_status: 'active' | 'expired' | 'disabled' | string;
}
```

#### 步骤 2: 修改 OnlineSessionRecord 类型

找到 `OnlineSessionRecord` interface，追加字段：

```typescript
export interface OnlineSessionRecord {
  // ... 现有字段 ...
  user_session_id?: string;  // 新增
  session_id?: string;        // 新增
  [key: string]: unknown;
}
```

#### 步骤 3: 新增 API 方法

在 ApiClient 返回对象中追加：

```typescript
/** 通过 5 位 session_id 查询用户会话授权详情 */
getUserSessionBySessionID: (sessionID: string) =>
  request<UserSessionDetail>(`/api/user-sessions/by-session-id/${encodeURIComponent(sessionID)}`),
```

- [ ] **步骤 4: 提交**

```bash
git add web/src/api/client.ts
git commit -m "feat: 前端新增 UserSessionDetail 类型与详情查询 API"
```

---

### Task 5: 前端 — UserSessionDetailDialog 组件

**文件：**
- 新增：`web/src/components/UserSessionDetailDialog.vue`

**接口：**
- 消费：`UserSessionDetail` 类型、`getUserSessionBySessionID` API
- 产出：`modelValue: boolean`（显隐）、`sessionId: string`（要查看的 SessionID）
- 事件：`update:modelValue`

#### 步骤 1: 创建组件文件

```vue
<!-- web/src/components/UserSessionDetailDialog.vue -->
<script setup lang="ts">
import { ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { api } from '@/api/client';
import type { UserSessionDetail } from '@/api/client';

const { t } = useI18n();

const props = defineProps<{
  modelValue: boolean;
  sessionId: string;
}>();

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void;
}>();

const loading = ref(false);
const detail = ref<UserSessionDetail | null>(null);
const error = ref('');

const authTypeMap: Record<string, string> = {
  normal: 'audit.authTypeNormal',
  temporary: 'audit.authTypeTemporary',
  ai: 'audit.authTypeAI',
  unknown: 'audit.authTypeUnknown',
};

const authTypeTagType: Record<string, string> = {
  normal: 'primary',
  temporary: 'warning',
  ai: 'success',
  unknown: 'info',
};

const statusTagType: Record<string, string> = {
  active: 'success',
  expired: 'info',
  disabled: 'danger',
};

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
});

watch(
  () => props.modelValue && props.sessionId,
  async (shouldLoad) => {
    if (!shouldLoad) return;
    loading.value = true;
    error.value = '';
    detail.value = null;
    try {
      detail.value = await api.getUserSessionBySessionID(props.sessionId);
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : '加载授权详情失败';
    } finally {
      loading.value = false;
    }
  },
);
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="t('audit.userSessionDetail')"
    width="520px"
    destroy-on-close
    @closed="detail = null; error = ''"
  >
    <div v-if="loading" v-loading="loading" style="min-height: 200px" />

    <div v-else-if="error" style="text-align: center; padding: 40px 0; color: var(--el-color-danger)">
      {{ error }}
    </div>

    <el-descriptions v-else-if="detail" :column="1" border>
      <el-descriptions-item :label="t('audit.sessionId')">
        {{ detail.session_id }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authType')">
        <el-tag :type="authTypeTagType[detail.authorization_type] || 'info'" size="small">
          {{ t(authTypeMap[detail.authorization_type] || 'audit.authTypeUnknown') }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authorizedUser')">
        {{ detail.username }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.authorizedBy')">
        {{ detail.authorized_by || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.startTime')">
        {{ detail.starts_at }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.validity')">
        <template v-if="detail.session_type === 'permanent' && !detail.expires_at">
          {{ t('audit.permanent') }}
        </template>
        <template v-else-if="detail.expires_at">
          {{ detail.expires_at }}
        </template>
        <template v-else>-</template>
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.remark')">
        {{ detail.remark || '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('audit.status')">
        <el-tag :type="statusTagType[detail.effective_status] || 'info'" size="small">
          {{ t(`audit.status${detail.effective_status.charAt(0).toUpperCase() + detail.effective_status.slice(1)}`) }}
        </el-tag>
      </el-descriptions-item>
    </el-descriptions>
  </el-dialog>
</template>
```

- [ ] **步骤 2: 确认 i18n key 存在** 或与 Task 6 一起补充。

- [ ] **步骤 3: 提交**

```bash
git add web/src/components/UserSessionDetailDialog.vue
git commit -m "feat: 新增 UserSession 授权详情弹窗组件"
```

---

### Task 6: 前端 — AuditView 集成与 i18n

**文件：**
- 修改：`web/src/views/AuditView.vue`
- 修改：`web/src/i18n/index.ts`

#### 步骤 1: 补充 i18n key

在 `web/src/i18n/index.ts` 的 `zhCN` 对象中，`audit.*` 相关 key 区域追加：

```typescript
'audit.sessionId': '会话ID',
'audit.userSessionDetail': '会话授权详情',
'audit.authType': '授权类型',
'audit.authTypeNormal': '普通用户',
'audit.authTypeTemporary': '临时授权',
'audit.authTypeAI': 'AI 授权',
'audit.authTypeUnknown': '未知授权',
'audit.authorizedUser': '授权用户',
'audit.authorizedBy': '授权创建人',
'audit.startTime': '开始时间',
'audit.validity': '授权有效期',
'audit.remark': '备注',
'audit.status': '状态',
'audit.statusActive': '生效中',
'audit.statusExpired': '已过期',
'audit.statusDisabled': '已禁用',
'audit.permanent': '永久有效',
```

#### 步骤 2: 修改 AuditView.vue — 导入组件

在 `<script setup>` 中新增导入：

```typescript
import UserSessionDetailDialog from '@/components/UserSessionDetailDialog.vue';
```

#### 步骤 3: 新增响应式状态

在 script setup 状态变量区域追加：

```typescript
// SessionID 详情弹窗
const sessionDetailVisible = ref(false);
const sessionDetailId = ref('');
```

#### 步骤 4: 新增点击处理函数

```typescript
function showUserSessionDetail(sessionID: string) {
  sessionDetailId.value = sessionID;
  sessionDetailVisible.value = true;
}
```

#### 步骤 5: 修改 SSH 表格 — 新增 SessionID 列，删除操作用户旁内联显示

**修改 SSH 表格操作者列** — 删除 `(session_id)` 内联文本：

找到 SSH tab 中 `el-table-column label="操作用户"`：
```vue
<!-- 修改前 -->
<el-table-column :label="t('audit.column.operator')" min-width="150">
  <template #default="{ row }">
    {{ sessionUser(row) }}
    <span v-if="row.session_id" style="color: #909399; margin-left:2px">({{ row.session_id }})</span>
  </template>
</el-table-column>

<!-- 修改后 -->
<el-table-column :label="t('audit.column.operator')" min-width="120">
  <template #default="{ row }">
    {{ sessionUser(row) }}
  </template>
</el-table-column>
```

**在操作用户列之后新增 SessionID 列：**

```vue
<el-table-column :label="t('audit.sessionId')" width="90" align="center">
  <template #default="{ row }">
    <el-link
      v-if="row.session_id"
      type="primary"
      :underline="false"
      @click.stop="showUserSessionDetail(row.session_id)"
    >
      {{ row.session_id }}
    </el-link>
    <span v-else>-</span>
  </template>
</el-table-column>
```

#### 步骤 6: 修改 DB 表格 — 同样的处理

找到 DB tab 中操作者列的 `session_id` 内联显示并删除，新增 SessionID 列（代码与步骤 5 相同）。

#### 步骤 7: 修改在线会话表格 — 新增 SessionID 列

在在线会话 table 中合适位置（开始时间或操作用户之后）追加同样的 `SessionID` 列。

#### 步骤 8: 模板末尾添加详情弹窗组件

在 `</template>` 之前、所有现有 dialog/drawer 之后追加：

```vue
<UserSessionDetailDialog
  v-model="sessionDetailVisible"
  :session-id="sessionDetailId"
/>
```

- [ ] **步骤 9: 类型检查**

```bash
cd web
npm run typecheck
```

预期：无 TypeScript 错误。

- [ ] **步骤 10: 提交**

```bash
git add web/src/views/AuditView.vue web/src/i18n/index.ts
git commit -m "feat: 审计列表增加独立可点击 SessionID 列"
```

---

### Task 7: 全面验证

- [ ] **步骤 1: 运行全部 Go 测试**

```bash
cd C:/02-codespace/Jianmen/.claude/worktrees/audit-session-detail
go test ./... -count=1
```

预期：所有测试 PASS（允许有预先存在的跳过或失败，确认没有新增失败）。

- [ ] **步骤 2: 运行前端类型检查和生产构建**

```bash
cd web
npm run typecheck
npm run build
```

预期：typecheck 无错误，build 成功。

- [ ] **步骤 3: 回归验证关键功能**

确认以下已有功能未被破坏：

```bash
# SSH 审计查询
go test ./internal/store/ -run TestAuditSession -v -count=1
# 在线会话
go test ./internal/server/admin/ -run TestHandleOnlineSessions -v -count=1
# 用户会话
go test ./internal/store/ -run TestUserSession -v -count=1
# 临时授权
go test ./internal/store/ -run TestTemporaryAccess -v -count=1
```

- [ ] **步骤 4: 最终提交**

```bash
git status
# 确认无遗漏文件
git add -A
git diff --cached --stat
# 若有额外改动则提交
```

---

## 任务依赖图

```
Task 1 (Store 查询方法)
  └─> Task 2 (REST API)
        ├─> Task 4 (前端 API 类型)
        │     ├─> Task 5 (弹窗组件)
        │     └─> Task 6 (AuditView 集成 + i18n)
        │           └─> Task 7 (全面验证)
        └─> Task 3 (在线会话补充)
              └─> Task 6 (AuditView 集成 + i18n)
```

Task 1 必须先完成。Task 2、3 依赖 Task 1 但互不依赖可并行。Task 4、5、6 依赖 Task 2（Task 3 也依赖 Task 2 中的 `dbstore` 引用），Task 5、6 依赖 Task 4。Task 6 需同时等 Task 3 完成。
