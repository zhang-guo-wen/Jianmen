package storage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

const auditEventPhaseBackfillMigrationVersion = "202608250001"

var legacyAuditIntentIDPattern = regexp.MustCompile(`"intent_id"\s*:\s*"([^"]*)"`)

// migrateAuditEventPhaseBackfill converts legacy audit_events rows that were
// written before the dedicated phase/intent_id columns into the modern
// two-phase structure, so ListAuditEvents can collapse intent/result pairs via
// the (phase, intent_id) composite index instead of falling into the per-row
// table-scan (O(N²)) legacy branch of logicalAuditEventRowsCondition.
//
// The row linkage is the same one the runtime query infers: intent rows carry
// "phase":"intent" in their detail JSON; result rows carry "phase":"result" plus
// "intent_id":"<intent-id>". Rows whose detail cannot be classified (or rows
// that already have a phase) are left untouched. The migration is idempotent and
// processed in small batches so it is safe to run against large SQLite tables.
func migrateAuditEventPhaseBackfill(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.AuditEvent{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&model.AuditEvent{}, "phase") ||
		!tx.Migrator().HasColumn(&model.AuditEvent{}, "intent_id") {
		// The audit-fields migration that installs these columns has not run yet.
		return nil
	}
	type auditEventLegacyRow struct {
		ID     string `gorm:"column:id"`
		Detail string `gorm:"column:detail"`
	}
	const batchSize = 200
	lastID := ""
	for {
		var rows []auditEventLegacyRow
		query := tx.Table("audit_events").
			Select("id", "detail").
			Where("COALESCE(phase, '') = ''").
			Order("id").
			Limit(batchSize)
		if lastID != "" {
			query = query.Where("id > ?", lastID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return fmt.Errorf("backfill audit event phase: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			phase, intentID := classifyLegacyAuditEventPhase(row.Detail)
			if phase == "" {
				continue
			}
			updates := map[string]any{"phase": phase}
			if intentID != "" {
				updates["intent_id"] = intentID
			}
			if err := tx.Table("audit_events").
				Where("id = ?", row.ID).
				Updates(updates).Error; err != nil {
				return err
			}
		}
		lastID = rows[len(rows)-1].ID
	}
	return nil
}

// classifyLegacyAuditEventPhase reconstructs the phase and intent_id of a legacy
// audit event from its detail JSON. It mirrors the substring detection used by
// logicalAuditEventRowsCondition so the backfilled structure matches exactly
// what the query expects. intentID is only populated for result rows.
func classifyLegacyAuditEventPhase(detail string) (phase, intentID string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return "", ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(detail), &parsed); err == nil {
		if raw, ok := parsed["phase"].(string); ok {
			switch raw {
			case "intent":
				return "intent", ""
			case "result":
				if id, ok := parsed["intent_id"].(string); ok {
					return "result", id
				}
				return "result", ""
			}
		}
	}
	if strings.Contains(detail, `"phase":"intent"`) {
		return "intent", ""
	}
	if strings.Contains(detail, `"phase":"result"`) {
		return "result", extractLegacyAuditIntentID(detail)
	}
	return "", ""
}

func extractLegacyAuditIntentID(detail string) string {
	matches := legacyAuditIntentIDPattern.FindStringSubmatch(detail)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}
