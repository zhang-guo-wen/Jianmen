# 资源授权去重与批量授权 — 实现计划

> **For agentic workers:** 使用 superpowers:subagent-driven-development 按任务逐个实现。步骤使用 checkbox (`- [ ]`) 语法跟踪。

**目标:** 前端选定主体后禁用已有授权的资源行；后端支持批量授权，对已有授权执行逻辑删除+重新插入。

**架构:** 前端：watch principal_id → 查询已有授权 → 灰显禁用行 → 批量提交。后端：POST /batch → Service 预检 → 存在则软删+插入，不存在则直接插入 → 返回计数。

**技术栈:** Go 1.23+, GORM, Vue 3 + Element Plus, TypeScript

## 全局约束

- 所有表字段：`created_at`, `created_by`, `updated_at`, `updated_by`, `deleted_at`
- 唯一索引包含 `deleted_at`
- 删除使用 GORM 逻辑删除（`gorm.DeletedAt`）
- 注释使用中文，提交信息使用中文
- 功能开发使用 git worktree 隔离

## 文件结构

| 文件 | 改动 | 职责 |
|------|------|------|
| `internal/model/resource_grant.go` | 修改 | 添加 deleted_at、created_by、updated_by，调整唯一索引 |
| `internal/store/dbstore_resource_grants.go` | 修改 | 添加 FindGrantsByPrincipal、BatchUpsertGrants |
| `internal/service/resource_grant.go` | 修改 | 添加 BatchCreate、BatchResult，添加接口方法 |
| `internal/server/admin/resource_grant_handlers.go` | 修改 | 添加 batch handler、list 支持 principal 过滤 |
| `internal/server/admin/routes.go` | 修改 | 注册 /batch 路由 |
| `web/src/api/client.ts` | 修改 | 添加 batchCreateResourceGrants、getPrincipalGrants |
| `web/src/views/ResourceGrantView.vue` | 修改 | 主体选择后禁用已有授权行、批量保存 |

---

### Task 1: Model — 添加软删除和审计字段

**文件:**
- 修改: `internal/model/resource_grant.go`

**接口:**
- 产出: `ResourceGrant.DeletedAt gorm.DeletedAt`、`CreatedBy`、`UpdatedBy` 字段
- 产出: 唯一索引包含 `deleted_at`（priority 6）

- [ ] **Step 1: 修改 ResourceGrant 结构体**

编辑 `internal/model/resource_grant.go`，将：

```go
type ResourceGrant struct {
	ID            string     `gorm:"primaryKey;size:64" json:"id"`
	PrincipalType string     `gorm:"index;index:idx_resource_grants_principal,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:1;size:32;not null" json:"principal_type"`
	PrincipalID   string     `gorm:"index;index:idx_resource_grants_principal,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:2;size:64;not null" json:"principal_id"`
	ResourceType  string     `gorm:"index;index:idx_resource_grants_resource,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:3;size:64;not null" json:"resource_type"`
	ResourceID    string     `gorm:"index;index:idx_resource_grants_resource,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:4;size:64;not null" json:"resource_id"`
	Effect        string     `gorm:"index;uniqueIndex:uidx_resource_grants_logic,priority:5;size:16;not null;default:allow" json:"effect"`
	ExpiresAt     *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
```

改为：

```go
type ResourceGrant struct {
	ID            string         `gorm:"primaryKey;size:64" json:"id"`
	PrincipalType string         `gorm:"index;index:idx_resource_grants_principal,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:1;size:32;not null" json:"principal_type"`
	PrincipalID   string         `gorm:"index;index:idx_resource_grants_principal,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:2;size:64;not null" json:"principal_id"`
	ResourceType  string         `gorm:"index;index:idx_resource_grants_resource,priority:1;uniqueIndex:uidx_resource_grants_logic,priority:3;size:64;not null" json:"resource_type"`
	ResourceID    string         `gorm:"index;index:idx_resource_grants_resource,priority:2;uniqueIndex:uidx_resource_grants_logic,priority:4;size:64;not null" json:"resource_id"`
	Effect        string         `gorm:"index;uniqueIndex:uidx_resource_grants_logic,priority:5;size:16;not null;default:allow" json:"effect"`
	ExpiresAt     *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"uniqueIndex:uidx_resource_grants_logic,priority:6" json:"deleted_at,omitempty"`
	CreatedBy     string         `gorm:"size:64" json:"created_by,omitempty"`
	UpdatedBy     string         `gorm:"size:64" json:"updated_by,omitempty"`
}
```

- [ ] **Step 2: 编译验证**

```bash
cd internal && go build ./...
```

预期：编译通过（GORM AutoMigrate 会自动添加新列）

- [ ] **Step 3: 运行已有测试确保未破坏**

```bash
cd internal && go test ./model/... ./store/... ./service/... -run "ResourceGrant|resource_grant" -v -count=1
```

预期：所有已有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/model/resource_grant.go
git commit -m "feat: ResourceGrant 添加软删除(DeletedAt)和审计字段(CreatedBy/UpdatedBy)"
```

---

### Task 2: Store — 添加查询和批量 upsert 方法

**文件:**
- 修改: `internal/store/dbstore_resource_grants.go`
- 修改: `internal/service/resource_grant.go`（接口定义）

**接口:**
- 消费: `ResourceGrant`（含新增字段）
- 产出: `FindGrantsByPrincipal(ctx, principalType, principalID string) ([]model.ResourceGrant, error)`
- 产出: `BatchUpsertGrants(ctx context.Context, grants []model.ResourceGrant, actorID string) (created int, refreshed int, error)`

- [ ] **Step 1: 更新 ResourceGrantRepository 接口**

在 `internal/service/resource_grant.go` 的 `ResourceGrantRepository` 接口中新增两个方法：

```go
type ResourceGrantRepository interface {
	SearchResourceGrants(ctx context.Context, query string) ([]model.ResourceGrant, error)
	FindResourceGrant(ctx context.Context, id string) (model.ResourceGrant, bool, error)
	CreateResourceGrant(ctx context.Context, grant model.ResourceGrant) (model.ResourceGrant, error)
	EnsureResourceGrant(ctx context.Context, grant model.ResourceGrant) error
	DeleteResourceGrant(ctx context.Context, id string) error
	ResourceGrantPrincipalExists(ctx context.Context, principalType, principalID string) (bool, error)
	ResourceGrantResourceExists(ctx context.Context, resourceType, resourceID string) (bool, error)
	// 新增方法
	FindGrantsByPrincipal(ctx context.Context, principalType, principalID string) ([]model.ResourceGrant, error)
	BatchUpsertGrants(ctx context.Context, grants []model.ResourceGrant, actorID string) (created int, refreshed int, error)
}
```

- [ ] **Step 2: 实现 FindGrantsByPrincipal**

在 `internal/store/dbstore_resource_grants.go` 末尾添加：

```go
// FindGrantsByPrincipal 查询某主体所有未删除的授权
func (s *DBStore) FindGrantsByPrincipal(ctx context.Context, principalType, principalID string) ([]model.ResourceGrant, error) {
	principalType = strings.ToLower(strings.TrimSpace(principalType))
	principalID = strings.TrimSpace(principalID)
	if principalType == "" || principalID == "" {
		return nil, fmt.Errorf("principal_type and principal_id are required")
	}
	var grants []model.ResourceGrant
	if err := s.db.WithContext(ctx).
		Where("principal_type = ? AND principal_id = ?", principalType, principalID).
		Order("created_at DESC").
		Find(&grants).Error; err != nil {
		return nil, fmt.Errorf("find grants by principal: %w", err)
	}
	return grants, nil
}
```

- [ ] **Step 3: 实现 BatchUpsertGrants**

在同一文件末尾添加：

```go
// BatchUpsertGrants 批量处理授权：存在则软删旧记录+插入新记录，不存在则直接插入
func (s *DBStore) BatchUpsertGrants(ctx context.Context, grants []model.ResourceGrant, actorID string) (created int, refreshed int, error) {
	if len(grants) == 0 {
		return 0, 0, nil
	}
	// 在事务中逐条处理
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, grant := range grants {
			// 查找是否已有未删除的相同授权
			var existing model.ResourceGrant
			result := tx.Where(
				"principal_type = ? AND principal_id = ? AND resource_type = ? AND resource_id = ? AND effect = ?",
				grant.PrincipalType, grant.PrincipalID, grant.ResourceType, grant.ResourceID, grant.Effect,
			).First(&existing)

			if result.Error == nil {
				// 已存在：软删除旧记录
				if err := tx.Model(&existing).Updates(map[string]interface{}{
					"deleted_at": time.Now(),
					"updated_by": actorID,
				}).Error; err != nil {
					return fmt.Errorf("soft delete existing grant %s: %w", existing.ID, err)
				}
				refreshed++
			} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find existing grant: %w", result.Error)
			} else {
				created++
			}

			// 插入新记录
			grant.ID = "" // 使用 BeforeCreate 生成的 ID
			grant.DeletedAt = gorm.DeletedAt{} // 零值 = NULL
			grant.CreatedBy = actorID
			grant.UpdatedBy = actorID
			grant.CreatedAt = time.Now()
			grant.UpdatedAt = time.Now()
			if err := tx.Create(&grant).Error; err != nil {
				return fmt.Errorf("create resource grant: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return created, refreshed, nil
}
```

需要在文件头部添加 `time` 和 `errors` import（如果尚未导入）。

- [ ] **Step 4: 更新 fakeRepository（单元测试用）**

编辑 `internal/service/resource_grant_test.go`，在 `fakeResourceGrantRepository` 中添加新方法：

```go
// 添加到 fakeResourceGrantRepository struct
principalGrants []model.ResourceGrant
principalGrantsErr error
batchCreated int
batchRefreshed int
batchErr error

// 添加方法实现
func (f *fakeResourceGrantRepository) FindGrantsByPrincipal(_ context.Context, principalType, principalID string) ([]model.ResourceGrant, error) {
	return append([]model.ResourceGrant(nil), f.principalGrants...), f.principalGrantsErr
}

func (f *fakeResourceGrantRepository) BatchUpsertGrants(_ context.Context, grants []model.ResourceGrant, actorID string) (int, int, error) {
	return f.batchCreated, f.batchRefreshed, f.batchErr
}
```

- [ ] **Step 5: 编译和测试验证**

```bash
cd internal && go build ./...
go test ./store/... ./service/... -v -count=1
```

预期：编译通过，所有测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/store/dbstore_resource_grants.go internal/service/resource_grant.go internal/service/resource_grant_test.go
git commit -m "feat: 添加 FindGrantsByPrincipal 和 BatchUpsertGrants 存储方法"
```

---

### Task 3: Service — 添加 BatchCreate 方法

**文件:**
- 修改: `internal/service/resource_grant.go`
- 修改: `internal/service/resource_grant_test.go`

**接口:**
- 消费: `repository.FindGrantsByPrincipal`, `repository.BatchUpsertGrants`
- 产出: `BatchCreate(ctx, actorID string, bypass bool, grants []model.ResourceGrant) (BatchResult, error)`
- 产出: `BatchResult{ Created int; Refreshed int }`

- [ ] **Step 1: 添加 BatchResult 类型和 BatchCreate 方法**

在 `internal/service/resource_grant.go` 的 `ResourceGrantPage` 类型定义附近添加：

```go
// BatchResult 批量创建授权的结果
type BatchResult struct {
	Created   int
	Refreshed int
}
```

在 `Create` 方法之后添加 `BatchCreate` 方法：

```go
// BatchCreate 批量创建资源授权，对已有授权执行软删+重新插入
func (s *ResourceGrantService) BatchCreate(ctx context.Context, actorID string, bypass bool, grants []model.ResourceGrant) (BatchResult, error) {
	if len(grants) == 0 {
		return BatchResult{}, nil
	}

	// 规范化并验证每条授权
	normalized := make([]model.ResourceGrant, 0, len(grants))
	for _, grant := range grants {
		grant = normalizeResourceGrant(grant)
		if err := validateResourceGrant(grant); err != nil {
			return BatchResult{}, fmt.Errorf("验证授权失败: %w", err)
		}
		// 验证主体存在
		principalExists, err := s.repository.ResourceGrantPrincipalExists(ctx, grant.PrincipalType, grant.PrincipalID)
		if err != nil {
			return BatchResult{}, fmt.Errorf("检查授权主体: %w", err)
		}
		if !principalExists {
			return BatchResult{}, fmt.Errorf("%w: 主体不存在", ErrInvalidResourceGrant)
		}
		// 验证资源存在
		resourceExists, err := s.repository.ResourceGrantResourceExists(ctx, grant.ResourceType, grant.ResourceID)
		if err != nil {
			return BatchResult{}, fmt.Errorf("检查授权资源: %w", err)
		}
		if !resourceExists {
			return BatchResult{}, fmt.Errorf("%w: 资源不存在或资源类型不匹配", ErrInvalidResourceGrant)
		}
		// 授权检查
		if err := s.authorize(actorID, bypass, grant.ResourceType, grant.ResourceID); err != nil {
			return BatchResult{}, err
		}
		normalized = append(normalized, grant)
	}

	created, refreshed, err := s.repository.BatchUpsertGrants(ctx, normalized, actorID)
	if err != nil {
		return BatchResult{}, fmt.Errorf("批量创建授权失败: %w", err)
	}
	return BatchResult{Created: created, Refreshed: refreshed}, nil
}
```

- [ ] **Step 2: 编写 BatchCreate 单元测试**

在 `internal/service/resource_grant_test.go` 末尾添加测试：

```go
func TestResourceGrantServiceBatchCreateAllNew(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals:     map[string]bool{"user:u1": true},
		resources:      map[string]bool{model.ResourceTypeHost + ":h1": true, model.ResourceTypeHost + ":h2": true},
		batchCreated:   2,
		batchRefreshed: 0,
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h2", Effect: model.PermissionEffectAllow},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if result.Created != 2 || result.Refreshed != 0 {
		t.Fatalf("result = %#v, want created=2 refreshed=0", result)
	}
}

func TestResourceGrantServiceBatchCreatePartialRefresh(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals:     map[string]bool{"user:u1": true},
		resources:      map[string]bool{model.ResourceTypeHost + ":h1": true, model.ResourceTypeHost + ":h2": true},
		batchCreated:   1,
		batchRefreshed: 1,
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h2", Effect: model.PermissionEffectAllow},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if result.Created != 1 || result.Refreshed != 1 {
		t.Fatalf("result = %#v, want created=1 refreshed=1", result)
	}
}

func TestResourceGrantServiceBatchCreateRejectsMissingPrincipal(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals: map[string]bool{},
		resources:  map[string]bool{model.ResourceTypeHost + ":h1": true},
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "missing", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
	})
	if !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("batch create error = %v, want invalid grant", err)
	}
}

func TestResourceGrantServiceBatchCreateEmptyGrants(t *testing.T) {
	repository := &fakeResourceGrantRepository{}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, nil)
	if err != nil {
		t.Fatalf("batch create empty: %v", err)
	}
	if result.Created != 0 || result.Refreshed != 0 {
		t.Fatalf("result = %#v, want zeros", result)
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd internal && go test ./service/... -run "BatchCreate" -v -count=1
```

预期：4 个测试全部 PASS

- [ ] **Step 4: 运行全部已有测试**

```bash
cd internal && go test ./service/... -v -count=1
```

预期：所有已有测试通过，新增测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/service/resource_grant.go internal/service/resource_grant_test.go
git commit -m "feat: 添加 BatchCreate 批量创建授权方法，支持软删刷新"
```

---

### Task 4: Handler — 添加批量接口和 principal 过滤

**文件:**
- 修改: `internal/server/admin/resource_grant_handlers.go`
- 修改: `internal/server/admin/routes.go`

**接口:**
- 消费: `service.BatchCreate()`, `service.ResourceGrantService`
- 产出: `POST /api/resource-grants/batch` 路由
- 产出: `GET /api/resource-grants?principal_type=xxx&principal_id=xxx` 支持按主体过滤

- [ ] **Step 1: 添加批量请求类型和 handler**

在 `internal/server/admin/resource_grant_handlers.go` 的 `resourceGrantRequest` 类型定义附近添加：

```go
// 批量授权请求
type batchResourceGrantRequest struct {
	Grants []resourceGrantRequest `json:"grants"`
}
```

在文件末尾（`writeResourceGrantError` 之前）添加批量创建 handler：

```go
// 批量创建资源授权
func (s *Server) handleBatchResourceGrants(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}

	var req batchResourceGrantRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "无效的JSON: "+err.Error())
		return
	}
	if len(req.Grants) == 0 {
		s.writeErrorText(w, r, http.StatusBadRequest, "grants 不能为空")
		return
	}

	grants := make([]model.ResourceGrant, 0, len(req.Grants))
	for _, g := range req.Grants {
		grants = append(grants, model.ResourceGrant{
			PrincipalType: g.PrincipalType,
			PrincipalID:   g.PrincipalID,
			ResourceType:  g.ResourceType,
			ResourceID:    g.ResourceID,
			Effect:        g.Effect,
			ExpiresAt:     g.ExpiresAt,
		})
	}

	result, err := resourceGrants.BatchCreate(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), grants)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}

	message := buildBatchGrantMessage(result.Created, result.Refreshed)
	s.writeJSON(w, r, http.StatusCreated, map[string]interface{}{
		"created":   result.Created,
		"refreshed": result.Refreshed,
		"message":   message,
	})
}
```

在文件末尾添加消息构建函数：

```go
// buildBatchGrantMessage 构建批量授权结果的中文提示
func buildBatchGrantMessage(created, refreshed int) string {
	switch {
	case created == 0 && refreshed > 0:
		return fmt.Sprintf("全部资源已授权，刷新%d项授权", refreshed)
	case created > 0 && refreshed == 0:
		return fmt.Sprintf("新增%d项授权", created)
	case created > 0 && refreshed > 0:
		return fmt.Sprintf("新增%d项授权，刷新%d项授权", created, refreshed)
	default:
		return "未创建任何授权"
	}
}
```

需要在文件头部添加 `"fmt"` import（如果尚未导入）。

- [ ] **Step 2: 为 listResourceGrants 添加 principal 过滤**

编辑 `listResourceGrants` 函数以支持 `principal_type` 和 `principal_id` 查询参数。在当前实现中，`SearchResourceGrants` 通过 `q` 参数搜索。我们需要新增按 principal 精确过滤的能力。

修改 `listResourceGrants`：

```go
func (s *Server) listResourceGrants(w http.ResponseWriter, r *http.Request) {
	resourceGrants, ok := s.requireResourceGrantService(w, r)
	if !ok {
		return
	}
	// 检查是否按主体过滤
	principalType := r.URL.Query().Get("principal_type")
	principalID := r.URL.Query().Get("principal_id")
	if principalType != "" && principalID != "" {
		s.listResourceGrantsByPrincipal(w, r, resourceGrants, principalType, principalID)
		return
	}
	page, err := resourceGrants.List(
		r.Context(),
		userIDFromRequest(r),
		isSuperAdminRequest(r),
		r.URL.Query().Get("q"),
		positiveIntRequestQuery(r, "page", 1),
		positiveIntRequestQuery(r, "page_size", defaultPageSize),
	)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: page.Items, Total: page.Total, Page: page.Page, PageSize: page.PageSize,
	})
}

// listResourceGrantsByPrincipal 按主体类型和ID查询授权
func (s *Server) listResourceGrantsByPrincipal(w http.ResponseWriter, r *http.Request, svc *service.ResourceGrantService, principalType, principalID string) {
	grants, err := svc.ListByPrincipal(r.Context(), userIDFromRequest(r), isSuperAdminRequest(r), principalType, principalID)
	if err != nil {
		s.writeResourceGrantError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: grants, Total: len(grants), Page: 1, PageSize: len(grants),
	})
}
```

- [ ] **Step 3: 在 Service 层添加 ListByPrincipal 方法**

在 `internal/service/resource_grant.go` 中添加：

```go
// ListByPrincipal 按主体类型和ID查询授权列表
func (s *ResourceGrantService) ListByPrincipal(ctx context.Context, actorID string, bypass bool, principalType, principalID string) ([]model.ResourceGrant, error) {
	if !bypass && strings.TrimSpace(actorID) == "" {
		return nil, ErrResourceGrantForbidden
	}
	grants, err := s.repository.FindGrantsByPrincipal(ctx, principalType, principalID)
	if err != nil {
		return nil, fmt.Errorf("查询主体授权: %w", err)
	}
	return grants, nil
}
```

- [ ] **Step 4: 注册批量路由**

在 `internal/server/admin/routes.go` 中，在 `"/api/resource-grants"` 行之后添加：

```go
s.muxHandle(mux, "/api/resource-grants/batch", s.withAuthAndUser(s.handleBatchResourceGrants))
```

注意：必须放在 `"/api/resource-grants"` 的精确路由之后、`"/api/resource-grants/"` 的带参数路由之前。

调整后的路由顺序：

```go
s.muxHandle(mux, "/api/resource-grants", s.withAuthAndUser(s.handleResourceGrants))
s.muxHandle(mux, "/api/resource-grants/batch", s.withAuthAndUser(s.handleBatchResourceGrants))
s.muxHandle(mux, "/api/resource-grants/check", s.withAuthAndUser(s.handleResourceGrantCheck))
s.muxHandle(mux, "/api/resource-grants/", s.withAuthAndUser(s.handleResourceGrant))
```

- [ ] **Step 5: 编译**

```bash
cd internal && go build ./...
```

预期：编译通过

- [ ] **Step 6: 运行已有 handler 测试**

```bash
cd internal && go test ./server/admin/... -run "Grant" -v -count=1
```

预期：已有测试通过

- [ ] **Step 7: 提交**

```bash
git add internal/server/admin/resource_grant_handlers.go internal/server/admin/routes.go internal/service/resource_grant.go
git commit -m "feat: 添加批量授权接口 POST /api/resource-grants/batch 和按主体过滤查询"
```

---

### Task 5: 前端 API Client — 添加批量授权和主体查询方法

**文件:**
- 修改: `web/src/api/client.ts`

**接口:**
- 产出: `batchCreateResourceGrants(payload): Promise<{created, refreshed, message}>`
- 产出: `getPrincipalGrants(principalType, principalId): Promise<ResourceGrantRecord[]>`

- [ ] **Step 1: 添加 API 方法**

在 `web/src/api/client.ts` 中，`apiClient` 对象的 `// ── Resource Grants ──` 区域添加：

```typescript
// 批量创建资源授权
batchCreateResourceGrants: (payload: { grants: ResourceGrantPayload[] }) =>
  request<{ created: number; refreshed: number; message: string }>('/api/resource-grants/batch', {
    method: 'POST',
    body: JSON.stringify(payload)
  }),

// 按主体查询已有授权
getPrincipalGrants: (principalType: string, principalId: string) =>
  request<PageResponse<ResourceGrantRecord>>(
    `/api/resource-grants?principal_type=${encodeURIComponent(principalType)}&principal_id=${encodeURIComponent(principalId)}`
  ).then(resp => resp.items),
```

- [ ] **Step 2: 类型检查**

```bash
cd web && npx vue-tsc --noEmit 2>&1 | head -20
```

预期：无新增错误

- [ ] **Step 3: 提交**

```bash
git add web/src/api/client.ts
git commit -m "feat: API Client 添加 batchCreateResourceGrants 和 getPrincipalGrants 方法"
```

---

### Task 6: 前端 View — 禁用已授权行 + 批量保存

**文件:**
- 修改: `web/src/views/ResourceGrantView.vue`

**接口:**
- 消费: `apiClient.getPrincipalGrants()`, `apiClient.batchCreateResourceGrants()`
- 产出: 主体选择后灰显已有授权的资源行；保存时使用批量 API

- [ ] **Step 1: 添加已授权资源 ID 集合的状态**

在 `<script setup>` 区域，`selectedResourceMap` 定义之后添加：

```typescript
// 已授权资源ID集合（当前主体已授权的资源，用于禁用表格行）
const authorizedResourceIds = ref<Set<string>>(new Set())
```

- [ ] **Step 2: 添加拉取已有授权的方法**

在 `loadResources` 附近添加：

```typescript
// 拉取当前主体的已有授权，用于禁用资源列表中的已授权行
const fetchPrincipalGrants = async () => {
  const principalId = grantForm.principal_id
  if (!principalId || !grantDialogVisible.value) {
    authorizedResourceIds.value = new Set()
    return
  }
  try {
    const items = await apiClient.getPrincipalGrants(grantForm.principal_type, principalId)
    // 仅标记当前资源类型下的已授权资源
    authorizedResourceIds.value = new Set(
      items
        .filter(g => g.resource_type === resourceTabType.value && g.effect === grantForm.effect)
        .map(g => g.resource_id)
    )
  } catch {
    authorizedResourceIds.value = new Set()
  }
}
```

- [ ] **Step 3: 监听主体变化和资源 Tab 变化**

在现有 `watch` 区域添加监听：

```typescript
// 主体变更时，重新拉取已有授权并刷新资源列表
watch(() => grantForm.principal_id, async (newId) => {
  if (!newId || !grantDialogVisible.value) {
    authorizedResourceIds.value = new Set()
    return
  }
  await fetchPrincipalGrants()
  // 刷新资源列表以更新禁用状态
  await loadResources(true)
})

// 资源Tab切换时，重新拉取已有授权
watch(resourceTabType, async () => {
  if (grantForm.principal_id) {
    await fetchPrincipalGrants()
    // loadResources 已在 handleResourceTabChange 中调用
  }
})

// effect 变更时也刷新
watch(() => grantForm.effect, async () => {
  if (grantForm.principal_id) {
    await fetchPrincipalGrants()
    await loadResources(true)
  }
})
```

- [ ] **Step 4: 修改资源表格，添加 selectable 和 row-class-name**

在 `<el-table>` 标签上添加 `selectable` 和 `row-class-name` 属性：

```html
<el-table
  ref="resourceTableRef"
  :data="filteredResources"
  v-loading="loadingResources"
  height="210"
  row-key="id"
  stripe
  @select="handleResourceSelect"
  @select-all="handleResourceSelectAll"
  class="resource-table"
>
  <el-table-column type="selection" width="50" 
    :selectable="(row: ResourceRow) => !authorizedResourceIds.has(row.id)" />
```

同时添加 `:row-class-name`：

```html
<el-table
  ref="resourceTableRef"
  :data="filteredResources"
  v-loading="loadingResources"
  height="210"
  row-key="id"
  stripe
  @select="handleResourceSelect"
  @select-all="handleResourceSelectAll"
  :row-class-name="(row: ResourceRow) => authorizedResourceIds.has(row.id) ? 'row-authorized' : ''"
  class="resource-table"
>
```

- [ ] **Step 5: 添加灰显的 CSS 样式**

在 `<style scoped>` 末尾添加：

```css
/* 已授权行灰显 */
:deep(.row-authorized) {
  opacity: 0.45;
  pointer-events: none;
}
```

注意：`pointer-events: none` 会同时禁用 selection checkbox（这正是我们要的效果）。

- [ ] **Step 6: 改造 saveGrant 使用批量 API**

将 `saveGrant` 函数从循环调用单条 API 改为调用批量 API：

```typescript
const saveGrant = async () => {
  const resourceIds = grantForm.resource_ids && grantForm.resource_ids.length > 0
    ? grantForm.resource_ids
    : grantForm.resource_id ? [grantForm.resource_id] : []

  if (!grantForm.principal_id || resourceIds.length === 0) {
    ElMessage.warning(t('resourceGrant.fillRequired'))
    return
  }

  let expiresAt: string | undefined
  if (expiresOption.value === '8h') {
    expiresAt = new Date(Date.now() + 8 * 3600 * 1000).toISOString()
  } else if (expiresOption.value === '7d') {
    expiresAt = new Date(Date.now() + 7 * 86400 * 1000).toISOString()
  } else if (expiresOption.value === '1y') {
    expiresAt = new Date(Date.now() + 365 * 86400 * 1000).toISOString()
  } else if (expiresOption.value === 'custom' && customExpiresAt.value) {
    expiresAt = customExpiresAt.value.toISOString()
  }

  saving.value = true
  try {
    const result = await apiClient.batchCreateResourceGrants({
      grants: resourceIds.map(rid => ({
        principal_type: grantForm.principal_type,
        principal_id: grantForm.principal_id,
        resource_type: grantForm.resource_type,
        resource_id: rid,
        effect: grantForm.effect,
        expires_at: expiresAt
      }))
    })
    ElMessage.success(result.message || t('common.created'))
    grantDialogVisible.value = false
    await loadGrants()
  } catch (e: any) {
    ElMessage.error(e.message || t('common.error'))
  } finally {
    saving.value = false
  }
}
```

- [ ] **Step 7: 类型检查**

```bash
cd web && npx vue-tsc --noEmit
```

预期：无新增错误

- [ ] **Step 8: 提交**

```bash
git add web/src/views/ResourceGrantView.vue
git commit -m "feat: 新增授权时禁用已授权资源行，改用批量授权接口"
```

---

### Task 7: 端到端验证

- [ ] **Step 1: 启动应用**

启动前后端后，进入权限管理 → 资源授权 → 点击新增授权

- [ ] **Step 2: 验证已授权行禁用**

1. 选择主体类型 = "用户"，选择某用户
2. 切换到"主机" Tab
3. 验证：该用户已有授权的主机行呈现灰显，无法勾选
4. 切换到其他主体 → 验证禁用状态更新

- [ ] **Step 3: 验证批量保存**

1. 选择主体，勾选多个未授权资源
2. 点击保存
3. 验证：弹出成功提示，列表刷新

- [ ] **Step 4: 验证刷新已有授权**

由于前端已禁用，需要直接调用 API 测试：
```bash
curl -X POST http://127.0.0.1:47100/api/resource-grants/batch \
  -H "Content-Type: application/json" \
  -d '{"grants":[...]}' # 包含已有授权的 resource_id
```

预期返回：`{"created":0,"refreshed":3,"message":"全部资源已授权，刷新3项授权"}`

---

### Task 8: 清理和提交

- [ ] **Step 1: 运行全部后端测试**

```bash
cd internal && go test ./... -count=1 2>&1 | tail -20
```

- [ ] **Step 2: 最终提交**

```bash
git add -A
git commit -m "feat: 资源授权去重与批量授权功能完成"
```
