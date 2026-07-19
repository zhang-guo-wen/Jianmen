package service

import (
	"context"
	"fmt"
	"strings"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

func (s *HostManagementService) CreateHost(ctx context.Context, actor HostManagementActor, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostCreate}, "", ""); err != nil {
		return HostManagementHostView{}, err
	}
	creatorID := actor.ID
	if actor.SuperAdmin {
		creatorID = ""
	}
	view, err := s.repository.CreateManagedHost(ctx, host, creatorID)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("create host: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) UpdateHost(ctx context.Context, actor HostManagementActor, hostID string, host HostManagementHostRecord) (HostManagementHostView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionHostUpdate}, model.ResourceTypeHost, hostID); err != nil {
		return HostManagementHostView{}, err
	}
	view, err := s.repository.UpdateHost(ctx, hostID, host)
	if err != nil {
		return HostManagementHostView{}, fmt.Errorf("update host: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) DeleteHost(ctx context.Context, actor HostManagementActor, hostID string) error {
	if err := s.require(ctx, actor, []string{rbac.ActionHostDelete}, model.ResourceTypeHost, hostID); err != nil {
		return err
	}
	if err := s.repository.DeleteHost(ctx, hostID); err != nil {
		return fmt.Errorf("delete host: %w", err)
	}
	return nil
}

func (s *HostManagementService) Target(ctx context.Context, actor HostManagementActor, targetID string) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetView}, model.ResourceTypeHostAccount, targetID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.Target(ctx, targetID)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("get host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) CreateTarget(ctx context.Context, actor HostManagementActor, target config.Target) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, "", ""); err != nil {
		return HostManagementTargetView{}, err
	}
	if strings.TrimSpace(target.HostID) == "" {
		if !actor.SuperAdmin {
			return HostManagementTargetView{}, fmt.Errorf("%w: host_id is required", ErrHostTargetInvalidInput)
		}
	} else if err := s.require(ctx, actor, []string{rbac.ActionTargetCreate}, model.ResourceTypeHost, target.HostID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.AddTarget(ctx, target)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("create host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) UpdateTarget(ctx context.Context, actor HostManagementActor, targetID string, target config.Target) (HostManagementTargetView, error) {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetUpdate}, model.ResourceTypeHostAccount, targetID); err != nil {
		return HostManagementTargetView{}, err
	}
	view, err := s.repository.UpdateTarget(ctx, targetID, target)
	if err != nil {
		return HostManagementTargetView{}, fmt.Errorf("update host account: %w", err)
	}
	return view, nil
}

func (s *HostManagementService) DeleteTarget(ctx context.Context, actor HostManagementActor, targetID string) error {
	if err := s.require(ctx, actor, []string{rbac.ActionTargetDelete}, model.ResourceTypeHostAccount, targetID); err != nil {
		return err
	}
	if err := s.repository.DeleteTarget(ctx, targetID); err != nil {
		return fmt.Errorf("delete host account: %w", err)
	}
	return nil
}
