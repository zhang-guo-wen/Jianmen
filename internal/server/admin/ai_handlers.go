package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

const (
	aiAccessTokenDefaultTTL  = time.Hour
	aiRefreshTokenDefaultTTL = 30 * 24 * time.Hour
	aiAccessTokenMinTTL      = 5 * time.Minute
	aiAccessTokenMaxTTL      = 24 * time.Hour
	aiRefreshTokenMinTTL     = 24 * time.Hour
	aiRefreshTokenMaxTTL     = 90 * 24 * time.Hour
)

type aiTokenRequest struct {
	Name              string `json:"name"`
	AccessTTLSeconds  int64  `json:"access_ttl_seconds"`
	RefreshTTLSeconds int64  `json:"refresh_ttl_seconds"`
}

type aiRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) handleAIDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	baseURL := s.aiBaseURL(r)
	content := fmt.Sprintf(`# Jianmen AI Bastion API

Base URL: %s

This API lets an AI agent use resources authorized to the signed-in Jianmen user. The agent never receives the user's administrator password or target server secret.

## Authentication

1. In Jianmen, open **AI access** and create an access token.
2. Send access_token as Authorization: Bearer <access_token>.
3. Before expiry, call POST /api/ai/auth/refresh with refresh_token. Refresh rotates both tokens; discard the old pair immediately.
4. A token only inherits the user's current RBAC resource grants. Revoking the token or the user's access takes effect immediately.

## Endpoints

- GET /api/ai/resources - list authorized host accounts and database accounts.
- GET /api/ai/resources/{type}/{id} - read connection metadata.
- POST /api/ai/resources/{type}/{id}/session - create or reuse a bastion session identity.
- POST /api/ai/resources/{type}/{id}/credentials - issue a reusable 30-minute connection password.
- POST /api/ai/auth/refresh - rotate an access/refresh token pair.

Supported resource types are host_account and database_account.

## Typical SSH flow

    curl -H "Authorization: Bearer $JIANMEN_AI_ACCESS_TOKEN" %s/api/ai/resources
    curl -X POST -H "Authorization: Bearer $JIANMEN_AI_ACCESS_TOKEN" %s/api/ai/resources/host_account/<id>/session
    curl -X POST -H "Authorization: Bearer $JIANMEN_AI_ACCESS_TOKEN" %s/api/ai/resources/host_account/<id>/credentials
    ssh -p <bastion_ssh_port> <compact_username>@<bastion_host>

Use compact_username as the SSH username. The temporary password authenticates to the bastion; the target server password is never returned.

## Typical database flow

    curl -X POST -H "Authorization: Bearer $JIANMEN_AI_ACCESS_TOKEN" %s/api/ai/resources/database_account/<id>/session
    curl -X POST -H "Authorization: Bearer $JIANMEN_AI_ACCESS_TOKEN" %s/api/ai/resources/database_account/<id>/credentials

Use the returned database gateway address, compact username, and temporary password. Do not log tokens or temporary passwords.

## Refresh example

    curl -X POST -H "Content-Type: application/json" -d '{"refresh_token":"<refresh_token>"}' %s/api/ai/auth/refresh

All successful responses use Jianmen's JSON envelope. Secret values are only returned at the moment they are issued.
`, baseURL, baseURL, baseURL, baseURL, baseURL, baseURL, baseURL)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleAITokens(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "user not authenticated")
		return
	}
	switch r.Method {
	case http.MethodGet:
		tokens, err := s.store.ListAIAccessTokens(r.Context(), userID)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		views := make([]map[string]any, 0, len(tokens))
		for _, token := range tokens {
			views = append(views, map[string]any{
				"id": token.ID, "name": token.Name,
				"access_expires_at": token.AccessExpiresAt, "refresh_expires_at": token.RefreshExpiresAt,
				"last_used_at": token.LastUsedAt, "revoked_at": token.RevokedAt, "created_at": token.CreatedAt,
			})
		}
		s.writeJSON(w, r, http.StatusOK, views)
	case http.MethodPost:
		var request aiTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
			return
		}
		name := strings.TrimSpace(request.Name)
		if name == "" {
			name = "AI client"
		}
		accessTTL, refreshTTL, err := aiTokenTTLs(request)
		if err != nil {
			s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
			return
		}
		issued, err := service.IssueAIAccessToken(time.Now().UTC(), accessTTL, refreshTTL)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		token := model.AIAccessToken{
			ID: model.NewID(), UserID: userID, Name: name,
			AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
			AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
		}
		if err := s.store.CreateAIAccessToken(r.Context(), token); err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.logger.Info("AI access token issued", "user_id", userID, "token_id", token.ID, "name", token.Name, "access_expires_at", token.AccessExpiresAt, "refresh_expires_at", token.RefreshExpiresAt)
		s.writeJSON(w, r, http.StatusCreated, map[string]any{
			"id": token.ID, "name": token.Name,
			"access_token": issued.AccessToken, "refresh_token": issued.RefreshToken,
			"access_expires_at": issued.AccessExpiresAt, "refresh_expires_at": issued.RefreshExpiresAt,
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/tokens/"), "/")
	if id == "" || strings.Contains(id, "/") {
		s.writeErrorText(w, r, http.StatusNotFound, "token not found")
		return
	}
	if err := s.store.RevokeAIAccessToken(r.Context(), userIDFromRequest(r), id, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrAIAccessTokenNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "token not found")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.logger.Info("AI access token revoked", "user_id", userIDFromRequest(r), "token_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAIRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var request aiRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.RefreshToken) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "refresh_token is required")
		return
	}
	accessTTL := aiAccessTokenDefaultTTL
	refreshTTL := aiRefreshTokenDefaultTTL
	issued, err := service.IssueAIAccessToken(time.Now().UTC(), accessTTL, refreshTTL)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	rotated, err := s.store.RotateAIAccessToken(r.Context(), service.HashAIAccessToken(request.RefreshToken), model.AIAccessToken{
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrAIAccessTokenInvalid) {
			s.writeErrorText(w, r, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.logger.Info("AI access token refreshed", "user_id", rotated.UserID, "token_id", rotated.ID)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"id": rotated.ID, "name": rotated.Name,
		"access_token": issued.AccessToken, "refresh_token": issued.RefreshToken,
		"access_expires_at": issued.AccessExpiresAt, "refresh_expires_at": issued.RefreshExpiresAt,
	})
}

func (s *Server) withAIToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" || token == auth {
			s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid AI bearer token")
			return
		}
		stored, err := s.store.AuthenticateAIAccessToken(r.Context(), service.HashAIAccessToken(token), time.Now().UTC())
		if err != nil {
			s.writeErrorText(w, r, http.StatusUnauthorized, "invalid or expired AI token")
			return
		}
		ctx := r.Context()
		ctx = contextWithString(ctx, ctxKeyUserID, stored.UserID)
		ctx = contextWithString(ctx, ctxKeyUsername, stored.User.Username)
		ctx = contextWithString(ctx, ctxKeyAITokenID, stored.ID)
		next(w, r.WithContext(ctx))
	}
}
