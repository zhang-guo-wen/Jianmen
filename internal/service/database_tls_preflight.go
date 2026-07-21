package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type DatabaseTLSPreflightRepository interface {
	DatabaseInstanceForProbe(context.Context, string) (DatabaseInstanceRecord, error)
}

type DatabaseTLSPreflightAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
}

type DatabaseTLSPreflightProber interface {
	ProbeTLS(context.Context, DatabaseInstanceRecord) error
}

type DatabaseTLSPreflightInput struct {
	InstanceID, Protocol, Address, TLSMode, TLSServerName string
	Port                                                  int
	TLSCAPEM                                              *string
	ClearTLSCA                                            bool
}

type DatabaseTLSPreflightService struct {
	repository DatabaseTLSPreflightRepository
	authorizer DatabaseTLSPreflightAuthorizer
	prober     DatabaseTLSPreflightProber
	probeSlots chan struct{}
}

var (
	ErrDatabaseTLSPreflightFailed = errors.New("database TLS preflight failed")
	ErrDatabaseTLSPreflightStale  = errors.New("database TLS settings changed during preflight")
)

const maxConcurrentDatabaseTLSPreflights = 8

func NewDatabaseTLSPreflightService(
	repository DatabaseTLSPreflightRepository,
	authorizer DatabaseTLSPreflightAuthorizer,
	prober DatabaseTLSPreflightProber,
) (*DatabaseTLSPreflightService, error) {
	if repository == nil {
		return nil, errors.New("database TLS preflight repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("database TLS preflight authorizer is required")
	}
	if prober == nil {
		return nil, errors.New("database TLS preflight prober is required")
	}
	return &DatabaseTLSPreflightService{
		repository: repository,
		authorizer: authorizer,
		prober:     prober,
		probeSlots: make(chan struct{}, maxConcurrentDatabaseTLSPreflights),
	}, nil
}

func (s *DatabaseTLSPreflightService) Probe(ctx context.Context, actorID string, input DatabaseTLSPreflightInput) error {
	target, err := s.target(ctx, actorID, input)
	if err != nil {
		return err
	}
	return s.probeResolved(ctx, target)
}

func (s *DatabaseTLSPreflightService) probeResolved(ctx context.Context, target DatabaseInstanceRecord) error {
	select {
	case s.probeSlots <- struct{}{}:
		defer func() { <-s.probeSlots }()
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrDatabaseTLSPreflightFailed, ctx.Err())
	}
	if err := s.prober.ProbeTLS(ctx, target); err != nil {
		return fmt.Errorf("%w: %w", ErrDatabaseTLSPreflightFailed, err)
	}
	return nil
}

func (s *DatabaseTLSPreflightService) target(ctx context.Context, actorID string, input DatabaseTLSPreflightInput) (DatabaseInstanceRecord, error) {
	instanceID := strings.TrimSpace(input.InstanceID)
	if err := s.authorize(ctx, actorID, instanceID); err != nil {
		return DatabaseInstanceRecord{}, err
	}

	existingCA := ""
	if instanceID != "" {
		instance, err := s.repository.DatabaseInstanceForProbe(ctx, instanceID)
		if err != nil {
			return DatabaseInstanceRecord{}, fmt.Errorf("load database instance for TLS preflight: %w", err)
		}
		existingCA = instance.TLSCAPEM
	}

	return resolveDatabaseTLSPreflightTarget(input, existingCA)
}

func resolveDatabaseTLSPreflightTarget(input DatabaseTLSPreflightInput, existingCA string) (DatabaseInstanceRecord, error) {
	protocol, err := normalizeTLSPreflightProtocol(input.Protocol)
	if err != nil {
		return DatabaseInstanceRecord{}, err
	}
	address := strings.TrimSpace(input.Address)
	if address == "" {
		return DatabaseInstanceRecord{}, fmt.Errorf("%w: address is required", ErrDatabaseManagementInvalid)
	}
	port := input.Port
	if port == 0 {
		port = defaultTLSPreflightPort(protocol)
	}
	if port < 1 || port > 65535 {
		return DatabaseInstanceRecord{}, fmt.Errorf("%w: port must be between 1 and 65535", ErrDatabaseManagementInvalid)
	}
	mode, err := dbtls.NormalizeMode(input.TLSMode)
	if err != nil || mode == dbtls.ModeDisable {
		return DatabaseInstanceRecord{}, fmt.Errorf("%w: enabled TLS mode is required", ErrDatabaseManagementInvalid)
	}

	caPEM := existingCA
	if input.TLSCAPEM != nil {
		caPEM = strings.TrimSpace(*input.TLSCAPEM)
	}
	if input.ClearTLSCA {
		caPEM = ""
	}
	serverName := strings.TrimSpace(input.TLSServerName)
	if _, err := dbtls.ClientConfig(
		dbtls.Config{Mode: mode, ServerName: serverName, CAPEM: caPEM},
		net.JoinHostPort(address, strconv.Itoa(port)),
	); err != nil {
		return DatabaseInstanceRecord{}, fmt.Errorf("%w: invalid TLS configuration", ErrDatabaseManagementInvalid)
	}

	return DatabaseInstanceRecord{
		ID:            strings.TrimSpace(input.InstanceID),
		Protocol:      protocol,
		Address:       address,
		Port:          port,
		TLSMode:       mode,
		TLSServerName: serverName,
		TLSCAPEM:      caPEM,
	}, nil
}

func (s *DatabaseTLSPreflightService) authorize(ctx context.Context, actorID, instanceID string) error {
	resourceType, resourceID := "", ""
	action := rbac.ActionDBProxyCreate
	if instanceID != "" {
		action = rbac.ActionDBProxyUpdate
		resourceType = model.ResourceTypeDatabaseInstance
		resourceID = instanceID
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		strings.TrimSpace(actorID),
		[]string{action},
		resourceType,
		resourceID,
	)
	if err != nil {
		return fmt.Errorf("authorize database TLS preflight: %w", err)
	}
	if !allowed {
		return ErrDatabaseManagementForbidden
	}
	return nil
}

func normalizeTLSPreflightProtocol(protocol string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "pg" || protocol == "postgresql" {
		protocol = "postgres"
	}
	switch protocol {
	case "mysql", "postgres", "redis":
		return protocol, nil
	default:
		return "", fmt.Errorf("%w: unsupported database protocol", ErrDatabaseManagementInvalid)
	}
}

func defaultTLSPreflightPort(protocol string) int {
	switch protocol {
	case "postgres":
		return 5432
	case "redis":
		return 6379
	default:
		return 3306
	}
}

func (s *DatabaseManagementService) preflightInstanceUpdate(ctx context.Context, id string, input DatabaseInstanceInput) (DatabaseInstanceInput, DatabaseInstanceTLSState, bool, error) {
	existing, err := s.repository.DatabaseInstanceForProbe(ctx, id)
	if err != nil {
		return DatabaseInstanceInput{}, DatabaseInstanceTLSState{}, false, fmt.Errorf("load database instance before update: %w", err)
	}
	proof := databaseInstanceTLSState(existing)
	if strings.TrimSpace(input.TLSMode) == "" {
		input.TLSMode = existing.TLSMode
		input.TLSServerName = existing.TLSServerName
	}
	mode, err := dbtls.NormalizeMode(input.TLSMode)
	if err != nil {
		return DatabaseInstanceInput{}, DatabaseInstanceTLSState{}, false, fmt.Errorf("%w: enabled TLS mode is required", ErrDatabaseManagementInvalid)
	}
	input.TLSMode = mode
	if mode == dbtls.ModeDisable {
		return input, proof, false, nil
	}
	if !databaseTLSSettingsChanged(existing, input, mode) {
		return input, proof, true, nil
	}
	target, err := resolveDatabaseTLSPreflightTarget(databaseTLSPreflightInput(id, input), existing.TLSCAPEM)
	if err != nil {
		return DatabaseInstanceInput{}, DatabaseInstanceTLSState{}, false, err
	}
	if err := s.tlsPreflight.probeResolved(ctx, target); err != nil {
		return DatabaseInstanceInput{}, DatabaseInstanceTLSState{}, false, err
	}
	input.Protocol = target.Protocol
	input.Address = target.Address
	input.Port = target.Port
	input.TLSMode = target.TLSMode
	input.TLSServerName = target.TLSServerName
	caPEM := target.TLSCAPEM
	input.TLSCAPEM = &caPEM
	input.ClearTLSCA = false
	return input, proof, true, nil
}

func databaseTLSModeEnabled(mode string) bool {
	normalized, err := dbtls.NormalizeMode(mode)
	return err != nil || normalized != dbtls.ModeDisable
}

func databaseTLSSettingsChanged(existing DatabaseInstanceRecord, input DatabaseInstanceInput, mode string) bool {
	caPEM := strings.TrimSpace(existing.TLSCAPEM)
	if input.TLSCAPEM != nil {
		caPEM = strings.TrimSpace(*input.TLSCAPEM)
	}
	if input.ClearTLSCA {
		caPEM = ""
	}
	return !strings.EqualFold(strings.TrimSpace(existing.Protocol), strings.TrimSpace(input.Protocol)) ||
		strings.TrimSpace(existing.Address) != strings.TrimSpace(input.Address) ||
		existing.Port != input.Port ||
		strings.TrimSpace(existing.TLSMode) != strings.TrimSpace(mode) ||
		strings.TrimSpace(existing.TLSServerName) != strings.TrimSpace(input.TLSServerName) ||
		strings.TrimSpace(existing.TLSCAPEM) != caPEM
}

func databaseTLSPreflightInput(instanceID string, input DatabaseInstanceInput) DatabaseTLSPreflightInput {
	return DatabaseTLSPreflightInput{
		InstanceID:    instanceID,
		Protocol:      input.Protocol,
		Address:       input.Address,
		Port:          input.Port,
		TLSMode:       input.TLSMode,
		TLSServerName: input.TLSServerName,
		TLSCAPEM:      input.TLSCAPEM,
		ClearTLSCA:    input.ClearTLSCA,
	}
}

func databaseInstanceTLSState(instance DatabaseInstanceRecord) DatabaseInstanceTLSState {
	return DatabaseInstanceTLSState{
		Protocol:      instance.Protocol,
		Address:       instance.Address,
		Port:          instance.Port,
		TLSMode:       instance.TLSMode,
		TLSServerName: instance.TLSServerName,
		TLSCAPEM:      instance.TLSCAPEM,
	}
}
