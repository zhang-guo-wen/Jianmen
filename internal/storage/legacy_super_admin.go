package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const (
	LegacySuperAdminIDsFile         = ".super_admin_ids"
	LegacySuperAdminIDsImportedFile = ".super_admin_ids.imported"
)

func ImportLegacySuperAdminIDs(ctx context.Context, db *gorm.DB, dataDir string) error {
	if db == nil {
		return errors.New("import legacy super administrators: nil database")
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil
	}
	source := filepath.Join(dataDir, LegacySuperAdminIDsFile)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy super administrators: %w", err)
	}

	uniqueIDs := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		if id := strings.TrimSpace(line); id != "" {
			uniqueIDs[id] = struct{}{}
		}
	}
	ids := make([]string, 0, len(uniqueIDs))
	for id := range uniqueIDs {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&model.User{}).
				Where("id IN ?", ids).
				Update("is_super_admin", true)
			if result.Error != nil {
				return result.Error
			}
			return nil
		}); err != nil {
			return fmt.Errorf("persist legacy super administrators: %w", err)
		}
	}

	imported := filepath.Join(dataDir, LegacySuperAdminIDsImportedFile)
	if err := os.Rename(source, imported); err != nil {
		return fmt.Errorf("mark legacy super administrators imported: %w", err)
	}
	return nil
}
