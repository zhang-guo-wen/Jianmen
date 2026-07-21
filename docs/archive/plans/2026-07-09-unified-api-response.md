# 统一 API 响应格式 - 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将所有 API 响应统一为标准云 API 风格格式（code + data/error + message + request_id + timestamp），替换当前各种不一致的裸 JSON 返回格式。

**Architecture:** 后端新增 `internal/pkg/apiresp/` 包定义统一响应结构和写响应工具函数；中间件层面注入 request_id 到 context；所有 handler 迁移到新的写响应函数；前端 `client.ts` 统一解析 envelope，错误时构造结构化错误对象。

**Tech Stack:** Go 1.x + net/http（标准库）, TypeScript + Vue 3, 不引入外部依赖

## Global Constraints

- 不引入任何新的第三方依赖
- Go 侧使用标准库 `net/http`，不使用框架
- 前端使用 fetch API（已有），不引入 axios 等新库
- 所有现有功能不变，只是响应格式变化
- 前端 `npm run typecheck` 和 `npm run build` 必须通过
- 后端 `go build ./...` 和 `go test ./... -count=1` 必须通过

---

### Task 1: 新建统一 API 响应包 `internal/pkg/apiresp/`

**Files:**
- Create: `internal/pkg/apiresp/apiresp.go`

**Interfaces:**
- Produces:
  - `type Envelope struct { Code int; Data any; Message string; RequestID string; Timestamp string }`
  - `type APIError struct { Code string; Message string; Details any }`
  - Error code 常量：`ErrCodeValidation`, `ErrCodeUnauthorized`, `ErrCodeForbidden`, `ErrCodeNotFound`, `ErrCodeConflict`, `ErrCodeInternal`, `ErrCodeServiceUnavailable`, `ErrCodeTooManyRequests`, `ErrCodePreconditionFailed`, `ErrCodeBadGateway`
  - `func Write(w http.ResponseWriter, status int, data any, reqID string)`
  - `func WriteError(w http.ResponseWriter, status int, errCode string, message string, details any, reqID string)`
  - `type ctxKeyRequestID` - context key for request ID

**Why this package:** 它被 handler、server、middleware 层共同使用，放在 internal 下避免外部依赖

- [ ] **Step 1: 创建 apiresp.go**

```go
package apiresp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ── 统一响应结构 ─────────────────────────────────────────────

// Envelope 成功响应封装
type Envelope struct {
	Code      int    `json:"code"`       // 0 表示成功
	Data      any    `json:"data"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// ErrorBody 错误详情
type ErrorBody struct {
	Code    string `json:"code"`    // 错误码：VALIDATION_ERROR, NOT_FOUND 等
	Message string `json:"message"` // 人类可读的错误描述
	Details any    `json:"details,omitempty"`
}

// ErrorEnvelope 错误响应封装
type ErrorEnvelope struct {
	Code      int       `json:"code"`       // 对应 HTTP 状态码
	Error     ErrorBody `json:"error"`
	RequestID string    `json:"request_id"`
	Timestamp string    `json:"timestamp"`
}

// ── 错误码常量 ──────────────────────────────────────────────

const (
	CodeValidation           = "VALIDATION_ERROR"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeForbidden            = "FORBIDDEN"
	CodeNotFound             = "NOT_FOUND"
	CodeConflict             = "CONFLICT"
	CodeInternal             = "INTERNAL_ERROR"
	CodeServiceUnavailable   = "SERVICE_UNAVAILABLE"
	CodeTooManyRequests      = "TOO_MANY_REQUESTS"
	CodePreconditionFailed   = "PRECONDITION_FAILED"
	CodeBadGateway           = "BAD_GATEWAY"
	CodeMethodNotAllowed     = "METHOD_NOT_ALLOWED"
)

// ── Context key ─────────────────────────────────────────────

type ctxKey string

const CtxKeyRequestID ctxKey = "request_id"

// ── Write 辅助函数 ──────────────────────────────────────────

// Write 写成功响应
func Write(w http.ResponseWriter, status int, data any, reqID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Code:      0,
		Data:      data,
		Message:   "ok",
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// WriteError 写错误响应
func WriteError(w http.ResponseWriter, status int, errCode string, message string, details any, reqID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Code: status,
		Error: ErrorBody{
			Code:    errCode,
			Message: message,
			Details: details,
		},
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// RequestID 从 context 中提取 request_id
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return id
	}
	return ""
}
```

- [ ] **Step 2: 验证编译**

Run: `go build ./internal/pkg/apiresp/`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/apiresp/apiresp.go
git commit -m "feat: add unified API response package (apiresp)"
```

---

### Task 2: 新增 request_id 中间件，改写 writeJSON/writeError/writeErrorText

**Files:**
- Create: `internal/server/admin/middleware.go`
- Modify: `internal/server/admin/response_utils.go`

**Interfaces:**
- Consumes: `apiresp.CtxKeyRequestID`, `apiresp.RequestID`, `apiresp.Write`, `apiresp.WriteError`
- Produces: `func (s *Server) writeJSON(w, status, value)`, `func (s *Server) writeError(w, status, errCode, message, details)`, `func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc`

- [ ] **Step 1: 创建 middleware.go**

```go
package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"jianmen/internal/pkg/apiresp"
)

// requestIDMiddleware 为每个请求注入短 UUID 作为 request_id
func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		reqID := hex.EncodeToString(b)
		ctx := r.Context()
		ctx = context.WithValue(ctx, apiresp.CtxKeyRequestID, reqID)
		next(w, r.WithContext(ctx))
	}
}
```

Wait — but we need to import `context`. Let me fix:

```go
package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"jianmen/internal/pkg/apiresp"
)

// requestIDMiddleware 为每个请求注入短 UUID 作为 request_id
func requestIDMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		reqID := hex.EncodeToString(b)
		ctx := context.WithValue(r.Context(), apiresp.CtxKeyRequestID, reqID)
		next(w, r.WithContext(ctx))
	}
}
```

- [ ] **Step 2: 重写 response_utils.go 的 writeJSON/writeError/writeErrorText**

当前 `writeJSON` 是包级函数，需要改为 Server 方法以支持从 request context 提取 request_id。同时保留旧函数作为过渡（标记 deprecated）。

替换 `response_utils.go` 中的 `writeJSON`、`writeError`、`writeErrorText`：

```go
package admin

import (
	// ... 保留现有 import ...
	"jianmen/internal/pkg/apiresp"
)

// writeJSON 写统一格式成功响应
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	reqID := apiresp.RequestID(r.Context())
	apiresp.Write(w, status, value, reqID)
}

// writeError 写统一格式错误响应
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, errCode string, message string, details any) {
	reqID := apiresp.RequestID(r.Context())
	apiresp.WriteError(w, status, errCode, message, details, reqID)
}

// writeErrorText 便捷方法：VALIDATION_ERROR 的 writeError
func (s *Server) writeErrorText(w http.ResponseWriter, r *http.Request, status int, message string) {
	var errCode string
	switch status {
	case http.StatusBadRequest:
		errCode = apiresp.CodeValidation
	case http.StatusUnauthorized:
		errCode = apiresp.CodeUnauthorized
	case http.StatusForbidden:
		errCode = apiresp.CodeForbidden
	case http.StatusNotFound:
		errCode = apiresp.CodeNotFound
	case http.StatusMethodNotAllowed:
		errCode = apiresp.CodeMethodNotAllowed
	case http.StatusConflict:
		errCode = apiresp.CodeConflict
	case http.StatusInternalServerError:
		errCode = apiresp.CodeInternal
	case http.StatusServiceUnavailable:
		errCode = apiresp.CodeServiceUnavailable
	case http.StatusTooManyRequests:
		errCode = apiresp.CodeTooManyRequests
	case http.StatusPreconditionFailed:
		errCode = apiresp.CodePreconditionFailed
	case http.StatusBadGateway:
		errCode = apiresp.CodeBadGateway
	default:
		errCode = apiresp.CodeInternal
	}
	s.writeError(w, r, status, errCode, message, nil)
}
```

同时删除旧的包级函数：
- 删除 `writeJSON(w, status, value)` (line 89-93)
- 删除 `writeError(w, status, err)` (line 95-97)
- 删除 `writeErrorText(w, status, message)` (line 99-101)

注意保留 `writeJSONFile`, `writeTextFile`, `writeJSONLines` 等文件读取函数不动（它们输出文件内容，不需要包裹 envelope）。

旧的 `friendlySSHError` 函数保留不动。

- [ ] **Step 3: 在 Server 路由注入中间件**

修改 `server.go` 的 `ListenAndServe` 方法，给所有 handler 包裹 requestIDMiddleware：

```go
// 在 server.go 的 ListenAndServe 中，将：
// mux.HandleFunc("/api/health", s.withAuthAndUser(s.handleHealth))
// 改为：
// mux.HandleFunc("/api/health", requestIDMiddleware(s.withAuthAndUser(s.handleHealth)))
```

所有路由注册都需要加上 `requestIDMiddleware` 包裹。

更优雅的做法：在 `logRequests` 的外层或创建一个 `chainMiddleware`：

```go
func (s *Server) muxHandle(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, requestIDMiddleware(handler))
}
```

然后把所有 `mux.HandleFunc(...)` 改为 `s.muxHandle(mux, ...)`。

- [ ] **Step 4: 验证编译**

Run: `go build ./...`
Expected: 编译失败（因为所有 handler 中调用 `writeJSON(w, ...)` 等改成了方法调用，需要传 `r` 参数），继续 Task 3 逐步修复。

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin/middleware.go internal/server/admin/response_utils.go internal/server/admin/server.go
git commit -m "feat: add request_id middleware and unified writeJSON/writeError helpers"
```

---

### Task 3: 迁移 init.go handler 到统一响应格式

**Files:**
- Modify: `internal/server/admin/init.go`

**Interfaces:**
- Consumes: `s.writeJSON(w, r, status, value)`, `s.writeErrorText(w, r, status, message)`

这个文件中有一些 handler 使用内联 `writeJSON(w, status, map[string]string{"error": "msg"})` 的写法，需要统一改为 `s.writeErrorText(w, r, status, msg)`。

- [ ] **Step 1: 改造 init.go 所有 handler**

将所有 `writeJSON(w, ...)` 改为 `s.writeJSON(w, r, ...)`，将所有 `writeErrorText(w, ...)` 改为 `s.writeErrorText(w, r, ...)`。

具体位置：
- `handleInitStatus`: line 60 (`writeJSON(w, http.StatusInternalServerError, ...)`) → `s.writeErrorText(w, r, http.StatusInternalServerError, "failed to check setup status")`
- `handleInitStatus`: line 69 (`writeJSON(w, http.StatusOK, resp)`) → `s.writeJSON(w, r, http.StatusOK, resp)`
- `handleLogin`: lines 109, 115, 124, 133, 139 → 改为 `s.writeErrorText(w, r, ...)` 
- `handleLogin`: line 146,154 → 改为 `s.writeErrorText(w, r, ...)`
- `handleLogin`: line 169 (`writeJSON(w, http.StatusOK, ...)`) → `s.writeJSON(w, r, http.StatusOK, ...)`
- `handleInitSetup`: lines 189, 198, 202, 208, 214, 248 → 改为 `s.writeErrorText(w, r, ...)`
- `handleInitSetup`: line 252 → `s.writeErrorText(w, r, http.StatusForbidden, "already initialized")`
- `handleInitSetup`: line 267 (`writeJSON(w, http.StatusCreated, ...)`) → `s.writeJSON(w, r, http.StatusCreated, ...)`
- `handleInitEncryptionKey`: lines 288, 292, 299, 308, 311, 317 → 改为 `s.writeErrorText(w, r, ...)`
- `handleInitEncryptionKey`: line 320 (`writeJSON(w, http.StatusOK, ...)`) → `s.writeJSON(w, r, http.StatusOK, ...)`

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 失败（还有其他 handler 未迁移），但不应该有 init.go 的错误。

- [ ] **Step 3: Commit**

```bash
git add internal/server/admin/init.go
git commit -m "refactor(init): migrate init handlers to unified API response format"
```

---

### Task 4: 迁移 server.go 基础 handler（health/me/index）

**Files:**
- Modify: `internal/server/admin/server.go`

- [ ] **Step 1: 改造 server.go 的 handler**

- `handleIndex` (line 194-195): `writeErrorText(w, ...)` → `s.writeErrorText(w, r, ...)`; `writeJSON(w, ...)` → `s.writeJSON(w, r, ...)`
- `handleHealth` (line 205-208): `writeJSON(w, ...)` → `s.writeJSON(w, r, ...)`
- `handleMe` (lines 214, 219, 222): 改造
- `handleMePermissions` (lines 231, 236, 241, 245, 261, 272): 改造
- `handleMeMenus` (lines 278, 283, 317, 292, 301, 332, 350): 改造

- [ ] **Step 2: 验证编译**

Run: `go build ./...`
Expected: 只剩其他 handler 文件的编译错误

- [ ] **Step 3: Commit**

```bash
git add internal/server/admin/server.go
git commit -m "refactor(server): migrate health/me/index handlers to unified API response format"
```

---

### Task 5: 迁移 user_handlers.go 和 host_handlers.go

**Files:**
- Modify: `internal/server/admin/user_handlers.go`
- Modify: `internal/server/admin/host_handlers.go`

- [ ] **Step 1: 改造 user_handlers.go**

所有 `writeJSON(w, ...)`, `writeError(w, ...)`, `writeErrorText(w, ...)` 改为 `s.writeJSON(w, r, ...)`, `s.writeErrorText(w, r, ...)`。

`writeRBACDBError` 和 `writeHostStoreError` 等辅助函数参数签名不变（它们调用 s.writeErrorText），但要注意这些 helper 函数内部现在需要调用 `s.writeErrorText(w, r, ...)`。

等等 — `writeHostStoreError`, `writeDBStoreError` 等是包级函数（在 `request_utils.go` 中），它们不接收 Server 实例。需要重构。

方案：把这些 store error helper 改为 Server 方法，或者直接在 handler 中展开。

**最简方案：** 在各 handler 文件中保留包级 helper 函数，但内部改为使用 `apiresp` 包直接写响应（不需要 Server 实例）。这样改动最小。

实际上，`apiresp.Write` / `apiresp.WriteError` 是包级函数，不依赖 Server。所以 `writeHostStoreError` 等可以在内部使用 `apiresp`：

```go
func writeHostStoreError(w http.ResponseWriter, r *http.Request, err error) {
    switch {
    case errors.Is(err, store.ErrHostNotFound):
        apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
    default:
        apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
    }
}
```

这样就保持了包级函数的便利性，但需要传 `r` 参数。

简化：直接去掉了这些 helper，在每个 handler 中内联调用 `s.writeErrorText(w, r, status, msg)` 或 `s.writeError(w, r, status, errCode, err.Error(), nil)`。

考虑到改动量，保留 helper 但改造为接收 `r *http.Request` 参数最实际。

- [ ] **Step 1 (继续): 改造 request_utils.go 的 store error helpers**

修改 `writeHostStoreError`, `writeDBStoreError`, `writeTargetStoreError`, `writeApplicationStoreError`, `writeRBACDBError` 签名，加入 `r *http.Request` 参数：

```go
func writeHostStoreError(w http.ResponseWriter, r *http.Request, err error) {
    switch {
    case errors.Is(err, store.ErrHostNotFound):
        apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
    default:
        apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
    }
}
```

- [ ] **Step 2: 改造 user_handlers.go 所有 handler**

所有调用加上 `r` 参数：`writeErrorText(w, ...)` → `s.writeErrorText(w, r, ...)`, `writeError(w, ...)` → `s.writeErrorText(w, r, ..., err.Error())` (当前 `writeError` 把 err.Error() 包进 {"error":"..."}）, `writeJSON(w, ...)` → `s.writeJSON(w, r, ...)`。

对于 `createUser` 中返回 `map[string]any{"user": user, "token": rawToken}` 的写法，改为用 struct：

`writeJSON` 仍然支持 `any` 类型，所以 `s.writeJSON(w, r, http.StatusCreated, map[string]any{"user": user, "token": rawToken})` 完全 OK。

- [ ] **Step 3: 改造 host_handlers.go 所有 handler**

同样的模式。注意 `handleTestConnection` 中返回 `{"ok": false, "message": "..."}` 的模式——这是"业务操作结果"而非"API 错误"，应该仍放在 data 里：

```go
s.writeJSON(w, r, http.StatusOK, map[string]any{"ok": false, "message": "连接失败: " + friendlySSHError(err)})
```

**注意：** `w.WriteHeader(http.StatusNoContent)` 不需要改动——DELETE 返回 204 无 body 是标准做法。

- [ ] **Step 4: 验证编译**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin/user_handlers.go internal/server/admin/host_handlers.go internal/server/admin/request_utils.go
git commit -m "refactor: migrate user and host handlers to unified API response format"
```

---

### Task 6: 迁移 db_handlers.go、db_provision.go 和 test_db.go

**Files:**
- Modify: `internal/server/admin/db_handlers.go`
- Modify: `internal/server/admin/db_provision.go`
- Modify: `internal/server/admin/test_db.go`

- [ ] **Step 1: 改造 db_handlers.go**

所有 `writeJSON(w, ...)` → `s.writeJSON(w, r, ...)`, `writeErrorText(w, ...)` → `s.writeErrorText(w, r, ...)`, `writeError(w, ...)` → `s.writeError(w, r, status, errCode, msg, nil)`。

**重要修复：** `handleDBProvisionAccount` (line 133) 和 `handleDBDatabases` (line 53) 中 `writeError(w, http.StatusBadGateway, err)` → 改为 `s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())`。502 语义对不上，应该用 500。

- [ ] **Step 2: 改造 db_provision.go**

同样模式。

- [ ] **Step 3: 改造 test_db.go**

同样模式。注意 `{"ok": false, "error": ..., "latency_ms": ...}` 这种测试连接的结果放在 data 里。

- [ ] **Step 4: 验证编译**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin/db_handlers.go internal/server/admin/db_provision.go internal/server/admin/test_db.go
git commit -m "refactor: migrate DB handlers to unified API response format, fix 502→500"
```

---

### Task 7: 迁移 session_handlers.go、audit_handlers.go 和 app_handlers.go

**Files:**
- Modify: `internal/server/admin/session_handlers.go`
- Modify: `internal/server/admin/audit_handlers.go`
- Modify: `internal/server/admin/app_handlers.go`

- [ ] **Step 1: 改造 session_handlers.go**

所有 write 调用改为加 `r` 参数。注意 `forbidden` 方法（server.go 中）也需要改为 Server 方法。

- [ ] **Step 2: 改造 audit_handlers.go**

同样模式。

- [ ] **Step 3: 改造 app_handlers.go**

同样模式。注意 `w.WriteHeader(http.StatusNoContent)` 保持不变。

- [ ] **Step 4: 验证编译**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/server/admin/session_handlers.go internal/server/admin/audit_handlers.go internal/server/admin/app_handlers.go
git commit -m "refactor: migrate session/audit/app handlers to unified API response format"
```

---

### Task 8: 迁移 rbac.go 和 auth_context.go

**Files:**
- Modify: `internal/server/admin/rbac.go`
- Modify: `internal/server/admin/auth_context.go`

- [ ] **Step 1: 改造 rbac.go**

所有 write 调用加上 `r` 参数。`forbidden` 方法需要改为加 `r` 参数。

- [ ] **Step 2: 改造 auth_context.go**

`withAuthAndUser` 中的 `writeErrorText(w, ...)` 改为直接使用 `apiresp` 包（因为是包级函数，没有 Server 实例）：

```go
// line 18
apiresp.WriteError(w, http.StatusUnauthorized, apiresp.CodeUnauthorized, "missing or invalid bearer token", nil, apiresp.RequestID(r.Context()))
// line 32
apiresp.WriteError(w, http.StatusUnauthorized, apiresp.CodeUnauthorized, "invalid token", nil, apiresp.RequestID(r.Context()))
```

`forbidden` 方法改为 Server 方法并加 `r` 参数：
```go
func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
    s.writeErrorText(w, r, http.StatusForbidden, "forbidden")
}
```

- [ ] **Step 3: 验证编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/server/admin/rbac.go internal/server/admin/auth_context.go
git commit -m "refactor: migrate RBAC and auth handlers to unified API response format"
```

---

### Task 9: 后端测试验证

**Files:**
- 检查: `internal/server/admin/*_test.go`

- [ ] **Step 1: 运行全部后端测试**

Run: `go test ./... -count=1`
Expected: 所有测试通过

- [ ] **Step 2: 检查测试文件是否也需要更新**

查看 `server_test.go`、`rbac_test.go`、`webterminal_test.go` 是否有直接调用 `writeJSON` / `writeErrorText` 的地方。如有，更新测试代码。

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "test: verify backend tests pass after API response format migration"
```

---

### Task 10: 前端 client.ts 适配新的统一响应格式

**Files:**
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Consumes: 新的响应格式 `{code, data, message, request_id, timestamp}` 和 `{code, error, request_id, timestamp}`
- Produces: `ApiResponse<T>` 类型, `ApiError` 类, 更新的 `request()` 函数

- [ ] **Step 1: 重写 client.ts 的类型和 request 函数**

```typescript
const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');
const TOKEN_KEY = 'jianmen_token';

// ── 新的统一响应格式 ──────────────────────────────────────────

export interface ApiEnvelope<T = unknown> {
  code: number;        // 0 = 成功
  data: T;
  message: string;
  request_id: string;
  timestamp: string;
}

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: unknown;
}

export interface ApiErrorEnvelope {
  code: number;        // HTTP 状态码
  error: ApiErrorBody;
  request_id: string;
  timestamp: string;
}

export class ApiError extends Error {
  code: string;
  statusCode: number;
  requestId: string;
  details?: unknown;

  constructor(statusCode: number, errorCode: string, message: string, requestId: string, details?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.code = errorCode;
    this.requestId = requestId;
    this.details = details;
  }
}

export interface PageResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// ... 保留所有现有的接口定义（HealthResponse, UserRecord 等）不变 ...

// ── token helpers ──────────────────────────────────────────────

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? '';
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

// ── request core ───────────────────────────────────────────────

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers = new Headers(init.headers);

  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json');
  }

  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers
  });

  // 204 No Content - 无 body
  if (response.status === 204) {
    return undefined as T;
  }

  const contentType = response.headers.get('content-type') ?? '';
  if (!contentType.includes('application/json')) {
    // 非 JSON 响应（如 asciicast replay 文件）
    if (!response.ok) {
      throw new ApiError(response.status, 'UNKNOWN', response.statusText, '');
    }
    return (await response.text()) as unknown as T;
  }

  const payload = await response.json();

  if (!response.ok) {
    // 新格式：{code, error: {code, message, details}, request_id, timestamp}
    if (payload && typeof payload === 'object' && 'error' in payload) {
      const errBody = payload.error as ApiErrorBody;
      throw new ApiError(
        response.status,
        errBody.code || 'UNKNOWN',
        errBody.message || response.statusText,
        payload.request_id || ''
      );
    }
    // 兼容旧格式：{error: "msg"}
    if (payload && typeof payload === 'object' && 'error' in payload) {
      throw new ApiError(response.status, 'UNKNOWN', String(payload.error), '');
    }
    throw new ApiError(response.status, 'UNKNOWN', response.statusText, '');
  }

  // 成功：从 {code: 0, data: ..., message: "ok", request_id: "...", timestamp: "..."} 中提取 data
  if (payload && typeof payload === 'object' && 'code' in payload && payload.code === 0) {
    if (response.status === 401) {
      clearToken();
      if (window.location.pathname !== '/login') {
        window.location.href = '/login';
      }
    }
    return payload.data as T;
  }

  // 兼容旧格式：直接用原始 payload（逐步淘汰）
  return payload as T;
}

// ── API client ─────────────────────────────────────────────────
// 所有方法签名保持不变，但返回类型不再需要 ApiEnvelope<T> | T 的联合类型

export const apiClient = {
  // health
  getHealth: () => request<HealthResponse>('/api/health'),

  // users
  getUsers: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<UserRecord>>(`/api/users${buildQS(params as Record<string, string | number | undefined>)}`),
  createUser: (payload: UserPayload) =>
    request<{ user: UserRecord; token: string }>('/api/users', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),
  updateUser: (id: string | number, payload: UserPayload) =>
    request<UserRecord>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  deleteUser: (id: string | number) =>
    request<void>(`/api/users/${encodeURIComponent(String(id))}`, {
      method: 'DELETE',
    }),

  // me
  getMyMenus: () => request<MyMenusResponse>('/api/me/menus'),
  getMyPermissions: () => request<{ actions: string[] }>('/api/me/permissions'),

  // hosts
  getHosts: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<HostView>>(`/api/hosts${buildQS(params as Record<string, string | number | undefined>)}`),
  createHost: (payload: HostPayload) =>
    request<HostView>('/api/hosts', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateHost: (id: string | number, payload: HostPayload) =>
    request<HostView>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteHost: (id: string | number) =>
    request<void>(`/api/hosts/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  // host accounts
  getHostAccounts: (id: string | number, params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<TargetRecord>>(`/api/hosts/${encodeURIComponent(String(id))}/accounts${buildQS(params as Record<string, string | number | undefined>)}`),

  // targets
  getTargets: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<TargetRecord>>(`/api/targets${buildQS(params as Record<string, string | number | undefined>)}`),
  getTarget: (id: string | number) =>
    request<TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`),
  createTarget: (payload: TargetPayload) =>
    request<TargetRecord>('/api/targets', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateTarget: (id: string | number, payload: TargetPayload) =>
    request<TargetRecord>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteTarget: (id: string | number) =>
    request<void>(`/api/targets/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  testTargetConnection: (payload: TargetPayload) =>
    request<TestConnectionResult>('/api/targets/test-connection', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),

  // sessions
  createUserSession: (targetId: string) =>
    request<UserSessionRecord>('/api/user-sessions', {
      method: 'POST',
      body: JSON.stringify({ target_id: targetId })
    }),
  getSessions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<SessionRecord>>(`/api/audit/ssh${buildQS(params as Record<string, string | number | undefined>)}`),
  getSessionMeta: (id: string | number) =>
    request<SessionMetaRecord>(`/api/audit/ssh/${encodeURIComponent(String(id))}`),
  getSessionCommands: (id: string | number) =>
    request<SessionCommandRecord[]>(`/api/audit/ssh/${encodeURIComponent(String(id))}/commands`),
  getSessionFiles: (id: string | number) =>
    request<SessionFileEventRecord[]>(`/api/audit/ssh/${encodeURIComponent(String(id))}/files`),
  getSessionFileSummary: (id: string | number) =>
    request<Record<string, unknown>>(`/api/audit/ssh/${encodeURIComponent(String(id))}/files`),
  getSessionReplay: (id: string | number) =>
    request<string>(`/api/audit/ssh/${encodeURIComponent(String(id))}/replay`),

  // database gateway & instances
  getDBGateway: () => request<DBGatewayConfig>('/api/db/gateway'),
  getDBInstances: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DatabaseInstanceView>>(`/api/db/instances${buildQS(params as Record<string, string | number | undefined>)}`),
  createDBInstance: (payload: DBInstancePayload) =>
    request<DatabaseInstanceView>('/api/db/instances', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateDBInstance: (id: string, payload: DBInstancePayload & { status?: string }) =>
    request<DatabaseInstanceView>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBInstance: (id: string) =>
    request<void>(`/api/db/instances/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),

  // database accounts
  getDBAccounts: (instanceID: string, params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DBAccountRecord>>(`/api/db/instances/${encodeURIComponent(instanceID)}/accounts${buildQS(params as Record<string, string | number | undefined>)}`),
  createDBAccount: (instanceID: string, payload: DBAccountPayload) =>
    request<DBAccountRecord>(`/api/db/instances/${encodeURIComponent(instanceID)}/accounts`, {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  getDBAccount: (id: string) =>
    request<DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`),
  updateDBAccount: (id: string, payload: DBAccountUpdatePayload) =>
    request<DBAccountRecord>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteDBAccount: (id: string) =>
    request<void>(`/api/db/accounts/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  testDBConnection: (id: string) =>
    request<{ ok: boolean; error?: string; latency_ms: number }>(`/api/db/accounts/test/${encodeURIComponent(id)}`, { method: 'POST' }),
  testDBConnectionPayload: (payload: DBAccountTestPayload) =>
    request<{ ok: boolean; error?: string; latency_ms: number }>('/api/db/accounts/test', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),

  // auto-provision
  listDBDatabases: (instanceId: string, adminAccountId: string) =>
    request<{ databases: string[] }>(`/api/db/instances/${encodeURIComponent(instanceId)}/databases?admin_account_id=${encodeURIComponent(adminAccountId)}`),
  provisionDBAccount: (instanceId: string, payload: {
    admin_account_id: string
    new_username?: string
    password?: string
    host?: string
    grants: Array<{ database: string; privilege: string }>
    group?: string
    remark?: string
    expires_at?: string
  }) =>
    request<{ ok: boolean; account: unknown; generated_password: string }>(`/api/db/instances/${encodeURIComponent(instanceId)}/provision-account`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  // database connections (audit)
  getDBConnections: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<DBConnectionRecord>>(`/api/audit/db${buildQS(params as Record<string, string | number | undefined>)}`),
  getDBConnectionMeta: (id: string | number) =>
    request<DBConnectionMetaRecord>(`/api/audit/db/${encodeURIComponent(String(id))}`),
  getDBConnectionQueries: (id: string | number) =>
    request<DBQueryEventRecord[]>(`/api/audit/db/${encodeURIComponent(String(id))}/queries`),

  // applications
  getApplications: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<ApplicationView>>(`/api/applications${buildQS(params as Record<string, string | number | undefined>)}`),
  createApplication: (payload: ApplicationPayload) =>
    request<ApplicationView>('/api/applications', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateApplication: (id: string, payload: ApplicationPayload & { status?: string }) =>
    request<ApplicationView>(`/api/applications/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteApplication: (id: string) =>
    request<void>(`/api/applications/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),

  // rbac
  getRBACRoles: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACRoleRecord>>(`/api/rbac/roles${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACRole: (payload: RBACRolePayload) =>
    request<RBACRoleRecord>('/api/rbac/roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateRBACRole: (id: string | number, payload: RBACRolePayload) =>
    request<RBACRoleRecord>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    }),
  deleteRBACRole: (id: string | number) =>
    request<void>(`/api/rbac/roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  getRBACPermissions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACPermissionRecord>>(`/api/rbac/permissions${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACPermission: (payload: RBACPermissionPayload) =>
    request<RBACPermissionRecord>('/api/rbac/permissions', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACPermission: (id: string | number) =>
    request<void>(`/api/rbac/permissions/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  getRBACUserRoles: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACUserRoleRecord>>(`/api/rbac/user-roles${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACUserRole: (payload: RBACUserRolePayload) =>
    request<RBACUserRoleRecord>('/api/rbac/user-roles', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACUserRole: (id: string | number) =>
    request<void>(`/api/rbac/user-roles/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),

  getRBACRolePermissions: (params?: { page?: number; page_size?: number; q?: string }) =>
    request<PageResponse<RBACRolePermissionRecord>>(`/api/rbac/role-permissions${buildQS(params as Record<string, string | number | undefined>)}`),
  createRBACRolePermission: (payload: RBACRolePermissionPayload) =>
    request<RBACRolePermissionRecord>('/api/rbac/role-permissions', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  deleteRBACRolePermission: (id: string | number) =>
    request<void>(`/api/rbac/role-permissions/${encodeURIComponent(String(id))}`, {
      method: 'DELETE'
    }),
  checkRBACEffective: (payload: RBACEffectiveCheckPayload) => {
    const params = new URLSearchParams({
      user_id: payload.user_id,
      action: payload.action
    });
    if (payload.resource_type) {
      params.set('resource_type', payload.resource_type);
    }
    if (payload.resource_id) {
      params.set('resource_id', payload.resource_id);
    }
    return request<RBACEffectiveCheckResult>(`/api/rbac/effective?${params.toString()}`);
  },

  // auth & init
  login: (username: string, password: string) =>
    request<{ token: string }>('/api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  getInitStatus: () => request<InitStatusResponse>('/api/init/status'),
  setup: (payload: { username: string; password: string; email: string; display_name?: string }) =>
    request<{ token: string }>('/api/init/setup', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  getEncryptionKey: () => request<{ key: string }>('/api/init/encryption-key')
};
```

- [ ] **Step 2: 导出 ApiError 类**

确保 `ApiError` 在文件顶部正确导出，以便 View 层使用。

- [ ] **Step 3: 验证 typecheck 和 build**

Run: `npm run typecheck`
Expected: 通过（可能有 View 层的类型错误，Task 11-13 处理）

Run: `npm run build`
Expected: 通过

- [ ] **Step 4: Commit**

```bash
git add web/src/api/client.ts
git commit -m "refactor(frontend): adapt client.ts to unified API response format, add ApiError class"
```

---

### Task 11: 适配前端 View 层 - 移除 ApiEnvelope 解包

**Files:**
- Modify: `web/src/views/HostsView.vue`
- Modify: `web/src/views/DatabaseView.vue`
- Modify: `web/src/views/UsersView.vue`
- Modify: `web/src/views/RolesView.vue`
- Modify: `web/src/views/RBACView.vue`
- Modify: `web/src/views/QuickConnectView.vue`
- Modify: `web/src/views/AuditView.vue`
- Modify: `web/src/views/LoginView.vue`
- Modify: `web/src/views/SetupView.vue`
- Modify: `web/src/views/ApplicationsView.vue`
- Modify: `web/src/stores/permission.ts`

- [ ] **Step 1: 搜索所有使用 `ApiEnvelope` 或 `.data` 解包的地方**

在每个 View 中搜索以下模式：
- `ApiEnvelope<...>` 类型引用 → 删除
- `const { data } = await apiClient.xxx()` → `const data = await apiClient.xxx()`
- `.then(res => res.data)` → `.then(data => ...)`
- `res.ok` / `res.error` 检查 → 检查 `instanceof ApiError`

因为 `request<T>` 现在直接从 envelope 中提取 `data`，View 层不需要再做解包。只需要替换类型标注。

- [ ] **Step 2: 逐个 View 修复**

对每个 View 文件：
1. 搜索 `ApiEnvelope` 删除类型引用
2. 确保 `catch` 块使用 `ApiError` 类

- [ ] **Step 3: 验证 typecheck 和 build**

Run: `npm run typecheck`
Run: `npm run build`
Expected: 两个都通过

- [ ] **Step 4: Commit**

```bash
git add web/src/views/ web/src/stores/
git commit -m "refactor(frontend): adapt views to unified response format, remove ApiEnvelope usage"
```

---

### Task 12: 修正 provision-account 502 问题

**Files:**
- Modify: `internal/server/admin/db_provision.go` (line 133)
- Modify: `internal/service/db_provision.go` (line 273-305) - 错误消息包装

- [ ] **Step 1: 修改 status code**

`db_provision.go` line 133: `writeError(w, http.StatusBadGateway, err)` → `s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())`

`db_provision.go` line 53: 同样的修改

- [ ] **Step 2: 增强 service 层错误信息**

`internal/service/db_provision.go` 的 `ProvisionMySQLAccount` 函数，在返回错误时提供更多上下文：

```go
if err := mysqlExec(conn, createSQL); err != nil {
    return fmt.Errorf("无法创建MySQL用户: %w", err)
}
```

但不要硬编码中文错误——让 handler 层做错误转译。保持 service 返回技术性错误，handler 层或专门的错误映射函数转译为友好消息。

不改 service 层的错误消息，保留英文/技术性错误。错误详细信息在 `error.details` 字段展示。

- [ ] **Step 3: Commit**

```bash
git add internal/server/admin/db_provision.go
git commit -m "fix: change provision-account 502 to 500 for MySQL connection errors"
```

---

### Task 13: 端到端验证

**Files:**
- 运行脚本: `start.ps1`

- [ ] **Step 1: 启动服务**

```bash
.\start.ps1
```
验证前后端正常启动。

- [ ] **Step 2: 测试 API 响应格式**

使用 curl 或浏览器 DevTools 检查：
```bash
curl http://127.0.0.1:47100/api/health
```
Expected: `{"code":0,"data":{"status":"ok","time":"..."},"message":"ok","request_id":"...","timestamp":"..."}"`

```bash
curl http://127.0.0.1:47100/api/login -X POST -d '{}'
```
Expected: `{"code":400,"error":{"code":"VALIDATION_ERROR","message":"...","details":null},"request_id":"...","timestamp":"..."}"`

- [ ] **Step 3: 测试前端错误提示**

打开浏览器，触发一个错误操作（如错误的登录密码），检查：
- 是否弹出友好的错误提示
- 不再出现 502 (Bad Gateway) 的错误

- [ ] **Step 4: 验证 typecheck/build/tests**

Run: `npm run typecheck`
Run: `npm run build`
Run: `go build ./...`
Run: `go test ./... -count=1`

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: final verification after API response format migration"
```

---

### Task 14: 合并回主目录

按照 CLAUDE.md 中的准则：先合并最新 dev 到 worktree，验证后再合并回去。

- [ ] **Step 1: 合并 dev 到 worktree**

```bash
git merge dev
```

- [ ] **Step 2: 验证编译和测试**

```bash
npm run typecheck && npm run build && go build ./... && go test ./... -count=1
```

- [ ] **Step 3: 合并回 dev**

```bash
git checkout dev && git merge <worktree-branch>
```

---

## 附录 A: 响应格式对照表

| 旧格式 | 新格式 |
|--------|--------|
| `{"status":"ok","time":"..."}` | `{"code":0,"data":{"status":"ok","time":"..."},"message":"ok","request_id":"...","timestamp":"..."}` |
| `{"error":"something wrong"}` | `{"code":400,"error":{"code":"VALIDATION_ERROR","message":"something wrong","details":null},"request_id":"...","timestamp":"..."}` |
| `{"items":[...],"total":N}` | `{"code":0,"data":{"items":[...],"total":N},"message":"ok","request_id":"...","timestamp":"..."}` |
| `{"ok":true,"account":{...}}` | `{"code":0,"data":{"ok":true,"account":{...}},"message":"ok","request_id":"...","timestamp":"..."}` |
| `204 No Content` (DELETE) | 不变，DELETE 仍返回 204 |

## 附录 B: HTTP 状态码 → 错误码映射

| HTTP Status | errCode | 说明 |
|-------------|---------|------|
| 400 | `VALIDATION_ERROR` | 参数校验失败 |
| 401 | `UNAUTHORIZED` | 未认证 |
| 403 | `FORBIDDEN` | 无权限 |
| 404 | `NOT_FOUND` | 资源不存在 |
| 405 | `METHOD_NOT_ALLOWED` | 方法不允许 |
| 409 | `CONFLICT` | 冲突（如删除内置角色） |
| 412 | `PRECONDITION_FAILED` | 前置条件不满足 |
| 429 | `TOO_MANY_REQUESTS` | 请求过于频繁 |
| 500 | `INTERNAL_ERROR` | 服务器内部错误 |
| 502 | `BAD_GATEWAY` | 上游服务不可达（仅用于网关/代理场景） |
| 503 | `SERVICE_UNAVAILABLE` | 服务不可用（如数据库未就绪） |
