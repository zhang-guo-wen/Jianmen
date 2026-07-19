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
	Version string
	Name    string
	Run     func(*gorm.DB) error
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
		Run: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.Permission{})
		},
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
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		migration := migration
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := migration.Run(tx); err != nil {
				return err
			}
			return tx.Create(&SchemaMigration{
				Version:   migration.Version,
				Name:      migration.Name,
				AppliedAt: time.Now().UTC(),
			}).Error
		}); err != nil {
			return fmt.Errorf("migration %s %s: %w", migration.Version, migration.Name, err)
		}
	}
	return nil
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
	if db.Migrator().HasTable(&model.Host{}) {
		var hosts []model.Host
		if err := db.Find(&hosts).Error; err != nil {
			return err
		}
		for _, host := range hosts {
			if err := upsertResource(db, model.ResourceTypeHost, host.ID, hostResourceDisplayName(host), ""); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.HostAccount{}) {
		var accounts []model.HostAccount
		if err := db.Preload("Host").Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := upsertResource(db, model.ResourceTypeHostAccount, account.ID, hostAccountResourceDisplayName(account), account.HostID); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.DatabaseInstance{}) {
		var instances []model.DatabaseInstance
		if err := db.Find(&instances).Error; err != nil {
			return err
		}
		for _, inst := range instances {
			if err := upsertResource(db, model.ResourceTypeDatabaseInstance, inst.ID, databaseInstanceResourceDisplayName(inst), ""); err != nil {
				return err
			}
		}
	}
	if db.Migrator().HasTable(&model.DatabaseAccount{}) {
		var accounts []model.DatabaseAccount
		if err := db.Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := upsertResource(db, model.ResourceTypeDatabaseAccount, account.ID, databaseAccountResourceDisplayName(account), account.InstanceID); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertResource(db *gorm.DB, resourceType, resourceID, name, parentID string) error {
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
		Columns: []clause.Column{{Name: "type"}, {Name: "resource_id"}},
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
