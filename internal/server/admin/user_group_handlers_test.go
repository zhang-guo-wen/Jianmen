package admin

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/model"
)

func TestAddUserGroupMemberReturnsOKWhenMembershipAlreadyExists(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.User{ID: "u-member", Username: "member", Status: "active"}).Error; err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if err := db.Create(&model.UserGroup{ID: "group", Name: "operators"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	add := func() *httptest.ResponseRecorder {
		req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/user-groups/group/members", bytes.NewBufferString(`{"user_id":"u-member"}`)))
		rec := httptest.NewRecorder()
		server.handleUserGroupOrMembers(rec, req)
		return rec
	}
	if rec := add(); rec.Code != http.StatusCreated {
		t.Fatalf("first add status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := add(); rec.Code != http.StatusOK {
		t.Fatalf("idempotent add status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUserGroupMemberPathRejectsExtraSegments(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.User{ID: "u-member", Username: "member", Status: "active"}).Error; err != nil {
		t.Fatalf("create member user: %v", err)
	}
	if err := db.Create(&model.UserGroup{ID: "group", Name: "operators"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodDelete, "/api/user-groups/group/members/u-member/extra", nil))
	rec := httptest.NewRecorder()
	server.handleUserGroupOrMembers(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("extra path segment status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUserAndUserGroupErrorsDoNotExposeRepositoryDetails(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	const sensitive = "gorm: duplicate key value SQLSTATE=23505"

	userRecorder := httptest.NewRecorder()
	server.writeUserServiceError(userRecorder, request, errors.New(sensitive))
	if userRecorder.Code != http.StatusInternalServerError || bytes.Contains(userRecorder.Body.Bytes(), []byte(sensitive)) {
		t.Fatalf("user error response leaked repository detail: status=%d body=%s", userRecorder.Code, userRecorder.Body.String())
	}

	groupRecorder := httptest.NewRecorder()
	server.writeUserGroupServiceError(groupRecorder, request, errors.New(sensitive))
	if groupRecorder.Code != http.StatusInternalServerError || bytes.Contains(groupRecorder.Body.Bytes(), []byte(sensitive)) {
		t.Fatalf("user group error response leaked repository detail: status=%d body=%s", groupRecorder.Code, groupRecorder.Body.String())
	}
}

func TestAddUserGroupMemberRejectsBlankUserID(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	if err := db.Create(&model.UserGroup{ID: "group", Name: "operators"}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}

	req := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/user-groups/group/members", bytes.NewBufferString(`{"user_id":"   "}`)))
	rec := httptest.NewRecorder()
	server.handleUserGroupOrMembers(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank user id status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
