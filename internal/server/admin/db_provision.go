package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type databaseProvisioningService interface {
	ListDatabases(
		context.Context,
		service.ListProvisioningDatabasesRequest,
	) ([]string, error)
	Provision(
		context.Context,
		service.ProvisionDatabaseAccountRequest,
	) (service.ProvisionDatabaseAccountResult, error)
	Deprovision(context.Context, string) error
}

type provisionDatabaseAccountPayload struct {
	AdminAccountID string            `json:"admin_account_id"`
	Grants         []service.DBGrant `json:"grants"`
	Group          string            `json:"group"`
	Remark         string            `json:"remark"`
	ExpiresAt      *time.Time        `json:"expires_at"`
}

// handleDBDatabases handles GET /api/db/instances/{id}/databases.
func (s *Server) handleDBDatabases(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyView) {
		s.forbidden(w, r)
		return
	}
	adminAccountID := strings.TrimSpace(r.URL.Query().Get("admin_account_id"))
	if adminAccountID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin_account_id is required")
		return
	}
	if !s.requireResourceAction(
		w,
		r,
		rbac.ActionDBConnect,
		model.ResourceTypeDatabaseAccount,
		adminAccountID,
	) {
		return
	}
	if s.databaseProvisioning == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "database provisioning is unavailable")
		return
	}
	databases, err := s.databaseProvisioning.ListDatabases(
		r.Context(),
		service.ListProvisioningDatabasesRequest{
			InstanceID: instanceID, AdminAccountID: adminAccountID,
			Actor: databaseProvisioningActorFromRequest(r),
		},
	)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadGateway, "database operation failed")
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"databases": databases})
}

// handleDBProvisionAccount handles POST /api/db/instances/{id}/provision-account.
func (s *Server) handleDBProvisionAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
		s.forbidden(w, r)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload provisionDatabaseAccountPayload
	if err := decodeStrictProvisioningPayload(r, &payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database provisioning request")
		return
	}
	payload.AdminAccountID = strings.TrimSpace(payload.AdminAccountID)
	if payload.AdminAccountID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database provisioning request")
		return
	}
	idempotencyKey, err := service.ValidateDatabaseProvisioningIdempotencyKey(
		r.Header.Get("Idempotency-Key"),
	)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid idempotency key")
		return
	}
	if !s.requireResourceAction(
		w,
		r,
		rbac.ActionDBConnect,
		model.ResourceTypeDatabaseAccount,
		payload.AdminAccountID,
	) {
		return
	}
	if s.databaseProvisioning == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "database provisioning is unavailable")
		return
	}
	result, err := s.databaseProvisioning.Provision(
		r.Context(),
		service.ProvisionDatabaseAccountRequest{
			InstanceID: instanceID, AdminAccountID: payload.AdminAccountID,
			Grants: payload.Grants,
			Group:  payload.Group, Remark: payload.Remark, ExpiresAt: payload.ExpiresAt,
			Actor:          databaseProvisioningActorFromRequest(r),
			IdempotencyKey: idempotencyKey,
		},
	)
	if err != nil {
		s.writeDatabaseProvisioningServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"ok": true, "account": result.Account, "operation_id": result.OperationID,
	})
}

func decodeStrictProvisioningPayload(
	r *http.Request,
	payload *provisionDatabaseAccountPayload,
) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(payload); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("database provisioning request contains trailing data")
	}
	return nil
}

func databaseProvisioningActorFromRequest(r *http.Request) service.DatabaseProvisioningActor {
	return service.DatabaseProvisioningActor{
		UserID: userIDFromRequest(r), Username: usernameFromRequest(r),
		ClientIP: requestClientIP(r),
	}
}

func (s *Server) writeDatabaseProvisioningServiceError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, service.ErrInvalidDatabaseProvisioningRequest):
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database provisioning request")
	case errors.Is(err, service.ErrDatabaseProvisioningIdempotencyConflict):
		s.writeErrorText(w, r, http.StatusConflict, "database provisioning request conflicts with idempotency key")
	case errors.Is(err, service.ErrDatabaseProvisioningInProgress):
		s.writeErrorText(w, r, http.StatusConflict, "database account provisioning is in progress")
	case errors.Is(err, service.ErrDatabaseProvisioningCleanupRequired):
		s.writeErrorText(
			w,
			r,
			http.StatusInternalServerError,
			"database account provisioning failed; cleanup is pending",
		)
	default:
		s.writeErrorText(w, r, http.StatusBadGateway, "database account provisioning failed")
	}
}

func (s *Server) writeDatabaseDeprovisionServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrDatabaseDeprovisionInProgress):
		s.writeErrorText(w, r, http.StatusConflict, "database account deletion is in progress")
	default:
		s.writeErrorText(w, r, http.StatusBadGateway, "database account deletion failed")
	}
}
