package storage

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

// MigrateHostAccountIDActiveIndex 幂等地把 host_accounts.id 从单列主键迁移为
// (id, active_marker) 复合唯一索引，落实设计文档「停用后可重建」的语义：
// 软删行 active_marker = NULL 不再占用 id 的唯一性，重建同 ID 账号不再冲突。
//
// 列定义保持不变，仅移除主键约束；复合唯一索引由 MigrateAuditUniqueIndexes
// 统一补齐（allIndexMigrations 中的 idx_host_accounts_id_active）。
func MigrateHostAccountIDActiveIndex(db *gorm.DB) error {
	if !db.Migrator().HasTable("host_accounts") {
		return nil
	}
	switch db.Dialector.Name() {
	case "sqlite":
		return migrateHostAccountIDActiveSQLite(db)
	case "mysql":
		return migrateHostAccountIDActiveMySQL(db)
	case "postgres":
		return migrateHostAccountIDActivePostgres(db)
	default:
		return nil
	}
}

// migrateHostAccountIDActiveSQLite 通过表重建移除主键：
//  1. 旧表改名为备份表（数据完整保留）；
//  2. 按旧 DDL 建新表（去掉 PRIMARY KEY，id 加 NOT NULL）；
//  3. 显式列名复制并校验行数一致；
//  4. 确认无误后删除备份表并重建索引。
//
// 全程在事务中执行，任一步失败整体回滚；即使进程中断，备份表仍保留数据。
func migrateHostAccountIDActiveSQLite(db *gorm.DB) error {
	var ddl string
	if err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'host_accounts'`).Scan(&ddl).Error; err != nil {
		return fmt.Errorf("read host_accounts ddl: %w", err)
	}
	if ddl == "" {
		return nil
	}
	// 已迁移（无主键）则跳过
	if !strings.Contains(strings.ToUpper(ddl), "PRIMARY KEY") {
		return nil
	}

	// 收集旧表索引 SQL（重建时原样恢复；idx_host_accounts_id_active 由
	// MigrateAuditUniqueIndexes 统一校验列后补齐）
	var indexRows []struct{ SQL string }
	if err := db.Raw(`SELECT sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'host_accounts' AND sql IS NOT NULL`).Scan(&indexRows).Error; err != nil {
		return fmt.Errorf("read host_accounts indexes: %w", err)
	}
	indexSQLs := make([]string, 0, len(indexRows))
	for _, row := range indexRows {
		indexSQLs = append(indexSQLs, row.SQL)
	}

	columns, err := sqliteColumnNames(ddl)
	if err != nil {
		return err
	}
	newDDL, err := sqliteDDLWithoutPrimaryKey(ddl)
	if err != nil {
		return err
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`ALTER TABLE host_accounts RENAME TO host_accounts_mig_bak`).Error; err != nil {
			return fmt.Errorf("rename host_accounts to backup: %w", err)
		}
		if err := tx.Exec(newDDL).Error; err != nil {
			return fmt.Errorf("create host_accounts without primary key: %w", err)
		}
		colList := strings.Join(columns, ", ")
		if err := tx.Exec(fmt.Sprintf(
			"INSERT INTO host_accounts (%s) SELECT %s FROM host_accounts_mig_bak",
			colList, colList,
		)).Error; err != nil {
			return fmt.Errorf("copy host_accounts rows: %w", err)
		}
		var before, after int64
		if err := tx.Table("host_accounts_mig_bak").Count(&before).Error; err != nil {
			return fmt.Errorf("count backup rows: %w", err)
		}
		if err := tx.Table("host_accounts").Count(&after).Error; err != nil {
			return fmt.Errorf("count migrated rows: %w", err)
		}
		if before != after {
			return fmt.Errorf("host_accounts row count mismatch: backup=%d migrated=%d", before, after)
		}
		if err := tx.Exec(`DROP TABLE host_accounts_mig_bak`).Error; err != nil {
			return fmt.Errorf("drop backup table: %w", err)
		}
		for _, stmt := range indexSQLs {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("rebuild index: %w", err)
			}
		}
		return nil
	})
}

// migrateHostAccountIDActiveMySQL 移除 MySQL 的单列主键。
func migrateHostAccountIDActiveMySQL(db *gorm.DB) error {
	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'host_accounts'
		AND CONSTRAINT_TYPE = 'PRIMARY KEY'`).Scan(&count).Error; err != nil {
		return fmt.Errorf("check host_accounts primary key: %w", err)
	}
	if count == 0 {
		return nil
	}
	if err := db.Exec(`ALTER TABLE host_accounts MODIFY id VARCHAR(64) NOT NULL, DROP PRIMARY KEY`).Error; err != nil {
		return fmt.Errorf("drop host_accounts primary key: %w", err)
	}
	return nil
}

// migrateHostAccountIDActivePostgres 移除 PostgreSQL 的单列主键约束。
func migrateHostAccountIDActivePostgres(db *gorm.DB) error {
	var constraintName string
	if err := db.Raw(`SELECT conname FROM pg_constraint
		WHERE conrelid = 'host_accounts'::regclass AND contype = 'p'`).Scan(&constraintName).Error; err != nil {
		return fmt.Errorf("check host_accounts primary key: %w", err)
	}
	if constraintName == "" {
		return nil
	}
	if err := db.Exec(fmt.Sprintf(
		`ALTER TABLE host_accounts DROP CONSTRAINT %s`,
		quotePostgresIdent(constraintName),
	)).Error; err != nil {
		return fmt.Errorf("drop host_accounts primary key: %w", err)
	}
	return nil
}

// sqliteColumnNames 从 SQLite 建表 DDL 中提取列名清单（跳过约束子句）。
func sqliteColumnNames(ddl string) ([]string, error) {
	start := strings.Index(ddl, "(")
	end := strings.LastIndex(ddl, ")")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("invalid sqlite ddl: %s", ddl)
	}
	body := ddl[start+1 : end]
	var columns []string
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "PRIMARY") || strings.HasPrefix(upper, "CONSTRAINT") ||
			strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "FOREIGN") ||
			strings.HasPrefix(upper, "CHECK") {
			continue
		}
		m := sqliteColumnNameRe.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("cannot parse column definition: %q", part)
		}
		columns = append(columns, m[1])
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("no columns found in sqlite ddl: %s", ddl)
	}
	return columns, nil
}

var sqliteColumnNameRe = regexp.MustCompile(`^[` + "`\"'" + `]?([A-Za-z_][A-Za-z0-9_]*)`)

// sqliteDDLWithoutPrimaryKey 移除 DDL 中的单列主键并给 id 加 NOT NULL。
func sqliteDDLWithoutPrimaryKey(ddl string) (string, error) {
	pkRe := regexp.MustCompile(`(?i),\s*PRIMARY\s+KEY\s*\(\s*[` + "`\"'" + `]?id[` + "`\"'" + `]?\s*\)`)
	out := pkRe.ReplaceAllString(ddl, "")
	if strings.Contains(strings.ToUpper(out), "PRIMARY KEY") {
		return "", fmt.Errorf("failed to remove primary key from ddl: %s", ddl)
	}
	// id 列加 NOT NULL（已带 NOT NULL 则跳过）
	idRe := regexp.MustCompile(`(?i)([` + "`\"'" + `]?id[` + "`\"'" + `]?\s+text\s+)`)
	loc := idRe.FindStringIndex(out)
	if loc == nil {
		return out, nil
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(out[loc[1]:])), "NOT NULL") {
		return out, nil
	}
	out = out[:loc[1]] + "NOT NULL " + out[loc[1]:]
	return out, nil
}

func quotePostgresIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
