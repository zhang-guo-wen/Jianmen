//go:build integration

package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

func TestPermanentUserSessionAllocationAgainstMetadataDatabases(t *testing.T) {
	for _, databaseCase := range metadataDatabaseCases() {
		databaseCase := databaseCase
		t.Run(databaseCase.name, func(t *testing.T) {
			db := openMetadataDatabase(t, databaseCase)
			if err := storage.Migrate(db); err != nil {
				t.Fatalf("run versioned metadata migrations: %v", err)
			}
			if err := storage.EnsureSequenceNextValue(db, storage.SequenceUserSession, 1); err != nil {
				t.Fatalf("bootstrap user session sequence: %v", err)
			}

			const sharedUserID = "atomic-shared-user"
			users := []model.User{{
				ID: sharedUserID, Username: sharedUserID, Status: "active",
			}}
			const distinctUsers = 16
			distinctUserIDs := make([]string, distinctUsers)
			for index := range distinctUserIDs {
				distinctUserIDs[index] = fmt.Sprintf("atomic-distinct-user-%02d", index)
				users = append(users, model.User{
					ID: distinctUserIDs[index], Username: distinctUserIDs[index], Status: "active",
				})
			}
			if err := db.Create(&users).Error; err != nil {
				t.Fatalf("create allocation users: %v", err)
			}

			repository := store.NewDBStore(db)
			const sameUserCallers = 16
			sharedUserIDs := make([]string, sameUserCallers)
			for index := range sharedUserIDs {
				sharedUserIDs[index] = sharedUserID
			}
			sharedSessions := allocatePermanentSessionsConcurrently(t, repository, sharedUserIDs)
			sharedSessionID := sharedSessions[0].SessionID
			sharedSessionSeq := sharedSessions[0].SessionSeq
			for index, session := range sharedSessions {
				if session.SessionID != sharedSessionID || session.SessionSeq != sharedSessionSeq {
					t.Fatalf(
						"same-user allocation %d returned session %q/%d, want %q/%d",
						index,
						session.SessionID,
						session.SessionSeq,
						sharedSessionID,
						sharedSessionSeq,
					)
				}
			}
			assertActivePermanentSessionCount(t, db, sharedUserID, 1)

			distinctSessions := allocatePermanentSessionsConcurrently(t, repository, distinctUserIDs)
			seenSessionIDs := map[string]string{sharedSessionID: sharedUserID}
			seenSessionSeqs := map[int]string{sharedSessionSeq: sharedUserID}
			for index, session := range distinctSessions {
				userID := distinctUserIDs[index]
				if session.UserID != userID {
					t.Fatalf("distinct-user allocation %d returned user %q, want %q", index, session.UserID, userID)
				}
				if owner, exists := seenSessionIDs[session.SessionID]; exists {
					t.Fatalf("session ID %q reused by %q and %q", session.SessionID, owner, userID)
				}
				if owner, exists := seenSessionSeqs[session.SessionSeq]; exists {
					t.Fatalf("session sequence %d reused by %q and %q", session.SessionSeq, owner, userID)
				}
				seenSessionIDs[session.SessionID] = userID
				seenSessionSeqs[session.SessionSeq] = userID
				assertActivePermanentSessionCount(t, db, userID, 1)
			}

			var persisted []model.UserSession
			if err := db.Where("type = ? AND status = ?", "permanent", "active").
				Order("session_seq ASC").
				Find(&persisted).Error; err != nil {
				t.Fatalf("load persisted permanent sessions: %v", err)
			}
			if len(persisted) != 1+distinctUsers {
				t.Fatalf("persisted active permanent sessions = %d, want %d", len(persisted), 1+distinctUsers)
			}
			persistedIDs := make(map[string]struct{}, len(persisted))
			persistedSeqs := make(map[int]struct{}, len(persisted))
			for _, session := range persisted {
				persistedIDs[session.SessionID] = struct{}{}
				persistedSeqs[session.SessionSeq] = struct{}{}
			}
			if len(persistedIDs) != len(persisted) || len(persistedSeqs) != len(persisted) {
				t.Fatalf(
					"persisted global identity is not unique: sessions=%d ids=%d sequences=%d",
					len(persisted),
					len(persistedIDs),
					len(persistedSeqs),
				)
			}
		})
	}
}

type permanentSessionAllocator interface {
	GetOrCreateActivePermanentUserSession(context.Context, string) (model.UserSession, error)
}

func allocatePermanentSessionsConcurrently(
	t *testing.T,
	repository permanentSessionAllocator,
	userIDs []string,
) []model.UserSession {
	t.Helper()
	type allocationResult struct {
		session model.UserSession
		err     error
	}
	results := make([]allocationResult, len(userIDs))
	start := make(chan struct{})
	var group sync.WaitGroup
	for index, userID := range userIDs {
		index, userID := index, userID
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			results[index].session, results[index].err =
				repository.GetOrCreateActivePermanentUserSession(ctx, userID)
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("permanent session allocation did not finish after request contexts expired")
	}

	sessions := make([]model.UserSession, len(results))
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("allocation %d for user %q: %v", index, userIDs[index], result.err)
		}
		sessions[index] = result.session
	}
	return sessions
}

func assertActivePermanentSessionCount(t *testing.T, db *gorm.DB, userID string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.UserSession{}).
		Where("user_id = ? AND type = ? AND status = ?", userID, "permanent", "active").
		Count(&count).Error; err != nil {
		t.Fatalf("count active permanent sessions for %q: %v", userID, err)
	}
	if count != want {
		t.Fatalf("active permanent sessions for %q = %d, want %d", userID, count, want)
	}
}
