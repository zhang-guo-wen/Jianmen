package storage

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jianmen/internal/model"
)

func TestReconcileMetadataUsesActiveResourceUniqueKey(t *testing.T) {
	for _, test := range []struct {
		name         string
		migrateIndex bool
	}{
		{name: "legacy business key"},
		{name: "active marker key", migrateIndex: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := gorm.Open(
				sqlite.Open(filepath.Join(t.TempDir(), "reconcile.db")),
				&gorm.Config{},
			)
			if err != nil {
				t.Fatalf("open database: %v", err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatalf("get SQL database: %v", err)
			}
			t.Cleanup(func() {
				_ = sqlDB.Close()
			})
			if err := db.AutoMigrate(
				&model.ResourceSequence{},
				&model.Host{},
				&model.HostAccount{},
				&model.Resource{},
			); err != nil {
				t.Fatalf("migrate metadata tables: %v", err)
			}
			if test.migrateIndex {
				if err := MigrateAuditUniqueIndexes(db); err != nil {
					t.Fatalf("migrate active-resource unique index: %v", err)
				}
			}

			host := model.Host{
				ID:       "host-1",
				Name:     "Original host",
				Address:  "192.0.2.10",
				Port:     22,
				Protocol: "ssh",
			}
			if err := db.Create(&host).Error; err != nil {
				t.Fatalf("create host: %v", err)
			}
			if err := ReconcileMetadata(db); err != nil {
				t.Fatalf("first metadata reconciliation: %v", err)
			}

			if err := db.Model(&model.Host{}).
				Where("id = ?", host.ID).
				Update("name", "Renamed host").Error; err != nil {
				t.Fatalf("rename host: %v", err)
			}
			if err := ReconcileMetadata(db); err != nil {
				t.Fatalf("second metadata reconciliation: %v", err)
			}

			var resources []model.Resource
			if err := db.Where(
				"type = ? AND resource_id = ?",
				model.ResourceTypeHost,
				host.ID,
			).Find(&resources).Error; err != nil {
				t.Fatalf("load reconciled resource: %v", err)
			}
			if len(resources) != 1 {
				t.Fatalf("resource count = %d, want 1", len(resources))
			}
			if resources[0].Name != "Renamed host" {
				t.Fatalf("resource name = %q, want %q", resources[0].Name, "Renamed host")
			}
			if resources[0].ActiveMarker == nil || *resources[0].ActiveMarker != model.ActiveMarkerValue {
				t.Fatalf("resource active marker = %v, want active", resources[0].ActiveMarker)
			}

			if err := db.Model(&model.Host{}).
				Where("id = ?", host.ID).
				Updates(map[string]any{
					"name":          "Deleted host",
					"active_marker": nil,
				}).Error; err != nil {
				t.Fatalf("tombstone host: %v", err)
			}
			if err := db.Model(&model.Resource{}).
				Where("type = ? AND resource_id = ?", model.ResourceTypeHost, host.ID).
				Updates(map[string]any{
					"name":          "Deleted resource",
					"active_marker": nil,
				}).Error; err != nil {
				t.Fatalf("tombstone resource: %v", err)
			}
			if err := ReconcileMetadata(db); err != nil {
				t.Fatalf("reconcile tombstones: %v", err)
			}

			var tombstonedResources []model.Resource
			if err := db.Where(
				"type = ? AND resource_id = ?",
				model.ResourceTypeHost,
				host.ID,
			).Find(&tombstonedResources).Error; err != nil {
				t.Fatalf("load tombstoned resource: %v", err)
			}
			if len(tombstonedResources) != 1 {
				t.Fatalf("tombstoned resource count = %d, want 1", len(tombstonedResources))
			}
			if tombstonedResources[0].Name != "Deleted resource" {
				t.Fatalf(
					"tombstoned resource name = %q, want unchanged",
					tombstonedResources[0].Name,
				)
			}
			if tombstonedResources[0].ActiveMarker != nil {
				t.Fatalf(
					"tombstoned resource marker = %v, want nil",
					tombstonedResources[0].ActiveMarker,
				)
			}

			deletedWithoutResource := model.Host{
				ID:       "host-deleted-without-resource",
				Name:     "Never backfill",
				Address:  "192.0.2.20",
				Port:     22,
				Protocol: "ssh",
			}
			if err := db.Create(&deletedWithoutResource).Error; err != nil {
				t.Fatalf("create second host: %v", err)
			}
			if err := db.Model(&model.Host{}).
				Where("id = ?", deletedWithoutResource.ID).
				Update("active_marker", nil).Error; err != nil {
				t.Fatalf("tombstone second host: %v", err)
			}
			accountUnderDeletedHost := model.HostAccount{
				ID:          "account-under-deleted-host",
				HostID:      deletedWithoutResource.ID,
				Username:    "root",
				Status:      "active",
				ResourceSeq: 41,
				ResourceID:  "0041",
			}
			if err := db.Omit("Host").Create(&accountUnderDeletedHost).Error; err != nil {
				t.Fatalf("create account under deleted host: %v", err)
			}
			deletedHighSequenceAccount := model.HostAccount{
				ID:          "deleted-high-sequence-account",
				HostID:      deletedWithoutResource.ID,
				Username:    "deleted-root",
				Status:      "active",
				ResourceSeq: 42,
				ResourceID:  "0042",
			}
			if err := db.Omit("Host").Create(&deletedHighSequenceAccount).Error; err != nil {
				t.Fatalf("create high-sequence account: %v", err)
			}
			if err := db.Model(&model.HostAccount{}).
				Where("id = ?", deletedHighSequenceAccount.ID).
				Update("active_marker", nil).Error; err != nil {
				t.Fatalf("tombstone high-sequence account: %v", err)
			}
			if err := db.Where("name = ?", SequenceHostAccount).
				Delete(&model.ResourceSequence{}).Error; err != nil {
				t.Fatalf("remove host-account sequence: %v", err)
			}
			if err := ReconcileMetadata(db); err != nil {
				t.Fatalf("reconcile host without resource: %v", err)
			}
			var resurrectedCount int64
			if err := db.Model(&model.Resource{}).
				Where(
					"type = ? AND resource_id = ?",
					model.ResourceTypeHost,
					deletedWithoutResource.ID,
				).
				Count(&resurrectedCount).Error; err != nil {
				t.Fatalf("count resources for deleted host: %v", err)
			}
			if resurrectedCount != 0 {
				t.Fatalf("deleted host resources = %d, want 0", resurrectedCount)
			}
			if err := db.Model(&model.Resource{}).
				Where(
					"type = ? AND resource_id = ?",
					model.ResourceTypeHostAccount,
					accountUnderDeletedHost.ID,
				).
				Count(&resurrectedCount).Error; err != nil {
				t.Fatalf("count resources for account under deleted host: %v", err)
			}
			if resurrectedCount != 0 {
				t.Fatalf(
					"account-under-deleted-host resources = %d, want 0",
					resurrectedCount,
				)
			}
			var sequence model.ResourceSequence
			if err := db.First(
				&sequence,
				"name = ?",
				SequenceHostAccount,
			).Error; err != nil {
				t.Fatalf("load rebuilt host-account sequence: %v", err)
			}
			if sequence.NextValue != 43 {
				t.Fatalf(
					"rebuilt host-account next value = %d, want 43",
					sequence.NextValue,
				)
			}
		})
	}
}

func TestActiveRowPredicateSupportsLegacySQLiteMarkers(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "markers.db")),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.Exec(`CREATE TABLE marker_probe (
		id text PRIMARY KEY,
		deleted_at datetime
	)`).Error; err != nil {
		t.Fatalf("create marker probe: %v", err)
	}
	if err := db.Exec(`INSERT INTO marker_probe (id, deleted_at) VALUES
		('active-int', 1),
		('active-time', '0001-01-01 00:00:00+00:00'),
		('deleted-time', '2026-07-24 09:00:00+08:00'),
		('deleted-null', NULL)`).Error; err != nil {
		t.Fatalf("seed marker probe: %v", err)
	}

	predicate, args, err := activeRowPredicate(
		db,
		"marker_probe",
		"marker_probe",
	)
	if err != nil {
		t.Fatalf("build active marker predicate: %v", err)
	}
	var ids []string
	if err := db.Table("marker_probe").
		Where(predicate, args...).
		Order("id").
		Pluck("id", &ids).Error; err != nil {
		t.Fatalf("load active markers: %v", err)
	}
	if len(ids) != 2 || ids[0] != "active-int" || ids[1] != "active-time" {
		t.Fatalf("active marker ids = %v, want [active-int active-time]", ids)
	}
}
