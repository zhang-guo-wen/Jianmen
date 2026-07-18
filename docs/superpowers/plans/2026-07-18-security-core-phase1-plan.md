# Security Core Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close Jianmen's release-blocking identity, authorization, credential, transport-default, and CI risks without splitting the modular monolith.

**Architecture:** Introduce database-backed identity state and one context-aware authorization service, then migrate each protocol adapter to it. Browser and AI credentials become non-retrievable server-side sessions or one-time secrets, while unsafe network and host-verification defaults become explicit startup or validation errors.

**Tech Stack:** Go 1.23, net/http, x/crypto/ssh, GORM, SQLite/MySQL/PostgreSQL metadata stores, Vue 3, TypeScript, GitHub Actions.

## Global Constraints

- Work only in a `codex/` git worktree branch.
- Follow red-green-refactor; every behavior change starts with a failing automated test.
- `cmd/` files stay under 150 lines; model files under 200; handler/service/store files under 500.
- Handler performs request parsing and response formatting only; service owns business rules; store owns SQL and transactions.
- Store I/O methods take `context.Context` as the first parameter.
- No new resource types, protocols, or product features during this phase.
- No long-lived browser, AI, or WebSocket credential may appear in a URL or be returned after initial issuance.
- Final validation must run all commands listed in Task 10.

---

### Task 1: Add Required CI Quality Gates

**Files:**
- Create: `.github/workflows/ci.yml`
- Modify: `.github/workflows/container.yml`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `web/package-lock.json`, `go.mod`, existing build scripts.
- Produces: required frontend, backend, vet, and package jobs that run before publish jobs.

- [ ] **Step 1: Add a failing workflow contract test**

Create `scripts/verify-ci.ps1` in the same commit as the workflow and make it assert that
`.github/workflows/ci.yml` contains these commands:

```powershell
$required = @(
  'npm run typecheck',
  'npm run build',
  'go build ./...',
  'go test ./... -count=1',
  'go vet ./...'
)
$content = Get-Content '.github/workflows/ci.yml' -Raw
foreach ($command in $required) {
  if (-not $content.Contains($command)) {
    throw "missing CI command: $command"
  }
}
```

- [ ] **Step 2: Run the contract test and verify RED**

Run:

```powershell
.\scripts\verify-ci.ps1
```

Expected: FAIL because `.github/workflows/ci.yml` does not exist.

- [ ] **Step 3: Add the CI workflow**

The workflow must:

- trigger on pull requests and pushes to `dev`;
- install Node 24 and Go from `go.mod`;
- run frontend typecheck/build;
- run backend build/test/vet;
- build the Docker image without pushing;
- use job dependencies so publish workflows cannot bypass equivalent build steps.

- [ ] **Step 4: Verify GREEN**

Run:

```powershell
.\scripts\verify-ci.ps1
go test ./... -count=1
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: all commands exit 0.

- [ ] **Step 5: Commit**

```powershell
git add .github/workflows/ci.yml .github/workflows/container.yml .github/workflows/release.yml scripts/verify-ci.ps1
git commit -m "ci: enforce build and test gates"
```

### Task 2: Make Super Administrator Database-Backed

**Files:**
- Modify: `internal/model/session.go`
- Modify: `internal/config/config.go`
- Modify: `internal/storage/migrations.go`
- Modify: `internal/storage/bootstrap.go`
- Create: `internal/service/identity.go`
- Create: `internal/service/identity_test.go`
- Create: `internal/store/dbstore_identity.go`
- Create: `internal/store/dbstore_identity_test.go`
- Modify: `internal/server/admin/init.go`
- Modify: `internal/server/admin/auth_context.go`
- Modify: `internal/server/admin/user_handlers.go`
- Modify: `internal/server/admin/server.go`
- Modify: `internal/server/sshserver/server.go`
- Modify: `cmd/bastion-core/main.go`

**Interfaces:**
- Produces:

```go
type IdentitySubject struct {
	ID           string
	Username     string
	SuperAdmin   bool
	Status       string
	ExpiresAt    *time.Time
}

type IdentityRepository interface {
	FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error)
}
```

- [ ] **Step 1: Write failing persistence and setup tests**

Tests must prove:

- `User.IsSuperAdmin` persists through GORM.
- setup creates exactly one user with `is_super_admin=true`.
- a normal config user is not implicitly super admin.
- `super_admin=true` in config is explicit and persisted.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/store ./internal/server/admin ./internal/storage -run 'SuperAdmin|InitSetup' -count=1
```

Expected: compile or assertion failure because the field and Repository do not exist.

- [ ] **Step 3: Add the model, migration, bootstrap, and Repository**

Add to `model.User`:

```go
IsSuperAdmin bool `gorm:"index;not null;default:false" json:"is_super_admin"`
```

Add to `config.User`:

```go
SuperAdmin bool `json:"super_admin"`
```

Add a new immutable migration after `202607170002` that migrates `model.User`.
Update setup and bootstrap writes to persist the explicit flag.

`IdentityService` owns active/expired/super-admin subject validation. Store returns persistence data
and does not decide whether an expired subject may authenticate.

- [ ] **Step 4: Remove runtime Map authorization**

Replace `LoadSuperAdminIDs`, `isSuperAdmin` Map reads, and protocol-specific snapshots with
database-backed identity lookup. Keep one startup-only legacy importer for `.super_admin_ids`;
runtime request handling must not read the file.

- [ ] **Step 5: Verify GREEN**

Run:

```powershell
go test ./internal/store ./internal/server/admin ./internal/server/sshserver ./internal/storage -run 'SuperAdmin|InitSetup' -count=1
go test ./... -count=1
```

Expected: all tests pass and this search returns no protocol-owned Map:

```powershell
rg "superAdminIDs|LoadSuperAdminIDs" internal/server cmd
```

- [ ] **Step 6: Commit**

```powershell
git add internal/model/session.go internal/config/config.go internal/storage internal/service/identity* internal/store internal/server/admin internal/server/sshserver cmd/bastion-core/main.go
git commit -m "refactor: centralize super administrator identity"
```

### Task 3: Introduce the Unified Authorization Service

**Files:**
- Create: `internal/service/authorization.go`
- Create: `internal/service/authorization_test.go`
- Modify: `internal/rbac/checker.go`
- Modify: `internal/rbac/resource_grant_checker.go`
- Modify: `internal/server/admin/server.go`
- Modify: `internal/server/admin/connection_auth.go`
- Modify: `internal/server/admin/auth_context.go`
- Modify: `internal/server/sshserver/server.go`
- Modify: `internal/server/dbproxy/server.go`
- Modify: `internal/server/dbproxy/authorization.go`
- Modify: `internal/server/appproxy/server.go`
- Modify: `cmd/bastion-core/main.go`

**Interfaces:**
- Produces:

```go
type AuthorizationRequest struct {
	UserID       string
	Actions      []string
	ResourceType string
	ResourceID   string
}

type AuthorizationDecision struct {
	Allowed bool
	Reason  string
}

func (s *AuthorizationService) Authorize(
	ctx context.Context,
	request AuthorizationRequest,
) (AuthorizationDecision, error)
```

- [ ] **Step 1: Write failing service table tests**

Cover:

- empty user denied;
- active super admin allowed;
- normal user requires one allowed action;
- concrete resource requires grant;
- deny result is preserved;
- action/store errors propagate;
- request cancellation reaches every checker.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/service -run Authorization -count=1
```

Expected: FAIL because `AuthorizationService` is undefined.

- [ ] **Step 3: Implement the minimal service**

Define small interfaces in `internal/service/authorization.go`:

```go
type AuthorizationIdentity interface {
	FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error)
}

type ActionAuthorizer interface {
	HasPermissionContext(ctx context.Context, userID, action, resourceType, resourceID string) (bool, error)
}

type ResourceAuthorizer interface {
	HasGrantContext(ctx context.Context, userID, resourceType, resourceID string) (bool, error)
}
```

Add context-aware checker methods and retain temporary wrappers only while unmigrated tests require them.

- [ ] **Step 4: Migrate protocol adapters**

Inject one `AuthorizationService` instance from `main`. Remove new direct authorization queries
from Admin connection, SSH, DB and application proxy paths.

- [ ] **Step 5: Add cross-entry consistency tests**

For one normal user, assert identical allow/deny results for:

- Admin `authorizeConnection`;
- SSH target connection;
- database `authorizeConnect`;
- application `authorizeApp`.

- [ ] **Step 6: Verify GREEN**

Run:

```powershell
go test ./internal/service ./internal/rbac ./internal/server/admin ./internal/server/sshserver ./internal/server/dbproxy ./internal/server/appproxy -count=1
go test ./... -count=1
```

- [ ] **Step 7: Commit**

```powershell
git add internal/service/authorization* internal/rbac internal/server cmd/bastion-core/main.go
git commit -m "refactor: route connections through one authorizer"
```

### Task 4: Close Container Authorization and Transport Gaps

**Files:**
- Modify: `internal/server/admin/container_handlers.go`
- Create: `internal/server/admin/container_authorization_test.go`
- Modify: `internal/service/container.go`
- Modify: `internal/service/container_test.go`

**Interfaces:**
- Consumes: `AuthorizationService.Authorize`.
- Produces: resource-scoped container listing/runtime access and validated Docker endpoint transport.

- [ ] **Step 1: Write failing route tests**

Use the real Admin router and assert:

- `container:connect` without endpoint grant returns 403;
- explicit deny returns 403;
- an allowed endpoint appears while an ungranted endpoint does not;
- logs for an ungranted endpoint return 403.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/server/admin -run ContainerAuthorization -count=1
```

Expected: at least the ungranted endpoint case returns 200 before the fix.

- [ ] **Step 3: Enforce endpoint authorization**

Call the unified authorizer with:

```go
service.AuthorizationRequest{
	UserID:       userIDFromRequest(r),
	Actions:      []string{rbac.ActionContainerConnect},
	ResourceType: model.ResourceTypeContainerEndpoint,
	ResourceID:   endpointID,
}
```

Filter endpoint lists through a batch grant query or request-scoped authorization cache.

- [ ] **Step 4: Write failing Docker transport tests**

Assert:

- non-loopback `http://` Docker API endpoint is rejected;
- loopback HTTP remains available for local development;
- `https://` is accepted and verifies server certificates.

- [ ] **Step 5: Implement transport validation and verify GREEN**

Run:

```powershell
go test ./internal/server/admin -run ContainerAuthorization -count=1
go test ./internal/service -run Container -count=1
```

- [ ] **Step 6: Commit**

```powershell
git add internal/server/admin/container_handlers.go internal/server/admin/container_authorization_test.go internal/service/container.go internal/service/container_test.go
git commit -m "fix: enforce container resource authorization"
```

### Task 5: Make SSH Host Verification Secure by Default

**Files:**
- Modify: `internal/server/admin/host_handlers.go`
- Modify: `internal/server/admin/server_test.go`
- Modify: `web/src/views/HostsView.vue`
- Modify: `web/src/components/ConnectionConfigDialog.vue`

**Interfaces:**
- Produces: no automatic `InsecureIgnoreHostKey` fallback.

- [ ] **Step 1: Write failing backend tests**

Add tests that submit a connection test without fingerprint, known-hosts data, or explicit insecure
choice and expect a configuration error before dialing.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/server/admin -run HostKey -count=1
```

Expected: FAIL because the handler currently enables insecure verification automatically.

- [ ] **Step 3: Remove the fallback**

Delete the assignment that turns on `InsecureIgnoreHostKey` when no verification material exists.
Return the existing `ClientConfigForTarget` error as a validation response.

- [ ] **Step 4: Change frontend defaults**

New account forms default to fingerprint mode. The user must explicitly choose “忽略主机密钥验证”
before `insecure_ignore_host_key=true` is submitted.

- [ ] **Step 5: Verify GREEN**

Run:

```powershell
go test ./internal/server/admin ./internal/store -run HostKey -count=1
npm --prefix web run typecheck
npm --prefix web run build
```

- [ ] **Step 6: Commit**

```powershell
git add internal/server/admin/host_handlers.go internal/server/admin/server_test.go web/src/views/HostsView.vue web/src/components/ConnectionConfigDialog.vue
git commit -m "fix: require explicit SSH host verification"
```

### Task 6: Make AI Credentials One-Time Secrets

**Files:**
- Modify: `internal/model/ai_access_token.go`
- Modify: `internal/store/dbstore_ai_access.go`
- Modify: `internal/store/dbstore_ai_access_test.go`
- Modify: `internal/server/admin/ai_handlers.go`
- Modify: `internal/server/admin/ai_handlers_test.go`
- Modify: `web/src/api/client.ts`
- Modify: `web/src/views/AIAccessView.vue`
- Add migration in: `internal/storage/migrations.go`

**Interfaces:**
- Produces: metadata-only token detail and hash-only storage.

- [ ] **Step 1: Write failing tests**

Assert:

- persisted rows contain only access and refresh hashes;
- token detail never returns either secret;
- issuance and refresh responses return new secrets once;
- copy prompt and full prompt do not contain either secret;
- old refresh token is invalid after rotation.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/store ./internal/server/admin -run AIAccessToken -count=1
```

Expected: FAIL because token detail currently returns decrypted secrets.

- [ ] **Step 3: Remove encrypted secret columns and response paths**

Remove `AccessToken` and `RefreshToken` from the model and all persistence requirements.
`aiTokenResponse` always returns metadata and `has_secret=false`.
Build copyable setup instructions with placeholders rather than actual credentials.

- [ ] **Step 4: Update the frontend**

Show secrets only from the immediate create/refresh response. Reopening a token shows metadata and
offers revoke/reissue instead of “查看密钥”.

- [ ] **Step 5: Verify GREEN**

Run:

```powershell
go test ./internal/store ./internal/server/admin -run AIAccessToken -count=1
npm --prefix web run typecheck
npm --prefix web run build
```

- [ ] **Step 6: Commit**

```powershell
git add internal/model/ai_access_token.go internal/store/dbstore_ai_access* internal/server/admin/ai_handlers* internal/storage/migrations.go web/src
git commit -m "fix: make AI credentials non-retrievable"
```

### Task 7: Replace Browser Bearer Storage with Server Sessions

**Files:**
- Create: `internal/model/admin_session.go`
- Modify: `internal/model/core.go`
- Create: `internal/service/browser_session.go`
- Create: `internal/service/browser_session_test.go`
- Create: `internal/store/dbstore_admin_sessions.go`
- Create: `internal/store/dbstore_admin_sessions_test.go`
- Modify: `internal/server/admin/init.go`
- Modify: `internal/server/admin/auth_context.go`
- Modify: `internal/server/admin/routes.go`
- Modify: `internal/server/admin/server.go`
- Modify: `internal/server/admin/webterminal.go`
- Modify: `internal/server/appproxy/server.go`
- Modify: `internal/storage/migrations.go`
- Modify: `web/src/api/client.ts`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/composables/useWebTerminal.ts`
- Modify: `web/src/views/LoginView.vue`
- Modify: `web/src/views/SetupView.vue`

**Interfaces:**
- Produces:

```go
type BrowserSession struct {
	Token     string
	CSRFToken string
	ExpiresAt time.Time
}

func (s *BrowserSessionService) Create(ctx context.Context, userID string, now time.Time) (BrowserSession, error)
func (s *BrowserSessionService) Authenticate(ctx context.Context, token string, now time.Time) (Subject, error)
func (s *BrowserSessionService) Revoke(ctx context.Context, token string, now time.Time) error
func (s *BrowserSessionService) IssueWebSocketTicket(ctx context.Context, userID, resourceID string, now time.Time) (string, error)
func (s *BrowserSessionService) ConsumeWebSocketTicket(ctx context.Context, ticket, resourceID string, now time.Time) (Subject, error)
```

- [ ] **Step 1: Write failing service/store tests**

Cover hash-only persistence, expiry, revocation, CSRF mismatch, one-time WS consumption, target binding,
and concurrent double-consume where exactly one caller succeeds.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/service ./internal/store -run 'BrowserSession|WebSocketTicket' -count=1
```

- [ ] **Step 3: Implement session entities and service**

Use 32 random bytes for session and ticket values. Persist only SHA-256 hashes. Default browser
session TTL is 12 hours and WebSocket ticket TTL is 30 seconds.

- [ ] **Step 4: Migrate Admin and application authentication**

Login/setup set:

```go
http.Cookie{
	Name:     "jianmen_session",
	Path:     "/",
	HttpOnly: true,
	Secure:   secureRequest(r, s.cfg.Admin.PublicURL),
	SameSite: http.SameSiteLaxMode,
}
```

State-changing Admin requests validate `X-CSRF-Token`. Application proxy authenticates the same
server session through `BrowserSessionService`.

- [ ] **Step 5: Replace WebSocket query credentials**

Add `POST /api/web-terminal/tickets`. The WebSocket endpoint accepts only `ticket`, never `token`
or `access_token`.

- [ ] **Step 6: Update frontend session behavior**

All fetch calls use `credentials: "include"`. Remove `jianmen_token` from `localStorage`.
Store only CSRF state in `sessionStorage`. Build WebSocket URLs with the one-time ticket.

- [ ] **Step 7: Verify GREEN**

Run:

```powershell
go test ./internal/service ./internal/store ./internal/server/admin ./internal/server/appproxy -run 'BrowserSession|WebSocket|Login' -count=1
rg 'localStorage.*jianmen_token|Query\\(\\).*token|access_token' web/src internal/server/admin/webterminal.go
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: tests pass and the search finds no long-lived browser credential path.

- [ ] **Step 8: Commit**

```powershell
git add internal/model/admin_session.go internal/model/core.go internal/service/browser_session* internal/store/dbstore_admin_sessions* internal/server/admin internal/server/appproxy internal/storage/migrations.go web/src
git commit -m "refactor: use server-side browser sessions"
```

### Task 8: Enforce Safe Admin Deployment

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/server/admin/server_runtime.go`
- Modify: `config.example.json`
- Modify: `config.docker.json`
- Modify: `README.md`

**Interfaces:**
- Produces:

```go
type AdminTLSConfig struct {
	CertFile          string `json:"cert_file"`
	KeyFile           string `json:"key_file"`
	AllowInsecureHTTP bool   `json:"allow_insecure_http"`
}
```

- [ ] **Step 1: Write failing validation tests**

Assert:

- non-loopback HTTP without explicit override is rejected;
- loopback HTTP is accepted for development;
- certificate and key must be configured together;
- `public_url=https://...` plus loopback listener is accepted for reverse-proxy deployment.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/config -run AdminTLS -count=1
```

- [ ] **Step 3: Add config validation and direct TLS serving**

Use `ListenAndServeTLS` when cert/key are present. The Docker example must either document reverse
proxy termination or set the explicit insecure development override; it must not appear secure by default.

- [ ] **Step 4: Verify GREEN**

Run:

```powershell
go test ./internal/config ./internal/server/admin -run 'AdminTLS|ListenAndServe' -count=1
go test ./... -count=1
```

- [ ] **Step 5: Commit**

```powershell
git add internal/config internal/server/admin/server_runtime.go config.example.json config.docker.json README.md
git commit -m "feat: enforce safe admin transport configuration"
```

### Task 9: Split Database Listeners and Require Secure Password Transport

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Create: `internal/server/dbproxy/listeners.go`
- Create: `internal/server/dbproxy/listeners_test.go`
- Modify: `internal/server/dbproxy/server.go`
- Modify: `internal/server/dbproxy/mysql_auth.go`
- Modify: `internal/server/dbproxy/redis.go`
- Modify: `internal/server/dbproxy/account_auth.go`
- Modify: `internal/service/db_provision.go`
- Modify: `config.example.json`
- Modify: `config.docker.json`
- Modify: `README.md`

**Interfaces:**
- Produces protocol-specific listener configuration:

```go
type DatabaseProtocolListener struct {
	Enabled  bool   `json:"enabled"`
	Address  string `json:"listen_addr"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}
```

- [ ] **Step 1: Write failing config/listener tests**

Cover unique listener addresses, TLS certificate pairing, PostgreSQL SSL negotiation, Redis
non-TLS rejection, and MySQL full-auth refusal on insecure upstream.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/config ./internal/server/dbproxy ./internal/service -run 'ProtocolListener|TLS|InsecurePassword' -count=1
```

- [ ] **Step 3: Implement independent listeners**

Start separate MySQL, PostgreSQL, and Redis accept loops. Remove the 200ms first-byte protocol
detection from new configurations.

- [ ] **Step 4: Enforce TLS password rules**

PostgreSQL must negotiate TLS before `AuthenticationCleartextPassword`. Redis AUTH must reject
non-TLS remote clients. MySQL provisioning must not send plaintext full-auth passwords over an
unverified upstream.

- [ ] **Step 5: Verify GREEN**

Run:

```powershell
go test ./internal/config ./internal/server/dbproxy ./internal/service -count=1
go test -tags=integration ./internal/integration -count=1
```

- [ ] **Step 6: Commit**

```powershell
git add internal/config internal/server/dbproxy internal/service/db_provision.go config.example.json config.docker.json README.md
git commit -m "refactor: isolate secure database listeners"
```

### Task 10: Phase 1 Completion Verification

**Files:**
- Modify: `docs/superpowers/plans/2026-07-18-security-core-phase1-plan.md`
- Modify: `docs/backend-audit-2026-07-18.md`

**Interfaces:**
- Consumes all Phase 1 deliverables.
- Produces command evidence and a closed blocker matrix.

- [ ] **Step 1: Run source invariants**

```powershell
rg 'superAdminIDs|LoadSuperAdminIDs' cmd internal/server
rg 'localStorage.*jianmen_token|[?&](token|access_token)=' web/src internal
rg 'InsecureIgnoreHostKey\\(\\)' internal --glob '!**/*_test.go'
rg 'AccessToken.GetPlaintext|RefreshToken.GetPlaintext' internal
```

Expected: no production-path matches except explicitly approved compatibility adapters documented in
the audit.

- [ ] **Step 2: Run backend verification**

```powershell
go test ./... -count=1
go build ./...
go vet ./...
```

- [ ] **Step 3: Run frontend verification**

```powershell
npm --prefix web run typecheck
npm --prefix web run build
```

- [ ] **Step 4: Run integration and packaging**

```powershell
go test -tags=integration ./internal/integration -count=1
.\build.ps1
```

- [ ] **Step 5: Synchronize with dev before merge**

```powershell
git fetch origin
git merge dev
go test ./... -count=1
npm --prefix web run typecheck
npm --prefix web run build
```

- [ ] **Step 6: Record evidence and commit**

Update the audit with each blocker, its test path, command result, and closing commit.

```powershell
git add docs/superpowers/plans/2026-07-18-security-core-phase1-plan.md docs/backend-audit-2026-07-18.md
git commit -m "docs: close phase one security blockers"
```
