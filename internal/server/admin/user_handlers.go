package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/util"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createUser(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	id, ok := userIDFromPath(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUser(w, r, id)
	case http.MethodPut:
		s.updateUser(w, r, id)
	case http.MethodDelete:
		s.deleteUser(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if s.db != nil {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		tx := s.db.Model(&model.User{})
		if q != "" {
			like := "%" + q + "%"
			tx = tx.Where("username LIKE ? OR display_name LIKE ? OR email LIKE ?", like, like, like)
		}
		var total int64
		tx.Count(&total)
		page := positiveIntRequestQuery(r, "page", 1)
		pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
		if pageSize > 200 {
			pageSize = 200
		}
		var users []model.User
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		// 为每个用户附加 is_super_admin 标记
		type userWithFlag struct {
			model.User
			IsSuperAdmin bool `json:"is_super_admin"`
		}
		out := make([]userWithFlag, len(users))
		for i, u := range users {
			if !s.isSuperAdmin(u.ID) && u.IsExpired(time.Now().UTC()) && u.Status == "active" {
				u.Status = "disabled"
				_ = s.db.Model(&model.User{}).Where("id = ?", u.ID).Update("status", "disabled").Error
			}
			out[i] = userWithFlag{User: u, IsSuperAdmin: s.isSuperAdmin(u.ID)}
		}
		s.writeJSON(w, r, http.StatusOK, pageResponse{Items: out, Total: int(total), Page: page, PageSize: pageSize})
		return
	}
	// Fallback to store-based listing
	s.writeJSON(w, r, http.StatusOK, s.store.Users())
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username is required")
		return
	}
	if password == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "password is required")
		return
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	rawToken, tokenHash, err := newAPIToken()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var expiresAt *time.Time
	if !req.Permanent {
		value := time.Now().UTC().AddDate(1, 0, 0)
		if req.ExpiresAt != nil {
			value = req.ExpiresAt.UTC()
		}
		if !value.After(time.Now().UTC()) {
			s.writeErrorText(w, r, http.StatusBadRequest, "expires_at must be in the future")
			return
		}
		expiresAt = &value
	}

	user := model.User{
		ID:              model.NewID(),
		Username:        username,
		PasswordHash:    passwordHash,
		MySQLNativeHash: util.MySQLNativePasswordHash(password),
		TokenHash:       tokenHash,
		DisplayName:     strings.TrimSpace(req.DisplayName),
		Email:           strings.TrimSpace(req.Email),
		Status:          "active",
		ExpiresAt:       expiresAt,
	}
	if err := s.db.Create(&user).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"user":  user,
		"token": rawToken,
	})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, user)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		user.Email = strings.TrimSpace(*req.Email)
	}
	if req.Permanent != nil {
		if *req.Permanent {
			user.ExpiresAt = nil
		} else if req.ExpiresAt != nil {
			value := req.ExpiresAt.UTC()
			user.ExpiresAt = &value
		} else {
			value := time.Now().UTC().AddDate(1, 0, 0)
			user.ExpiresAt = &value
		}
	} else if req.ExpiresAt != nil {
		value := req.ExpiresAt.UTC()
		user.ExpiresAt = &value
	}
	if user.ExpiresAt != nil && !user.ExpiresAt.After(time.Now().UTC()) && (req.Status == nil || strings.TrimSpace(*req.Status) == "active") {
		s.writeErrorText(w, r, http.StatusBadRequest, "expires_at must be in the future when user is active")
		return
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "active" && status != "disabled" {
			s.writeErrorText(w, r, http.StatusBadRequest, "status must be active or disabled")
			return
		}
		// 不允许禁用超级管理员
		if status == "disabled" && s.isSuperAdmin(id) {
			s.writeErrorText(w, r, http.StatusForbidden, "cannot disable super admin")
			return
		}
		if status == "active" && !s.isSuperAdmin(id) && (user.ExpiresAt == nil || user.IsExpired(time.Now().UTC())) {
			value := time.Now().UTC().AddDate(1, 0, 0)
			user.ExpiresAt = &value
		}
		user.Status = status
	}
	if err := s.db.Save(&user).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	currentUserID := userIDFromRequest(r)
	if currentUserID != "" && currentUserID == id {
		s.writeErrorText(w, r, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	// 不允许删除超级管理员
	if s.isSuperAdmin(id) {
		s.writeErrorText(w, r, http.StatusForbidden, "cannot delete super admin")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	// Cascade delete user_roles
	if err := s.db.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.Delete(&user).Error; err != nil {
		writeRBACDBError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
