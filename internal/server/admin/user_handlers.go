package admin

import (
	"encoding/json"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"net/http"
	"strings"
)

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listUsers(w, r)
	case http.MethodPost:
		s.createUser(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w)
		return
	}
	id, ok := userIDFromPath(r.URL.Path)
	if !ok {
		writeErrorText(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUser(w, id)
	case http.MethodPut:
		s.updateUser(w, r, id)
	case http.MethodDelete:
		s.deleteUser(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
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
		pageSize := positiveIntRequestQuery(r, "page_size", 20)
		if pageSize > 200 {
			pageSize = 200
		}
		var users []model.User
		if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error; err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		// 为每个用户附加 is_super_admin 标记
		type userWithFlag struct {
			model.User
			IsSuperAdmin bool `json:"is_super_admin"`
		}
		out := make([]userWithFlag, len(users))
		for i, u := range users {
			out[i] = userWithFlag{User: u, IsSuperAdmin: s.isSuperAdmin(u.ID)}
		}
		writeJSON(w, http.StatusOK, pageResponse{Items: out, Total: int(total), Page: page, PageSize: pageSize})
		return
	}
	// Fallback to store-based listing
	writeJSON(w, http.StatusOK, s.store.Users())
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	password := strings.TrimSpace(req.Password)
	if username == "" {
		writeErrorText(w, http.StatusBadRequest, "username is required")
		return
	}
	if password == "" {
		writeErrorText(w, http.StatusBadRequest, "password is required")
		return
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rawToken, tokenHash, err := newAPIToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	user := model.User{
		ID:           model.NewID(),
		Username:     username,
		PasswordHash: passwordHash,
		TokenHash:    tokenHash,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Email:        strings.TrimSpace(req.Email),
		Status:       "active",
	}
	if err := s.db.Create(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":  user,
		"token": rawToken,
	})
}

func (s *Server) getUser(w http.ResponseWriter, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		user.Email = strings.TrimSpace(*req.Email)
	}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "active" && status != "disabled" {
			writeErrorText(w, http.StatusBadRequest, "status must be active or disabled")
			return
		}
		// 不允许禁用超级管理员
		if status == "disabled" && s.isSuperAdmin(id) {
			writeErrorText(w, http.StatusForbidden, "cannot disable super admin")
			return
		}
		user.Status = status
	}
	if err := s.db.Save(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	if s.db == nil {
		writeErrorText(w, http.StatusServiceUnavailable, "metadata database unavailable")
		return
	}
	currentUserID := userIDFromRequest(r)
	if currentUserID != "" && currentUserID == id {
		writeErrorText(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	// 不允许删除超级管理员
	if s.isSuperAdmin(id) {
		writeErrorText(w, http.StatusForbidden, "cannot delete super admin")
		return
	}
	var user model.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	// Cascade delete user_roles
	if err := s.db.Where("user_id = ?", id).Delete(&model.UserRole{}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.db.Delete(&user).Error; err != nil {
		writeRBACDBError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
