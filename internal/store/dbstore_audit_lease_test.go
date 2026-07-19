package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func newAuditLeaseTestStore(t *testing.T, now time.Time) (*DBStore, func()) {
	t.Helper()
	db, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          t.TempDir() + "/audit-lease.db",
		MaxOpenConns: 4,
		MaxIdleConns: 4,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AuditSession{}, &model.AuditArtifact{}); err != nil {
		t.Fatalf("migrate audit lease schema: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	repository := NewDBStore(db)
	repository.auditLeaseOwner = "process-a"
	repository.auditLeaseDuration = 90 * time.Second
	repository.now = func() time.Time { return now }
	return repository, func() { _ = sqlDB.Close() }
}

func TestDBStoreAuditLeaseCoversAllUnifiedAuditSessionProducers(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	for _, protocol := range []string{"ssh", "web-terminal", "mysql"} {
		session := model.AuditSession{
			ID: protocol, Protocol: protocol, State: "started", StartedAt: now,
		}
		if err := repository.CreateAuditSession(&session); err != nil {
			t.Fatalf("create %s audit session: %v", protocol, err)
		}
		assertAuditLease(t, session, now, now.Add(90*time.Second))
	}
	rdp := model.AuditSession{
		ID: "rdp", Protocol: "rdp", State: "started", StartedAt: now,
	}
	if err := repository.BeginRDPAuditSession(
		context.Background(),
		&rdp,
		&model.AuditArtifact{
			ID: "rdp-artifact", Kind: model.AuditArtifactKindRecording,
			ObjectKey: "rdp/rdp/recording.guac",
		},
	); err != nil {
		t.Fatalf("begin RDP audit session: %v", err)
	}
	assertAuditLease(t, rdp, now, now.Add(90*time.Second))

	ids := repository.activeAuditSessionIDs()
	slices.Sort(ids)
	if want := []string{"mysql", "rdp", "ssh", "web-terminal"}; !slices.Equal(ids, want) {
		t.Fatalf("active lease ids = %#v, want %#v", ids, want)
	}

	heartbeatAt := now.Add(time.Minute)
	if err := repository.HeartbeatActiveAuditSessions(
		context.Background(),
		heartbeatAt,
	); err != nil {
		t.Fatalf("heartbeat active audit sessions: %v", err)
	}
	if recovered, err := repository.RecoverExpiredAuditSessions(
		context.Background(),
		now.Add(2*time.Minute),
	); err != nil || recovered != 0 {
		t.Fatalf("recovered live sessions = %d, err = %v", recovered, err)
	}
	for _, id := range ids {
		var stored model.AuditSession
		if err := repository.db.First(&stored, "id = ?", id).Error; err != nil {
			t.Fatalf("load heartbeated audit session %s: %v", id, err)
		}
		assertAuditLease(t, stored, heartbeatAt, heartbeatAt.Add(90*time.Second))
	}
}

func TestDBStoreAuditLeaseEndFailureStopsFutureHeartbeats(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	session := model.AuditSession{
		ID: "end-failed", Protocol: "ssh", State: "started", StartedAt: now,
	}
	if err := repository.CreateAuditSession(&session); err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	if err := repository.db.Migrator().RenameTable(
		&model.AuditSession{},
		"audit_sessions_unavailable",
	); err != nil {
		t.Fatalf("hide audit session table: %v", err)
	}
	if err := repository.EndAuditSession(session.ID); err == nil {
		t.Fatal("end audit session unexpectedly succeeded")
	}
	if ids := repository.activeAuditSessionIDs(); len(ids) != 0 {
		t.Fatalf("failed end remained registered for heartbeat: %#v", ids)
	}
	if err := repository.db.Migrator().RenameTable(
		"audit_sessions_unavailable",
		&model.AuditSession{},
	); err != nil {
		t.Fatalf("restore audit session table: %v", err)
	}
	heartbeatAt := now.Add(time.Minute)
	if err := repository.HeartbeatActiveAuditSessions(
		context.Background(),
		heartbeatAt,
	); err != nil {
		t.Fatalf("heartbeat after failed end: %v", err)
	}
	var stored model.AuditSession
	if err := repository.db.First(&stored, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("load audit session: %v", err)
	}
	assertAuditLease(t, stored, now, now.Add(90*time.Second))
}

func TestDBStoreAuditLeaseEndRequiresCurrentOwner(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	session := model.AuditSession{
		ID: "foreign-owner", Protocol: "ssh", State: "started", StartedAt: now,
	}
	if err := repository.CreateAuditSession(&session); err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	if err := repository.db.Model(&model.AuditSession{}).
		Where("id = ?", session.ID).
		Update("lease_owner", "other-process").Error; err != nil {
		t.Fatalf("transfer audit lease owner: %v", err)
	}

	err := repository.EndAuditSession(session.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign owner end error = %v, want record not found", err)
	}
	var stored model.AuditSession
	if err := repository.db.First(&stored, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("load foreign-owner session: %v", err)
	}
	if stored.State != "started" || stored.EndedAt != nil ||
		stored.LeaseOwner != "other-process" {
		t.Fatalf("foreign owner ended live audit session: %#v", stored)
	}
}

func TestDBStoreAuditLeaseHeartbeatBatchesAndRequiresExactRowCount(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	const sessionCount = 1001
	sessions := make([]model.AuditSession, 0, sessionCount)
	for index := 0; index < sessionCount; index++ {
		session := leasedSession(
			fmt.Sprintf("batch-%04d", index),
			"ssh",
			now,
			now.Add(90*time.Second),
		)
		session.LeaseOwner = repository.auditLeaseOwner
		sessions = append(sessions, session)
		repository.activeAuditLeases[session.ID] = struct{}{}
	}
	if err := repository.db.CreateInBatches(&sessions, 200).Error; err != nil {
		t.Fatalf("seed batched audit sessions: %v", err)
	}
	heartbeatAt := now.Add(time.Minute)
	if err := repository.HeartbeatActiveAuditSessions(
		context.Background(),
		heartbeatAt,
	); err != nil {
		t.Fatalf("heartbeat batched audit sessions: %v", err)
	}
	var renewed int64
	if err := repository.db.Model(&model.AuditSession{}).
		Where("heartbeat_at = ?", heartbeatAt).
		Count(&renewed).Error; err != nil {
		t.Fatalf("count renewed audit sessions: %v", err)
	}
	if renewed != sessionCount {
		t.Fatalf("renewed sessions = %d, want %d", renewed, sessionCount)
	}

	if err := repository.db.Model(&model.AuditSession{}).
		Where("id = ?", sessions[0].ID).
		Update("lease_owner", "other-process").Error; err != nil {
		t.Fatalf("change lease owner: %v", err)
	}
	if err := repository.db.Model(&model.AuditSession{}).
		Where("id = ?", sessions[1].ID).
		Update("state", "ended").Error; err != nil {
		t.Fatalf("end registered audit session: %v", err)
	}
	if err := repository.db.Delete(
		&model.AuditSession{},
		"id = ?",
		sessions[2].ID,
	).Error; err != nil {
		t.Fatalf("delete registered audit session: %v", err)
	}
	err := repository.HeartbeatActiveAuditSessions(
		context.Background(),
		heartbeatAt.Add(time.Minute),
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"renewed 998 of 1001 active sessions",
	) {
		t.Fatalf("partial heartbeat error = %v", err)
	}
}

func TestDBStoreAuditLeaseHeartbeatSerializesNormalEndWithActiveSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	session := model.AuditSession{
		ID: "heartbeat-end-race", Protocol: "ssh", State: "started", StartedAt: now,
	}
	if err := repository.CreateAuditSession(&session); err != nil {
		t.Fatalf("create audit session: %v", err)
	}
	heartbeatStarted := make(chan struct{})
	releaseHeartbeat := make(chan struct{})
	var once sync.Once
	if err := repository.db.Callback().Update().
		Before("gorm:update").
		Register("test:block_audit_heartbeat", func(tx *gorm.DB) {
			if tx.Statement.Table != "audit_sessions" {
				return
			}
			updates, ok := tx.Statement.Dest.(map[string]any)
			if !ok {
				return
			}
			if _, isHeartbeat := updates["heartbeat_at"]; !isHeartbeat {
				return
			}
			once.Do(func() {
				close(heartbeatStarted)
				<-releaseHeartbeat
			})
		}); err != nil {
		t.Fatalf("register heartbeat blocker: %v", err)
	}

	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- repository.HeartbeatActiveAuditSessions(
			context.Background(),
			now.Add(time.Minute),
		)
	}()
	select {
	case <-heartbeatStarted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat update did not reach synchronization point")
	}

	endDone := make(chan error, 1)
	go func() {
		endDone <- repository.EndAuditSession(session.ID)
	}()
	select {
	case err := <-endDone:
		t.Fatalf("normal end crossed active heartbeat snapshot: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseHeartbeat)
	if err := <-heartbeatDone; err != nil {
		t.Fatalf("heartbeat falsely failed during normal end: %v", err)
	}
	select {
	case err := <-endDone:
		if err != nil {
			t.Fatalf("end audit session after heartbeat: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("normal end remained blocked after heartbeat")
	}
}

func TestDBStoreAuditLeaseRecoveryRequiresExpiredConsistentEvidence(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	expiredHeartbeat := now.Add(-2 * time.Minute)
	expiredAt := now.Add(-30 * time.Second)
	futureHeartbeat := now.Add(-time.Minute)
	futureExpiry := now.Add(30 * time.Second)
	corruptHeartbeat := now.Add(time.Minute)
	for _, session := range []model.AuditSession{
		leasedSession("expired-ssh", "ssh", expiredHeartbeat, expiredAt),
		leasedSession("expired-web-terminal", "web-terminal", expiredHeartbeat, expiredAt),
		leasedSession("expired-db", "mysql", expiredHeartbeat, expiredAt),
		leasedSession("expired-rdp", "rdp", expiredHeartbeat, expiredAt),
		leasedSession("live", "ssh", futureHeartbeat, futureExpiry),
		leasedSession("corrupt", "ssh", corruptHeartbeat, expiredAt),
		{ID: "legacy-unleased", Protocol: "ssh", State: "started", StartedAt: now.Add(-24 * time.Hour)},
	} {
		if err := repository.db.Create(&session).Error; err != nil {
			t.Fatalf("seed %s: %v", session.ID, err)
		}
	}

	recovered, err := repository.RecoverExpiredAuditSessions(
		context.Background(),
		now,
	)
	if err != nil {
		t.Fatalf("recover expired audit sessions: %v", err)
	}
	if recovered != 4 {
		t.Fatalf("recovered sessions = %d, want 4", recovered)
	}
	for _, id := range []string{
		"expired-ssh",
		"expired-web-terminal",
		"expired-db",
		"expired-rdp",
	} {
		var session model.AuditSession
		if err := repository.db.First(&session, "id = ?", id).Error; err != nil {
			t.Fatalf("load recovered session %s: %v", id, err)
		}
		if session.State != "ended" ||
			session.Outcome != model.AuditOutcomeTerminated ||
			session.FailureCode != auditLeaseExpiredFailureCode ||
			session.EndedAt == nil ||
			!session.EndedAt.Equal(expiredAt) {
			t.Fatalf("recovered session %s = %#v", id, session)
		}
	}
	for _, id := range []string{"live", "corrupt", "legacy-unleased"} {
		var session model.AuditSession
		if err := repository.db.First(&session, "id = ?", id).Error; err != nil {
			t.Fatalf("load preserved session %s: %v", id, err)
		}
		if session.State != "started" || session.EndedAt != nil {
			t.Fatalf("session %s was reclaimed without expired lease evidence: %#v", id, session)
		}
	}
}

func TestDBStoreAuditLeaseRecoveryFencesLateRDPFinish(t *testing.T) {
	now := time.Date(2026, 7, 19, 15, 0, 0, 0, time.UTC)
	repository, closeStore := newAuditLeaseTestStore(t, now)
	defer closeStore()

	session := model.AuditSession{
		ID: "rdp-fenced", Protocol: "rdp", State: "started",
		Outcome: model.AuditOutcomeActive, StartedAt: now.Add(-time.Hour),
	}
	if err := repository.BeginRDPAuditSession(
		context.Background(),
		&session,
		nil,
	); err != nil {
		t.Fatalf("begin RDP audit session: %v", err)
	}
	expiredHeartbeat := now.Add(-2 * time.Minute)
	expiredAt := now.Add(-time.Minute)
	if err := repository.db.Model(&model.AuditSession{}).
		Where("id = ?", session.ID).
		Updates(map[string]any{
			"heartbeat_at":     expiredHeartbeat,
			"lease_expires_at": expiredAt,
		}).Error; err != nil {
		t.Fatalf("expire RDP audit lease: %v", err)
	}
	if recovered, err := repository.RecoverExpiredAuditSessions(
		context.Background(),
		now,
	); err != nil || recovered != 1 {
		t.Fatalf("recover RDP audit session = %d, err = %v", recovered, err)
	}

	if err := repository.FinishAuditSession(
		context.Background(),
		session.ID,
		model.AuditOutcomeSucceeded,
		"",
		"",
		model.RecordingStatusReady,
		now,
	); err == nil {
		t.Fatal("late active RDP finish overwrote recovered outcome")
	}
	var recovered model.AuditSession
	if err := repository.db.First(&recovered, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("load recovered RDP audit session: %v", err)
	}
	if recovered.Outcome != model.AuditOutcomeTerminated ||
		recovered.FailureCode != auditLeaseExpiredFailureCode ||
		recovered.EndedAt == nil ||
		!recovered.EndedAt.Equal(expiredAt) {
		t.Fatalf("late finish changed recovered session: %#v", recovered)
	}
	if err := repository.FinishAuditSession(
		context.Background(),
		session.ID,
		recovered.Outcome,
		"late_process",
		"late process attempted to replace lease-expired evidence",
		model.RecordingStatusReady,
		now,
	); err != nil {
		t.Fatalf("finish recovered RDP recording: %v", err)
	}
	var finalized model.AuditSession
	if err := repository.db.First(&finalized, "id = ?", session.ID).Error; err != nil {
		t.Fatalf("load finalized RDP audit session: %v", err)
	}
	if finalized.Outcome != model.AuditOutcomeTerminated ||
		finalized.FailureCode != auditLeaseExpiredFailureCode ||
		finalized.FailureMessage != auditLeaseExpiredFailureMessage ||
		finalized.EndedAt == nil ||
		!finalized.EndedAt.Equal(expiredAt) ||
		finalized.RecordingStatus != model.RecordingStatusReady {
		t.Fatalf("recording finalization replaced lease-expired evidence: %#v", finalized)
	}
}

func assertAuditLease(
	t *testing.T,
	session model.AuditSession,
	heartbeatAt time.Time,
	expiresAt time.Time,
) {
	t.Helper()
	if session.LeaseOwner != "process-a" ||
		session.HeartbeatAt == nil ||
		!session.HeartbeatAt.Equal(heartbeatAt) ||
		session.LeaseExpiresAt == nil ||
		!session.LeaseExpiresAt.Equal(expiresAt) {
		t.Fatalf("audit session lease = %#v", session)
	}
}

func leasedSession(
	id string,
	protocol string,
	heartbeatAt time.Time,
	expiresAt time.Time,
) model.AuditSession {
	return model.AuditSession{
		ID: id, Protocol: protocol, State: "started",
		StartedAt:  expiresAt.Add(-time.Hour),
		LeaseOwner: "other-process", HeartbeatAt: &heartbeatAt,
		LeaseExpiresAt: &expiresAt,
	}
}
