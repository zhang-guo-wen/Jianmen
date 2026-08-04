---
title: 版本迁移机制
---

# 版本迁移机制

Jianmen 的元数据库结构演进由两类迁移共同承担:**版本化迁移**(记录版本,逐版升级)与**幂等迁移**(每次启动检测现状,无需迁移则跳过)。

## 版本化迁移

- 迁移定义集中在 `internal/storage/migrations.go` 的 `migrations` 数组,每项包含版本号(如 `202607240001`)、名称与执行函数。
- 已应用版本记录在 `schema_migrations` 表(version/name/applied_at)。
- `Migrate()` 启动时读取已应用集合,按序执行**未应用**的迁移;每个迁移在事务中执行,成功后记录版本,失败整体回滚。
- 新数据库 `schema_migrations` 为空,按序执行全部迁移;迁移均为幂等操作(建表/加列/加索引),新库执行后即为最新结构。
- 版本化迁移仅由生产启动的 `storage.Migrate` 执行;测试路径使用 `storage.AutoMigrate` 直接建最新结构。

## 幂等迁移

- 不记录版本,每次启动执行,内部检测现状后决定是否动作。
- `MigrateAuditUniqueIndexes`(internal/storage/migration_audit_indexes.go):将业务表唯一索引统一重建为 `(business_key..., active_marker)` 复合唯一索引,使软删行(`active_marker = NULL`)不占用唯一性,支持「停用后可重建」。
- `MigrateHostAccountIDActiveIndex`:将 `host_accounts.id` 从单列主键迁移为 `(id, active_marker)` 复合唯一索引(SQLite 通过表重建实现,含备份表与行数校验)。
- 生产启动在 `storage.Migrate` 之后由 `cmd/jianmen/bootstrap.go` 显式执行;测试由 `storage.AutoMigrate` 内执行。

## 执行时机对比

| 路径 | 版本化迁移 | 幂等迁移 |
|------|-----------|---------|
| 生产启动 | `storage.Migrate`(schema_migrations 判断) | bootstrap.go 在 Migrate 后显式调用 |
| 测试/开发 | `storage.AutoMigrate` 建最新结构 | AutoMigrate 内自动执行 |
| 新库 | 全量按序执行(幂等,结果=最新结构) | 检测到旧结构才动作 |

## 设计原则

- **幂等**:同一迁移可在任意库上重复执行,结果一致。
- **复合唯一索引**:需要「停用后可重建」的唯一业务键统一使用 `(business_key..., active_marker)`,`active_marker` 必须为最后一列;软删行置 NULL 后不占用唯一性,可保留多条历史。
- **软删除**:逻辑删除统一置 `active_marker = NULL` 并写 `status = disabled`、审计字段,不物理删除行。
- **停用后可重建**:同一业务键活跃状态只能存在一条,停用后允许重建(设计文档:docs/2026-07-23-auditable-fields-design.md)。

## 跨库兼容性

- **SQLite**:不支持 ALTER 删除主键,结构性变更通过表重建(改名备份 → 建新表 → 显式列复制 → 行数校验 → 删备份),全程事务。
- **MySQL / PostgreSQL**:主键约束可直接删除(ALTER TABLE DROP PRIMARY KEY / DROP CONSTRAINT)。
- 迁移代码按 `db.Dialector().Name()` 分派,需覆盖 sqlite / mysql / postgres 三种驱动。
