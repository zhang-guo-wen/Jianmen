package model

import "testing"

func TestAuditSessionBeforeCreateAllowsNilTransaction(t *testing.T) {
	session := AuditSession{}

	if err := session.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate(nil) error = %v", err)
	}
	if session.ID == "" {
		t.Fatal("BeforeCreate(nil) did not generate an audit session ID")
	}
}
