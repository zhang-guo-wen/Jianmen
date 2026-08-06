# 登录验证码开关纳入系统配置（热加载）设计

日期：2026-08-06
状态：已批准

## 背景与目标

登录页面的验证码功能目前由配置文件 `admin.login_captcha_enabled` 控制（默认关闭），只能在部署时修改。目标：

1. 将验证码开关纳入系统配置，可在「系统设置」页面修改；
2. 保存后**立即生效（热加载）**，同时持久化到数据库，重启后保持一致；
3. 数据库是权威源，内存是运行时读取源，只在启动加载与保存时写入内存，登录流程零数据库访问。

## 现状分析

- 验证码开关定义在 `internal/config/config.go` 的 `AdminConfig.LoginCaptchaEnabled`，默认 `false`；
- 登录处理器 `internal/server/admin/init.go` 的 `handleLogin`、`handleLoginCaptchaChallenge`、`handleInitStatus` 均直接读取 `s.cfg.Admin.LoginCaptchaEnabled`；
- 系统设置机制（`internal/handler/systemsettings` + `internal/service/system_setting.go`）已有完整链路：DB 持久化（`model.SystemSetting`）→ 启动时 `bootstrapSystemSettings` 加载 → `applySystemSettings` 写回 `cfg`，并具备修订历史、`pending_restart` 状态、风险二次确认；
- 现有系统设置**全部为重启生效**，无热加载能力。

## 设计

### 1. 热加载机制（服务层新增能力）

`service.SystemSettingsService` 增加两个能力，与现有"重启生效"路径并存：

- **可热加载字段集合**：本设计先包含 `login_captcha_enabled` 一个字段（后续其他字段可按需扩展）；
- **运行时应用器**：`RegisterHotReloadApplier(fn func(SystemSettings) error)` 注册回调，用于更新依赖该配置的运行时组件。

`Update` 流程改造：

1. 原有校验、乐观锁（revision）、风险二次确认逻辑不变；
2. DB 持久化成功后，若**变更字段全部属于可热加载集合且应用器已注册**：
   - 调用应用器更新运行时组件；
   - 更新内存 `effective` 与 `effectiveRevision`；
   - `MarkSystemSettingApplied` 记录应用时间；
   - 返回 `pending_restart=false`（保存即生效）；
3. 否则保持原行为：仅更新 DB，内存生效值不变，返回 `pending_restart=true`（重启生效）。

`stateFromModel` 的 `pending_restart` 计算（`persisted.Revision != s.effectiveRevision`）天然适配两种路径，无需改动。

启动行为不变：`bootstrapSystemSettings` 加载 DB 生效值到内存并写回 `cfg`；表为空时以配置文件为 baseline 初始化（配置文件仅作首次默认值来源，已有部署升级后新字段默认关闭）。

### 2. 登录开关改为内存原子值（admin.Server）

- `internal/server/admin/server.go`：`Server` 增加 `loginCaptchaEnabled atomic.Bool` 字段，`New` 时以 `cfg.Admin.LoginCaptchaEnabled` 初始化；
- 新增 `ApplyLoginCaptchaEnabled(enabled bool)` 方法（热加载应用器落点）；
- `internal/server/admin/init.go`：`handleLogin`、`handleLoginCaptchaChallenge`、`handleInitStatus` 改读 `s.loginCaptchaEnabled.Load()`；
- `cmd/jianmen/admin_runtime.go`：`admin.New` 之后注册应用器：

```go
settings.RegisterHotReloadApplier(func(settings service.SystemSettings) error {
    adminServer.ApplyLoginCaptchaEnabled(settings.LoginCaptchaEnabled)
    return nil
})
```

### 3. 配置字段全链路接入

在现有 SystemSettings 全链路增加 `login_captcha_enabled`（bool，默认关闭）：

| 层 | 文件 | 改动 |
|---|---|---|
| 数据模型 | `internal/model/system_setting.go` | `SystemSetting` 增加 `LoginCaptchaEnabled bool` |
| 服务层 | `internal/service/system_setting.go` | `SystemSettings` 增加字段；`systemSettingFieldNames`、`changedSystemSettingFields`、`systemSettingModel`/`systemSettingsFromModel` 同步映射；`riskySystemSettingFields` 中"从开启改为关闭"记为风险项（需二次确认） |
| 运行时映射 | `cmd/jianmen/system_settings_runtime.go` | `systemSettingsFromConfig` 读 `cfg.Admin.LoginCaptchaEnabled`；`applySystemSettings` 写回 |
| HTTP 层 | `internal/handler/systemsettings/view.go` | `settingsValues`/`settingsValuesRequest` 增加字段；`toService`/`mapValues` 同步（全字段必填，跟随现有模式） |
| 前端 API | `web/src/api/systemSettings.ts` | `SystemSettingsValues`、`SYSTEM_SETTINGS_FIELDS` 增加字段；`weakerProtectionReasons` 增加"关闭登录验证码"风险提示 |
| 前端页面 | `web/src/views/SystemSettingsView.vue` | "代理与审计"tab 新增「登录安全」section，`el-switch` 开关，说明文案注明保存即生效；`emptySettings`、`FIELD_LABELS` 同步 |

前端保存成功提示无需改动：现有逻辑已按 `pending_restart` 区分"已保存，重启后生效"与"已保存"。

### 4. 数据流

```
保存开关（系统设置页面）
  → PUT /api/system-settings（含 expected_revision、confirm_risk）
  → service.Update：校验 → 乐观锁 → 写 DB（revision+1）
  → 变更字段可热加载 → 调用应用器（admin.Server.ApplyLoginCaptchaEnabled）
  → 更新内存 effective → MarkSystemSettingApplied → 返回 pending_restart=false

登录流程（每次请求，零 DB 访问）
  → handleLogin / handleInitStatus 读 Server.loginCaptchaEnabled（内存原子值）

重启后
  → bootstrapSystemSettings 从 DB 加载生效值 → 写回 cfg → New() 初始化原子值
```

### 5. 错误处理

- 应用器执行失败：返回错误，DB 已更新而内存未生效，`pending_restart` 将显示为待重启（可重启恢复一致）；验证码应用器仅为原子写，实际不会失败；
- 热加载字段与应用器未注册（防御性）：退回"重启生效"路径，行为与现状一致。

### 6. 升级与兼容

- 已有部署升级：`SystemSetting` 表新增列默认为 `false`（关闭），与"默认关闭"一致；配置文件 `login_captcha_enabled` 仅作首次初始化默认值，不再影响已有持久化记录；
- 配置文件字段保留，语义变为"首次初始化的默认值"。

## 测试计划

- 服务层：`internal/service/system_setting_test.go` 增加热加载用例（更新验证码开关 → applier 被调用、`pending_restart=false`、内存 `effective` 更新）；含不可热加载字段时仍走重启生效路径；applier 未注册时回退；
- admin 服务：`internal/server/admin/server_test.go` 中验证码相关用例改用 `ApplyLoginCaptchaEnabled`（原直接改 `cfg` 的写法同步更新）；
- HTTP 层：`internal/handler/systemsettings/handler_test.go` 增加新字段映射断言；
- 运行时映射：`cmd/jianmen/system_settings_runtime_test.go` 覆盖 `systemSettingsFromConfig`/`applySystemSettings`；
- 前端：`web/src/api/systemSettings.test.ts`、`web/src/views/SystemSettingsView.mount.test.ts` 同步更新；
- 验证码登录流程测试（`internal/service/login_captcha_test.go`、`internal/server/admin/server_test.go` 已有）不涉及开关读取路径，应保持通过。

## 不改动

- 登录页面 `web/src/views/LoginView.vue`（读 `handleInitStatus` 返回的 `login_captcha_enabled`，该值改为读内存原子值后语义不变）；
- 验证码服务 `internal/service/login_captcha.go`；
- 登录限流、审计（`login_security.go`）。
