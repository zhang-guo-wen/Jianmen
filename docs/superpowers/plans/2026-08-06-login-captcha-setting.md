# 登录验证码开关纳入系统配置（热加载）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将登录验证码开关从纯配置文件迁移到系统配置（数据库持久化），保存后立即生效（热加载），同时保留重启后一致性与修订历史。

**Architecture:** 在现有 systemsettings 链路（model → service → handler → 前端）中新增 `login_captcha_enabled` 字段；为 `SystemSettingsService` 增加"可热加载字段 + 运行时应用器"机制（本设计仅 `login_captcha_enabled` 可热加载）；登录开关改为 `admin.Server` 上的原子布尔值，登录处理器读取内存值而非配置文件。

**Tech Stack:** Go 1.23+（gorm、sync/atomic）、Vue 3 + TypeScript（Element Plus、vitest、tsx）、测试命令见下。

## Global Constraints

- 所有注释与 git 提交信息使用中文；
- 所有任务在**功能 worktree** 中执行（创建方式见 superpowers:using-git-worktrees 技能），不得直接修改主工作区；本计划文件已提交到 `dev` 分支；
- `login_captcha_enabled` 默认值为 `false`（关闭）；已有部署升级后新字段默认关闭，配置文件仅作首次初始化的默认值来源；
- 更新系统设置沿用现有约束：全字段必填（`toService` 校验）、乐观锁（`expected_revision`）、风险字段二次确认（`confirm_risk`）；
- 后端测试：`go test ./internal/service/... ./internal/handler/systemsettings/... ./cmd/jianmen/... ./internal/server/admin/...`（各任务按需收窄）；
- 前端测试：`npm run typecheck`、`npm run test:connection-commands`（含 `src/api/systemSettings.test.ts`）、`npm run test:connection-dialog`（含 `SystemSettingsView.mount.test.ts`），在 `web/` 目录执行。

---

### Task 1: 配置字段全链路接入（model + service + snapshot）

在数据模型与服务层接入 `login_captcha_enabled` 字段，并使"关闭验证码"进入风险二次确认集合。

**Files:**
- Modify: `internal/model/system_setting.go:8-26`（`SystemSetting` 结构体）
- Modify: `internal/service/system_setting.go`（`SystemSettings` 结构、`systemSettingFieldNames`、`changedSystemSettingFields`、`riskySystemSettingFields`、`systemSettingModel`、`systemSettingsFromModel`）
- Modify: `internal/service/system_setting_snapshot.go`（`systemSettingsSnapshot`、`snapshotFromSystemSettings`、`systemSettings()`）
- Test: `internal/service/system_setting_test.go`

**Interfaces:**
- Produces: `SystemSettings.LoginCaptchaEnabled bool`；`model.SystemSetting.LoginCaptchaEnabled bool`；`systemSettingFieldNames` 含 `"login_captcha_enabled"`；`riskySystemSettingFields` 在 before=true→after=false 时输出 `"login_captcha_enabled"`

- [ ] **Step 1: 写失败测试**

在 `internal/service/system_setting_test.go` 末尾（`validSystemSettings` 定义之前）追加：

```go
func TestSystemSettingsDisableLoginCaptchaRequiresRiskConfirmation(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	baseline.LoginCaptchaEnabled = true
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	desired := baseline
	desired.LoginCaptchaEnabled = false
	_, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
	})
	if !errors.Is(err, ErrSystemSettingsRiskConfirmationRequired) ||
		!strings.Contains(err.Error(), "login_captcha_enabled") {
		t.Fatalf("Update() error = %v, want login captcha confirmation", err)
	}
	if repository.setting.Revision != 1 || len(repository.revisions) != 1 {
		t.Fatalf("unconfirmed change was persisted: %#v", repository)
	}

	state, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1, ConfirmRisk: true,
	})
	if err != nil {
		t.Fatalf("confirmed Update() error = %v", err)
	}
	if state.Desired.LoginCaptchaEnabled {
		t.Fatalf("desired captcha = %v, want false", state.Desired.LoginCaptchaEnabled)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/service/ -run TestSystemSettingsDisableLoginCaptchaRequiresRiskConfirmation -v`
Expected: FAIL —— `baseline.LoginCaptchaEnabled` 不存在导致编译错误（`validSystemSettings` 返回的结构体无该字段）。

- [ ] **Step 3: 实现字段接入**

`internal/model/system_setting.go` 的 `SystemSetting` 结构体，在 `WebRDPEnabled` 之前（即 `DatabaseGatewayClientTLSMode` 之后）插入：

```go
	LoginCaptchaEnabled bool   `gorm:"not null"`
```

`internal/service/system_setting.go`：

1. `systemSettingFieldNames` 切片在 `"database_gateway_client_tls_mode"` 之后加入 `"login_captcha_enabled"`：

```go
var systemSettingFieldNames = []string{
	"database_gateway_mode",
	"database_gateway_client_tls_mode",
	"login_captcha_enabled",
	"web_rdp_enabled",
	"web_rdp_connect_timeout_seconds",
	"web_rdp_allow_unrecorded",
	"recording_enabled",
	"recording_record_input",
	"recording_record_commands",
	"recording_retention_days",
	"recording_max_replay_bytes",
	"recording_cleanup_batch_size",
	"database_max_client_message_bytes",
}
```

2. `SystemSettings` 结构体在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
	LoginCaptchaEnabled bool
```

3. `changedSystemSettingFields` 在 client TLS 判断之后加入：

```go
	if before.LoginCaptchaEnabled != after.LoginCaptchaEnabled {
		changed = append(changed, "login_captcha_enabled")
	}
```

4. `riskySystemSettingFields` 在 `WebRDPAllowUnrecorded` 判断之前加入：

```go
	if before.LoginCaptchaEnabled && !after.LoginCaptchaEnabled {
		risky = append(risky, "login_captcha_enabled")
	}
```

5. `systemSettingModel` 在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
		LoginCaptchaEnabled:         settings.LoginCaptchaEnabled,
```

6. `systemSettingsFromModel` 在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
		LoginCaptchaEnabled:         setting.LoginCaptchaEnabled,
```

`internal/service/system_setting_snapshot.go`：

1. `systemSettingsSnapshot` 在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
	LoginCaptchaEnabled bool   `json:"login_captcha_enabled"`
```

2. `snapshotFromSystemSettings` 对应位置加入：

```go
		LoginCaptchaEnabled:         settings.LoginCaptchaEnabled,
```

3. `systemSettings()` 对应位置加入：

```go
		LoginCaptchaEnabled:         snapshot.LoginCaptchaEnabled,
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/service/ -run 'TestSystemSettings(DisableLoginCaptchaRequiresRiskConfirmation|BootstrapUpdateAndRestart|UpdateRequiresRiskConfirmation)$' -v`
Expected: PASS（含既有测试回归）。

- [ ] **Step 5: 提交**

```bash
git add internal/model/system_setting.go internal/service/system_setting.go internal/service/system_setting_snapshot.go internal/service/system_setting_test.go
git commit -m "feat: 系统设置接入登录验证码开关字段与风险确认

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: 服务层热加载机制（核心）

为 `SystemSettingsService` 增加可热加载字段集合与运行时应用器，`Update` 持久化成功后对可热加载变更立即更新内存生效值。

**Files:**
- Modify: `internal/service/system_setting.go`（`SystemSettingsService` 结构、`Update`、新增集合与三个方法）
- Test: `internal/service/system_setting_test.go`

**Interfaces:**
- Consumes: `SystemSettings.LoginCaptchaEnabled`（Task 1）、`systemSettingFieldNames`、`changedSystemSettingFields`
- Produces: `func (s *SystemSettingsService) RegisterHotReloadApplier(applier SystemSettingsHotReloadApplier)`；`type SystemSettingsHotReloadApplier func(SystemSettings) error`；`hotReloadableSystemSettingFields` 含 `"login_captcha_enabled"`；热加载成功后 `state.PendingRestart == false` 且 `EffectiveRevision == Revision`

- [ ] **Step 1: 写失败测试**

在 `internal/service/system_setting_test.go` 末尾追加三个测试：

```go
func TestSystemSettingsHotReloadLoginCaptcha(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	var applied []bool
	svc.RegisterHotReloadApplier(func(settings SystemSettings) error {
		applied = append(applied, settings.LoginCaptchaEnabled)
		return nil
	})

	desired := baseline
	desired.LoginCaptchaEnabled = true
	state, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if state.PendingRestart || state.EffectiveRevision != 2 || state.Revision != 2 {
		t.Fatalf("hot-reloaded state = %#v", state)
	}
	if !state.Effective.LoginCaptchaEnabled || !state.Desired.LoginCaptchaEnabled {
		t.Fatalf("hot-reloaded settings = %#v", state)
	}
	if len(applied) != 1 || !applied[0] {
		t.Fatalf("hot-reload applier calls = %v", applied)
	}
	if repository.setting.AppliedRevision != 2 {
		t.Fatalf("hot-reload applied revision = %d", repository.setting.AppliedRevision)
	}
}

func TestSystemSettingsHotReloadOnlyForHotReloadableFields(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	applierCalls := 0
	svc.RegisterHotReloadApplier(func(SystemSettings) error {
		applierCalls++
		return nil
	})

	desired := baseline
	desired.LoginCaptchaEnabled = true
	desired.WebRDPEnabled = true
	state, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !state.PendingRestart || state.EffectiveRevision != 1 || state.Revision != 2 {
		t.Fatalf("mixed-fields state = %#v", state)
	}
	if state.Effective.LoginCaptchaEnabled || applierCalls != 0 {
		t.Fatalf("non-hot-reloadable update was applied: effective=%v calls=%d",
			state.Effective.LoginCaptchaEnabled, applierCalls)
	}
}

func TestSystemSettingsHotReloadWithoutApplierFallsBack(t *testing.T) {
	ctx := context.Background()
	repository := &systemSettingsMemoryRepository{}
	svc := newTestSystemSettingsService(t, repository, time.Now())
	baseline := validSystemSettings()
	if _, err := svc.Bootstrap(ctx, baseline); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	desired := baseline
	desired.LoginCaptchaEnabled = true
	state, err := svc.Update(ctx, SystemSettingsUpdate{
		Settings: desired, ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !state.PendingRestart || state.EffectiveRevision != 1 {
		t.Fatalf("fallback state = %#v", state)
	}
	if state.Effective.LoginCaptchaEnabled {
		t.Fatalf("fallback still applied captcha: %#v", state)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/service/ -run 'TestSystemSettingsHotReload' -v`
Expected: FAIL —— `RegisterHotReloadApplier` 未定义（编译错误）。

- [ ] **Step 3: 实现热加载机制**

`internal/service/system_setting.go`：

1. 在 `systemSettingFieldNames` 定义之后新增：

```go
// hotReloadableSystemSettingFields 保存后立即生效的字段集合；
// 变更仅涉及这些字段且已注册应用器时，Update 会在持久化成功后同步内存生效值。
var hotReloadableSystemSettingFields = map[string]struct{}{
	"login_captcha_enabled": {},
}

// SystemSettingsHotReloadApplier 将新配置应用到运行时组件的回调。
type SystemSettingsHotReloadApplier func(SystemSettings) error

func systemSettingFieldsHotReloadable(fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if _, ok := hotReloadableSystemSettingFields[field]; !ok {
			return false
		}
	}
	return true
}
```

2. `SystemSettingsService` 结构体在 `now` 字段之后加入：

```go
	appliers []SystemSettingsHotReloadApplier
```

3. 在 `NewSystemSettingsService` 之后新增三个方法：

```go
// RegisterHotReloadApplier 注册热加载应用器：可热加载字段保存成功后，
// 将新配置依次传给已注册应用器以更新运行时组件。
func (s *SystemSettingsService) RegisterHotReloadApplier(
	applier SystemSettingsHotReloadApplier,
) {
	if applier == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appliers = append(s.appliers, applier)
}

func (s *SystemSettingsService) hasHotReloadAppliers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.appliers) > 0
}

func (s *SystemSettingsService) applyHotReload(settings SystemSettings) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, applier := range s.appliers {
		if err := applier(settings); err != nil {
			return err
		}
	}
	return nil
}
```

（约束：应用器（本设计的 `ApplyLoginCaptchaEnabled`）只做原子写、不得回调本服务的加锁方法，避免死锁；如未来注册复杂应用器，需改为在锁外调用。）

4. `Update` 中，在 `if !updated { return SystemSettingsState{}, ErrSystemSettingsRevisionConflict }` 之后、`return s.stateFromModel(persisted)` 之前插入：

```go
	if systemSettingFieldsHotReloadable(changedFields) && s.hasHotReloadAppliers() {
		if err := s.applyHotReload(update.Settings); err != nil {
			return SystemSettingsState{}, fmt.Errorf("apply hot-reloadable system settings: %w", err)
		}
		applied, marked, err := s.repository.MarkSystemSettingApplied(ctx, persisted.Revision, now)
		if err != nil {
			return SystemSettingsState{}, fmt.Errorf("mark hot-reloaded system settings applied: %w", err)
		}
		if !marked ||
			applied.Revision != persisted.Revision ||
			applied.AppliedRevision != persisted.Revision {
			return SystemSettingsState{}, fmt.Errorf("%w: revision %d changed during hot reload",
				ErrSystemSettingsRevisionConflict, persisted.Revision)
		}
		s.mu.Lock()
		s.effective = update.Settings
		s.effectiveRevision = persisted.Revision
		s.mu.Unlock()
	}
```

（`now` 变量已在 `Update` 前文定义为 `s.now().UTC()`，直接复用。）

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/service/ -run 'TestSystemSettings' -v`
Expected: PASS（含 Task 1 与全部既有 service 测试）。

- [ ] **Step 5: 提交**

```bash
git add internal/service/system_setting.go internal/service/system_setting_test.go
git commit -m "feat: 系统设置支持可热加载字段与应用器机制

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: HTTP 层与运行时映射

在 handler 视图层与 `cmd` 运行时映射中接入字段，使 API 请求/响应与启动时 cfg 回写包含 `login_captcha_enabled`。

**Files:**
- Modify: `internal/handler/systemsettings/view.go`（`settingsValues`、`settingsValuesRequest`、`toService`、`mapValues`）
- Modify: `cmd/jianmen/system_settings_runtime.go`（`systemSettingsFromConfig`、`applySystemSettings`）
- Test: `internal/handler/systemsettings/handler_test.go`、`cmd/jianmen/system_settings_runtime_test.go`

**Interfaces:**
- Consumes: `SystemSettings.LoginCaptchaEnabled`（Task 1）
- Produces: HTTP JSON 字段 `login_captcha_enabled`；`systemSettingsFromConfig(cfg)` 读 `cfg.Admin.LoginCaptchaEnabled`；`applySystemSettings(cfg, settings)` 写回 `cfg.Admin.LoginCaptchaEnabled`

- [ ] **Step 1: 更新测试数据（使其先失败）**

`internal/handler/systemsettings/handler_test.go`：

1. `validUpdateBody()` 的 `settings` 对象在 `"database_gateway_client_tls_mode"` 之后加入 `"login_captcha_enabled":false,`；
2. `TestCollectionUpdatesWithAuthenticatedActor` 的请求体（约第 114-131 行）在 `"database_gateway_client_tls_mode": "required"` 之后同样加入 `"login_captcha_enabled": false,`；
3. `testSystemSettingsState()` 的 `values` 在 `DatabaseGatewayClientTLSMode` 之后加入 `LoginCaptchaEnabled: true,`；
4. `TestCollectionReturnsStateAndRedactedInfrastructure` 在 `database client message limit` 断言（约第 99-104 行）之后追加映射断言：

```go
	if !envelope.Data.Desired.LoginCaptchaEnabled {
		t.Fatalf("login captcha was not mapped: %#v", envelope.Data)
	}
```

5. `TestCollectionUpdatesWithAuthenticatedActor` 的 if 条件（`settings.update.Settings.DatabaseGatewayClientTLSMode != "required" ||` 之后）追加：

```go
		settings.update.Settings.LoginCaptchaEnabled != false ||
```

`cmd/jianmen/system_settings_runtime_test.go`：

1. `validManagedSettingsConfig()` 返回值加入：

```go
		Admin: config.AdminConfig{LoginCaptchaEnabled: true},
```

2. `TestSystemSettingsBecomeEffectiveAfterRestartBootstrap` 在 `if !state.PendingRestart ...` 断言之后追加：

```go
	if !cfg.Admin.LoginCaptchaEnabled {
		t.Fatalf("running config lost login captcha after bootstrap")
	}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/handler/systemsettings/ ./cmd/jianmen/ -v`
Expected: FAIL —— `toService` 报 "all system settings fields are required"（handler 测试 400），`applied != desired` 不一致（cmd 测试）。

- [ ] **Step 3: 实现映射**

`internal/handler/systemsettings/view.go`：

1. `settingsValues` 在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
	LoginCaptchaEnabled             bool   `json:"login_captcha_enabled"`
```

2. `settingsValuesRequest` 对应位置加入：

```go
	LoginCaptchaEnabled             *bool  `json:"login_captcha_enabled"`
```

3. `toService` 中把 `v.DatabaseGatewayClientTLSMode == nil` 的条件扩展为：

```go
	if v.DatabaseGatewayMode == nil ||
		v.DatabaseGatewayClientTLSMode == nil ||
		v.LoginCaptchaEnabled == nil || v.WebRDPEnabled == nil || v.WebRDPConnectTimeoutSeconds == nil ||
		v.WebRDPAllowUnrecorded == nil || v.RecordingEnabled == nil ||
		v.RecordingRecordInput == nil || v.RecordingRecordCommands == nil ||
		v.RecordingRetentionDays == nil || v.RecordingMaxReplayBytes == nil ||
		v.RecordingCleanupBatchSize == nil ||
		v.DatabaseMaxClientMessageBytes == nil {
		return service.SystemSettings{}, errors.New("all system settings fields are required")
	}
```

4. `toService` 返回值在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
		LoginCaptchaEnabled:         *v.LoginCaptchaEnabled,
```

5. `mapValues` 返回值对应位置加入：

```go
		LoginCaptchaEnabled:         value.LoginCaptchaEnabled,
```

`cmd/jianmen/system_settings_runtime.go`：

1. `systemSettingsFromConfig` 在 `DatabaseGatewayClientTLSMode` 之后加入：

```go
		LoginCaptchaEnabled:         cfg.Admin.LoginCaptchaEnabled,
```

2. `applySystemSettings` 在 `DatabaseGateway.ClientTLSMode` 之后加入：

```go
	cfg.Admin.LoginCaptchaEnabled = settings.LoginCaptchaEnabled
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/handler/systemsettings/ ./cmd/jianmen/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/handler/systemsettings/view.go internal/handler/systemsettings/handler_test.go cmd/jianmen/system_settings_runtime.go cmd/jianmen/system_settings_runtime_test.go
git commit -m "feat: 验证码开关接入系统设置 HTTP 层与运行时映射

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: admin.Server 登录开关原子化

登录验证码开关从 `cfg` 迁移到 `Server` 上的原子布尔值，登录相关处理器读取内存值，支持运行时切换。

**Files:**
- Modify: `internal/server/admin/server.go`（`Server` 结构、`New`）
- Modify: `internal/server/admin/init.go`（`handleInitStatus`、`handleLoginCaptchaChallenge`、`handleLogin`）
- Test: `internal/server/admin/server_test.go`

**Interfaces:**
- Consumes: `SystemSettings.LoginCaptchaEnabled`（Task 1）
- Produces: `func (s *Server) ApplyLoginCaptchaEnabled(enabled bool)`；`Server.loginCaptchaEnabled atomic.Bool`，`New` 时以 `cfg.Admin.LoginCaptchaEnabled` 初始化；`handleLogin`/`handleLoginCaptchaChallenge`/`handleInitStatus` 读 `s.loginCaptchaEnabled.Load()`

- [ ] **Step 1: 写失败测试**

`internal/server/admin/server_test.go`：

1. `TestLoginRequiresCaptcha`（约第 449 行）把：

```go
	server.cfg.Admin.LoginCaptchaEnabled = true
```

替换为：

```go
	server.ApplyLoginCaptchaEnabled(true)
```

2. `TestLoginCaptchaChallengeIsNotCached`（约第 489 行）同样替换。

3. 在 `TestLoginCaptchaChallengeIsNotCached` 之后新增：

```go
func TestLoginCaptchaHotReloadUpdatesInitStatus(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/init/status", nil)
	rec := httptest.NewRecorder()
	server.handleInitStatus(rec, req)
	var got InitStatusResponse
	if err := decodeTestData(t, rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if got.LoginCaptchaEnabled {
		t.Fatal("captcha enabled by default, want disabled")
	}

	server.ApplyLoginCaptchaEnabled(true)
	rec = httptest.NewRecorder()
	server.handleInitStatus(rec, req)
	if err := decodeTestData(t, rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rec.Body.String())
	}
	if !got.LoginCaptchaEnabled {
		t.Fatal("captcha disabled after hot reload, want enabled")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/server/admin/ -run 'TestLogin(CaptchaHotReloadUpdatesInitStatus|RequiresCaptcha|CaptchaChallengeIsNotCached)' -v`
Expected: FAIL —— `ApplyLoginCaptchaEnabled` 未定义（编译错误）。

- [ ] **Step 3: 实现**

`internal/server/admin/server.go`：

1. import 块加入 `"sync/atomic"`（加在 `"reflect"` 之后）。

2. `Server` 结构体在 `loginLimiter` 之后加入：

```go
	loginCaptchaEnabled atomic.Bool
```

3. `New` 中 `return &Server{...}` 之前加入：

```go
	server := &Server{
		cfg: cfg, db: db, logger: logger,
		adminAuth: adminAuth, aiAccessTokens: aiAccessTokens, aiResources: aiResources,
		hostTargets: dependencies.hostTargets, hostManagement: hostManagement, databases: dependencies.databases,
		databaseManagement: databaseManagement, databaseTLSPreflight: databaseTLSPreflight, applicationService: applicationService,
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
		webRDP:          webRDP,
		systemSettings:  systemSettings,
		sqlConsole:      sqlConsole,
		dbstore:         store.NewDBStore(db),
	}
	server.loginCaptchaEnabled.Store(cfg.Admin.LoginCaptchaEnabled)
	return server, nil
```

（即把原 `return &Server{...}` 改为先构造再返回，函数体其余不变。）

4. 在 `isNilAdminAuthorization` 函数之前新增：

```go
// ApplyLoginCaptchaEnabled 热加载入口：系统设置保存验证码开关后立即更新运行时开关。
func (s *Server) ApplyLoginCaptchaEnabled(enabled bool) {
	s.loginCaptchaEnabled.Store(enabled)
}
```

`internal/server/admin/init.go`：

1. `handleInitStatus` 中 `LoginCaptchaEnabled: s.cfg.Admin.LoginCaptchaEnabled,` 改为：

```go
		LoginCaptchaEnabled: s.loginCaptchaEnabled.Load(),
```

2. `handleLoginCaptchaChallenge` 中 `if !s.cfg.Admin.LoginCaptchaEnabled {` 改为：

```go
	if !s.loginCaptchaEnabled.Load() {
```

3. `handleLogin` 中 `if s.cfg.Admin.LoginCaptchaEnabled {` 改为：

```go
	if s.loginCaptchaEnabled.Load() {
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/server/admin/ -run 'TestLogin' -v`
Expected: PASS（含热加载、要求验证码、禁用放行、challenge 不缓存四个测试）。

- [ ] **Step 5: 提交**

```bash
git add internal/server/admin/server.go internal/server/admin/init.go internal/server/admin/server_test.go
git commit -m "feat: 登录验证码开关改为内存原子值并支持热加载

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: cmd 组装注册热加载应用器

在 `startAdminRuntime` 中把 `adminServer.ApplyLoginCaptchaEnabled` 注册为系统设置的热加载应用器。

**Files:**
- Modify: `cmd/jianmen/admin_runtime.go:82-91`

**Interfaces:**
- Consumes: `RegisterHotReloadApplier`（Task 2）、`ApplyLoginCaptchaEnabled`（Task 4）
- Produces: 运行时接线：保存验证码开关 → service 调用 applier → `adminServer` 原子开关更新

- [ ] **Step 1: 实现注册**

`cmd/jianmen/admin_runtime.go` 在 `admin.New` 成功返回之后、`go func() { errCh <- adminServer.ListenAndServe(ctx) }()` 之前插入：

```go
	settings.RegisterHotReloadApplier(func(settings service.SystemSettings) error {
		adminServer.ApplyLoginCaptchaEnabled(settings.LoginCaptchaEnabled)
		return nil
	})
```

（`service` 包已在文件 import 中。）

- [ ] **Step 2: 编译验证**

Run: `go build ./cmd/jianmen/`
Expected: 编译成功，无输出。

- [ ] **Step 3: 提交**

```bash
git add cmd/jianmen/admin_runtime.go
git commit -m "feat: 注册验证码开关热加载应用器

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: 前端 API 层

前端 API 类型、字段清单与风险提示接入 `login_captcha_enabled`。

**Files:**
- Modify: `web/src/api/systemSettings.ts`
- Test: `web/src/api/systemSettings.test.ts`

**Interfaces:**
- Consumes: 后端 JSON 字段 `login_captcha_enabled`
- Produces: `SystemSettingsValues.login_captcha_enabled: boolean`；`SYSTEM_SETTINGS_FIELDS` 含 `'login_captcha_enabled'`；`weakerProtectionReasons` 对"开启→关闭"返回 `'关闭登录验证码，登录不再要求完成安全验证'`

- [ ] **Step 1: 写失败测试**

`web/src/api/systemSettings.test.ts`：

1. `settings()` 工厂（第 20-36 行）在 `database_gateway_client_tls_mode` 之后加入 `login_captcha_enabled: false,`。

2. 在 `'changing the database and Redis client message limit requires confirmation'` 测试之后追加：

```ts
test('disabling login captcha requires confirmation', () => {
  const current = settings({ login_captcha_enabled: true });
  const next = settings({ login_captcha_enabled: false });

  assert.equal(SYSTEM_SETTINGS_FIELDS.includes('login_captcha_enabled'), true);
  assert.deepEqual(weakerProtectionReasons(current, next), [
    '关闭登录验证码，登录不再要求完成安全验证',
  ]);
  assert.deepEqual(weakerProtectionReasons(next, current), []);
});
```

- [ ] **Step 2: 运行测试确认失败**

Run（在 `web/` 目录）: `npm run test:connection-commands`
Expected: FAIL —— `settings()` 类型错误（`SystemSettingsValues` 缺字段）。

- [ ] **Step 3: 实现**

`web/src/api/systemSettings.ts`：

1. `SystemSettingsValues` 接口在 `database_gateway_client_tls_mode` 之后加入：

```ts
  login_captcha_enabled: boolean;
```

2. `SYSTEM_SETTINGS_FIELDS` 常量在 `'database_gateway_client_tls_mode'` 之后加入 `'login_captcha_enabled',`。

3. `weakerProtectionReasons` 函数开头（`const reasons: string[] = [];` 之后）加入：

```ts
  if (current.login_captcha_enabled && !next.login_captcha_enabled) {
    reasons.push('关闭登录验证码，登录不再要求完成安全验证');
  }
```

- [ ] **Step 4: 运行测试确认通过**

Run（在 `web/` 目录）: `npm run test:connection-commands`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/api/systemSettings.ts web/src/api/systemSettings.test.ts
git commit -m "feat: 前端接入登录验证码系统设置字段

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: 前端系统设置页面

系统设置页面新增「登录安全」区块，提供验证码开关并随表单保存。

**Files:**
- Modify: `web/src/views/SystemSettingsView.vue`
- Test: `web/src/views/SystemSettingsView.mount.test.ts`

**Interfaces:**
- Consumes: `SystemSettingsValues.login_captcha_enabled`（Task 6）
- Produces: `form.login_captcha_enabled` 参与 `changedSystemSettingsFields` 差异与保存；`FIELD_LABELS.login_captcha_enabled = '登录验证码'`

- [ ] **Step 1: 写失败测试**

`web/src/views/SystemSettingsView.mount.test.ts`：

1. `buildState` 的 `desired` 与 `effective` 对象在 `database_gateway_client_tls_mode` 之后各加入 `login_captcha_enabled: false,`。

2. 在 `describe('SystemSettingsView header', ...)` 之后追加：

```ts
describe('SystemSettingsView login captcha setting', () => {
  it('shows the login captcha switch under a 登录安全 section', async () => {
    mocks.apiClient.getSystemSettings.mockResolvedValue(buildState(false))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('登录安全')
    expect(wrapper.text()).toContain('登录验证码')
    const captchaRow = wrapper
      .findAll('.setting-row')
      .find(row => row.text().includes('登录验证码'))
    expect(captchaRow).toBeDefined()
    expect(captchaRow!.find('input[type="checkbox"]').exists()).toBe(true)
  })

  it('saves the switch change to the server', async () => {
    mocks.apiClient.getSystemSettings.mockResolvedValue(buildState(false))
    mocks.apiClient.updateSystemSettings.mockResolvedValue(buildState(false))

    const wrapper = mountView()
    await flushPromises()

    const captchaRow = wrapper
      .findAll('.setting-row')
      .find(row => row.text().includes('登录验证码'))
    await captchaRow!.find('input[type="checkbox"]').setValue(true)
    const saveButton = wrapper
      .findAll('button')
      .find(button => button.text() === '保存配置')
    await saveButton!.trigger('click')
    await flushPromises()

    expect(mocks.apiClient.updateSystemSettings).toHaveBeenCalledWith({
      settings: expect.objectContaining({ login_captcha_enabled: true }),
      expected_revision: 1,
      confirm_risk: false,
    })
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run（在 `web/` 目录）: `npm run test:connection-dialog`
Expected: FAIL —— 页面不包含"登录安全"/"登录验证码"文案。

- [ ] **Step 3: 实现**

`web/src/views/SystemSettingsView.vue`：

1. `FIELD_LABELS`（第 448-461 行）在 `database_gateway_client_tls_mode` 之后加入：

```ts
  login_captcha_enabled: '登录验证码',
```

2. `emptySettings()`（第 509-524 行）在 `database_gateway_client_tls_mode` 之后加入：

```ts
    login_captcha_enabled: false,
```

3. 「代理与审计」tab 的 `.policy-grid` 内、数据库网关 section 之前新增：

```vue
              <section class="settings-section">
                <div class="section-heading">
                  <div>
                    <h2>登录安全</h2>
                    <p>控制管理端登录页面的验证码要求，保存后立即生效。</p>
                  </div>
                </div>

                <div class="setting-row">
                  <div class="setting-copy">
                    <strong>登录验证码</strong>
                    <span>启用后登录页面要求完成 ALTCHA 安全验证，防暴力破解。</span>
                  </div>
                  <el-switch v-model="form.login_captcha_enabled" />
                </div>
              </section>
```

- [ ] **Step 4: 运行测试确认通过**

Run（在 `web/` 目录）: `npm run test:connection-dialog && npm run typecheck`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/views/SystemSettingsView.vue web/src/views/SystemSettingsView.mount.test.ts
git commit -m "feat: 系统设置页面新增登录验证码开关

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: 全量验证与收尾

**Files:** 无新文件。

- [ ] **Step 1: 后端全量测试**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS。

- [ ] **Step 2: 前端全量验证**

Run（在 `web/` 目录）: `npm run typecheck && npm run test:connection-commands && npm run test:connection-dialog`
Expected: 全部 PASS。

- [ ] **Step 3: 核对配置示例注释**

确认 `configs/config.example.json` 中 `admin.login_captcha_enabled` 保持 `false`，如该文件含字段说明注释则补充一句"该字段仅作为首次初始化的默认值，可在系统设置中修改，保存后立即生效"。若有改动，提交：

```bash
git add configs/config.example.json
git commit -m "docs: 说明验证码配置仅作为首次初始化默认值

Co-Authored-By: Claude <noreply@anthropic.com>"
```

（无改动则跳过本步。）

- [ ] **Step 4: 最终提交确认**

Run: `git status --short && git log --oneline -8`
Expected: 工作区干净（除忽略文件外），最近 8 条提交为本次功能各任务的提交记录。

## 参考：设计与现状核对

- 设计文档：`docs/superpowers/specs/2026-08-06-login-captcha-setting-design.md`
- 关键机制：`bootstrapSystemSettings`（`cmd/jianmen/system_settings_runtime.go`）启动时把 DB 生效值写回 `cfg`；`admin.Server.New` 以该值初始化原子开关；保存时 service `Update` 热加载路径调用 applier 更新原子开关，无需重启。
