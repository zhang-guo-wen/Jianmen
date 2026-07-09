package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// -- application CRUD (DB-backed) --

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
	return ApplicationView{
		ID:             app.ID,
		Name:           app.Name,
		AppGroup:       app.AppGroup,
		ListenPort:     app.ListenPort,
		InternalScheme: app.InternalScheme,
		InternalHost:   app.InternalHost,
		InternalPort:   app.InternalPort,
		Remark:         app.Remark,
		Status:         app.Status,
		CreatedAt:      app.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      app.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) AddApplication(name, scheme, host string, port, listenPort int, group, remark string) (ApplicationView, error) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return ApplicationView{}, fmt.Errorf("internal host is required")
	}
	if listenPort <= 0 || listenPort > 65535 {
		return ApplicationView{}, fmt.Errorf("listen port must be 1-65535")
	}
	if port <= 0 {
		port = 80
		if scheme == "https" {
			port = 443
		}
	}
	app := model.Application{
		Name:           strings.TrimSpace(name),
		InternalScheme: scheme,
		InternalHost:   host,
		InternalPort:   port,
		ListenPort:     listenPort,
		AppGroup:       strings.TrimSpace(group),
		Remark:         strings.TrimSpace(remark),
	}
	if app.Name == "" {
		app.Name = fmt.Sprintf("%s://%s:%d", scheme, host, port)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&app).Error; err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
	}); err != nil {
		return ApplicationView{}, err
	}
	return applicationView(app), nil
}

func (s *DBStore) UpdateApplication(id, name, scheme, host string, port, listenPort int, group, remark, status string) (ApplicationView, error) {
	id = strings.TrimSpace(id)
	var app model.Application
	if err := s.db.First(&app, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ApplicationView{}, fmt.Errorf("%w: %q", ErrApplicationNotFound, id)
		}
		return ApplicationView{}, err
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		scheme = app.InternalScheme
	}
	host = strings.TrimSpace(host)
	if host != "" {
		app.InternalHost = host
	}
	if port > 0 {
		app.InternalPort = port
	}
	if listenPort > 0 {
		app.ListenPort = listenPort
	}
	app.Name = strings.TrimSpace(name)
	app.InternalScheme = scheme
	app.AppGroup = strings.TrimSpace(group)
	app.Remark = strings.TrimSpace(remark)
	if status != "" {
		app.Status = status
	}
	if app.Name == "" {
		app.Name = fmt.Sprintf("%s://%s:%d", app.InternalScheme, app.InternalHost, app.InternalPort)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&app).Error; err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeApplication, app.ID, app.Name, "")
	}); err != nil {
		return ApplicationView{}, err
	}
	return applicationView(app), nil
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
