# Core Stabilization Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete Jianmen's backend layering, transactional temporary access, stable resource relationships, protocol boundaries, audit lifecycle, and production migration governance.

**Architecture:** Preserve one deployable process while moving business rules from Admin handlers into focused services backed by small context-aware repositories. Replace string-based resource membership and mutable schema startup behavior with explicit relational and versioned boundaries.

**Tech Stack:** Go 1.23, GORM, SQLite/MySQL/PostgreSQL metadata stores, Vue 3, TypeScript, GitHub Actions.

## Global Constraints

- Phase 1 identity and authorization interfaces are authoritative and must not be bypassed.
- Work only in a `codex/` git worktree branch.
- Follow red-green-refactor for every behavior change.
- Handler performs request parsing and response formatting only.
- Service must not import `internal/server/*` or GORM.
- Store I/O methods take `context.Context` as the first parameter.
- Existing `DBStore` may remain as a concrete composite, but consumers depend only on small interfaces.
- No source file may exceed the limits in `AGENTS.md`.
- Every task is an independently testable commit.
- Final validation must run all commands listed in Task 10.

---

### Task 1: Make Temporary Access Atomic

**Files:**
- Create: `internal/service/temporary_access.go`
- Create: `internal/service/temporary_access_test.go`
- Create: `internal/store/dbstore_temporary_access.go`
- Create: `internal/store/dbstore_temporary_access_test.go`
- Modify: `internal/server/admin/temporary_access_handlers.go`
- Modify: `internal/server/admin/server.go`

**Interfaces:**
- Produces:

```go
type TemporaryAccessRepository interface {
	CreateTemporaryAccess(ctx context.Context, input CreateTemporaryAccessInput) (TemporaryAccessResult, error)
	ExtendTemporaryAccess(ctx context.Context, id string, expiresAt time.Time) error
	DisableTemporaryAccess(ctx context.Context, id string, now time.Time) error
}
```

- [ ] **Step 1: Write failing rollback tests**

Use SQLite with an injected failing transaction step and assert that failure leaves zero rows in
`user_sessions`, `temporary_accounts`, `temporary_account_grants`, `connection_passwords`, and
`ai_access_tokens`.

- [ ] **Step 2: Verify RED**

Run:

```powershell
go test ./internal/store ./internal/service -run TemporaryAccess -count=1
```

Expected: FAIL because the aggregate Repository and Service do not exist.

- [ ] **Step 3: Implement aggregate transaction methods**

Create, extend, and disable each run in one `db.WithContext(ctx).Transaction`. Every update checks
`RowsAffected`; missing accounts return a sentinel not-found error.

- [ ] **Step 4: Reduce handlers to DTO mapping**

Handlers parse payloads, call Service, map sentinel errors to HTTP status, and serialize results.
Remove GORM and `internal/storage` imports from `temporary_access_handlers.go`.

- [ ] **Step 5: Verify GREEN**

```powershell
go test ./internal/store ./internal/service ./internal/server/admin -run TemporaryAccess -count=1
```

- [ ] **Step 6: Commit**

```powershell
git add internal/service/temporary_access* internal/store/dbstore_temporary_access* internal/server/admin/temporary_access_handlers.go internal/server/admin/server.go
git commit -m "refactor: make temporary access transactional"
```

### Task 2: Extract User and User Group Services

**Files:**
- Create: `internal/service/user.go`
- Create: `internal/service/user_test.go`
- Create: `internal/service/user_group.go`
- Create: `internal/service/user_group_test.go`
- Create: `internal/store/dbstore_users.go`
- Create: `internal/store/dbstore_users_test.go`
- Create: `internal/store/dbstore_user_groups.go`
- Create: `internal/store/dbstore_user_groups_test.go`
- Modify: `internal/server/admin/user_handlers.go`
- Modify: `internal/server/admin/user_group_handlers.go`
- Modify: `internal/server/admin/server.go`

**Interfaces:**
- Produces focused `UserRepository` and `UserGroupRepository` interfaces in their consuming services.

- [ ] **Step 1: Write failing service behavior tests**

Cover duplicate usernames/groups, super-admin disable/delete protection, expiry validation, group
member idempotency, missing user/group, and transaction rollback.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/service -run 'User|UserGroup' -count=1
```

- [ ] **Step 3: Implement minimal services and repositories**

Use explicit input and view types; do not accept `model.User` as an HTTP payload.

- [ ] **Step 4: Migrate handlers and verify no direct GORM**

```powershell
rg 's\\.db|gorm\\.' internal/server/admin/user_handlers.go internal/server/admin/user_group_handlers.go
```

Expected: no matches.

- [ ] **Step 5: Verify GREEN**

```powershell
go test ./internal/service ./internal/store ./internal/server/admin -run 'User|UserGroup' -count=1
```

- [ ] **Step 6: Commit**

```powershell
git add internal/service/user* internal/store/dbstore_user* internal/server/admin/user_handlers.go internal/server/admin/user_group_handlers.go internal/server/admin/server.go
git commit -m "refactor: extract user administration services"
```

### Task 3: Extract Role and Effective Permission Services

**Files:**
- Create: `internal/service/role.go`
- Create: `internal/service/role_test.go`
- Create: `internal/store/dbstore_roles.go`
- Create: `internal/store/dbstore_roles_test.go`
- Modify: `internal/server/admin/rbac.go`
- Modify: `internal/server/admin/rbac_catalog_handlers.go`
- Modify: `internal/server/admin/effective_permissions.go`
- Modify: `internal/server/admin/server.go`

**Interfaces:**
- Produces transactionally replaced role actions and context-aware effective-permission queries.

- [ ] **Step 1: Write failing tests**

Cover built-in role protection, catalog action validation, transactional action replacement, role
expiry, deny precedence, and full unpaged effective permissions.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/service -run 'Role|EffectivePermission' -count=1
```

- [ ] **Step 3: Implement services and repositories**

Move SQL joins and transactions out of handlers. Service depends on the RBAC catalog through an
interface or pure function, not on HTTP.

- [ ] **Step 4: Migrate handlers**

```powershell
rg 's\\.db|gorm\\.' internal/server/admin/rbac.go internal/server/admin/rbac_catalog_handlers.go internal/server/admin/effective_permissions.go
```

Expected: no matches.

- [ ] **Step 5: Verify GREEN and commit**

```powershell
go test ./internal/service ./internal/store ./internal/server/admin ./internal/rbac -run 'Role|Permission|RBAC' -count=1
git add internal/service/role* internal/store/dbstore_roles* internal/server/admin
git commit -m "refactor: extract role and permission services"
```

### Task 4: Replace Aggregate Store Dependencies with Small Interfaces

**Files:**
- Modify: `internal/store/store.go`
- Modify: constructors in `internal/server/admin`, `internal/server/sshserver`, `internal/server/dbproxy`
- Modify: service constructors under `internal/service`
- Modify: affected test fakes

**Interfaces:**
- Produces consumer-owned interfaces such as `TargetResolver`, `AuditWriter`, `SessionRepository`,
`HostRepository`, and `DatabaseRepository`.

- [ ] **Step 1: Add architecture tests**

Add `internal/store/architecture_test.go` assertions that:

- no service imports `internal/server`;
- Server structs do not require the full `store.Store` when fewer than ten methods are used;
- context-aware interfaces expose `context.Context` first.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/store -run Architecture -count=1
```

- [ ] **Step 3: Split interfaces by consumer**

Keep `DBStore` methods and implementation files, but remove the exported aggregate interface once
all consumers use focused interfaces.

- [ ] **Step 4: Propagate contexts**

Replace ignored Context parameters and internal `context.Background()` calls in authentication,
containers, DB authorization, and audit with caller Context.

- [ ] **Step 5: Verify GREEN**

```powershell
go test ./... -count=1
rg 'context\\.Background\\(\\)' internal/service internal/store internal/server/dbproxy/authorization.go
```

Every remaining match must be an explicit process-lifetime boundary documented in code.

- [ ] **Step 6: Commit**

```powershell
git add internal/store internal/service internal/server
git commit -m "refactor: depend on focused repository interfaces"
```

### Task 5: Introduce Stable Resource Group Membership

**Files:**
- Create: `internal/model/resource_group_member.go`
- Modify: `internal/model/core.go`
- Modify: `internal/storage/migrations.go`
- Create: `internal/storage/resource_group_migration_test.go`
- Modify: `internal/store/dbstore_resource_groups.go`
- Modify: `internal/store/dbstore_resources.go`
- Modify: `internal/rbac/resource_grant_checker.go`
- Modify: `internal/service/resource_group.go`
- Modify: `internal/model/host.go`
- Modify: `internal/model/database.go`
- Modify: `internal/model/core.go`
- Modify: `internal/model/container.go`
- Modify relevant host/database/application/container stores.

**Interfaces:**
- Produces a canonical `(group_id, resource_type, resource_id)` membership relationship.

- [ ] **Step 1: Write failing model/migration/checker tests**

Cover unique membership, resource and account groups, rename without member rewrites, delete cascade,
and inherited grant resolution.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/storage ./internal/store ./internal/rbac ./internal/service -run ResourceGroupMember -count=1
```

- [ ] **Step 3: Add entity and migration**

Create the unique composite index and backfill memberships from existing group strings in one
versioned migration.

- [ ] **Step 4: Switch writes and authorization reads**

New writes update only membership rows. `ResourceGrantChecker` resolves groups through the membership
table. Remove string-based rename fan-out after all resource types are migrated.

- [ ] **Step 5: Remove legacy group-name columns**

Because the product is unreleased, add a subsequent versioned migration that drops the legacy
columns after backfill tests prove complete coverage.

- [ ] **Step 6: Verify GREEN and commit**

```powershell
go test ./internal/storage ./internal/store ./internal/rbac ./internal/service ./internal/server/admin -run 'ResourceGroup|Grant' -count=1
git add internal/model internal/storage internal/store internal/rbac internal/service
git commit -m "refactor: use stable resource group membership"
```

### Task 6: Split Database Protocol Adapters Without Behavior Drift

**Files:**
- Create: `internal/server/dbproxy/mysql_gateway.go`
- Create: `internal/server/dbproxy/postgres_gateway.go`
- Create: `internal/server/dbproxy/redis_gateway.go`
- Create: `internal/server/dbproxy/session.go`
- Rename/split: `internal/server/dbproxy/observer.go`
- Modify: `internal/server/dbproxy/server.go`
- Move protocol-specific tests to matching files.

**Interfaces:**
- Consumes Phase 1 protocol listeners and `AuthorizationService`.
- Produces one adapter per protocol behind a shared gateway session interface.

- [ ] **Step 1: Add characterization tests**

Before moving code, cover successful and denied authentication, online registration, audit start/end,
query observation, and clean disconnect for all three protocols.

- [ ] **Step 2: Run characterization tests**

```powershell
go test ./internal/server/dbproxy -run 'Gateway|MySQL|Postgres|Redis|Audit' -count=1
```

Expected: PASS; these tests freeze current intended behavior.

- [ ] **Step 3: Extract protocol adapters one at a time**

After each move, rerun the protocol-specific test subset. Shared session lifecycle remains in
`session.go`; packet parsing and auth remain protocol-local.

- [ ] **Step 4: Add fuzz tests**

Add fuzz targets for MySQL packet lengths, PostgreSQL startup messages, Redis RESP frames, and query
observers. Seed each target with valid and truncated packets.

- [ ] **Step 5: Verify GREEN and commit**

```powershell
go test ./internal/server/dbproxy -count=1
go test ./internal/server/dbproxy -run Fuzz -fuzztime=10s
git add internal/server/dbproxy
git commit -m "refactor: isolate database protocol adapters"
```

### Task 7: Add Audit Redaction and Retention

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/service/audit_policy.go`
- Create: `internal/service/audit_policy_test.go`
- Create: `internal/store/dbstore_audit_retention.go`
- Create: `internal/store/dbstore_audit_retention_test.go`
- Modify: `internal/store/dbstore_audit.go`
- Modify: `internal/recording/session.go`
- Modify: `internal/server/dbproxy/recorder.go`
- Modify: `cmd/bastion-core/main.go`
- Modify: config examples and README.

**Interfaces:**
- Produces:

```go
type AuditPolicy interface {
	Redact(kind, value string) string
	RetentionCutoff(now time.Time) time.Time
}

type AuditRetentionRepository interface {
	ExpiredAuditSessions(ctx context.Context, cutoff time.Time, limit int) ([]ExpiredAuditSession, error)
	DeleteAuditSession(ctx context.Context, id string) error
}
```

- [ ] **Step 1: Write failing redaction and consistency tests**

Cover common password/token assignments, private key markers, SQL string values, input-recording
default false, and file/database cleanup rollback behavior.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/service ./internal/store ./internal/recording ./internal/server/dbproxy -run 'AuditPolicy|Retention|Redact' -count=1
```

- [ ] **Step 3: Implement policy and apply before writes**

All command/query/event writers receive already-redacted values. Raw input is not recorded unless the
administrator explicitly enables it.

- [ ] **Step 4: Implement bounded cleanup**

Run cleanup in batches. Delete Replay files first only after recording intent; delete DB rows only
after file deletion succeeds. Missing files are treated as already deleted.

- [ ] **Step 5: Verify GREEN and commit**

```powershell
go test ./internal/service ./internal/store ./internal/recording ./internal/server/dbproxy -run 'AuditPolicy|Retention|Redact' -count=1
git add internal/config internal/service/audit_policy* internal/store/dbstore_audit* internal/recording internal/server/dbproxy/recorder.go cmd config.example.json config.docker.json README.md
git commit -m "feat: govern audit redaction and retention"
```

### Task 8: Make Versioned Migrations the Production Path

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/storage/storage.go`
- Modify: `internal/storage/migrations.go`
- Create: `internal/storage/migrations_test.go` or extend existing migration tests
- Modify: config examples and README.

**Interfaces:**
- Produces immutable forward-only migrations without a runtime migration switch.

- [ ] **Step 1: Write failing migration tests**

Cover empty database, a schema at the last pre-refactor migration, failed migration rollback, and
idempotent second startup.

- [ ] **Step 2: Verify RED**

```powershell
go test ./internal/storage -run Migration -count=1
```

- [ ] **Step 3: Remove the production AutoMigrate setting**

Tests may continue to use `AutoMigrate` for isolated unit fixtures, but application startup always
uses versioned `Migrate` without a configuration switch.

- [ ] **Step 4: Verify GREEN and commit**

```powershell
go test ./internal/storage ./cmd/bastion-core -run 'Migration|Metadata' -count=1
git add internal/config internal/storage cmd config.example.json config.docker.json README.md
git commit -m "refactor: make migrations forward-only"
```

### Task 9: Add Required Integration Coverage

**Files:**
- Modify: `internal/integration/proxy_integration_test.go`
- Add protocol-specific integration test files under `internal/integration`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces required Docker-backed tests for real SSH, MySQL, PostgreSQL, Redis, and dynamic lifecycle.

- [ ] **Step 1: Add failing or skipped-environment-explicit tests**

Each test must fail with a clear prerequisite message when Docker is unavailable; it must not silently
pass. Cover create/enable/connect, account grant change, disable/delete, and listener shutdown.

- [ ] **Step 2: Run and verify expected initial failures**

```powershell
go test -tags=integration ./internal/integration -count=1
```

Expected: failures identify missing lifecycle or protocol behavior, not test setup ambiguity.

- [ ] **Step 3: Complete fixtures and CI service setup**

CI starts real upstream services and executes the integration-tagged package as a required job.

- [ ] **Step 4: Verify GREEN and commit**

```powershell
go test -tags=integration ./internal/integration -count=1
git add internal/integration .github/workflows/ci.yml
git commit -m "test: require real proxy integration coverage"
```

### Task 10: Phase 2 Completion Verification

**Files:**
- Modify: `docs/superpowers/plans/2026-07-18-core-stabilization-phase2-plan.md`
- Modify: `docs/backend-audit-2026-07-18.md`

**Interfaces:**
- Produces final requirement-by-requirement evidence.

- [ ] **Step 1: Verify architecture invariants**

```powershell
rg 'gorm\\.io/gorm|s\\.db' internal/server/admin --glob '!**/*_test.go'
rg 'internal/server/' internal/service
rg 'context\\.Background\\(\\)' internal/service internal/store
```

Expected: no business-path violations; explicitly documented process-lifetime boundaries are the only
permitted Context exceptions.

- [ ] **Step 2: Verify line limits**

Run the repository line-limit checker or an equivalent script against every Go production file.
Expected: zero violations.

- [ ] **Step 3: Run all backend checks**

```powershell
go test ./... -count=1
go build ./...
go vet ./...
go test -tags=integration ./internal/integration -count=1
```

- [ ] **Step 4: Run frontend and packaging checks**

```powershell
npm --prefix web run typecheck
npm --prefix web run build
.\build.ps1
```

- [ ] **Step 5: Merge latest dev and repeat required checks**

```powershell
git fetch origin
git merge dev
go test ./... -count=1
go build ./...
npm --prefix web run typecheck
npm --prefix web run build
```

- [ ] **Step 6: Update evidence and commit**

Record the closing commit and command result for every task.

```powershell
git add docs/superpowers/plans/2026-07-18-core-stabilization-phase2-plan.md docs/backend-audit-2026-07-18.md
git commit -m "docs: complete core stabilization evidence"
```
