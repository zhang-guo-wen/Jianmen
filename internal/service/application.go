package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrApplicationForbidden = errors.New("application access forbidden")
	ErrInvalidApplication   = errors.New("invalid application")
	ErrApplicationRuntime   = errors.New("application proxy runtime update failed")
)

type ApplicationAddress struct {
	Address, EntryPath, Scheme, Host string
	Port                             int
}

type ApplicationActor struct {
	UserID     string
	SuperAdmin bool
}

type ApplicationRequest struct {
	Name, Address, Group, Remark, Status string
	ListenPort                           int
}

type Application struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AppGroup       string `json:"group"`
	ListenPort     int    `json:"listen_port"`
	Address        string `json:"address"`
	EntryPath      string `json:"entry_path"`
	InternalScheme string `json:"internal_scheme"`
	InternalHost   string `json:"internal_host"`
	InternalPort   int    `json:"internal_port"`
	Remark         string `json:"remark,omitempty"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	CanManage      bool   `json:"can_manage"`
}

type ApplicationRepository interface {
	ListApplications(context.Context) []model.Application
	GetApplication(context.Context, string) (model.Application, error)
	CreateManagedApplication(context.Context, model.Application, string) (model.Application, error)
	UpdateManagedApplication(context.Context, string, model.Application) (model.Application, error)
	DeleteManagedApplication(context.Context, string) error
}

type ApplicationAuthorizer interface {
	AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error)
}

type ApplicationProxy interface {
	AddProxy(model.Application) error
	UpdateProxy(int, model.Application) error
	RemoveProxy(int)
}

type ApplicationService struct {
	repository         ApplicationRepository
	authorizer         ApplicationAuthorizer
	proxy              ApplicationProxy
	portStart, portEnd int
	sagaTimeout        time.Duration
}

func NewApplicationService(repository ApplicationRepository, authorizer ApplicationAuthorizer, proxy ApplicationProxy, portStart, portEnd int) (*ApplicationService, error) {
	if repository == nil {
		return nil, errors.New("application repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("application authorizer is required")
	}
	if portStart <= 0 || portEnd < portStart {
		portStart, portEnd = 47110, 47199
	}
	return &ApplicationService{
		repository:  repository,
		authorizer:  authorizer,
		proxy:       proxy,
		portStart:   portStart,
		portEnd:     portEnd,
		sagaTimeout: 5 * time.Second,
	}, nil
}

func (s *ApplicationService) List(ctx context.Context, actor ApplicationActor) ([]Application, error) {
	if strings.TrimSpace(actor.UserID) == "" {
		return nil, ErrApplicationForbidden
	}
	records := s.repository.ListApplications(ctx)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	applications := make([]Application, len(records))
	for i := range records {
		applications[i] = applicationView(records[i])
	}
	if actor.SuperAdmin || len(applications) == 0 {
		for i := range applications {
			applications[i].CanManage = true
		}
		return applications, nil
	}

	requests := make([]AuthorizationRequest, 0, len(applications)*2)
	for _, application := range applications {
		requests = append(requests,
			AuthorizationRequest{
				Actions:      []string{rbac.ActionAppView},
				ResourceType: model.ResourceTypeApplication,
				ResourceID:   application.ID,
			},
			AuthorizationRequest{
				Actions:      []string{rbac.ActionAppUpdate, rbac.ActionAppDelete},
				ResourceType: model.ResourceTypeApplication,
				ResourceID:   application.ID,
			},
		)
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, actor.UserID, requests)
	if err != nil {
		return nil, fmt.Errorf("authorize application list: %w", err)
	}
	if len(decisions) != len(requests) {
		return nil, errors.New("authorize application list: decision count mismatch")
	}
	visible := make([]Application, 0, len(applications))
	for i, application := range applications {
		if !decisions[i*2].Allowed {
			continue
		}
		application.CanManage = decisions[i*2+1].Allowed
		visible = append(visible, application)
	}
	return visible, nil
}

func (s *ApplicationService) Get(ctx context.Context, actor ApplicationActor, id string) (Application, error) {
	if err := s.authorize(ctx, actor, rbac.ActionAppView, id); err != nil {
		return Application{}, err
	}
	record, err := s.repository.GetApplication(ctx, strings.TrimSpace(id))
	if err != nil {
		return Application{}, fmt.Errorf("get application: %w", err)
	}
	return applicationView(record), nil
}

func (s *ApplicationService) Create(
	ctx context.Context,
	actor ApplicationActor,
	request ApplicationRequest,
) (Application, error) {
	actor.UserID = strings.TrimSpace(actor.UserID)
	if actor.UserID == "" {
		return Application{}, ErrApplicationForbidden
	}
	input, err := s.applicationInput(ctx, request, 0)
	if err != nil {
		return Application{}, err
	}
	creatorID := actor.UserID
	if actor.SuperAdmin {
		creatorID = ""
	}
	record, err := s.repository.CreateManagedApplication(ctx, input, creatorID)
	if err != nil {
		return Application{}, fmt.Errorf("create application: %w", err)
	}
	application := applicationView(record)
	if err := ctx.Err(); err != nil {
		return Application{}, s.compensateCreatedApplication(ctx, application, err)
	}
	if s.proxy != nil && application.Status == "active" {
		if err := s.proxy.AddProxy(applicationModel(application)); err != nil {
			return Application{}, s.compensateCreatedApplication(
				ctx,
				application,
				fmt.Errorf("%w: start application proxy: %v", ErrApplicationRuntime, err),
			)
		}
	}
	return application, nil
}

func (s *ApplicationService) Update(
	ctx context.Context,
	actor ApplicationActor,
	id string,
	request ApplicationRequest,
) (Application, error) {
	id = strings.TrimSpace(id)
	if err := s.authorize(ctx, actor, rbac.ActionAppUpdate, id); err != nil {
		return Application{}, err
	}
	previousRecord, err := s.repository.GetApplication(ctx, id)
	if err != nil {
		return Application{}, fmt.Errorf("get application before update: %w", err)
	}
	previous := applicationView(previousRecord)
	input, err := s.applicationInput(ctx, request, previous.ListenPort)
	if err != nil {
		return Application{}, err
	}
	updatedRecord, err := s.repository.UpdateManagedApplication(ctx, id, input)
	if err != nil {
		return Application{}, fmt.Errorf("update application: %w", err)
	}
	updated := applicationView(updatedRecord)
	if s.proxy == nil {
		return updated, nil
	}
	if err := s.reconcileUpdatedProxy(previous, updated); err != nil {
		return Application{}, s.compensateApplicationUpdate(ctx, previous, updated, err)
	}
	return updated, nil
}

func (s *ApplicationService) Delete(ctx context.Context, actor ApplicationActor, id string) error {
	id = strings.TrimSpace(id)
	if err := s.authorize(ctx, actor, rbac.ActionAppDelete, id); err != nil {
		return err
	}
	record, err := s.repository.GetApplication(ctx, id)
	if err != nil {
		return fmt.Errorf("get application before delete: %w", err)
	}
	if err := s.repository.DeleteManagedApplication(ctx, id); err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	if s.proxy != nil && record.Status == "active" {
		s.proxy.RemoveProxy(record.ListenPort)
	}
	return nil
}

func (s *ApplicationService) authorize(ctx context.Context, actor ApplicationActor, action string, id string) error {
	actor.UserID = strings.TrimSpace(actor.UserID)
	id = strings.TrimSpace(id)
	if actor.UserID == "" || id == "" {
		return ErrApplicationForbidden
	}
	if actor.SuperAdmin {
		return nil
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, actor.UserID, []AuthorizationRequest{{
		Actions:      []string{action},
		ResourceType: model.ResourceTypeApplication,
		ResourceID:   id,
	}})
	if err != nil {
		return fmt.Errorf("authorize application: %w", err)
	}
	if len(decisions) != 1 {
		return errors.New("authorize application: decision count mismatch")
	}
	if !decisions[0].Allowed {
		return ErrApplicationForbidden
	}
	return nil
}

func (s *ApplicationService) applicationInput(
	ctx context.Context,
	request ApplicationRequest,
	fallbackPort int,
) (model.Application, error) {
	parsed, err := ParseApplicationAddress(request.Address)
	if err != nil {
		return model.Application{}, fmt.Errorf("%w: %v", ErrInvalidApplication, err)
	}
	listenPort := request.ListenPort
	if listenPort == 0 {
		listenPort = fallbackPort
	}
	if listenPort == 0 {
		listenPort, err = s.nextListenPort(ctx)
		if err != nil {
			return model.Application{}, fmt.Errorf("%w: %v", ErrInvalidApplication, err)
		}
	}
	if listenPort <= 0 || listenPort > 65535 {
		return model.Application{}, fmt.Errorf("%w: listen port must be 1-65535", ErrInvalidApplication)
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = parsed.Host
	}
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if status == "" {
		status = "active"
	}
	return model.Application{
		Name:           name,
		Address:        parsed.Address,
		EntryPath:      parsed.EntryPath,
		InternalScheme: parsed.Scheme,
		InternalHost:   parsed.Host,
		InternalPort:   parsed.Port,
		ListenPort:     listenPort,
		AppGroup:       strings.TrimSpace(request.Group),
		Remark:         strings.TrimSpace(request.Remark),
		Status:         status,
	}, nil
}

func (s *ApplicationService) nextListenPort(ctx context.Context) (int, error) {
	used := make(map[int]struct{})
	for _, record := range s.repository.ListApplications(ctx) {
		used[record.ListenPort] = struct{}{}
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("select application listen port: %w", err)
	}
	for port := s.portStart; port <= s.portEnd; port++ {
		if _, exists := used[port]; !exists {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available application proxy port in range %d-%d", s.portStart, s.portEnd)
}

func (s *ApplicationService) reconcileUpdatedProxy(previous, updated Application) error {
	previousActive := previous.Status == "active"
	updatedActive := updated.Status == "active"
	switch {
	case !previousActive && !updatedActive:
		return nil
	case !previousActive && updatedActive:
		if err := s.proxy.AddProxy(applicationModel(updated)); err != nil {
			return fmt.Errorf("%w: start application proxy: %v", ErrApplicationRuntime, err)
		}
	case previousActive && !updatedActive:
		s.proxy.RemoveProxy(previous.ListenPort)
	case previous.ListenPort != updated.ListenPort:
		if err := s.proxy.AddProxy(applicationModel(updated)); err != nil {
			return fmt.Errorf("%w: start replacement application proxy: %v", ErrApplicationRuntime, err)
		}
		s.proxy.RemoveProxy(previous.ListenPort)
	default:
		if err := s.proxy.UpdateProxy(previous.ListenPort, applicationModel(updated)); err != nil {
			return fmt.Errorf("%w: replace application proxy: %v", ErrApplicationRuntime, err)
		}
	}
	return nil
}

func (s *ApplicationService) compensateCreatedApplication(
	ctx context.Context,
	application Application,
	cause error,
) error {
	compensationCtx, cancel := s.compensationContext(ctx)
	defer cancel()
	if err := s.repository.DeleteManagedApplication(compensationCtx, application.ID); err != nil {
		return errors.Join(cause, fmt.Errorf("compensate application create: %w", err))
	}
	return cause
}

func (s *ApplicationService) compensateApplicationUpdate(
	ctx context.Context,
	previous Application,
	updated Application,
	cause error,
) error {
	compensationCtx, cancel := s.compensationContext(ctx)
	defer cancel()

	previousInput := applicationModel(previous)
	if previous.Status == "active" && updated.Status == "active" && previous.ListenPort == updated.ListenPort {
		if err := s.proxy.AddProxy(applicationModel(previous)); err != nil {
			inactive := previousInput
			inactive.Status = "inactive"
			if _, dbErr := s.repository.UpdateManagedApplication(compensationCtx, previous.ID, inactive); dbErr != nil {
				return errors.Join(
					cause,
					fmt.Errorf("restore previous application proxy: %w", err),
					fmt.Errorf("mark application inactive after proxy restore failure: %w", dbErr),
				)
			}
			return errors.Join(cause, fmt.Errorf("restore previous application proxy: %w", err))
		}
	}
	if _, err := s.repository.UpdateManagedApplication(compensationCtx, previous.ID, previousInput); err != nil {
		if updated.Status == "active" && previous.Status != "active" {
			s.proxy.RemoveProxy(updated.ListenPort)
		}
		return errors.Join(cause, fmt.Errorf("rollback application update: %w", err))
	}
	return cause
}

func (s *ApplicationService) compensationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), s.sagaTimeout)
}

func applicationModel(application Application) model.Application {
	return model.Application{
		ID:             application.ID,
		Name:           application.Name,
		Address:        application.Address,
		EntryPath:      application.EntryPath,
		ListenPort:     application.ListenPort,
		InternalScheme: application.InternalScheme,
		InternalHost:   application.InternalHost,
		InternalPort:   application.InternalPort,
		AppGroup:       application.AppGroup,
		Remark:         application.Remark,
		Status:         application.Status,
	}
}

func applicationView(application model.Application) Application {
	return Application{
		ID:             application.ID,
		Name:           application.Name,
		AppGroup:       application.AppGroup,
		ListenPort:     application.ListenPort,
		Address:        application.Address,
		EntryPath:      application.EntryPath,
		InternalScheme: application.InternalScheme,
		InternalHost:   application.InternalHost,
		InternalPort:   application.InternalPort,
		Remark:         application.Remark,
		Status:         application.Status,
		CreatedAt:      application.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      application.UpdatedAt.Format(time.RFC3339),
	}
}

func ParseApplicationAddress(raw string) (ApplicationAddress, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ApplicationAddress{}, fmt.Errorf("application address is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ApplicationAddress{}, fmt.Errorf("parse application address: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ApplicationAddress{}, fmt.Errorf("application address scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return ApplicationAddress{}, fmt.Errorf("application address host is required")
	}
	if parsed.User != nil {
		return ApplicationAddress{}, fmt.Errorf("application address must not contain credentials")
	}

	port := 80
	if parsed.Scheme == "https" {
		port = 443
	}
	if rawPort := parsed.Port(); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port <= 0 || port > 65535 {
			return ApplicationAddress{}, fmt.Errorf("application address port must be 1-65535")
		}
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	entryPath := parsed.EscapedPath()
	if entryPath == "" {
		entryPath = "/"
	}
	if parsed.RawQuery != "" {
		entryPath += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		entryPath += "#" + parsed.EscapedFragment()
	}

	return ApplicationAddress{
		Address:   parsed.String(),
		EntryPath: entryPath,
		Scheme:    parsed.Scheme,
		Host:      parsed.Hostname(),
		Port:      port,
	}, nil
}
