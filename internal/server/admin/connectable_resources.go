package admin

import (
	"fmt"
	"net/http"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func connectableOnly(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("connectable")), "true")
}

func (s *Server) connectableTargets(r *http.Request, targets []store.TargetView) ([]store.TargetView, error) {
	if !connectableOnly(r) || s.isSuperAdmin(userIDFromRequest(r)) {
		return targets, nil
	}
	userID := userIDFromRequest(r)
	result := make([]store.TargetView, 0, len(targets))
	for _, target := range targets {
		allowed, err := s.authorizeAnyConnection(r.Context(), userID, []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}, model.ResourceTypeHostAccount, target.ID)
		if err != nil {
			return nil, fmt.Errorf("authorize host account %q: %w", target.ID, err)
		}
		if allowed {
			result = append(result, target)
		}
	}
	return result, nil
}

func (s *Server) connectableDatabaseAccounts(r *http.Request, accounts []store.DatabaseAccountView) ([]store.DatabaseAccountView, error) {
	if !connectableOnly(r) || s.isSuperAdmin(userIDFromRequest(r)) {
		return accounts, nil
	}
	userID := userIDFromRequest(r)
	result := make([]store.DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		allowed, err := s.authorizeConnection(r.Context(), userID, rbac.ActionDBConnect, model.ResourceTypeDatabaseAccount, account.ID)
		if err != nil {
			return nil, fmt.Errorf("authorize database account %q: %w", account.ID, err)
		}
		if allowed {
			result = append(result, account)
		}
	}
	return result, nil
}
