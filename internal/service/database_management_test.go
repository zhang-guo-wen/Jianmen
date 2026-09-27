package service

import (
	"testing"
	"time"
)

func TestDatabaseAccountConnectableTreatsExpiryBoundaryAsExpired(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	account := DatabaseAccount{Status: "active", ExpiresAt: &now}
	instance := DatabaseInstance{Status: "active"}
	if databaseAccountConnectable(account, instance, now) {
		t.Fatal("account expiring exactly at now was connectable")
	}
}
