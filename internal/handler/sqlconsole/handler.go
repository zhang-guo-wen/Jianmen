package sqlconsole

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/service"
)

const maxRequestBodyBytes = 70 * 1024

type Actor struct {
	UserID, Username, ClientIP string
}

type Handler struct {
	service *service.SQLConsoleService
}

func New(sqlService *service.SQLConsoleService) (*Handler, error) {
	if sqlService == nil {
		return nil, errors.New("SQL console service is required")
	}
	return &Handler{service: sqlService}, nil
}

func (h *Handler) HandleCreateSession(w http.ResponseWriter, r *http.Request, actor Actor) {
	if r.Method != http.MethodPost {
		apiresp.WriteError(
			w, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed,
			"method not allowed", nil, apiresp.RequestID(r.Context()),
		)
		return
	}
	var request struct {
		AccountID string `json:"account_id"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	result, err := h.service.CreateSession(
		r.Context(),
		service.SQLConsoleActor{UserID: actor.UserID, Username: actor.Username, ClientIP: actor.ClientIP},
		request.AccountID,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	apiresp.Write(w, http.StatusCreated, result, apiresp.RequestID(r.Context()))
}

func (h *Handler) HandleExecute(w http.ResponseWriter, r *http.Request, actor Actor, sessionID string) {
	if r.Method != http.MethodPost {
		apiresp.WriteError(
			w, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed,
			"method not allowed", nil, apiresp.RequestID(r.Context()),
		)
		return
	}
	var request struct {
		Database     string `json:"database"`
		SQL          string `json:"sql"`
		ConfirmWrite bool   `json:"confirm_write"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	result, err := h.service.Execute(
		r.Context(),
		service.SQLConsoleActor{UserID: actor.UserID, Username: actor.Username, ClientIP: actor.ClientIP},
		service.SQLConsoleRequest{
			SessionID:    sessionID,
			Database:     request.Database,
			SQL:          request.SQL,
			ConfirmWrite: request.ConfirmWrite,
		},
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	apiresp.Write(w, http.StatusOK, result, apiresp.RequestID(r.Context()))
}

func (h *Handler) HandleCloseSession(w http.ResponseWriter, r *http.Request, actor Actor, sessionID string) {
	if r.Method != http.MethodDelete {
		apiresp.WriteError(
			w, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed,
			"method not allowed", nil, apiresp.RequestID(r.Context()),
		)
		return
	}
	err := h.service.CloseSession(
		r.Context(),
		service.SQLConsoleActor{UserID: actor.UserID, Username: actor.Username, ClientIP: actor.ClientIP},
		sessionID,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		apiresp.WriteError(
			w, http.StatusBadRequest, apiresp.CodeValidation,
			"invalid SQL request", nil, apiresp.RequestID(r.Context()),
		)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		apiresp.WriteError(
			w, http.StatusBadRequest, apiresp.CodeValidation,
			"invalid SQL request", nil, apiresp.RequestID(r.Context()),
		)
		return false
	}
	return true
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	code := apiresp.CodeValidation
	message := err.Error()
	switch {
	case errors.Is(err, service.ErrSQLConsoleForbidden):
		status, code, message = http.StatusForbidden, apiresp.CodeForbidden, "SQL console access forbidden"
	case errors.Is(err, service.ErrSQLConsoleNotFound):
		status, code, message = http.StatusNotFound, apiresp.CodeNotFound, "database account not found"
	case errors.Is(err, service.ErrSQLConsoleSession):
		status, code, message = http.StatusNotFound, apiresp.CodeNotFound, "SQL console session not found"
	case errors.Is(err, service.ErrSQLConsoleUnavailable):
		status, code, message = http.StatusConflict, apiresp.CodeConflict, "database account is unavailable"
	case errors.Is(err, service.ErrSQLConsoleWriteConfirmation):
		status, code, message = http.StatusPreconditionFailed, apiresp.CodePreconditionFailed, "write statement confirmation is required"
	case errors.Is(err, service.ErrSQLConsoleAudit):
		status, code, message = http.StatusServiceUnavailable, apiresp.CodeServiceUnavailable, "SQL audit is unavailable"
	case errors.Is(err, service.ErrSQLConsoleExecution):
		status, code = http.StatusBadGateway, apiresp.CodeBadGateway
	}
	apiresp.WriteError(w, status, code, message, nil, apiresp.RequestID(r.Context()))
}
