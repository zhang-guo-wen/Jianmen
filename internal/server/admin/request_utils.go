package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/store"
)

const defaultPageSize = 50
const defaultHostPageSize = 20

func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
	s.writeErrorText(w, r, http.StatusForbidden, "forbidden")
}

func splitArtifactPath(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func targetIDFromPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/api/targets/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func userIDFromPath(path string) (string, bool) {
	const prefix = "/api/users/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func paginateHosts(hosts []store.HostView, r *http.Request) pageResponse {
	hosts = filterByGroup(hosts, r, func(host store.HostView) string {
		return host.Group
	})
	return paginateSliceWithDefault(hosts, r, defaultHostPageSize, func(h store.HostView, q string) bool {
		return strings.Contains(strings.ToLower(h.Name), q) ||
			strings.Contains(strings.ToLower(h.Address), q) ||
			strings.Contains(strings.ToLower(h.Group), q) ||
			strings.Contains(strings.ToLower(h.Remark), q) ||
			strings.Contains(strings.ToLower(strconv.Itoa(h.Port)), q)
	})
}

func filterByGroup[T any](items []T, r *http.Request, groupName func(T) string) []T {
	group := strings.TrimSpace(r.URL.Query().Get("group"))
	ungrouped, _ := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("ungrouped")))
	if group == "" && !ungrouped {
		return items
	}
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		itemGroup := strings.TrimSpace(groupName(item))
		if ungrouped && itemGroup == "" {
			filtered = append(filtered, item)
			continue
		}
		if !ungrouped && strings.EqualFold(itemGroup, group) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func paginateSlice[T any](items []T, r *http.Request, match func(T, string) bool) pageResponse {
	return paginateSliceWithDefault(items, r, defaultPageSize, match)
}

func paginateSliceWithDefault[T any](items []T, r *http.Request, fallbackPageSize int, match func(T, string) bool) pageResponse {
	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	if q != "" {
		filtered := make([]T, 0, len(items))
		for _, item := range items {
			if match(item, q) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", fallbackPageSize)
	if pageSize > 200 {
		pageSize = 200
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return pageResponse{
		Items:    items[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

func positiveIntRequestQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func hostPathParts(path string) (string, string, bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/hosts/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "accounts", "refresh-identity":
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func dbInstancePathParts(path string) (id, child string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/db/instances/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "accounts", "databases", "provision-account":
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func dbAccountIDFromPath(path string) (string, bool) {
	id := strings.TrimPrefix(path, "/api/db/accounts/")
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func writeHostStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrHostNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
	}
}

func writeDBStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrDBProxyNotFound) || errors.Is(err, store.ErrDBAccountNotFound) || errors.Is(err, store.ErrDBInstanceNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, "database resource not found", nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, store.ErrReferencedDatabaseAdministrator), errors.Is(err, store.ErrReferencedDatabaseInstance):
		apiresp.WriteError(w, http.StatusConflict, apiresp.CodeConflict, "database resource is referenced by provisioning operation", nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, "invalid database request", nil, apiresp.RequestID(r.Context()))
	}
}

func writeTargetStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrTargetNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
	}
}

func appPathParts(path string) (id, child string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/applications/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	return "", "", false
}

func writeApplicationStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrApplicationNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
	}
}
