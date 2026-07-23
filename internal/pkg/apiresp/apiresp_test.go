package apiresp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWriteProducesSuccessEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()

	Write(recorder, http.StatusCreated, map[string]string{"id": "resource-1"}, "request-1")

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}

	var envelope struct {
		Code      int               `json:"code"`
		Data      map[string]string `json:"data"`
		Message   string            `json:"message"`
		RequestID string            `json:"request_id"`
		Timestamp string            `json:"timestamp"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 || envelope.Message != "ok" || envelope.RequestID != "request-1" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if envelope.Data["id"] != "resource-1" {
		t.Fatalf("data = %#v", envelope.Data)
	}
	assertRFC3339Timestamp(t, envelope.Timestamp)
}

func TestWriteErrorProducesErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	details := map[string]any{"field": "name", "retryable": false}

	WriteError(
		recorder,
		http.StatusBadRequest,
		CodeValidation,
		"name is required",
		details,
		"request-2",
	)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var envelope struct {
		Code      int       `json:"code"`
		Error     ErrorBody `json:"error"`
		RequestID string    `json:"request_id"`
		Timestamp string    `json:"timestamp"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != http.StatusBadRequest ||
		envelope.Error.Code != CodeValidation ||
		envelope.Error.Message != "name is required" ||
		envelope.RequestID != "request-2" {
		t.Fatalf("envelope = %#v", envelope)
	}
	decodedDetails, ok := envelope.Error.Details.(map[string]any)
	if !ok || decodedDetails["field"] != "name" || decodedDetails["retryable"] != false {
		t.Fatalf("details = %#v", envelope.Error.Details)
	}
	assertRFC3339Timestamp(t, envelope.Timestamp)
}

func TestWriteErrorOmitsNilDetails(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusNotFound, CodeNotFound, "missing", nil, "request-3")

	var envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, exists := envelope.Error["details"]; exists {
		t.Fatalf("nil details should be omitted: %s", recorder.Body.String())
	}
}

func TestRequestIDReadsOnlyStringContextValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), CtxKeyRequestID, "request-4")
	if got := RequestID(ctx); got != "request-4" {
		t.Fatalf("RequestID() = %q", got)
	}

	wrongType := context.WithValue(context.Background(), CtxKeyRequestID, 4)
	if got := RequestID(wrongType); got != "" {
		t.Fatalf("RequestID() with non-string value = %q", got)
	}
	if got := RequestID(context.Background()); got != "" {
		t.Fatalf("RequestID() without value = %q", got)
	}
}

func assertRFC3339Timestamp(t *testing.T, value string) {
	t.Helper()
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", value, err)
	}
}
