package admin

import (
	"context"
	"errors"

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
			FailureCode: item.FailureCode, FailureMessage: item.FailureMessage, RecordingStatus: item.RecordingStatus, HasReplay: item.HasReplay, LogCount: item.LogCount,
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
		result[i] = service.AuditDBQueryPreview{ID: item.ID, AuditSessionID: item.AuditSessionID, Timestamp: item.Timestamp, SQLText: item.SQLText, SQLStoredBytes: item.SQLStoredBytes, OriginalSQLBytes: item.OriginalSQLBytes, SQLTruncated: item.SQLTruncated, QueryKind: item.QueryKind, DurationMs: item.DurationMs}
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
		result[i] = service.LoginAuditLog{ID: item.ID, UserID: item.UserID, Username: item.Username, Outcome: item.Outcome, Reason: item.Reason, ClientIP: item.ClientIP, UserAgent: item.UserAgent, CreatedAt: item.CreatedAt}
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
	return service.AuditEvent{ID: item.ID, ActorID: item.ActorID, ActorUsername: item.ActorUsername, Action: item.Action, ResourceType: item.ResourceType, ResourceID: item.ResourceID, ResourceName: item.ResourceName, Detail: item.Detail, ClientIP: item.ClientIP, CreatedAt: item.CreatedAt}
}

type adminAuditQueryAuthorizer struct{ authorization authorizationService }

func (a adminAuditQueryAuthorizer) AuthorizeAuditQuery(ctx context.Context, userID string, actions []string) (bool, error) {
	return a.authorization.AuthorizeConnection(ctx, userID, actions, "", "")
}
