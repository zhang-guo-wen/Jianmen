package rbac

import (
	"fmt"
	"sort"
	"strings"

	"jianmen/internal/model"
)

// PermissionDefinition describes an action that can be assigned to a role.
type PermissionDefinition struct {
	Action        string   `json:"action"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	ResourceTypes []string `json:"resource_types,omitempty"`
	Assignable    bool     `json:"assignable"`
}

// PermissionPageDefinition groups button/API actions under one visible page.
type PermissionPageDefinition struct {
	Key     string                 `json:"key"`
	Label   string                 `json:"label"`
	Path    string                 `json:"path"`
	Order   int                    `json:"order"`
	Actions []PermissionDefinition `json:"actions"`
}

// PageAccess is the page portion returned for the current user.
type PageAccess struct {
	Key   string `json:"key"`
	Path  string `json:"path"`
	Order int    `json:"order"`
}

var permissionPages = []PermissionPageDefinition{
	page("quickConnect", "快速连接", "/quick-connect", 10,
		action(ActionSessionConnect, "SSH 连接", "通过堡垒机建立 SSH 会话", model.ResourceTypeHostAccount),
		action(ActionSFTPConnect, "XFTP 连接", "通过 SFTP/XFTP 连接主机账号", model.ResourceTypeHostAccount),
		action(ActionDBConnect, "数据库连接", "通过数据库代理连接指定账号", model.ResourceTypeDatabaseAccount),
		action(ActionAppConnect, "访问应用", "通过应用代理访问指定应用", model.ResourceTypeApplication),
	),
	page("hosts", "主机管理", "/hosts", 20,
		action(ActionHostView, "查看主机", "浏览主机列表与详情"),
		action(ActionHostCreate, "新增主机", "创建主机资源"),
		action(ActionHostUpdate, "编辑主机", "修改主机资源"),
		action(ActionHostDelete, "删除主机", "删除主机资源"),
		action(ActionTargetView, "查看主机账号", "浏览主机账号列表与详情"),
		action(ActionTargetCreate, "新增主机账号", "创建主机账号资源"),
		action(ActionTargetUpdate, "编辑主机账号", "修改主机账号资源"),
		action(ActionTargetDelete, "删除主机账号", "删除主机账号资源"),
	),
	page("databases", "数据库管理", "/databases", 30,
		action(ActionDBProxyView, "查看数据库", "浏览数据库实例与账号"),
		action(ActionDBProxyCreate, "新增数据库", "创建数据库实例与账号"),
		action(ActionDBProxyUpdate, "编辑数据库", "修改数据库实例与账号"),
		action(ActionDBProxyDelete, "删除数据库", "删除数据库实例与账号"),
	),
	page("platformAccounts", "平台账号", "/platform-accounts", 40,
		action(ActionPlatformAccountView, "查看平台账号", "浏览平台账号列表与详情"),
		action(ActionPlatformAccountCreate, "新增平台账号", "创建平台账号"),
		action(ActionPlatformAccountUpdate, "编辑平台账号", "修改平台账号"),
		action(ActionPlatformAccountDelete, "删除平台账号", "删除平台账号"),
		action(ActionPlatformAccountUse, "使用平台账号", "使用指定平台账号访问外部平台", model.ResourceTypePlatformAccount),
	),
	page("applications", "应用发布", "/applications", 50,
		action(ActionAppView, "查看应用", "浏览应用代理列表"),
		action(ActionAppCreate, "新增应用", "创建应用代理"),
		action(ActionAppUpdate, "编辑应用", "修改应用代理"),
		action(ActionAppDelete, "删除应用", "删除应用代理"),
	),
	page("containers", "容器管理", "/containers", 55,
		action(ActionContainerView, "查看容器", "浏览容器连接与运行实例"),
		action(ActionContainerCreate, "新增容器连接", "创建 Docker 或 containerd 连接"),
		action(ActionContainerUpdate, "编辑容器连接", "修改容器连接配置"),
		action(ActionContainerDelete, "删除容器连接", "删除容器连接配置"),
		action(ActionContainerConnect, "读取容器", "读取容器列表和日志", model.ResourceTypeContainerEndpoint),
	),
	page("audit", "审计中心", "/audit", 60,
		action(ActionAuditView, "查看 SSH 审计", "查看 SSH 会话、命令与文件审计"),
		action(ActionDBAuditView, "查看数据库审计", "查看数据库连接与 SQL 审计"),
		action(ActionSessionView, "查看会话", "查看在线及历史会话"),
		action(ActionSessionDisconnect, "断开会话", "强制中断在线会话"),
	),
	page("rbac", "权限管理", "/rbac", 70,
		action(ActionRBACManage, "管理权限", "管理用户、角色、操作与资源授权"),
		action(ActionAIManage, "AI ??", "????????? AI ????"),
	),
}

var actionDependencies = map[string][]string{
	ActionHostCreate:            {ActionHostView},
	ActionHostUpdate:            {ActionHostView},
	ActionHostDelete:            {ActionHostView},
	ActionTargetView:            {ActionHostView},
	ActionTargetCreate:          {ActionHostView, ActionTargetView},
	ActionTargetUpdate:          {ActionHostView, ActionTargetView},
	ActionTargetDelete:          {ActionHostView, ActionTargetView},
	ActionDBProxyCreate:         {ActionDBProxyView},
	ActionDBProxyUpdate:         {ActionDBProxyView},
	ActionDBProxyDelete:         {ActionDBProxyView},
	ActionAppCreate:             {ActionAppView},
	ActionAppUpdate:             {ActionAppView},
	ActionAppDelete:             {ActionAppView},
	ActionContainerCreate:       {ActionContainerView},
	ActionContainerUpdate:       {ActionContainerView},
	ActionContainerDelete:       {ActionContainerView},
	ActionContainerConnect:      {ActionContainerView},
	ActionPlatformAccountCreate: {ActionPlatformAccountView},
	ActionPlatformAccountUpdate: {ActionPlatformAccountView},
	ActionPlatformAccountDelete: {ActionPlatformAccountView},
	ActionPlatformAccountUse:    {ActionPlatformAccountView},
	ActionSessionDisconnect:     {ActionSessionView},
}

var pageVisibilityActions = map[string][]string{
	"quickConnect":     {ActionSessionConnect, ActionSFTPConnect, ActionDBConnect, ActionAppConnect},
	"hosts":            {ActionHostView},
	"databases":        {ActionDBProxyView},
	"platformAccounts": {ActionPlatformAccountView},
	"applications":     {ActionAppView},
	"containers":       {ActionContainerView},
	"audit":            {ActionAuditView, ActionDBAuditView, ActionSessionView},
	"rbac":             {ActionRBACManage},
}

var permissionCatalog = flattenPermissionPages(permissionPages)
var permissionCatalogByAction = buildPermissionCatalogIndex(permissionCatalog)

func page(key, label, path string, order int, actions ...PermissionDefinition) PermissionPageDefinition {
	return PermissionPageDefinition{Key: key, Label: label, Path: path, Order: order, Actions: actions}
}

func action(actionKey, label, description string, resourceTypes ...string) PermissionDefinition {
	return PermissionDefinition{
		Action: actionKey, Label: label, Description: description,
		ResourceTypes: resourceTypes, Assignable: true,
	}
}

func flattenPermissionPages(pages []PermissionPageDefinition) []PermissionDefinition {
	items := make([]PermissionDefinition, 0)
	for _, page := range pages {
		items = append(items, page.Actions...)
	}
	return items
}

func buildPermissionCatalogIndex(items []PermissionDefinition) map[string]PermissionDefinition {
	index := make(map[string]PermissionDefinition, len(items))
	for _, item := range items {
		index[item.Action] = clonePermissionDefinition(item)
	}
	return index
}

func clonePermissionDefinition(item PermissionDefinition) PermissionDefinition {
	item.ResourceTypes = append([]string(nil), item.ResourceTypes...)
	return item
}

func clonePermissionPage(page PermissionPageDefinition) PermissionPageDefinition {
	page.Actions = append([]PermissionDefinition(nil), page.Actions...)
	for i := range page.Actions {
		page.Actions[i] = clonePermissionDefinition(page.Actions[i])
	}
	return page
}

// PermissionCatalog returns a copy of the complete action catalog.
func PermissionCatalog() []PermissionDefinition {
	items := make([]PermissionDefinition, len(permissionCatalog))
	for i, item := range permissionCatalog {
		items[i] = clonePermissionDefinition(item)
	}
	return items
}

// PermissionPages returns the page/action permission tree.
func PermissionPages() []PermissionPageDefinition {
	pages := make([]PermissionPageDefinition, len(permissionPages))
	for i, page := range permissionPages {
		pages[i] = clonePermissionPage(page)
	}
	return pages
}

// FindPermissionDefinition returns catalog metadata for one action.
func FindPermissionDefinition(actionKey string) (PermissionDefinition, bool) {
	item, ok := permissionCatalogByAction[strings.TrimSpace(actionKey)]
	return clonePermissionDefinition(item), ok
}

// ValidateAssignableActions validates role actions and adds required page-view dependencies.
func ValidateAssignableActions(actions []string) ([]string, error) {
	unique := make(map[string]struct{}, len(actions))
	pending := append([]string(nil), actions...)
	for len(pending) > 0 {
		actionKey := strings.TrimSpace(pending[0])
		pending = pending[1:]
		if actionKey == "" {
			continue
		}
		item, ok := permissionCatalogByAction[actionKey]
		if !ok || !item.Assignable {
			return nil, fmt.Errorf("action %q is not assignable", actionKey)
		}
		if _, exists := unique[actionKey]; exists {
			continue
		}
		unique[actionKey] = struct{}{}
		pending = append(pending, actionDependencies[actionKey]...)
	}
	result := make([]string, 0, len(unique))
	for actionKey := range unique {
		result = append(result, actionKey)
	}
	sort.Strings(result)
	return result, nil
}

// AccessiblePages derives visible pages from effective actions.
func AccessiblePages(actions []string) []PageAccess {
	actionSet := make(map[string]struct{}, len(actions))
	for _, actionKey := range actions {
		actionSet[actionKey] = struct{}{}
	}
	_, wildcard := actionSet["*"]
	pages := make([]PageAccess, 0, len(permissionPages))
	for _, page := range permissionPages {
		allowed := wildcard
		if !allowed {
			for _, actionKey := range pageVisibilityActions[page.Key] {
				if _, ok := actionSet[actionKey]; ok {
					allowed = true
					break
				}
			}
		}
		if allowed {
			pages = append(pages, PageAccess{Key: page.Key, Path: page.Path, Order: page.Order})
		}
	}
	return pages
}

// ValidatePermissionCatalog checks page and action uniqueness and required metadata.
func ValidatePermissionCatalog() error {
	actions := make(map[string]struct{}, len(permissionCatalog))
	pageKeys := make(map[string]struct{}, len(permissionPages))
	paths := make(map[string]struct{}, len(permissionPages))
	for pageIndex, page := range permissionPages {
		if page.Key == "" || page.Label == "" || page.Path == "" || page.Order <= 0 || len(page.Actions) == 0 {
			return fmt.Errorf("permission page %d has incomplete metadata", pageIndex)
		}
		if _, exists := pageKeys[page.Key]; exists {
			return fmt.Errorf("duplicate page key %q", page.Key)
		}
		if _, exists := paths[page.Path]; exists {
			return fmt.Errorf("duplicate page path %q", page.Path)
		}
		pageKeys[page.Key] = struct{}{}
		paths[page.Path] = struct{}{}
		for actionIndex, item := range page.Actions {
			if item.Action == "" || item.Label == "" || item.Description == "" {
				return fmt.Errorf("permission page %q action %d has incomplete metadata", page.Key, actionIndex)
			}
			if _, exists := actions[item.Action]; exists {
				return fmt.Errorf("duplicate action %q", item.Action)
			}
			actions[item.Action] = struct{}{}
		}
	}
	for _, page := range permissionPages {
		visibleActions, ok := pageVisibilityActions[page.Key]
		if !ok || len(visibleActions) == 0 {
			return fmt.Errorf("permission page %q has no visibility actions", page.Key)
		}
		for _, actionKey := range visibleActions {
			if _, ok := actions[actionKey]; !ok {
				return fmt.Errorf("page visibility action %q is missing", actionKey)
			}
		}
	}
	for actionKey, dependencies := range actionDependencies {
		if _, ok := actions[actionKey]; !ok {
			return fmt.Errorf("dependency source action %q is missing", actionKey)
		}
		for _, dependency := range dependencies {
			if _, ok := actions[dependency]; !ok {
				return fmt.Errorf("dependency action %q is missing", dependency)
			}
		}
	}
	return nil
}
