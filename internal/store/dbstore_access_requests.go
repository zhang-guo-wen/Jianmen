package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) CreateAccessRequest(ctx context.Context, request *model.AccessRequest) error {
	if request == nil {
		return errors.New("access request is required")
	}
	return s.db.WithContext(ctx).Create(request).Error
}

func (s *DBStore) AccessRequest(ctx context.Context, id string) (model.AccessRequest, error) {
	var request model.AccessRequest
	err := s.db.WithContext(ctx).First(&request, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AccessRequest{}, service.ErrAccessRequestNotFound
	}
	if err != nil {
		return model.AccessRequest{}, fmt.Errorf("get access request: %w", err)
	}
	return request, nil
}

func (s *DBStore) ListAccessRequests(
	ctx context.Context,
	params service.AccessRequestListParams,
) ([]model.AccessRequest, int64, error) {
	query := s.db.WithContext(ctx).Model(&model.AccessRequest{})
	for column, value := range map[string]string{
		"requester_id":  params.RequesterID,
		"resource_type": params.ResourceType,
		"resource_id":   params.ResourceID,
		"protocol":      params.Protocol,
		"status":        params.Status,
	} {
		if value = strings.TrimSpace(value); value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count access requests: %w", err)
	}
	page, size := normalizeAuditPage(params.Page, params.Size)
	var requests []model.AccessRequest
	if err := query.Order("requested_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&requests).Error; err != nil {
		return nil, 0, fmt.Errorf("list access requests: %w", err)
	}
	return requests, total, nil
}

func (s *DBStore) DecideAccessRequest(
	ctx context.Context,
	id string,
	status string,
	decidedBy string,
	remark string,
	decidedAt time.Time,
) (model.AccessRequest, error) {
	return s.changePendingAccessRequest(ctx, id, func(tx *gorm.DB, request *model.AccessRequest) error {
		request.Status = status
		request.DecidedBy = decidedBy
		request.DecidedAt = &decidedAt
		request.DecisionRemark = remark
		return tx.Save(request).Error
	})
}

func (s *DBStore) CancelAccessRequest(
	ctx context.Context,
	id string,
	requesterID string,
	cancelledAt time.Time,
) (model.AccessRequest, error) {
	return s.changePendingAccessRequest(ctx, id, func(tx *gorm.DB, request *model.AccessRequest) error {
		if request.RequesterID != requesterID {
			return service.ErrAccessRequestNotFound
		}
		request.Status = model.AccessRequestCancelled
		request.CancelledAt = &cancelledAt
		return tx.Save(request).Error
	})
}

func (s *DBStore) FindActiveAccessRequest(
	ctx context.Context,
	requesterID string,
	resourceType string,
	resourceID string,
	protocol string,
	now time.Time,
	requiredActions []string,
) (model.AccessRequest, bool, error) {
	var requests []model.AccessRequest
	err := s.db.WithContext(ctx).
		Where(
			"requester_id = ? AND resource_type = ? AND resource_id = ? AND protocol = ? AND status = ?",
			requesterID, resourceType, resourceID, protocol, model.AccessRequestApproved,
		).
		Where("(access_starts_at IS NULL OR access_starts_at <= ?) AND access_expires_at > ?", now, now).
		Order("access_expires_at DESC").
		Find(&requests).Error
	if err != nil {
		return model.AccessRequest{}, false, fmt.Errorf("find active access request: %w", err)
	}
	for _, request := range requests {
		var actions []string
		if err := json.Unmarshal([]byte(request.ActionsJSON), &actions); err != nil {
			return model.AccessRequest{}, false, fmt.Errorf(
				"decode active access request %q actions: %w",
				request.ID,
				err,
			)
		}
		matches := true
		for _, action := range requiredActions {
			if !slices.Contains(actions, strings.TrimSpace(action)) {
				matches = false
				break
			}
		}
		if matches {
			return request, true, nil
		}
	}
	return model.AccessRequest{}, false, nil
}

func (s *DBStore) changePendingAccessRequest(
	ctx context.Context,
	id string,
	change func(*gorm.DB, *model.AccessRequest) error,
) (model.AccessRequest, error) {
	var request model.AccessRequest
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("id = ?", strings.TrimSpace(id))
		if s.db.Dialector.Name() != "sqlite" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&request).Error; err != nil {
			return err
		}
		if request.Status != model.AccessRequestPending {
			return service.ErrAccessRequestConflict
		}
		return change(tx, &request)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.AccessRequest{}, service.ErrAccessRequestNotFound
	}
	if err != nil {
		return model.AccessRequest{}, err
	}
	return request, nil
}
