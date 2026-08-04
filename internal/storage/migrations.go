package storage

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

type SchemaMigration struct {
	Version   string    `gorm:"primaryKey;size:64" json:"version"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	AppliedAt time.Time `gorm:"not null" json:"applied_at"`
}

func (SchemaMigration) TableName() string {
	return "schema_migrations"
}

type Migration struct {
	Version                  string
	Name                     string
	Run                      func(*gorm.DB) error
	SQLiteDisableForeignKeys bool
}

var migrations = []Migration{
	{
		Version: "202606290001",
		Name:    "prepare metadata sequences",
		Run: func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&model.ResourceSequence{}); err != nil {
				return err
			}
			if tx.Migrator().HasTable(&model.UserSession{}) {
				return repairUserSessions(tx)
			}
			return nil
		},
	},
	{
		Version: "202606290002",
		Name:    "core metadata schema",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(metadataModelsBeforeDatabaseProvisioning()...)
		},
	},
	{
		Version: "202606290003",
		Name:    "reconcile metadata resources",
		Run: func(tx *gorm.DB) error {
			return ReconcileMetadata(tx)
		},
	},
	{
		Version: "202606290004",
		Name:    "global compact session identity",
		Run: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&model.UserSession{}) {
				return nil
			}
			if err := repairUserSessions(tx); err != nil {
				return err
			}
			return tx.AutoMigrate(&model.UserSession{})
		},
	},
	{
		Version: "202606290005",
		Name:    "metadata query indexes",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(metadataModelsBeforeDatabaseProvisioning()...)
		},
	},
	{
		Version: "202607130001",
		Name:    "user groups and resource grants",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.UserGroup{}, &model.UserGroupMember{}, &model.ResourceGrant{})
		},
	},
	{
		Version: "202607160001",
		Name:    "AI access tokens",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AIAccessToken{})
		},
	},
	{
		Version: "202607160002",
		Name:    "encrypted AI token values",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AIAccessToken{})
		},
	},
	{
		Version: "202607170001",
		Name:    "container management endpoints",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.ContainerEndpoint{})
		},
	},
	{
		Version: "202607170002",
		Name:    "user expiry and temporary authorization metadata",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.User{}, &model.TemporaryAccount{}, &model.TemporaryCredential{}, &model.TemporaryAccountGrant{}, &model.AIAccessToken{})
		},
	},
	{
		Version: "202607180001",
		Name:    "database backed super administrator identity",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.User{})
		},
	},
	{
		Version: "202607180002",
		Name:    "temporary access connection password lifecycle",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.ConnectionPassword{})
		},
	},
	{
		Version: "202607180003",
		Name:    "atomic system initialization guard",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.SystemInitialization{})
		},
	},
	{
		Version: "202607180004",
		Name:    "browser sessions and websocket tickets",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AdminSession{}, &webSocketTicketBeforeWebRDP{})
		},
	},
	{
		Version: "202607180005",
		Name:    "remove reversible AI token secrets",
		Run: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable(&model.AIAccessToken{}) {
				return nil
			}
			for _, column := range []string{"access_token", "refresh_token"} {
				if tx.Migrator().HasColumn("ai_access_tokens", column) {
					if err := tx.Exec("ALTER TABLE ai_access_tokens DROP COLUMN " + column).Error; err != nil {
						return fmt.Errorf("drop legacy AI token column %s: %w", column, err)
					}
				}
			}
			return nil
		},
	},
	{
		Version: "202607180006",
		Name:    "database instance upstream TLS policy",
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.DatabaseInstance{})
		},
	},
	{
		Version: "202607180007",
		Name:    "permission logical uniqueness",
		Run:     migratePermissionLogicalUniqueness,
	},
	{
		Version: "202607180008",
		Name:    "database account instance username uniqueness",
		Run:     migrateDatabaseAccountInstanceUsernameUniqueness,
	},
	{
		Version: "202607180009",
		Name:    "database provisioning saga recovery state",
		Run:     migrateDatabaseProvisioningSaga,
	},
	{
		Version: "202607190001",
		Name:    "resource grant logical uniqueness",
		Run:     migrateResourceGrantLogicalUniqueness,
	},
	{
		Version: "202607190002",
		Name:    "audit retention cleanup state",
		Run:     migrateAuditRetentionCleanup,
	},
	{
		Version: webRDPAuditMigrationVersion,
		Name:    "web RDP access control and audit schema",
		Run:     migrateWebRDPAuditSchema,
	},
	{
		Version: auditSessionLeaseMigrationVersion,
		Name:    "audit session lease recovery",
		Run:     migrateAuditSessionLease,
	},
	{
		Version: systemSettingMigrationVersion,
		Name:    "system configuration management",
		Run:     migrateSystemSettings,
	},
	{
		Version: auditDBQueryLargePayloadMigrationVersion,
		Name:    "large database proxy client message support",
		Run:     migrateAuditDBQueryLargePayload,
	},
	{
		Version: databaseGatewayModeMigrationVersion,
		Name:    "database gateway mode system setting",
		Run:     migrateDatabaseGatewayMode,
	},
	{
		Version: databaseTLSDefaultMigrationVersion,
		Name:    "database instance upstream TLS default",
		Run:     migrateDatabaseTLSDefault,
	},
	{
		Version: sshHostIdentityMigrationVersion,
		Name:    "SSH host identity",
		Run:     migrateSSHHostIdentity,
	},
	{
		Version: databaseGatewayClientTLSModeMigrationVersion,
		Name:    "database gateway client TLS mode",
		Run:     migrateDatabaseGatewayClientTLSMode,
	},
	{
		Version: userPreferenceClientsMigrationVersion,
		Name:    "user preference local client fields",
		Run:     migrateUserPreferenceClients,
	},
	{
		Version: removeRDPApprovalMigrationVersion,
		Name:    "remove RDP access approval",
		Run:     migrateRemoveRDPApproval,
	},
	{
		Version:                  auditFieldsMigrationVersion,
		Name:                     "统一审计字段和 active_marker 活跃标记",
		Run:                      migrateAuditFields,
		SQLiteDisableForeignKeys: true,
	},
}

func rejectDuplicateDatabaseAccounts(tx *gorm.DB) error {
	var duplicateGroups []struct {
		InstanceID string `gorm:"column:instance_id"`
	}
	result := tx.Model(&model.DatabaseAccount{}).
		Select("instance_id").
		Group("instance_id, username").
		Having("COUNT(*) > ?", 1).
		Limit(1).
		Scan(&duplicateGroups)
	if result.Error != nil {
		return fmt.Errorf("check duplicate database accounts: %w", result.Error)
	}
	if len(duplicateGroups) > 0 {
		return errors.New("database account uniqueness migration blocked: duplicate database accounts share the same instance_id and username; delete or rename duplicate accounts before retrying")
	}
	return nil
}

func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("storage: nil database")
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		return fmt.Errorf("migrate schema migrations: %w", err)
	}

	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}
	// The 202607240001 audit refactor is intentionally a single unpublished
	// migration. Local development databases may have applied an earlier
	// revision of that same migration before the structured result fields were
	// folded into it. Reconcile only those additive audit columns without
	// creating a second migration version.
	if applied[auditFieldsMigrationVersion] {
		if err := repairAppliedAuditFieldsMigration(db); err != nil {
			return fmt.Errorf(
				"repair applied migration %s: %w",
				auditFieldsMigrationVersion,
				err,
			)
		}
	}
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		migration := migration
		if migration.SQLiteDisableForeignKeys && db.Dialector.Name() == "sqlite" {
			if err := runSQLiteMigrationWithForeignKeysDisabled(db, migration); err != nil {
				return fmt.Errorf("migration %s %s: %w", migration.Version, migration.Name, err)
			}
			continue
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := migration.Run(tx); err != nil {
				return err
			}
			return recordMigration(tx, migration)
		}); err != nil {
			return fmt.Errorf("migration %s %s: %w", migration.Version, migration.Name, err)
		}
	}
	return nil
}

func repairAppliedAuditFieldsMigration(db *gorm.DB) error {
	required := []struct {
		model   any
		columns []string
	}{
		{
			model: &model.AuditEvent{},
			columns: []string{
				"phase", "result", "intent_id", "request_id", "status_code",
			},
		},
		{
			model: &model.LoginAuditLog{},
			columns: []string{
				"phase", "result", "intent_id", "request_id", "status_code",
			},
		},
		{
			model: &model.AuditDBQuery{},
			columns: []string{
				"status", "error_code", "error_message", "rows_affected", "rows",
			},
		},
	}
	needsRepair := false
	for _, item := range required {
		if !db.Migrator().HasTable(item.model) {
			needsRepair = true
			break
		}
		for _, column := range item.columns {
			if !db.Migrator().HasColumn(item.model, column) {
				needsRepair = true
				break
			}
		}
		if needsRepair {
			break
		}
	}
	if !needsRepair {
		return nil
	}
	if err := db.AutoMigrate(
		&model.AuditEvent{},
		&model.LoginAuditLog{},
		&model.AuditDBQuery{},
	); err != nil {
		return fmt.Errorf("reconcile structured audit result fields: %w", err)
	}
	return nil
}

func runSQLiteMigrationWithForeignKeysDisabled(db *gorm.DB, migration Migration) error {
	return db.Connection(func(conn *gorm.DB) (resultErr error) {
		connectionDB := conn.Session(&gorm.Session{NewDB: true})
		var foreignKeys int
		if err := connectionDB.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
			return fmt.Errorf("read SQLite foreign key state: %w", err)
		}
		if foreignKeys != 0 {
			if err := connectionDB.Session(&gorm.Session{NewDB: true}).
				Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
				return fmt.Errorf("disable SQLite foreign keys: %w", err)
			}
			defer func() {
				restoreDB := connectionDB.Session(&gorm.Session{NewDB: true})
				restoreErr := restoreDB.Exec("PRAGMA foreign_keys = ON").Error
				if restoreErr == nil {
					var restoredState int
					restoreErr = restoreDB.Session(&gorm.Session{NewDB: true}).
						Raw("PRAGMA foreign_keys").Scan(&restoredState).Error
					if restoreErr == nil && restoredState != 1 {
						restoreErr = errors.New("pragma remained disabled")
					}
				}
				if restoreErr != nil {
					resultErr = errors.Join(
						resultErr,
						fmt.Errorf("restore SQLite foreign keys: %w", restoreErr),
					)
				}
			}()
			var disabledState int
			if err := connectionDB.Session(&gorm.Session{NewDB: true}).
				Raw("PRAGMA foreign_keys").Scan(&disabledState).Error; err != nil {
				return fmt.Errorf("verify disabled SQLite foreign keys: %w", err)
			}
			if disabledState != 0 {
				return errors.New("disable SQLite foreign keys: pragma remained enabled")
			}
		}

		return connectionDB.Session(&gorm.Session{NewDB: true}).
			Transaction(func(migrationTx *gorm.DB) error {
				if err := migration.Run(
					migrationTx.Session(&gorm.Session{NewDB: true}),
				); err != nil {
					return err
				}
				return recordMigration(
					migrationTx.Session(&gorm.Session{NewDB: true}),
					migration,
				)
			})
	})
}

func recordMigration(db *gorm.DB, migration Migration) error {
	return db.Create(&SchemaMigration{
		Version:   migration.Version,
		Name:      migration.Name,
		AppliedAt: time.Now().UTC(),
	}).Error
}

func appliedMigrations(db *gorm.DB) (map[string]bool, error) {
	var rows []SchemaMigration
	if err := db.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	applied := make(map[string]bool, len(rows))
	for _, row := range rows {
		applied[row.Version] = true
	}
	return applied, nil
}

func ReconcileMetadata(db *gorm.DB) error {
	if db == nil {
		return errors.New("storage: nil database")
	}
	if err := backfillCompactSequences(db); err != nil {
		return err
	}
	if db.Migrator().HasTable(&model.UserSession{}) {
		if err := repairUserSessions(db); err != nil {
			return err
		}
	}
	return backfillResources(db)
}

func backfillCompactSequences(db *gorm.DB) error {
	if db.Migrator().HasTable(&model.HostAccount{}) {
		var maxSeq int
		if err := db.Model(&model.HostAccount{}).
			Select("COALESCE(MAX(resource_seq), 0)").
			Scan(&maxSeq).Error; err != nil {
			return err
		}
		if err := EnsureSequenceNextValue(db, SequenceHostAccount, maxSeq+1); err != nil {
			return err
		}
	}
	if db.Migrator().HasTable(&model.DatabaseAccount{}) {
		var maxSeq int
		if err := db.Model(&model.DatabaseAccount{}).
			Select("COALESCE(MAX(resource_seq), 0)").
			Scan(&maxSeq).Error; err != nil {
			return err
		}
		if err := EnsureSequenceNextValue(db, SequenceDatabaseAccount, maxSeq+1); err != nil {
			return err
		}
	}
	return nil
}

func backfillResources(db *gorm.DB) error {
	if !db.Migrator().HasTable(&model.Resource{}) {
		return nil
	}
	conflictColumns, err := resourceUpsertConflictColumns(db)
	if err != nil {
		return err
	}
	if db.Migrator().HasTable(&model.Host{}) {
		active, args, err := activeRowPredicate(db, "hosts", "hosts")
		if err != nil {
			return err
		}
		var hosts []model.Host
		if err := db.Where(active, args...).Find(&hosts).Error; err != nil {
			return err
		}
		for _, host := range hosts {
			if err := upsertResource(db, conflictColumns, model.ResourceTypeHost, host.ID, hostResourceDisplayName(host), ""); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.HostAccount{}) {
		accountActive, accountArgs, err := activeRowPredicate(
			db,
			"host_accounts",
			"host_accounts",
		)
		if err != nil {
			return err
		}
		hostActive, hostArgs, err := activeRowPredicate(db, "hosts", "hosts")
		if err != nil {
			return err
		}
		var accounts []model.HostAccount
		if err := db.
			Where(accountActive, accountArgs...).
			Where(
				`EXISTS (
					SELECT 1
					FROM hosts
					WHERE hosts.id = host_accounts.host_id
					  AND `+hostActive+`
				)`,
				hostArgs...,
			).
			Preload("Host").
			Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := upsertResource(db, conflictColumns, model.ResourceTypeHostAccount, account.ID, hostAccountResourceDisplayName(account), account.HostID); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.DatabaseInstance{}) {
		active, args, err := activeRowPredicate(
			db,
			"database_instances",
			"database_instances",
		)
		if err != nil {
			return err
		}
		var instances []model.DatabaseInstance
		if err := db.Where(active, args...).Find(&instances).Error; err != nil {
			return err
		}
		for _, inst := range instances {
			if err := upsertResource(db, conflictColumns, model.ResourceTypeDatabaseInstance, inst.ID, databaseInstanceResourceDisplayName(inst), ""); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.DatabaseAccount{}) {
		accountActive, accountArgs, err := activeRowPredicate(
			db,
			"database_accounts",
			"database_accounts",
		)
		if err != nil {
			return err
		}
		instanceActive, instanceArgs, err := activeRowPredicate(
			db,
			"database_instances",
			"database_instances",
		)
		if err != nil {
			return err
		}
		var accounts []model.DatabaseAccount
		if err := db.
			Where(accountActive, accountArgs...).
			Where(
				`EXISTS (
					SELECT 1
					FROM database_instances
					WHERE database_instances.id = database_accounts.instance_id
					  AND `+instanceActive+`
				)`,
				instanceArgs...,
			).
			Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := upsertResource(db, conflictColumns, model.ResourceTypeDatabaseAccount, account.ID, databaseAccountResourceDisplayName(account), account.InstanceID); err != nil {
				return err
			}
		}
	}
	return nil
}

func activeRowPredicate(
	db *gorm.DB,
	table string,
	qualifiedTable string,
) (string, []any, error) {
	if db.Migrator().HasColumn(table, "active_marker") {
		return qualifiedTable + ".active_marker = ?",
			[]any{model.ActiveMarkerValue},
			nil
	}
	if !db.Migrator().HasColumn(table, "deleted_at") {
		return "1 = 1", nil, nil
	}
	castType := "TEXT"
	switch db.Dialector.Name() {
	case "sqlite", "postgres":
	case "mysql":
		castType = "CHAR"
	default:
		return "", nil, fmt.Errorf(
			"storage: unsupported database dialect %q for deleted_at filtering",
			db.Dialector.Name(),
		)
	}
	predicate := fmt.Sprintf(
		"(CAST(%s AS %s) = ? OR CAST(%s AS %s) LIKE ?)",
		qualifiedTable+".deleted_at",
		castType,
		qualifiedTable+".deleted_at",
		castType,
	)
	return predicate, []any{
		fmt.Sprint(model.ActiveMarkerValue),
		"0001-01-01%",
	}, nil
}

func resourceUpsertConflictColumns(db *gorm.DB) ([]clause.Column, error) {
	indexes, err := db.Migrator().GetIndexes("resources")
	if err != nil {
		return nil, fmt.Errorf("get resource indexes: %w", err)
	}
	var legacy []clause.Column
	for _, index := range indexes {
		if !indexIsUnique(index) {
			continue
		}
		switch {
		case columnsMatch(index.Columns(), []string{"type", "resource_id", "active_marker"}):
			return []clause.Column{
				{Name: "type"},
				{Name: "resource_id"},
				{Name: "active_marker"},
			}, nil
		case columnsMatch(index.Columns(), []string{"type", "resource_id", "deleted_at"}):
			return []clause.Column{
				{Name: "type"},
				{Name: "resource_id"},
				{Name: "deleted_at"},
			}, nil
		case columnsMatch(index.Columns(), []string{"type", "resource_id"}):
			legacy = []clause.Column{
				{Name: "type"},
				{Name: "resource_id"},
			}
		}
	}
	if len(legacy) > 0 {
		return legacy, nil
	}
	return nil, errors.New("storage: resources table has no supported unique key")
}

func upsertResource(db *gorm.DB, conflictColumns []clause.Column, resourceType, resourceID, name, parentID string) error {
	resourceType = strings.TrimSpace(resourceType)
	resourceID = strings.TrimSpace(resourceID)
	if resourceType == "" || resourceID == "" {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = resourceID
	}
	resource := model.Resource{
		Type:       resourceType,
		ResourceID: resourceID,
		Name:       name,
		ParentID:   strings.TrimSpace(parentID),
	}
	return db.Clauses(clause.OnConflict{
		Columns: conflictColumns,
		DoUpdates: clause.Assignments(map[string]any{
			"name":      resource.Name,
			"parent_id": resource.ParentID,
		}),
	}).Create(&resource).Error
}

func hostResourceDisplayName(host model.Host) string {
	if name := strings.TrimSpace(host.Name); name != "" {
		return name
	}
	return displayAddress(host.Address, host.Port)
}

func hostAccountResourceDisplayName(account model.HostAccount) string {
	username := strings.TrimSpace(account.Username)
	if username == "" {
		return account.ID
	}
	host := strings.TrimSpace(account.Host.Address)
	port := account.Host.Port
	if host == "" {
		host = strings.TrimSpace(account.HostID)
	}
	if host == "" {
		return username
	}
	return username + "@" + displayAddress(host, port)
}

func databaseInstanceResourceDisplayName(inst model.DatabaseInstance) string {
	if name := strings.TrimSpace(inst.Name); name != "" {
		return name
	}
	return displayAddress(inst.Address, inst.Port)
}

func databaseAccountResourceDisplayName(account model.DatabaseAccount) string {
	if username := strings.TrimSpace(account.Username); username != "" {
		return username
	}
	return account.ID
}

func displayAddress(address string, port int) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if port == 0 {
		return address
	}
	return fmt.Sprintf("%s:%d", address, port)
}

const auditFieldsMigrationVersion = "202607240001"

type activeMarkerModel struct {
	table string
	value any
}

func modelsWithActiveMarker(db *gorm.DB) ([]activeMarkerModel, error) {
	models := make([]activeMarkerModel, 0, len(model.AllModels()))
	seen := make(map[string]struct{}, len(model.AllModels()))
	for _, value := range model.AllModels() {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(value); err != nil {
			return nil, fmt.Errorf("parse active_marker model %T: %w", value, err)
		}
		if statement.Schema.LookUpField("active_marker") == nil {
			continue
		}
		table := statement.Schema.Table
		if _, ok := seen[table]; ok {
			continue
		}
		seen[table] = struct{}{}
		models = append(models, activeMarkerModel{table: table, value: value})
	}
	return models, nil
}

// tablesWithActiveMarker derives the audited business tables from the runtime
// model schemas. This keeps association tables without FullAudit out while
// ensuring newly audited models are not silently missed.
func tablesWithActiveMarker(db *gorm.DB) ([]string, error) {
	models, err := modelsWithActiveMarker(db)
	if err != nil {
		return nil, err
	}
	tables := make([]string, 0, len(models))
	for _, item := range models {
		tables = append(tables, item.table)
	}
	return tables, nil
}

// migrateAuditFields installs the final audit schema and folds both unpublished
// deleted_at representations into active_marker:
//   - deleted_at = 1 or "0001-01-01..." becomes active_marker = 1
//   - deleted_at = NULL or a deletion timestamp becomes active_marker = NULL
//
// Every step is idempotent so a failed MySQL DDL migration can be retried
// safely even though that dialect may commit DDL outside the surrounding
// transaction.
func migrateAuditFields(tx *gorm.DB) error {
	models, err := modelsWithActiveMarker(tx)
	if err != nil {
		return err
	}
	for _, item := range models {
		if !tx.Migrator().HasTable(item.table) {
			continue
		}
		if err := ensureActiveMarkerColumn(tx, item); err != nil {
			return err
		}
		if tx.Migrator().HasColumn(item.table, "deleted_at") {
			if err := migrateLegacyDeletedAtValues(tx, item.table); err != nil {
				return err
			}
			if err := dropLegacyDeletedAtColumn(tx, item); err != nil {
				return err
			}
		} else if err := normalizeActiveMarkerValues(tx, item.table); err != nil {
			return err
		}
		if err := dropLegacyDeletedIndexes(tx, item.table); err != nil {
			return err
		}
	}

	// Build the composite indexes before AutoMigrate. Otherwise GORM would first
	// create the model-declared index name with only its business columns, which
	// rejects valid active/history pairs before it can be repaired.
	if err := MigrateAuditUniqueIndexes(tx); err != nil {
		return err
	}
	if err := tx.AutoMigrate(model.AllModels()...); err != nil {
		return fmt.Errorf("install final audit schema: %w", err)
	}
	if err := MigrateAuditUniqueIndexes(tx); err != nil {
		return err
	}
	if tx.Dialector.Name() == "sqlite" {
		return validateSQLiteForeignKeys(tx.Session(&gorm.Session{NewDB: true}))
	}
	return nil
}

type sqliteSchemaObject struct {
	Type string `gorm:"column:type"`
	Name string `gorm:"column:name"`
	SQL  string `gorm:"column:sql"`
}

func ensureActiveMarkerColumn(tx *gorm.DB, item activeMarkerModel) error {
	if tx.Migrator().HasColumn(item.table, "active_marker") {
		return nil
	}
	if err := tx.Migrator().AddColumn(item.value, "ActiveMarker"); err != nil {
		return fmt.Errorf("add %s.active_marker: %w", item.table, err)
	}
	return nil
}

func migrateLegacyDeletedAtValues(tx *gorm.DB, table string) error {
	expression, err := legacyDeletedAtConversionExpression(tx.Dialector.Name())
	if err != nil {
		return fmt.Errorf("migrate %s.deleted_at: %w", table, err)
	}
	if err := tx.Table(table).
		Where("1 = 1").
		UpdateColumn(
			"active_marker",
			gorm.Expr(expression, "1", "0001-01-01%", model.ActiveMarkerValue),
		).Error; err != nil {
		return fmt.Errorf("migrate %s.deleted_at to active_marker: %w", table, err)
	}
	return nil
}

func legacyDeletedAtConversionExpression(dialect string) (string, error) {
	castType := "TEXT"
	switch dialect {
	case "sqlite", "postgres":
	case "mysql":
		castType = "CHAR"
	default:
		return "", fmt.Errorf("unsupported database dialect %q", dialect)
	}
	return fmt.Sprintf(
		"CASE WHEN CAST(deleted_at AS %s) = ? OR CAST(deleted_at AS %s) LIKE ? THEN ? ELSE NULL END",
		castType,
		castType,
	), nil
}

func normalizeActiveMarkerValues(tx *gorm.DB, table string) error {
	if err := tx.Table(table).
		Where("active_marker IS NOT NULL AND active_marker <> ?", model.ActiveMarkerValue).
		UpdateColumn("active_marker", model.ActiveMarkerValue).Error; err != nil {
		return fmt.Errorf("normalize %s.active_marker: %w", table, err)
	}
	return nil
}

func dropLegacyDeletedAtColumn(tx *gorm.DB, item activeMarkerModel) error {
	var preserved []sqliteSchemaObject
	if tx.Dialector.Name() == "sqlite" {
		objects, err := sqliteTableSchemaObjects(tx, item.table)
		if err != nil {
			return err
		}
		for _, object := range objects {
			if strings.HasSuffix(strings.ToLower(object.Name), "_deleted") ||
				strings.Contains(strings.ToLower(object.SQL), "deleted_at") {
				if object.Type == "trigger" {
					if err := tx.Exec(
						"DROP TRIGGER IF EXISTS " + quoteIdentifier(tx, object.Name),
					).Error; err != nil {
						return fmt.Errorf(
							"drop legacy SQLite trigger %s on %s: %w",
							object.Name,
							item.table,
							err,
						)
					}
				}
				continue
			}
			preserved = append(preserved, object)
		}
	}
	if err := dropLegacyDeletedIndexes(tx, item.table); err != nil {
		return err
	}
	if err := tx.Migrator().DropColumn(item.value, "deleted_at"); err != nil {
		return fmt.Errorf("drop %s.deleted_at: %w", item.table, err)
	}
	if tx.Dialector.Name() == "sqlite" {
		return restoreSQLiteSchemaObjects(tx, item.table, preserved)
	}
	return nil
}

func dropLegacyDeletedIndexes(tx *gorm.DB, table string) error {
	indexes, err := tx.Migrator().GetIndexes(table)
	if err != nil {
		return fmt.Errorf("get indexes for %s: %w", table, err)
	}
	for _, index := range indexes {
		legacy := strings.HasSuffix(strings.ToLower(index.Name()), "_deleted")
		for _, column := range index.Columns() {
			if strings.EqualFold(column, "deleted_at") {
				legacy = true
				break
			}
		}
		if !legacy {
			continue
		}
		if err := tx.Migrator().DropIndex(table, index.Name()); err != nil {
			return fmt.Errorf(
				"drop legacy index %s on %s: %w",
				index.Name(),
				table,
				err,
			)
		}
	}
	return nil
}

func quoteIdentifier(db *gorm.DB, identifier string) string {
	if db.Dialector.Name() == "mysql" {
		return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func sqliteTableSchemaObjects(
	tx *gorm.DB,
	table string,
) ([]sqliteSchemaObject, error) {
	var objects []sqliteSchemaObject
	if err := tx.Session(&gorm.Session{NewDB: true}).Raw(
		`SELECT type, name, sql
		 FROM sqlite_master
		 WHERE tbl_name = ?
		   AND type IN ('index', 'trigger')
		   AND sql IS NOT NULL
		 ORDER BY CASE type WHEN 'index' THEN 0 ELSE 1 END, name`,
		table,
	).Scan(&objects).Error; err != nil {
		return nil, fmt.Errorf("capture SQLite schema objects for %s: %w", table, err)
	}
	return objects, nil
}

func restoreSQLiteSchemaObjects(
	tx *gorm.DB,
	table string,
	objects []sqliteSchemaObject,
) error {
	for _, object := range objects {
		var count int64
		if err := tx.Raw(
			`SELECT COUNT(*)
			 FROM sqlite_master
			 WHERE type = ? AND name = ?`,
			object.Type,
			object.Name,
		).Scan(&count).Error; err != nil {
			return fmt.Errorf(
				"check SQLite %s %s on %s: %w",
				object.Type,
				object.Name,
				table,
				err,
			)
		}
		if count != 0 {
			continue
		}
		if err := tx.Exec(object.SQL).Error; err != nil {
			return fmt.Errorf(
				"restore SQLite %s %s on %s: %w",
				object.Type,
				object.Name,
				table,
				err,
			)
		}
	}
	return nil
}

func validateSQLiteForeignKeys(tx *gorm.DB) error {
	var violations []struct {
		Table  string `gorm:"column:table"`
		RowID  int64  `gorm:"column:rowid"`
		Parent string `gorm:"column:parent"`
		FKID   int    `gorm:"column:fkid"`
	}
	if err := tx.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return fmt.Errorf("validate SQLite foreign keys: %w", err)
	}
	if len(violations) != 0 {
		return fmt.Errorf("validate SQLite foreign keys: %d violation(s)", len(violations))
	}
	return nil
}
