# 测试连接功能设计文档

日期: 2026-06-27

## 需求

在新增/编辑主机账号的弹窗中，增加"测试连接"按钮，点击后用表单中填写的值调用后端 API 测试 SSH 连接是否能够正常建立。

## 范围

- 新增账号弹窗：支持测试连接
- 编辑账号弹窗：支持测试连接
- 直接用表单值测试，不保存账号
- 显示 loading 状态，等待后端返回后显示成功/失败（10 秒超时）

## 后端设计

### API 端点

`POST /api/targets/test-connection`

**请求体：** 与创建/更新账号相同的 payload 结构
```json
{
  "host": "192.168.1.1",
  "port": 22,
  "username": "root",
  "password": "xxx",
  "private_key_pem": "-----BEGIN RSA PRIVATE KEY-----...",
  "passphrase": "xxx",
  "insecure_ignore_host_key": false,
  "host_key_fingerprint": "SHA256:..."
}
```

**成功响应 (200):**
```json
{ "ok": true, "message": "连接成功 (192.168.1.1:22)" }
```

**失败响应 (200):**
```json
{ "ok": false, "message": "连接失败: dial tcp 192.168.1.1:22: i/o timeout" }
```

**行为：**
- 不持久化任何数据
- 用传入参数构建 SSH ClientConfig，发起连接
- 连接成功后立即断开
- 10 秒连接超时
- 复用现有的 `ClientConfigForTarget()` 和 `ssh.Dial()` 逻辑
- RBAC 权限检查使用 `target:create` 操作

### 实现文件

- `internal/server/admin/server.go` — 新增 `handleTestConnection` handler，注册路由
- 复用 `internal/access/static.go` 中的 `ClientConfigForTarget()`

## 前端设计

### 按钮位置

在账号表单弹窗（`HostsView.vue`）底部按钮区，取消和确认之间增加"测试连接"按钮。

### 交互流程

1. 用户填写表单（至少填写主机地址、端口、用户名、认证方式及凭证）
2. 点击"测试连接"按钮
3. 按钮进入 loading 状态（disabled + 转圈图标），显示"正在测试..."
4. 调用 `POST /api/targets/test-connection`，传入当前表单数据构建的 payload
5. 成功：ElMessage.success 显示绿色提示
6. 失败：ElMessage.error 显示红色提示，包含具体错误原因
7. 按钮恢复可用状态

### 不依赖表单验证

测试连接按钮不触发 el-form 的 validate，允许用户在表单未完全填写时测试（因为测试只需要连接参数，不需要账号名称等元数据字段）。

### 实现文件

- `web/src/views/HostsView.vue` — 添加测试连接按钮和处理函数
- `web/src/api/client.ts` — 添加 `testTargetConnection` API 方法

## 测试

- 后端：新增 `TestHandleTestConnection` 测试用例
- 前端：确保 `npm run typecheck` 和 `npm run build` 通过
- 后端：确保 `go test ./... -count=1` 通过
