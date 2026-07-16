package store

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) Applications() []ApplicationView {
	var apps []model.Application
	if err := s.db.Order("name ASC").Find(&apps).Error; err != nil {
		return nil
	}
	views := make([]ApplicationView, 0, len(apps))
	for _, app := range apps {
		views = append(views, applicationView(app))
	}
	return views
}

func (s *DBStore) Application(id string) (ApplicationView, error) {
	id = strings.TrimSpace(id)
	var app model.Application
	if err := s.db.First(&app, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return ApplicationView{}, err
	}
	return applicationView(app), nil
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

func (s *DBStore) AddApplication(input ApplicationInput) (ApplicationView, error) {
	if err := validateApplicationInput(input); err != nil {
		return ApplicationView{}, err
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
	}
	if app.Name == "" {
		app.Name = app.Address
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&app).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, app.AppGroup); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
	}); err != nil {
		return ApplicationView{}, err
	}
	return applicationView(app), nil
}

func (s *DBStore) UpdateApplication(id string, input ApplicationInput) (ApplicationView, error) {
	id = strings.TrimSpace(id)
	var app model.Application
	if err := s.db.First(&app, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return ApplicationView{}, err
	}
	if input.ListenPort <= 0 {
		input.ListenPort = app.ListenPort
	}
	if err := validateApplicationInput(input); err != nil {
		return ApplicationView{}, err
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
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&app).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, app.AppGroup); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
	}); err != nil {
		return ApplicationView{}, err
	}
	return applicationView(app), nil
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

func (s *DBStore) DeleteApplication(id string) error {
	id = strings.TrimSpace(id)
	return s.db.Transaction(func(tx *gorm.DB) error {
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
		return tx.Delete(&app).Error
	})
}
