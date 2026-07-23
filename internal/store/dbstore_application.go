package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) Applications(ctx context.Context) []ApplicationView {
	var apps []model.Application
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&apps).Error; err != nil {
		return nil
	}
	views := make([]ApplicationView, 0, len(apps))
	for _, app := range apps {
		views = append(views, applicationView(app))
	}
	return views
}

func (s *DBStore) ListApplications(ctx context.Context) []model.Application {
	var applications []model.Application
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&applications).Error; err != nil {
		return nil
	}
	return applications
}

func (s *DBStore) Application(ctx context.Context, id string) (ApplicationView, error) {
	id = strings.TrimSpace(id)
	var app model.Application
	if err := s.db.WithContext(ctx).First(&app, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return ApplicationView{}, err
	}
	return applicationView(app), nil
}

func (s *DBStore) GetApplication(ctx context.Context, id string) (model.Application, error) {
	id = strings.TrimSpace(id)
	var application model.Application
	if err := s.db.WithContext(ctx).First(&application, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Application{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return model.Application{}, err
	}
	return application, nil
}

func applicationView(app model.Application) ApplicationView {
	address := strings.TrimSpace(app.Address)
	entryPath := normalizeApplicationEntryPath(app.EntryPath)
	if address == "" {
		address = fmt.Sprintf("%s://%s%s", app.InternalScheme, net.JoinHostPort(app.InternalHost, fmt.Sprintf("%d", app.InternalPort)), entryPath)
	}
	return ApplicationView{
		ID:             app.ID,
		Name:           app.Name,
		AppGroup:       app.AppGroup,
		ListenPort:     app.ListenPort,
		Address:        address,
		EntryPath:      entryPath,
		InternalScheme: app.InternalScheme,
		InternalHost:   app.InternalHost,
		InternalPort:   app.InternalPort,
		Remark:         app.Remark,
		Status:         app.Status,
		CreatedAt:      app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      app.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) AddApplication(ctx context.Context, input ApplicationInput) (ApplicationView, error) {
	application, err := s.createApplication(ctx, input, "")
	if err != nil {
		return ApplicationView{}, err
	}
	return applicationView(application), nil
}

// CreateManagedApplication atomically creates the application resource and, when
// creatorID is provided, the creator's management grant.
func (s *DBStore) CreateManagedApplication(
	ctx context.Context,
	input model.Application,
	creatorID string,
) (model.Application, error) {
	return s.createApplication(ctx, applicationInputFromModel(input), creatorID)
}

func (s *DBStore) createApplication(
	ctx context.Context,
	input ApplicationInput,
	creatorID string,
) (model.Application, error) {
	if err := validateApplicationInput(input); err != nil {
		return model.Application{}, err
	}
	app := model.Application{
		Name:           strings.TrimSpace(input.Name),
		Address:        strings.TrimSpace(input.Address),
		EntryPath:      normalizeApplicationEntryPath(input.EntryPath),
		InternalScheme: input.InternalScheme,
		InternalHost:   input.InternalHost,
		InternalPort:   input.InternalPort,
		ListenPort:     input.ListenPort,
		AppGroup:       strings.TrimSpace(input.AppGroup),
		Remark:         strings.TrimSpace(input.Remark),
		Status:         strings.TrimSpace(input.Status),
	}
	if app.Name == "" {
		app.Name = app.Address
	}
	creatorID = strings.TrimSpace(creatorID)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&app).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, app.AppGroup); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, ""); err != nil {
			return err
		}
		if creatorID == "" {
			return nil
		}
		var creatorCount int64
		if err := tx.Model(&model.User{}).Scopes(ActiveScope).Where("id = ?", creatorID).Count(&creatorCount).Error; err != nil {
			return fmt.Errorf("check application creator: %w", err)
		}
		if creatorCount == 0 {
			return fmt.Errorf("application creator not found: %q", creatorID)
		}
		grant := model.ResourceGrant{
			PrincipalType: "user",
			PrincipalID:   creatorID,
			ResourceType:  model.ResourceTypeApplication,
			ResourceID:    app.ID,
			Effect:        model.PermissionEffectAllow,
		}
		if err := tx.Where(&model.ResourceGrant{
			PrincipalType: grant.PrincipalType,
			PrincipalID:   grant.PrincipalID,
			ResourceType:  grant.ResourceType,
			ResourceID:    grant.ResourceID,
			Effect:        grant.Effect,
		}).FirstOrCreate(&grant).Error; err != nil {
			return fmt.Errorf("create application creator grant: %w", err)
		}
		return nil
	}); err != nil {
		return model.Application{}, err
	}
	return app, nil
}

func (s *DBStore) UpdateApplication(ctx context.Context, id string, input ApplicationInput) (ApplicationView, error) {
	application, err := s.updateApplication(ctx, id, input)
	if err != nil {
		return ApplicationView{}, err
	}
	return applicationView(application), nil
}

func (s *DBStore) updateApplication(ctx context.Context, id string, input ApplicationInput) (model.Application, error) {
	id = strings.TrimSpace(id)
	var app model.Application
	if err := s.db.WithContext(ctx).First(&app, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Application{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return model.Application{}, err
	}
	if input.ListenPort <= 0 {
		input.ListenPort = app.ListenPort
	}
	if err := validateApplicationInput(input); err != nil {
		return model.Application{}, err
	}

	app.Name = strings.TrimSpace(input.Name)
	app.Address = strings.TrimSpace(input.Address)
	app.EntryPath = normalizeApplicationEntryPath(input.EntryPath)
	app.InternalScheme = input.InternalScheme
	app.InternalHost = input.InternalHost
	app.InternalPort = input.InternalPort
	app.ListenPort = input.ListenPort
	app.AppGroup = strings.TrimSpace(input.AppGroup)
	app.Remark = strings.TrimSpace(input.Remark)
	if input.Status != "" {
		app.Status = input.Status
	}
	if app.Name == "" {
		app.Name = app.Address
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&app).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, app.AppGroup); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
	}); err != nil {
		return model.Application{}, err
	}
	return app, nil
}

func (s *DBStore) UpdateManagedApplication(
	ctx context.Context,
	id string,
	input model.Application,
) (model.Application, error) {
	return s.updateApplication(ctx, id, applicationInputFromModel(input))
}

func validateApplicationInput(input ApplicationInput) error {
	if strings.TrimSpace(input.Address) == "" {
		return fmt.Errorf("application address is required")
	}
	if input.InternalScheme != "http" && input.InternalScheme != "https" {
		return fmt.Errorf("application scheme must be http or https")
	}
	if strings.TrimSpace(input.InternalHost) == "" {
		return fmt.Errorf("application host is required")
	}
	if input.InternalPort <= 0 || input.InternalPort > 65535 {
		return fmt.Errorf("application port must be 1-65535")
	}
	if input.ListenPort <= 0 || input.ListenPort > 65535 {
		return fmt.Errorf("listen port must be 1-65535")
	}
	return nil
}

func normalizeApplicationEntryPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func (s *DBStore) DeleteApplication(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var app model.Application
		if err := tx.First(&app, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
			}
			return err
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeApplication, app.ID); err != nil {
			return err
		}
		return SoftDelete(ctx, tx, "applications", id)
	})
}

func (s *DBStore) DeleteManagedApplication(ctx context.Context, id string) error {
	return s.DeleteApplication(ctx, id)
}

func applicationInputFromModel(input model.Application) ApplicationInput {
	return ApplicationInput{
		Name:           input.Name,
		Address:        input.Address,
		EntryPath:      input.EntryPath,
		InternalScheme: input.InternalScheme,
		InternalHost:   input.InternalHost,
		InternalPort:   input.InternalPort,
		ListenPort:     input.ListenPort,
		AppGroup:       input.AppGroup,
		Remark:         input.Remark,
		Status:         input.Status,
	}
}
