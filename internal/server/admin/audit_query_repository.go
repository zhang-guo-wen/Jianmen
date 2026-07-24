package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

type adminAuditQueryRepository struct{ repository adminAuditRepository }

func (r adminAuditQueryRepository) ListAuditSessions(ctx context.Context, params service.AuditSessionListParams) ([]service.AuditSessionListItem, int64, error) {
	items, total, err := r.repository.ListAuditSessions(ctx, store.AuditListParams{
		Protocol: params.Protocol, Search: params.Search, Date: params.Date, Page: params.Page, Size: params.Size,
	})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.AuditSessionListItem, len(items))
	for i, item := range items {
		result[i] = service.AuditSessionListItem{
			ID: item.ID, UserID: item.UserID, Username: item.Username, Protocol: item.Protocol, ProtocolSubtype: item.ProtocolSubtype,
			ResourceType: item.ResourceType, ResourceID: item.ResourceID, HostID: item.HostID, AccountID: item.AccountID,
			TargetName: item.TargetName, TargetAddress: item.TargetAddress, AccountName: item.AccountName, AccountUsername: item.AccountUsername,
			ClientIP: item.ClientIP, StartedAt: item.StartedAt, EndedAt: item.EndedAt, State: item.State, Outcome: item.Outcome,
			FailureCode: item.FailureCode, FailureMessage: item.FailureMessage, RecordingStatus: item.RecordingStatus, HasReplay: item.HasReplay, LogCount: item.LogCount, SessionID: item.SessionID,
		}
	}
	return result, total, nil
}

func (r adminAuditQueryRepository) GetAuditSessionAccessMetadata(ctx context.Context, id string) (service.AuditSessionAccessMetadata, error) {
	item, err := r.repository.GetAuditSessionAccessMetadata(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return service.AuditSessionAccessMetadata{}, service.ErrAuditArtifactUnavailable
		}
		return service.AuditSessionAccessMetadata{}, err
	}
	return service.AuditSessionAccessMetadata{
		ID: item.ID, Protocol: item.Protocol, ProtocolSubtype: item.ProtocolSubtype, State: item.State,
	}, nil
}

func (r adminAuditQueryRepository) GetFullAuditSession(ctx context.Context, id string) (service.AuditSession, error) {
	item, err := r.repository.GetAuditSession(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return service.AuditSession{}, service.ErrAuditArtifactUnavailable
		}
		return service.AuditSession{}, err
	}
	return auditQuerySession(*item), nil
}

func (r adminAuditQueryRepository) ListSSHCommands(ctx context.Context, sessionID string, page service.Page) ([]service.AuditSSHCommand, int64, error) {
	items, total, err := r.repository.ListAuditSSHCommands(ctx, sessionID, store.PageOpts{Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.AuditSSHCommand, len(items))
	for i, item := range items {
		result[i] = service.AuditSSHCommand{ID: item.ID, AuditSessionID: item.AuditSessionID, Timestamp: item.Timestamp, Command: item.Command}
	}
	return result, total, nil
}

func (r adminAuditQueryRepository) ListSFTPEvents(ctx context.Context, sessionID string, page service.Page) ([]service.AuditSFTPEvent, int64, error) {
	items, total, err := r.repository.ListAuditSFTPEvents(ctx, sessionID, store.PageOpts{Limit: page.Limit, Offset: page.Offset})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.AuditSFTPEvent, len(items))
	for i, item := range items {
		result[i] = service.AuditSFTPEvent{ID: item.ID, AuditSessionID: item.AuditSessionID, Timestamp: item.Timestamp, Action: item.Action, Path: item.Path, Size: item.Size, Result: item.Result}
	}
	return result, total, nil
}

func (r adminAuditQueryRepository) ListDBQueryPreviews(ctx context.Context, sessionID string, params service.AuditDBQueryPreviewParams) ([]service.AuditDBQueryPreview, int64, error) {
	items, total, err := r.repository.ListAuditDBQueryPreviews(ctx, sessionID, store.AuditDBQueryPreviewParams{Search: params.Search, Limit: params.Limit, Offset: params.Offset})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.AuditDBQueryPreview, len(items))
	for i, item := range items {
		result[i] = service.AuditDBQueryPreview{
			ID:               item.ID,
			AuditSessionID:   item.AuditSessionID,
			Timestamp:        item.Timestamp,
			SQLText:          item.SQLText,
			SQLStoredBytes:   item.SQLStoredBytes,
			OriginalSQLBytes: item.OriginalSQLBytes,
			SQLTruncated:     item.SQLTruncated,
			QueryKind:        item.QueryKind,
			DurationMs:       item.DurationMs,
			Status:           item.Status,
			ErrorCode:        item.ErrorCode,
			ErrorMessage:     item.ErrorMessage,
			RowsAffected:     item.RowsAffected,
			Rows:             item.Rows,
		}
	}
	return result, total, nil
}

func (r adminAuditQueryRepository) ListAuditEvents(ctx context.Context, params service.AuditEventListParams) ([]service.AuditEvent, int64, error) {
	items, total, err := r.repository.ListAuditEvents(ctx, store.AuditEventListParams{Search: params.Search, Action: params.Action, ResourceType: params.ResourceType, Date: params.Date, Page: params.Page, Size: params.Size})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.AuditEvent, len(items))
	for i, item := range items {
		result[i] = auditQueryEvent(item)
	}
	return result, total, nil
}

func (r adminAuditQueryRepository) ListLoginAuditLogs(ctx context.Context, params service.LoginAuditListParams) ([]service.LoginAuditLog, int64, error) {
	items, total, err := r.repository.ListLoginAuditLogs(ctx, store.LoginAuditListParams{Search: params.Search, Outcome: params.Outcome, Date: params.Date, Page: params.Page, Size: params.Size})
	if err != nil {
		return nil, 0, err
	}
	result := make([]service.LoginAuditLog, len(items))
	for i, item := range items {
		result[i] = auditQueryLogin(item)
	}
	return result, total, nil
}

func auditQuerySession(item model.AuditSession) service.AuditSession {
	return service.AuditSession{
		ID: item.ID, UserSessionID: item.UserSessionID, UserID: item.UserID, Username: item.Username, Protocol: item.Protocol, ProtocolSubtype: item.ProtocolSubtype,
		ResourceType: item.ResourceType, ResourceID: item.ResourceID, HostID: item.HostID, AccountID: item.AccountID,
		TargetName: item.TargetName, TargetAddress: item.TargetAddress, AccountName: item.AccountName, AccountUsername: item.AccountUsername, ClientIP: item.ClientIP,
		StartedAt: item.StartedAt, EndedAt: item.EndedAt, State: item.State, Outcome: item.Outcome, FailureCode: item.FailureCode, FailureMessage: item.FailureMessage,
		PolicySnapshot: item.PolicySnapshot, RecordingStatus: item.RecordingStatus, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, ReplayDir: item.ReplayDir,
	}
}

func auditQueryEvent(item model.AuditEvent) service.AuditEvent {
	result := service.AuditEvent{
		ID: item.ID, ActorID: item.ActorID, ActorUsername: item.ActorUsername, Action: item.Action,
		ResourceType: item.ResourceType, ResourceID: item.ResourceID, ResourceName: item.ResourceName,
		Phase: item.Phase, Result: item.Result, IntentID: item.IntentID, RequestID: item.RequestID,
		StatusCode: item.StatusCode, Detail: item.Detail, ClientIP: item.ClientIP, CreatedAt: item.CreatedAt,
	}
	var detail map[string]any
	if json.Unmarshal([]byte(item.Detail), &detail) != nil {
		return result
	}
	if result.Phase == "" {
		result.Phase = auditDetailString(detail, "phase")
	}
	if result.Result == "" {
		result.Result = auditDetailString(detail, "result")
	}
	if result.IntentID == "" {
		result.IntentID = auditDetailString(detail, "intent_id")
	}
	if result.RequestID == "" {
		result.RequestID = auditDetailString(detail, "request_id")
	}
	if result.StatusCode == 0 {
		result.StatusCode = auditDetailInt(detail, "status")
	}
	return result
}

func auditQueryLogin(item model.LoginAuditLog) service.LoginAuditLog {
	phase := strings.TrimSpace(item.Phase)
	result := strings.TrimSpace(item.Result)
	intentID := strings.TrimSpace(item.IntentID)
	reason := strings.TrimSpace(item.Reason)

	if legacyIntentID, legacyReason, ok := parseLegacyLoginAuditReason(reason); ok {
		if intentID == "" {
			intentID = legacyIntentID
		}
		reason = legacyReason
	}
	if phase == "" {
		if item.Outcome == loginAuditOutcomePending && item.Reason == loginAuditReasonIntent {
			phase = "intent"
		} else {
			phase = "result"
		}
	}
	if result == "" {
		result = item.Outcome
	}
	statusCode := item.StatusCode
	if statusCode == 0 && phase == "result" {
		statusCode = loginAuditStatusCode(item.Outcome, reason)
	}
	return service.LoginAuditLog{
		ID: item.ID, UserID: item.UserID, Username: item.Username,
		Phase: phase, Result: result, IntentID: intentID, RequestID: item.RequestID,
		StatusCode: statusCode, Outcome: item.Outcome, Reason: reason,
		ClientIP: item.ClientIP, UserAgent: item.UserAgent, CreatedAt: item.CreatedAt,
	}
}

func parseLegacyLoginAuditReason(reason string) (string, string, bool) {
	const prefix = "intent_id="
	if !strings.HasPrefix(reason, prefix) {
		return "", reason, false
	}
	link, remainder, found := strings.Cut(strings.TrimPrefix(reason, prefix), ";")
	if !found {
		return strings.TrimSpace(link), "", true
	}
	return strings.TrimSpace(link), strings.TrimSpace(remainder), true
}

func auditDetailString(detail map[string]any, key string) string {
	value, _ := detail[key].(string)
	return strings.TrimSpace(value)
}

func auditDetailInt(detail map[string]any, key string) int {
	switch value := detail[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

type adminAuditQueryAuthorizer struct{ authorization authorizationService }

func (a adminAuditQueryAuthorizer) AuthorizeAuditQuery(ctx context.Context, userID string, actions []string) (bool, error) {
	return a.authorization.AuthorizeConnection(ctx, userID, actions, "", "")
}
