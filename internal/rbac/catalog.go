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
	Module        string   `json:"module"`
	ModuleLabel   string   `json:"module_label"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	MenuKey       string   `json:"menu_key,omitempty"`
	ResourceTypes []string `json:"resource_types,omitempty"`
	Assignable    bool     `json:"assignable"`
}

var permissionCatalog = []PermissionDefinition{
	definition(ActionDashboardView, "dashboard", "工作台", "查看工作台", "查看工作台概览", "dashboard"),
	definition(ActionHostView, "hosts", "主机管理", "查看主机", "浏览主机列表与详情", "hosts"),
	definition(ActionHostCreate, "hosts", "主机管理", "新增主机", "创建主机资源", ""),
	definition(ActionHostUpdate, "hosts", "主机管理", "编辑主机", "修改主机资源", ""),
	definition(ActionHostDelete, "hosts", "主机管理", "删除主机", "删除主机资源", ""),
	definition(ActionTargetView, "host_accounts", "主机账号", "查看主机账号", "浏览主机账号列表与详情", ""),
	definition(ActionTargetCreate, "host_accounts", "主机账号", "新增主机账号", "创建主机账号资源", ""),
	definition(ActionTargetUpdate, "host_accounts", "主机账号", "编辑主机账号", "修改主机账号资源", ""),
	definition(ActionTargetDelete, "host_accounts", "主机账号", "删除主机账号", "删除主机账号资源", ""),
	definition(ActionSessionConnect, "connections", "连接与传输", "连接 SSH", "通过堡垒机建立 SSH 会话", "quickConnect", model.ResourceTypeHostAccount),
	definition(ActionSFTPRead, "connections", "连接与传输", "SFTP 读取", "通过 SFTP 读取文件", "", model.ResourceTypeHostAccount),
	definition(ActionSFTPWrite, "connections", "连接与传输", "SFTP 写入", "通过 SFTP 修改文件", "", model.ResourceTypeHostAccount),
	definition(ActionSessionView, "sessions", "会话管理", "查看会话", "查看在线及历史会话", ""),
	definition(ActionDBProxyView, "databases", "数据库管理", "查看数据库", "浏览数据库实例与账号", "databases"),
	definition(ActionDBProxyCreate, "databases", "数据库管理", "新增数据库", "创建数据库实例与账号", ""),
	definition(ActionDBProxyUpdate, "databases", "数据库管理", "编辑数据库", "修改数据库实例与账号", ""),
	definition(ActionDBProxyDelete, "databases", "数据库管理", "删除数据库", "删除数据库实例与账号", ""),
	definition(ActionDBConnect, "database_connections", "数据库连接", "连接数据库", "通过数据库代理连接指定账号", "", model.ResourceTypeDatabaseAccount),
	definition(ActionAuditView, "audit", "审计中心", "查看 SSH 审计", "查看 SSH 会话、命令与文件审计", "audit"),
	definition(ActionDBAuditView, "audit", "审计中心", "查看数据库审计", "查看数据库连接与 SQL 审计", ""),
	definition(ActionRBACManage, "rbac", "权限管理", "管理权限", "管理用户、角色、动作与资源授权", "rbac"),
	definition(ActionAppView, "applications", "应用发布", "查看应用", "浏览应用代理列表", "applications"),
	definition(ActionAppCreate, "applications", "应用发布", "新增应用", "创建应用代理", ""),
	definition(ActionAppUpdate, "applications", "应用发布", "编辑应用", "修改应用代理", ""),
	definition(ActionAppDelete, "applications", "应用发布", "删除应用", "删除应用代理", ""),
	definition(ActionAppConnect, "applications", "应用发布", "访问应用", "通过应用代理访问指定应用", "", model.ResourceTypeApplication),
	definition(ActionPlatformAccountView, "platform_accounts", "平台账号", "查看平台账号", "浏览平台账号列表与详情", "platformAccounts"),
	definition(ActionPlatformAccountCreate, "platform_accounts", "平台账号", "新增平台账号", "创建平台账号", ""),
	definition(ActionPlatformAccountUpdate, "platform_accounts", "平台账号", "编辑平台账号", "修改平台账号", ""),
	definition(ActionPlatformAccountDelete, "platform_accounts", "平台账号", "删除平台账号", "删除平台账号", ""),
	definition(ActionPlatformAccountUse, "platform_accounts", "平台账号", "使用平台账号", "使用指定平台账号访问外部平台", "", model.ResourceTypePlatformAccount),
}

var permissionCatalogByAction = buildPermissionCatalogIndex(permissionCatalog)
var permissionCatalogByMenuKey = buildPermissionMenuIndex(permissionCatalog)

func definition(action, module, moduleLabel, label, description, menuKey string, resourceTypes ...string) PermissionDefinition {
	return PermissionDefinition{
		Action: action, Module: module, ModuleLabel: moduleLabel, Label: label,
		Description: description, MenuKey: menuKey, ResourceTypes: resourceTypes, Assignable: true,
	}
}

func buildPermissionCatalogIndex(items []PermissionDefinition) map[string]PermissionDefinition {
	index := make(map[string]PermissionDefinition, len(items))
	for _, item := range items {
		index[item.Action] = item
	}
	return index
}

func buildPermissionMenuIndex(items []PermissionDefinition) map[string]PermissionDefinition {
	index := make(map[string]PermissionDefinition)
	for _, item := range items {
		if item.MenuKey != "" {
			index[item.MenuKey] = item
		}
	}
	return index
}

// PermissionCatalog returns a copy of the complete permission catalog.
func PermissionCatalog() []PermissionDefinition {
	items := make([]PermissionDefinition, len(permissionCatalog))
	for i, item := range permissionCatalog {
		items[i] = item
		items[i].ResourceTypes = append([]string(nil), item.ResourceTypes...)
	}
	return items
}

// FindPermissionDefinition looks up an action in the catalog.
func FindPermissionDefinition(action string) (PermissionDefinition, bool) {
	item, ok := permissionCatalogByAction[strings.TrimSpace(action)]
	if ok {
		item.ResourceTypes = append([]string(nil), item.ResourceTypes...)
	}
	return item, ok
}

// FindMenuPermissionDefinition returns the catalog action controlling a menu.
func FindMenuPermissionDefinition(menuKey string) (PermissionDefinition, bool) {
	item, ok := permissionCatalogByMenuKey[strings.TrimSpace(menuKey)]
	if ok {
		item.ResourceTypes = append([]string(nil), item.ResourceTypes...)
	}
	return item, ok
}

// ValidateAssignableActions validates, trims and de-duplicates role actions.
func ValidateAssignableActions(actions []string) ([]string, error) {
	unique := make(map[string]struct{}, len(actions))
	for _, raw := range actions {
		action := strings.TrimSpace(raw)
		item, ok := FindPermissionDefinition(action)
		if !ok {
			return nil, fmt.Errorf("unknown action %q", action)
		}
		if !item.Assignable {
			return nil, fmt.Errorf("action %q is not assignable", action)
		}
		unique[action] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for action := range unique {
		result = append(result, action)
	}
	sort.Strings(result)
	return result, nil
}

// ValidatePermissionCatalog checks catalog uniqueness and required metadata.
func ValidatePermissionCatalog() error {
	actions := make(map[string]struct{}, len(permissionCatalog))
	menuKeys := make(map[string]string)
	for i, item := range permissionCatalog {
		if item.Action == "" || item.Module == "" || item.ModuleLabel == "" || item.Label == "" || item.Description == "" {
			return fmt.Errorf("catalog item %d has incomplete metadata", i)
		}
		if _, exists := actions[item.Action]; exists {
			return fmt.Errorf("duplicate action %q", item.Action)
		}
		actions[item.Action] = struct{}{}
		if previous, exists := menuKeys[item.MenuKey]; item.MenuKey != "" && exists {
			return fmt.Errorf("menu key %q is used by %q and %q", item.MenuKey, previous, item.Action)
		}
		if item.MenuKey != "" {
			menuKeys[item.MenuKey] = item.Action
		}
	}
	return nil
}
