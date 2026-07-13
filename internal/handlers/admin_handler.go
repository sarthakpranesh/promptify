package handlers

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"promptify/internal/store"
	"promptify/internal/validate"
)

type AdminHandler struct {
	Store         store.Store
	SessionSecret []byte
	LogPath       string
}

func NewAdminHandler(s store.Store, sessionSecret []byte, logPath string) *AdminHandler {
	return &AdminHandler{
		Store:         s,
		SessionSecret: sessionSecret,
		LogPath:       logPath,
	}
}

func (h *AdminHandler) ServerPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	ph := &PromptHandler{Store: h.Store}
	currentUser, err := ph.loadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	settings, err := h.Store.GetAdminSettings(r.Context())
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}

	const usersListLimit = 10
	users, err := h.Store.ListUsers(r.Context(), "", usersListLimit)
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	adminUID, _ := h.Store.GetAdminUID(r.Context())

	type userRow struct {
		UID         string
		Email       string
		Name        string
		CreatedAt   string
		LastLoginAt string
		IsAdmin     bool
	}
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		last := "—"
		if !u.LastLoginAt.IsZero() {
			last = u.LastLoginAt.Format("2006-01-02 15:04")
		}
		created := "—"
		if !u.CreatedAt.IsZero() {
			created = u.CreatedAt.Format("2006-01-02")
		}
		rows = append(rows, userRow{
			UID:         u.UID,
			Email:       u.Email,
			Name:        u.Name,
			CreatedAt:   created,
			LastLoginAt: last,
			IsAdmin:     u.UID == adminUID,
		})
	}

	data := struct {
		CurrentUser       CurrentUser
		AllowRegistration bool
		Users             []userRow
		LogFileName       string
	}{
		CurrentUser:       currentUser,
		AllowRegistration: settings.AllowRegistration,
		Users:             rows,
		LogFileName:       filepath.Base(h.LogPath),
	}

	tmpl := template.Must(template.ParseFiles("web/templates/layout.html", "web/templates/server.html"))
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *AdminHandler) GetRegistrationSetting(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Store.GetAdminSettings(r.Context())
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"allow_registration": settings.AllowRegistration,
	})
}

func (h *AdminHandler) SetRegistrationSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AllowRegistration bool `json:"allow_registration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	settings, err := h.Store.GetAdminSettings(r.Context())
	if err != nil {
		http.Error(w, "Failed to load settings", http.StatusInternalServerError)
		return
	}
	settings.AllowRegistration = body.AllowRegistration
	if err := h.Store.UpdateAdminSettings(r.Context(), settings); err != nil {
		http.Error(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"allow_registration": settings.AllowRegistration,
	})
}

func (h *AdminHandler) ListUsersAPI(w http.ResponseWriter, r *http.Request) {
	const usersListLimit = 10
	emailQ := strings.TrimSpace(r.URL.Query().Get("email"))
	users, err := h.Store.ListUsers(r.Context(), emailQ, usersListLimit)
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}
	adminUID, _ := h.Store.GetAdminUID(r.Context())
	type row struct {
		UID         string `json:"uid"`
		Email       string `json:"email"`
		Name        string `json:"name"`
		CreatedAt   string `json:"created_at"`
		LastLoginAt string `json:"last_login_at"`
		IsAdmin     bool   `json:"is_admin"`
	}
	out := make([]row, 0, len(users))
	for _, u := range users {
		last := "—"
		if !u.LastLoginAt.IsZero() {
			last = u.LastLoginAt.Format("2006-01-02 15:04")
		}
		created := "—"
		if !u.CreatedAt.IsZero() {
			created = u.CreatedAt.Format("2006-01-02")
		}
		out = append(out, row{
			UID:         u.UID,
			Email:       u.Email,
			Name:        u.Name,
			CreatedAt:   created,
			LastLoginAt: last,
			IsAdmin:     u.UID == adminUID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"users": out})
}

func (h *AdminHandler) CreateUserAPI(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(body.Email)
	name := strings.TrimSpace(body.Name)
	password := body.Password
	if email == "" || name == "" || password == "" {
		http.Error(w, "email, name, and password are required", http.StatusBadRequest)
		return
	}
	normalized, err := validate.Email(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	email = normalized
	if len(password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if _, err := h.Store.GetUserByEmail(r.Context(), email); err == nil {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	uid, err := newUserUID()
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	if err := h.Store.CreateUser(r.Context(), store.UserRecord{
		UID:          uid,
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
	}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"uid": uid, "email": email, "name": name})
}

func (h *AdminHandler) DeleteUserAPI(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	if uid == "" {
		http.Error(w, "uid is required", http.StatusBadRequest)
		return
	}
	adminUID, err := h.Store.GetAdminUID(r.Context())
	if err == nil && uid == adminUID {
		http.Error(w, "Cannot delete admin user", http.StatusForbidden)
		return
	}
	if err := h.Store.DeleteUserAndData(r.Context(), uid); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			http.Error(w, "Cannot delete admin user", http.StatusForbidden)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) DownloadLogs(w http.ResponseWriter, r *http.Request) {
	path := h.LogPath
	if path == "" {
		http.Error(w, "Log file not configured", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "Log file not available", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "Log file not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="promptify.log"`)
	http.ServeContent(w, r, "promptify.log", stat.ModTime(), f)
}
