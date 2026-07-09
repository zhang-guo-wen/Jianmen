package apiresp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ── 统一响应结构 ─────────────────────────────────────────────

// Envelope 成功响应封装
type Envelope struct {
	Code      int    `json:"code"`       // 0 表示成功
	Data      any    `json:"data"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// ErrorBody 错误详情
type ErrorBody struct {
	Code    string `json:"code"`    // 错误码：VALIDATION_ERROR, NOT_FOUND 等
	Message string `json:"message"` // 人类可读的错误描述
	Details any    `json:"details,omitempty"`
}

// ErrorEnvelope 错误响应封装
type ErrorEnvelope struct {
	Code      int       `json:"code"`       // 对应 HTTP 状态码
	Error     ErrorBody `json:"error"`
	RequestID string    `json:"request_id"`
	Timestamp string    `json:"timestamp"`
}

// ── 错误码常量 ──────────────────────────────────────────────

const (
	CodeValidation           = "VALIDATION_ERROR"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeForbidden            = "FORBIDDEN"
	CodeNotFound             = "NOT_FOUND"
	CodeConflict             = "CONFLICT"
	CodeInternal             = "INTERNAL_ERROR"
	CodeServiceUnavailable   = "SERVICE_UNAVAILABLE"
	CodeTooManyRequests      = "TOO_MANY_REQUESTS"
	CodePreconditionFailed   = "PRECONDITION_FAILED"
	CodeBadGateway           = "BAD_GATEWAY"
	CodeMethodNotAllowed     = "METHOD_NOT_ALLOWED"
)

// ── Context key ─────────────────────────────────────────────

type ctxKey string

const CtxKeyRequestID ctxKey = "request_id"

// ── Write 辅助函数 ──────────────────────────────────────────

// Write 写成功响应
func Write(w http.ResponseWriter, status int, data any, reqID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Code:      0,
		Data:      data,
		Message:   "ok",
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// WriteError 写错误响应
func WriteError(w http.ResponseWriter, status int, errCode string, message string, details any, reqID string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Code: status,
		Error: ErrorBody{
			Code:    errCode,
			Message: message,
			Details: details,
		},
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// RequestID 从 context 中提取 request_id
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return id
	}
	return ""
}
