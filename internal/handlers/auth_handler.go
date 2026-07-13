package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"promptify/internal/store"
	"promptify/internal/validate"
)

type AuthHandler struct {
	Store         store.Store
	SessionSecret []byte
}

func NewAuthHandler(s store.Store, sessionSecret []byte) *AuthHandler {
	return &AuthHandler{
		Store:         s,
		SessionSecret: sessionSecret,
	}
}

func (h *AuthHandler) HomePage(w http.ResponseWriter, r *http.Request) {
	allowReg := false
	if settings, err := h.Store.GetAdminSettings(r.Context()); err == nil {
		allowReg = settings.AllowRegistration
	}
	tmpl := template.Must(template.ParseFiles("web/templates/home.html"))
	_ = tmpl.Execute(w, map[string]interface{}{
		"AllowRegistration": allowReg,
	})
}

func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Store.GetAdminSettings(r.Context())
	if err != nil || !settings.AllowRegistration {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	tmpl := template.Must(template.ParseFiles("web/templates/register.html"))
	_ = tmpl.Execute(w, nil)
}

func (h *AuthHandler) SessionStatus(w http.ResponseWriter, r *http.Request) {
	redirectTo := sanitizeRedirect(r.URL.Query().Get("redirect"))
	authenticated := false

	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if userID, ok := ParseSignedSessionValue(cookie.Value, h.SessionSecret); ok {
			exists, err := h.Store.UserExists(r.Context(), userID)
			authenticated = err == nil && exists
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": authenticated,
		"redirect_to":   redirectTo,
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(body.Email)
	password := body.Password
	if email == "" || password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}
	normalized, err := validate.Email(email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	email = normalized

	ctx := r.Context()
	user, err := h.Store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	redirectTo := sanitizeRedirect(r.URL.Query().Get("redirect"))
	now := time.Now()
	if err := h.Store.TouchUserLogin(ctx, user.UID, now); err != nil {
		http.Error(w, "Failed to update session", http.StatusInternalServerError)
		return
	}

	signed, err := CreateSignedSessionValue(user.UID, h.SessionSecret)
	if err != nil {
		http.Error(w, "Server misconfiguration", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, r, signed)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"redirect_to": redirectTo,
	})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	settings, err := h.Store.GetAdminSettings(ctx)
	if err != nil || !settings.AllowRegistration {
		http.Error(w, "Registration is disabled", http.StatusForbidden)
		return
	}

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

	if _, err := h.Store.GetUserByEmail(ctx, email); err == nil {
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

	if err := h.Store.CreateUser(ctx, store.UserRecord{
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

	signed, err := CreateSignedSessionValue(uid, h.SessionSecret)
	if err != nil {
		http.Error(w, "Server misconfiguration", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, r, signed)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"redirect_to": "/dashboard",
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if adminUID, err := h.Store.GetAdminUID(r.Context()); err == nil && adminUID == userID {
		http.Error(w, "Admin account cannot be deleted", http.StatusForbidden)
		return
	}

	var body struct {
		ConfirmName string `json:"confirm_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	confirmName := normalizeAccountConfirmationName(body.ConfirmName)
	if confirmName == "" {
		http.Error(w, "confirm_name is required", http.StatusBadRequest)
		return
	}

	name, _, err := h.Store.GetUserProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	expectedName := normalizeAccountConfirmationName(name)
	if expectedName == "" {
		http.Error(w, "Full name is missing on your account", http.StatusBadRequest)
		return
	}
	if confirmName != expectedName {
		http.Error(w, "Confirmation name does not match", http.StatusBadRequest)
		return
	}

	if err := h.Store.DeleteUserAndData(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrForbidden) {
			http.Error(w, "Admin account cannot be deleted", http.StatusForbidden)
			return
		}
		http.Error(w, "Failed to delete account", http.StatusInternalServerError)
		return
	}

	clearSessionCookie(w, r)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"redirect_to": "/",
	})
}

func newUserUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func normalizeAccountConfirmationName(name string) string {
	return strings.TrimSpace(strings.ReplaceAll(name, `"`, ""))
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((24 * 7 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func sanitizeRedirect(raw string) string {
	if raw == "" {
		return "/dashboard"
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/dashboard"
	}
	if strings.HasPrefix(raw, "/") && !strings.Contains(raw, "://") {
		return raw
	}
	return "/dashboard"
}

func (h *AuthHandler) mustSessionConfigured() error {
	if len(h.SessionSecret) == 0 {
		return fmt.Errorf("PROMPTIFY_SESSION_SECRET is not configured")
	}
	return nil
}
