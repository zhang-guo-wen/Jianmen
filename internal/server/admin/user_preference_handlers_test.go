package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/model"
)

func TestMePreferencesDefaultsAndPersists(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	if err := db.Create(&model.User{ID: "u1", Username: "alice", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	getRequest := asTestUser(httptest.NewRequest(http.MethodGet, "/api/me/preferences", nil), "u1", "alice")
	getRecorder := httptest.NewRecorder()
	server.handleMePreferences(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get preferences status = %d, body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var defaults userPreferenceResponse
	if err := decodeTestData(t, getRecorder.Body.Bytes(), &defaults); err != nil {
		t.Fatalf("decode defaults: %v", err)
	}
	if defaults.Theme != "light" || defaults.TerminalFontSize != 14 {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}

	body := bytes.NewBufferString(`{"theme":"dark","ssh_client":"xshell","ssh_client_path":"C:\\Xshell.exe","terminal_font_family":"Cascadia Mono","terminal_font_size":16}`)
	putRequest := asTestUser(httptest.NewRequest(http.MethodPut, "/api/me/preferences", body), "u1", "alice")
	putRecorder := httptest.NewRecorder()
	server.handleMePreferences(putRecorder, putRequest)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put preferences status = %d, body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	stored, err := server.store.UserPreference(putRequest.Context(), "u1")
	if err != nil {
		t.Fatalf("read stored preference: %v", err)
	}
	if stored.Theme != "dark" || stored.SSHClient != "xshell" || stored.TerminalFontSize != 16 {
		t.Fatalf("unexpected stored preference: %#v", stored)
	}
}

func TestMePreferencesRejectsInvalidValues(t *testing.T) {
	server, _ := newAdminDBTestServer(t)
	body := bytes.NewBufferString(`{"theme":"neon","terminal_font_size":99}`)
	request := asTestUser(httptest.NewRequest(http.MethodPut, "/api/me/preferences", body), "u1", "alice")
	recorder := httptest.NewRecorder()
	server.handleMePreferences(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}
