package storage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// uniqueIndexMigration 定义一次唯一索引迁移。
type uniqueIndexMigration struct {
	table     string   // 表名
	indexName string   // 新索引名
	columns   []string // 新索引列（包含 active_marker）
}

// allIndexMigrations 列出所有需要重建为包含 active_marker 的复合唯一索引。
func allIndexMigrations() []uniqueIndexMigration {
	return []uniqueIndexMigration{
		// 核心模型
		{table: "users", indexName: "idx_users_username_active", columns: []string{"username", "active_marker"}},
		{table: "roles", indexName: "idx_roles_name_active", columns: []string{"name", "active_marker"}},
		{table: "permissions", indexName: "idx_permissions_logic_active", columns: []string{"action", "resource_type", "resource_id", "effect", "active_marker"}},
		{table: "applications", indexName: "idx_applications_listen_port_active", columns: []string{"listen_port", "active_marker"}},
		{table: "admin_sessions", indexName: "idx_admin_sessions_secret_hash_active", columns: []string{"secret_hash", "active_marker"}},
		{table: "websocket_tickets", indexName: "idx_websocket_tickets_secret_hash_active", columns: []string{"secret_hash", "active_marker"}},

		// 资源模型
		{table: "resources", indexName: "idx_resources_type_resource_id_active", columns: []string{"type", "resource_id", "active_marker"}},
		{table: "resource_groups", indexName: "idx_resource_groups_name_type_active", columns: []string{"name", "group_type", "active_marker"}},
		{table: "host_accounts", indexName: "idx_host_accounts_resource_id_active", columns: []string{"resource_id", "active_marker"}},
		{table: "database_instances", indexName: "idx_database_instances_name_active", columns: []string{"name", "active_marker"}},
		{table: "database_accounts", indexName: "idx_dba_instance_username_active", columns: []string{"instance_id", "username", "active_marker"}},
		{table: "database_accounts", indexName: "idx_dba_unique_name_active", columns: []string{"unique_name", "active_marker"}},
		{table: "database_accounts", indexName: "idx_dba_resource_id_active", columns: []string{"resource_id", "active_marker"}},
		{table: "database_accounts", indexName: "idx_dba_prov_op_active", columns: []string{"provisioning_operation_id", "active_marker"}},
		{table: "resource_grants", indexName: "idx_rg_logic_active", columns: []string{"principal_type", "principal_id", "resource_type", "resource_id", "effect", "active_marker"}},

		// 会话/访问模型
		{table: "user_sessions", indexName: "idx_user_sessions_session_seq_active", columns: []string{"session_seq", "active_marker"}},
		{table: "user_sessions", indexName: "idx_user_sessions_session_id_active", columns: []string{"session_id", "active_marker"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_session_id_active", columns: []string{"session_id", "active_marker"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_username_active", columns: []string{"username", "active_marker"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_access_hash_active", columns: []string{"access_token_hash", "active_marker"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_refresh_hash_active", columns: []string{"refresh_token_hash", "active_marker"}},

		// 设置模型
		{table: "system_setting_revisions", indexName: "idx_ss_rev_revision_active", columns: []string{"revision", "active_marker"}},
		{table: "user_groups", indexName: "idx_user_groups_name_active", columns: []string{"name", "active_marker"}},

		// 数据库供给
		{table: "database_provisioning_operations", indexName: "idx_dpo_actor_kind_idem_active", columns: []string{"actor_id", "kind", "idempotency_key", "active_marker"}},
		{table: "database_provisioning_operations", indexName: "idx_dpo_upstream_username_active", columns: []string{"upstream_username", "active_marker"}},
	}
}

// MigrateAuditUniqueIndexes 幂等地将业务表的唯一索引重建为包含 active_marker 的复合索引。
// 缺少表或列时会跳过，因此可在 AutoMigrate 前后调用：前置调用保护已有历史
// 数据，后置调用补齐新建表。
func MigrateAuditUniqueIndexes(db *gorm.DB) error {
	migrations := allIndexMigrations()

	for _, m := range migrations {
		if !db.Migrator().HasTable(m.table) {
			continue
		}
		hasAllColumns := true
		for _, column := range m.columns {
			if !db.Migrator().HasColumn(m.table, column) {
				hasAllColumns = false
				break
			}
		}
		if !hasAllColumns {
			continue
		}

		indexes, err := db.Migrator().GetIndexes(m.table)
		if err != nil {
			return fmt.Errorf("get indexes for %s: %w", m.table, err)
		}

		indexIsCurrent := false
		for _, idx := range indexes {
			if idx.Name() != m.indexName {
				continue
			}
			if indexIsUnique(idx) && columnsMatch(idx.Columns(), m.columns) {
				indexIsCurrent = true
				break
			}
			if err := db.Migrator().DropIndex(m.table, m.indexName); err != nil {
				return fmt.Errorf("drop malformed index %s on %s: %w", m.indexName, m.table, err)
			}
		}
		if !indexIsCurrent {
			// Build the replacement first. MySQL may use the legacy unique index
			// to enforce a foreign key and refuses to drop it until another
			// index with the foreign-key columns as its prefix is available.
			colList := strings.Join(m.columns, ", ")
			sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", m.indexName, m.table, colList)
			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("create index %s on %s: %w", m.indexName, m.table, err)
			}
		}

		// 找到与新索引业务列匹配的旧唯一索引并删除
		bizCols := m.columns[:len(m.columns)-1] // 去掉 active_marker 得到业务列
		for _, idx := range indexes {
			if idx.Name() == m.indexName {
				continue
			}
			if !indexIsUnique(idx) {
				continue
			}
			if columnsMatch(idx.Columns(), bizCols) {
				if err := db.Migrator().DropIndex(m.table, idx.Name()); err != nil {
					return fmt.Errorf("drop old index %s on %s: %w", idx.Name(), m.table, err)
				}
			}
		}
	}

	return nil
}

// indexIsUnique 判断索引是否唯一。
func indexIsUnique(idx gorm.Index) bool {
	type uniqueChecker interface{ Unique() (bool, bool) }
	if u, ok := idx.(uniqueChecker); ok {
		unique, _ := u.Unique()
		return unique
	}
	return false
}

// columnsMatch 判断两列集合是否相同（忽略大小写）。
func columnsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}
