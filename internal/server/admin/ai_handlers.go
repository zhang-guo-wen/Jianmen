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
	aiAccessTokenDefaultTTL  = 48 * time.Hour
	aiRefreshTokenDefaultTTL = 30 * 24 * time.Hour
	aiAccessTokenMinTTL      = 5 * time.Minute
	aiAccessTokenMaxTTL      = 48 * time.Hour
	aiRefreshTokenMinTTL     = 24 * time.Hour
	aiRefreshTokenMaxTTL     = 90 * 24 * time.Hour
)

type aiTokenRequest struct {
	Name              string     `json:"name"`
	AccessTTLSeconds  int64      `json:"access_ttl_seconds"`
	RefreshTTLSeconds int64      `json:"refresh_ttl_seconds"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	Remark            string     `json:"remark,omitempty"`
	Permanent         bool       `json:"permanent,omitempty"`
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
	content := s.aiDocsContent(r)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write([]byte(content))
}

func (s *Server) aiDocsContent(r *http.Request) string {
	baseURL := s.aiBaseURL(r)
	return fmt.Sprintf(`# Jianmen AI Bastion API

Base URL: %s
Documentation URL: %s/api/ai/docs

## Purpose

Use Jianmen as the controlled gateway for AI agents that need SSH, SFTP, or database access. The AI token inherits the issuing user's current RBAC permissions and resource grants. Jianmen never returns target server passwords, private keys, or database account passwords.

## Authentication

Send the access token with every AI API request:

    Authorization: Bearer <access_token>

Access tokens are short-lived. Refresh both credentials before the access token expires:

    POST /api/ai/auth/refresh
    Content-Type: application/json

    {"refresh_token":"<refresh_token>"}

A successful refresh rotates both access_token and refresh_token. Replace the old pair immediately.

## Resource discovery

List every currently authorized account resource:

    GET /api/ai/resources

Read one resource and its bastion endpoint metadata:

    GET /api/ai/resources/{type}/{id}

Supported types:

- host_account
- database_account

Revoked, disabled, expired, or no-longer-authorized resources are omitted or rejected.

## SSH and SFTP workflow

1. Select a host_account from GET /api/ai/resources.
2. Create or reuse the user's compact bastion identity:

       POST /api/ai/resources/host_account/{id}/session

3. Issue a reusable 30-minute bastion password:

       POST /api/ai/resources/host_account/{id}/credentials

4. Connect with the returned values:

       ssh -p <bastion.ssh_port> <compact_username>@<bastion.host>
       sftp -P <bastion.ssh_port> <compact_username>@<bastion.host>

The temporary password authenticates to Jianmen. It is not the target host password.

## Database workflow

1. Select a database_account from GET /api/ai/resources.
2. Create or reuse the compact database identity:

       POST /api/ai/resources/database_account/{id}/session

3. Issue a reusable 30-minute gateway password:

       POST /api/ai/resources/database_account/{id}/credentials

4. Use compact_username, the temporary password, database_gateway_host, and database_gateway_port returned by Jianmen.

## Response format

Successful JSON responses use this envelope:

    {"code":0,"data":{...},"message":"ok","request_id":"...","timestamp":"..."}

Errors use:

    {"code":401,"error":{"code":"UNAUTHORIZED","message":"..."},"request_id":"...","timestamp":"..."}

## Security rules for AI clients

- Never write access_token, refresh_token, or temporary passwords to logs.
- Keep the refresh token separate from normal task context where possible.
- Refresh before expiry and discard the old token pair after rotation.
- Re-query resources before each task because RBAC and resource grants are evaluated dynamically.
- Stop immediately when Jianmen returns 401 or 403; do not retry with unrelated credentials.
`, baseURL, baseURL)
}

func (s *Server) handleAITokens(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "user not authenticated")
		return
	}
	switch r.Method {
	case http.MethodGet:
		tokens, err := s.aiTokens.ListAIAccessTokens(r.Context(), userID)
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		views := make([]map[string]any, 0, len(tokens))
		for _, token := range tokens {
			views = append(views, aiTokenResponse(token))
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
		now := time.Now().UTC()
		var temporaryExpiresAt *time.Time
		if !request.Permanent {
			value := now.Add(7 * 24 * time.Hour)
			if request.ExpiresAt != nil {
				value = request.ExpiresAt.UTC()
			}
			if !value.After(now) {
				s.writeErrorText(w, r, http.StatusBadRequest, "AI authorization expiry must be in the future")
				return
			}
			temporaryExpiresAt = &value
		}
		accessTTL, refreshTTL, err := aiTokenTTLs(request)
		if err != nil {
			s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if temporaryExpiresAt != nil {
			remaining := time.Until(*temporaryExpiresAt)
			if remaining < accessTTL {
				accessTTL = remaining
			}
		}
		if accessTTL < aiAccessTokenMinTTL {
			s.writeErrorText(w, r, http.StatusBadRequest, "AI authorization must last at least 5 minutes")
			return
		}
		temporaryAccess, err := s.temporaryAccessService()
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
			return
		}
		result, err := temporaryAccess.CreateAI(r.Context(), service.CreateTemporaryAIAccessInput{
			UserID: userID, Name: name, Remark: request.Remark, CreatedBy: userID,
			ExpiresAt: temporaryExpiresAt, Now: now, AccessTTL: accessTTL, RefreshTTL: refreshTTL,
		})
		if err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		token := result.Token
		s.logger.Info("AI access token issued", "user_id", userID, "token_id", token.ID, "name", token.Name, "access_expires_at", token.AccessExpiresAt, "refresh_expires_at", token.RefreshExpiresAt)
		docsURL := s.aiBaseURL(r) + "/api/ai/docs"
		docsContent := s.aiDocsContent(r)
		prompt := "\u6388\u6743 AI \u4f7f\u7528\u5f53\u524d\u7528\u6237\u7684\u8d44\u6e90\u7684\u6743\u9650\u3002"
		copyPrompt := fmt.Sprintf("\u4f60\u53ef\u4ee5\u4f7f\u7528\u6211\u7684\u6743\u9650\u8bbf\u95ee\u6211\u7684\u670d\u52a1\u5668\u3001\u6570\u636e\u5e93\u7b49\u8d44\u6e90\uff0c\n\u8bbf\u95ee\u4ee4\u724c\uff1a<access_token>\n\u5237\u65b0\u4ee4\u724c\uff1a<refresh_token>\n\u5177\u4f53\u89c1\u6587\u6863\uff1a[%s](%s)", docsURL, docsURL)
		fullPrompt := copyPrompt + "\n\n\u5b8c\u6574\u63d0\u793a\u8bcd\uff1a\n" + docsContent
		s.writeJSON(w, r, http.StatusCreated, map[string]any{
			"id": token.ID, "name": token.Name, "temporary_account_id": token.TemporaryAccountID,
			"access_token": result.AccessToken, "refresh_token": result.RefreshToken,
			"access_expires_at": token.AccessExpiresAt, "refresh_expires_at": token.RefreshExpiresAt,
			"temporary_expires_at": temporaryExpiresAt, "prompt": prompt, "copy_prompt": copyPrompt, "full_prompt": fullPrompt,
			"docs_url": docsURL, "docs_content": docsContent,
		})
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAIToken(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/tokens/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "reissue" {
		s.handleAIReissue(w, r, parts[0])
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "token not found")
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodGet:
		token, err := s.aiTokens.AIAccessToken(r.Context(), userIDFromRequest(r), id)
		if err != nil {
			if errors.Is(err, store.ErrAIAccessTokenNotFound) {
				s.writeErrorText(w, r, http.StatusNotFound, "token not found")
				return
			}
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, r, http.StatusOK, aiTokenResponse(token))
	case http.MethodDelete:
		if err := s.aiTokens.RevokeAIAccessToken(r.Context(), userIDFromRequest(r), id, time.Now().UTC()); err != nil {
			if errors.Is(err, store.ErrAIAccessTokenNotFound) {
				s.writeErrorText(w, r, http.StatusNotFound, "token not found")
				return
			}
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		s.logger.Info("AI access token revoked", "user_id", userIDFromRequest(r), "token_id", id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAIReissue(w http.ResponseWriter, r *http.Request, tokenID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now().UTC()
	tokenService, err := s.aiAccessTokenService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "AI access token service is unavailable")
		return
	}
	result, err := tokenService.Reissue(
		r.Context(),
		userIDFromRequest(r),
		tokenID,
		now,
		aiAccessTokenDefaultTTL,
		aiRefreshTokenDefaultTTL,
	)
	if err != nil {
		if errors.Is(err, store.ErrAIAccessTokenNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "token not found")
			return
		}
		if errors.Is(err, store.ErrAIAccessTokenInvalid) {
			s.writeErrorText(w, r, http.StatusConflict, "token cannot be reissued")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	rotated := result.Token
	s.logger.Info("AI access token reissued", "user_id", rotated.UserID, "token_id", rotated.ID)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"id": rotated.ID, "name": rotated.Name,
		"access_token": result.AccessToken, "refresh_token": result.RefreshToken,
		"access_expires_at": rotated.AccessExpiresAt, "refresh_expires_at": rotated.RefreshExpiresAt,
	})
}

func (s *Server) aiAccessTokenService() (*service.AIAccessTokenService, error) {
	return service.NewAIAccessTokenService(s.aiTokens)
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
	refreshFingerprint := service.HashAIAccessToken(request.RefreshToken)
	intentID, err := s.recordAIRefreshIntent(r, refreshFingerprint)
	if err != nil {
		s.logger.Error("AI refresh audit gate failed", "error", err)
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "operation audit unavailable")
		return
	}
	accessTTL := aiAccessTokenDefaultTTL
	refreshTTL := aiRefreshTokenDefaultTTL
	issued, err := service.IssueAIAccessToken(time.Now().UTC(), accessTTL, refreshTTL)
	if err != nil {
		if auditErr := s.recordAIRefreshResult(r, intentID, refreshFingerprint, nil, http.StatusInternalServerError, "token_issue_failed"); auditErr != nil {
			s.logger.Warn("failed to write AI refresh failure audit", "intent_id", intentID, "error", auditErr)
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	rotated, err := s.aiTokens.RotateAIAccessToken(r.Context(), refreshFingerprint, model.AIAccessToken{
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}, time.Now().UTC())
	if err != nil {
		status := http.StatusInternalServerError
		reason := "token_rotate_failed"
		if errors.Is(err, store.ErrAIAccessTokenInvalid) {
			status = http.StatusUnauthorized
			reason = "invalid_or_expired"
		}
		if auditErr := s.recordAIRefreshResult(r, intentID, refreshFingerprint, nil, status, reason); auditErr != nil {
			s.logger.Warn("failed to write AI refresh failure audit", "intent_id", intentID, "error", auditErr)
		}
		if status == http.StatusUnauthorized {
			s.writeErrorText(w, r, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.recordAIRefreshResult(r, intentID, refreshFingerprint, &rotated, http.StatusOK, ""); err != nil {
		s.logger.Error("AI refresh result audit failed", "user_id", rotated.UserID, "token_id", rotated.ID, "intent_id", intentID, "error", err)
		if auditErr := s.recordAIRefreshResult(r, intentID, refreshFingerprint, &rotated, http.StatusServiceUnavailable, "success_audit_failed"); auditErr != nil {
			s.logger.Warn("failed to write AI refresh delivery failure audit", "intent_id", intentID, "error", auditErr)
		}
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "operation audit unavailable")
		return
	}
	s.logger.Info("AI access token refreshed", "user_id", rotated.UserID, "token_id", rotated.ID)
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"id": rotated.ID, "name": rotated.Name,
		"access_token": issued.AccessToken, "refresh_token": issued.RefreshToken,
		"access_expires_at": issued.AccessExpiresAt, "refresh_expires_at": issued.RefreshExpiresAt,
	})
}

func aiTokenResponse(token model.AIAccessToken) map[string]any {
	response := map[string]any{
		"id": token.ID, "name": token.Name,
		"access_expires_at": token.AccessExpiresAt, "refresh_expires_at": token.RefreshExpiresAt,
		"last_used_at": token.LastUsedAt, "revoked_at": token.RevokedAt, "created_at": token.CreatedAt,
		"has_secret": false,
	}
	return response
}

func (s *Server) withAIToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" || token == auth {
			s.writeErrorText(w, r, http.StatusUnauthorized, "missing or invalid AI bearer token")
			return
		}
		now := time.Now().UTC()
		stored, err := s.aiTokens.AuthenticateAIAccessToken(r.Context(), service.HashAIAccessToken(token), now)
		if err != nil {
			s.writeErrorText(w, r, http.StatusUnauthorized, "invalid or expired AI token")
			return
		}
		if stored.TemporaryAccountID != "" && (stored.TemporaryAccount.Status != "active" || (stored.TemporaryAccount.ExpiresAt != nil && !stored.TemporaryAccount.ExpiresAt.After(now))) {
			s.writeErrorText(w, r, http.StatusUnauthorized, "AI authorization expired or disabled")
			return
		}
		ctx := r.Context()
		ctx = contextWithString(ctx, ctxKeyUserID, stored.UserID)
		ctx = contextWithString(ctx, ctxKeyUsername, stored.User.Username)
		ctx = contextWithString(ctx, ctxKeyAITokenID, stored.ID)
		next(w, r.WithContext(ctx))
	}
}
