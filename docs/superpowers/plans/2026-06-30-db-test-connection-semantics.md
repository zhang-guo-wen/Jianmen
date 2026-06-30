# 数据库测试连接统一语义 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 统一数据库账号新增、编辑、连接弹窗中的测试连接行为，明确凭据来源并避免误用旧密码测试。

**Architecture:** 后端保留按账号 ID 使用已保存凭据的测试接口，新增一个临时凭据测试接口用于表单输入。前端在 `DatabaseView.vue` 账号表单中增加测试连接状态区：新增/编辑按钮测试用表单凭据；编辑弹窗打开时自动用保存凭据探测一次；连接弹窗保持现有保存凭据探测。

**Tech Stack:** Go Admin API、GORM/SQLite 测试、Vue 3 Composition API `<script setup lang="ts">`、Element Plus。

## Global Constraints

- 新增/编辑账号弹窗的“测试连接”使用用户当前输入的数据库用户名和密码。
- 编辑账号点击“测试连接”必须要求用户重新输入数据库密码；保存账号时密码留空仍表示保留旧密码。
- 编辑账号弹窗打开时自动探测一次，使用数据库中已保存的账号密码。
- 连接弹窗保持现有行为：打开时自动探测，使用数据库中已保存的账号密码。
- 临时测试接口不保存密码，不回显密码。
- API 返回格式沿用 `{ ok, error, latency_ms }`。

---

## File Structure

- Modify: `internal/server/admin/test_db.go`
  - Add request-body based temporary database connection test handler.
- Modify: `internal/server/admin/server.go`
  - Route `POST /api/db/accounts/test` to the new handler while preserving `POST /api/db/accounts/test/{id}`.
- Modify: `internal/server/admin/server_test.go`
  - Add backend tests for temporary test validation and no account persistence.
- Modify: `web/src/api/client.ts`
  - Add typed payload and API method for temporary DB account test.
- Modify: `web/src/views/DatabaseView.vue`
  - Add account form test connection state, UI, edit-open automatic saved-credential probe, and form credential probe.

### Task 1: Backend temporary DB account test API

**Files:**
- Modify: `internal/server/admin/test_db.go`
- Modify: `internal/server/admin/server.go`
- Test: `internal/server/admin/server_test.go`

**Interfaces:**
- Consumes existing `testDBAuth(conn, protocol, username, password)`.
- Produces `POST /api/db/accounts/test` with body `{ instance_id, username, password }` and response `{ ok, error, latency_ms }`.

- [ ] **Step 1: Write failing tests**

Add tests to `internal/server/admin/server_test.go`:

```go
func TestHandleTestDBConnectionPayloadRequiresCredentials(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", strings.NewReader(`{"instance_id":"","username":"","password":""}`))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()

	server.handleTestDBConnection(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleTestDBConnectionPayloadDoesNotCreateAccount(t *testing.T) {
	server := newTestServer(t)
	inst := model.DatabaseInstance{Name: "temp-test", Protocol: "mysql", Address: "127.0.0.1", Port: 1, Status: "active"}
	if err := server.db.Create(&inst).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/db/accounts/test", strings.NewReader(fmt.Sprintf(`{"instance_id":%q,"username":"probe","password":"secret"}`, inst.ID)))
	req = asTestSuperAdmin(req)
	rec := httptest.NewRecorder()

	server.handleTestDBConnection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var count int64
	if err := server.db.Model(&model.DatabaseAccount{}).Where("username = ?", "probe").Count(&count).Error; err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if count != 0 {
		t.Fatalf("temporary test created %d database accounts, want 0", count)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
go test ./internal/server/admin -run "TestHandleTestDBConnectionPayload" -count=1
```

Expected: tests fail because `POST /api/db/accounts/test` is currently interpreted as an account ID path and does not implement body validation.

- [ ] **Step 3: Implement request-body temporary test handler**

In `internal/server/admin/test_db.go`, add a branch at the start of `handleTestDBConnection` after method check:

```go
if strings.TrimSuffix(r.URL.Path, "/") == "/api/db/accounts/test" {
	s.handleTestDBConnectionPayload(w, r)
	return
}
```

Add:

```go
type testDBConnectionPayload struct {
	InstanceID string `json:"instance_id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (s *Server) handleTestDBConnectionPayload(w http.ResponseWriter, r *http.Request) {
	var payload testDBConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeErrorText(w, http.StatusBadRequest, "invalid json body")
		return
	}
	payload.InstanceID = strings.TrimSpace(payload.InstanceID)
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.InstanceID == "" || payload.Username == "" || payload.Password == "" {
		writeErrorText(w, http.StatusBadRequest, "instance_id, username and password are required")
		return
	}

	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", payload.InstanceID).Error; err != nil {
		writeErrorText(w, http.StatusNotFound, "instance not found")
		return
	}
	if inst.Status == "disabled" {
		writeErrorText(w, http.StatusForbidden, "database instance is disabled")
		return
	}

	start := time.Now()
	upstreamAddr := inst.Address
	if inst.Port > 0 {
		upstreamAddr = fmt.Sprintf("%s:%d", inst.Address, inst.Port)
	}
	conn, err := net.DialTimeout("tcp", upstreamAddr, 5*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": fmt.Sprintf("connect: %v", err), "latency_ms": time.Since(start).Milliseconds()})
		return
	}
	defer conn.Close()

	err = testDBAuth(conn, inst.Protocol, payload.Username, payload.Password)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "latency_ms": latencyMs})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "latency_ms": latencyMs})
}
```

Add `encoding/json` import if missing.

- [ ] **Step 4: Run backend tests**

Run:

```powershell
go test ./internal/server/admin -run "TestHandleTestDBConnectionPayload" -count=1
go test ./internal/server/admin -count=1
```

Expected: both pass.

### Task 2: Frontend API client for temporary DB account test

**Files:**
- Modify: `web/src/api/client.ts`

**Interfaces:**
- Produces `testDBConnectionPayload(payload: DBAccountTestPayload): Promise<unknown>`.

- [ ] **Step 1: Add types and method**

In `web/src/api/client.ts`, add:

```ts
export interface DBAccountTestPayload {
  instance_id: string
  username: string
  password: string
}
```

Inside `apiClient`, add:

```ts
testDBConnectionPayload(payload: DBAccountTestPayload) {
  return request('/db/accounts/test', {
    method: 'POST',
    body: JSON.stringify(payload)
  })
}
```

- [ ] **Step 2: Run typecheck**

Run:

```powershell
npm --prefix web run typecheck
```

Expected: pass.

### Task 3: DatabaseView account form test connection UX

**Files:**
- Modify: `web/src/views/DatabaseView.vue`

**Interfaces:**
- Consumes `api.apiClient.testDBConnection(accountId)` for saved credentials.
- Consumes `api.apiClient.testDBConnectionPayload(payload)` for form credentials.

- [ ] **Step 1: Add account form state**

Add near existing connection test refs:

```ts
const accountFormTesting = ref(false)
const accountFormTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)
const savedCredentialTesting = ref(false)
const savedCredentialTestResult = ref<{ ok: boolean; error?: string; latency_ms?: number } | null>(null)
```

- [ ] **Step 2: Reset state on open create**

In `openCreateAccount()`, add:

```ts
accountFormTestResult.value = null
savedCredentialTestResult.value = null
```

- [ ] **Step 3: Auto test saved credentials on edit open**

In `editAccount(row)`, after `accountDialogVisible.value = true`, call:

```ts
accountFormTestResult.value = null
testSavedAccountConnection(row)
```

Add method:

```ts
async function testSavedAccountConnection(row: api.DBAccountRecord) {
  const id = row.id || row.resource_id || ''
  if (!id) return
  savedCredentialTesting.value = true
  savedCredentialTestResult.value = null
  try {
    const result = await api.apiClient.testDBConnection(String(id))
    const data = (result as any).data ?? result
    savedCredentialTestResult.value = { ok: data.ok, latency_ms: data.latency_ms, error: data.ok ? undefined : (data.error || '连接失败') }
  } catch (err) {
    savedCredentialTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' }
  } finally {
    savedCredentialTesting.value = false
  }
}
```

- [ ] **Step 4: Add form credential test method**

Add:

```ts
async function testAccountFormConnection() {
  if (!selectedInstance.value?.id) {
    ElMessage.warning('请先选择数据库实例')
    return
  }
  if (!accountForm.username.trim()) {
    ElMessage.warning('请输入目标用户名')
    return
  }
  if (!accountForm.password) {
    ElMessage.warning('请先输入数据库密码再测试连接')
    return
  }
  accountFormTesting.value = true
  accountFormTestResult.value = null
  try {
    const result = await api.apiClient.testDBConnectionPayload({
      instance_id: selectedInstance.value.id,
      username: accountForm.username.trim(),
      password: accountForm.password,
    })
    const data = (result as any).data ?? result
    accountFormTestResult.value = { ok: data.ok, latency_ms: data.latency_ms, error: data.ok ? undefined : (data.error || '连接失败') }
  } catch (err) {
    accountFormTestResult.value = { ok: false, error: err instanceof Error ? err.message : '连接失败' }
  } finally {
    accountFormTesting.value = false
  }
}
```

- [ ] **Step 5: Add UI in account dialog**

After password form item in account dialog template, add:

```vue
<el-form-item label="连接测试">
  <div class="test-connection-row">
    <el-button :loading="accountFormTesting" @click="testAccountFormConnection">测试连接</el-button>
    <template v-if="accountFormTestResult">
      <el-tag :type="accountFormTestResult.ok ? 'success' : 'danger'" size="small">
        {{ accountFormTestResult.ok ? '可达' : '不可达' }}
      </el-tag>
      <span v-if="accountFormTestResult.latency_ms !== undefined" class="test-connection-meta">
        延迟 {{ accountFormTestResult.latency_ms }}ms
      </span>
      <span v-if="accountFormTestResult.error" class="test-connection-error">
        {{ accountFormTestResult.error }}
      </span>
    </template>
  </div>
  <div v-if="editingAccount" class="test-connection-hint">
    点击测试连接时必须重新输入数据库密码；保存时密码留空仍会保留原密码。
  </div>
  <div v-if="editingAccount" class="test-connection-row saved-credential-row">
    <span class="test-connection-meta">已保存凭据：</span>
    <el-tag v-if="savedCredentialTesting" type="info" size="small">测试中...</el-tag>
    <template v-else-if="savedCredentialTestResult">
      <el-tag :type="savedCredentialTestResult.ok ? 'success' : 'danger'" size="small">
        {{ savedCredentialTestResult.ok ? '可达' : '不可达' }}
      </el-tag>
      <span v-if="savedCredentialTestResult.latency_ms !== undefined" class="test-connection-meta">
        延迟 {{ savedCredentialTestResult.latency_ms }}ms
      </span>
      <span v-if="savedCredentialTestResult.error" class="test-connection-error">
        {{ savedCredentialTestResult.error }}
      </span>
    </template>
  </div>
</el-form-item>
```

- [ ] **Step 6: Add scoped styles**

Add to `<style scoped>`:

```css
.test-connection-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.saved-credential-row {
  margin-top: 8px;
}

.test-connection-meta {
  color: #667085;
  font-size: 12px;
}

.test-connection-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

.test-connection-hint {
  color: #667085;
  font-size: 12px;
  line-height: 1.5;
  margin-top: 6px;
  width: 100%;
}
```

- [ ] **Step 7: Verify frontend**

Run:

```powershell
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: both pass. Build may print existing Rolldown `INVALID_ANNOTATION` warnings from dependencies but must exit 0.

### Task 4: Full verification and merge

**Files:**
- All files changed above.

**Interfaces:**
- Ensures project-level checks pass before merge.

- [ ] **Step 1: Run backend verification**

```powershell
go build ./...
go test ./... -count=1
```

Expected: pass.

- [ ] **Step 2: Run frontend verification**

```powershell
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add internal/server/admin/test_db.go internal/server/admin/server.go internal/server/admin/server_test.go web/src/api/client.ts web/src/views/DatabaseView.vue docs/superpowers/specs/2026-06-30-db-test-connection-semantics-design.md docs/superpowers/plans/2026-06-30-db-test-connection-semantics.md
git commit -m "feat: unify database connection tests"
```

- [ ] **Step 4: Merge latest dev, verify, merge back to dev, restart**

Follow repository CLAUDE.md merge rule:

```powershell
git merge dev
go build ./...
go test ./... -count=1
npm --prefix web run typecheck
npm --prefix web run build
git -C "C:\02-codespace\Jianmen" merge worktree-pg-rbac-integration-test
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Set-Location 'C:\02-codespace\Jianmen'; .\start.ps1"
```

## Self-Review

- Spec coverage: backend temporary test endpoint, saved credential endpoint reuse, frontend create/edit/connect behaviors, validation rules, and verification are all covered.
- Placeholder scan: no TBD/TODO placeholders remain.
- Type consistency: payload fields match spec: `instance_id`, `username`, `password`; frontend method name `testDBConnectionPayload` is used consistently.
