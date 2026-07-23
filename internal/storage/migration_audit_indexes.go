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
	columns   []string // 新索引列（包含 deleted_at 的蛇形列名）
}

// allIndexMigrations 列出所有需要重建为包含 deleted_at 的复合唯一索引。
func allIndexMigrations() []uniqueIndexMigration {
	return []uniqueIndexMigration{
		// 核心模型
		{table: "users", indexName: "idx_users_username_deleted", columns: []string{"username", "deleted_at"}},
		{table: "roles", indexName: "idx_roles_name_deleted", columns: []string{"name", "deleted_at"}},
		{table: "permissions", indexName: "idx_permissions_logic_deleted", columns: []string{"action", "resource_type", "resource_id", "effect", "deleted_at"}},
		{table: "applications", indexName: "idx_applications_listen_port_deleted", columns: []string{"listen_port", "deleted_at"}},
		{table: "admin_sessions", indexName: "idx_admin_sessions_secret_hash_deleted", columns: []string{"secret_hash", "deleted_at"}},
		{table: "websocket_tickets", indexName: "idx_websocket_tickets_secret_hash_deleted", columns: []string{"secret_hash", "deleted_at"}},

		// 资源模型
		{table: "resources", indexName: "idx_resources_type_resource_id_deleted", columns: []string{"type", "resource_id", "deleted_at"}},
		{table: "resource_groups", indexName: "idx_resource_groups_name_type_deleted", columns: []string{"name", "group_type", "deleted_at"}},
		{table: "host_accounts", indexName: "idx_host_accounts_resource_id_deleted", columns: []string{"resource_id", "deleted_at"}},
		{table: "database_instances", indexName: "idx_database_instances_name_deleted", columns: []string{"name", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_instance_username_deleted", columns: []string{"instance_id", "username", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_unique_name_deleted", columns: []string{"unique_name", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_resource_id_deleted", columns: []string{"resource_id", "deleted_at"}},
		{table: "database_accounts", indexName: "idx_dba_prov_op_deleted", columns: []string{"provisioning_operation_id", "deleted_at"}},
		{table: "resource_grants", indexName: "idx_rg_logic_deleted", columns: []string{"principal_type", "principal_id", "resource_type", "resource_id", "effect", "deleted_at"}},

		// 会话/访问模型
		{table: "user_sessions", indexName: "idx_user_sessions_session_seq_deleted", columns: []string{"session_seq", "deleted_at"}},
		{table: "user_sessions", indexName: "idx_user_sessions_session_id_deleted", columns: []string{"session_id", "deleted_at"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_session_id_deleted", columns: []string{"session_id", "deleted_at"}},
		{table: "temporary_accounts", indexName: "idx_temp_accounts_username_deleted", columns: []string{"username", "deleted_at"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_access_hash_deleted", columns: []string{"access_token_hash", "deleted_at"}},
		{table: "ai_access_tokens", indexName: "idx_ai_tokens_refresh_hash_deleted", columns: []string{"refresh_token_hash", "deleted_at"}},

		// 设置模型
		{table: "system_setting_revisions", indexName: "idx_ss_rev_revision_deleted", columns: []string{"revision", "deleted_at"}},
		{table: "user_groups", indexName: "idx_user_groups_name_deleted", columns: []string{"name", "deleted_at"}},

		// 数据库供给
		{table: "database_provisioning_operations", indexName: "idx_dpo_actor_kind_idem_deleted", columns: []string{"actor_id", "kind", "idempotency_key", "deleted_at"}},
		{table: "database_provisioning_operations", indexName: "idx_dpo_upstream_username_deleted", columns: []string{"upstream_username", "deleted_at"}},
	}
}

// MigrateAuditUniqueIndexes 幂等地将业务表的唯一索引重建为包含 deleted_at 的复合索引。
// 应在 GORM AutoMigrate 之后调用。
func MigrateAuditUniqueIndexes(db *gorm.DB) error {
	migrations := allIndexMigrations()

	for _, m := range migrations {
		// 新索引已存在则跳过
		if db.Migrator().HasIndex(m.table, m.indexName) {
			continue
		}

		// 找到与新索引业务列匹配的旧唯一索引并删除
		indexes, err := db.Migrator().GetIndexes(m.table)
		if err != nil {
			return fmt.Errorf("get indexes for %s: %w", m.table, err)
		}

		bizCols := m.columns[:len(m.columns)-1] // 去掉 deleted_at 得到业务列
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

		// 创建新的复合唯一索引
		colList := strings.Join(m.columns, ", ")
		sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", m.indexName, m.table, colList)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("create index %s on %s: %w", m.indexName, m.table, err)
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
