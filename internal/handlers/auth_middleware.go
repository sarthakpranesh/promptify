package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"

	"promptify/internal/store"
)

type ctxKey string

const userIDKey ctxKey = "promptify_user_id"

const sessionCookieName = "promptify_session"

func UserIDFromContext(ctx context.Context) (string, bool) {
	v := ctx.Value(userIDKey)
	if v == nil {
		return "", false
	}
	userID, ok := v.(string)
	return userID, ok && userID != ""
}

func CreateSignedSessionValue(userID string, secret []byte) (string, error) {
	// Format: base64(userID) + "." + base64(hmacSha256(secret, userID))
	uidEnc := base64.RawURLEncoding.EncodeToString([]byte(userID))
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(userID))
	sigEnc := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return uidEnc + "." + sigEnc, nil
}

func ParseSignedSessionValue(value string, secret []byte) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}

	uidBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(uidBytes) == 0 {
		return "", false
	}
	userID := string(uidBytes)

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(userID))
	expectedSigEnc := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	// Avoid timing leaks by comparing in constant time.
	expected := expectedSigEnc
	got := parts[1]
	if !hmac.Equal([]byte(expected), []byte(got)) {
		return "", false
	}

	return userID, true
}

func RequireAuthHTML(sessionSecret []byte, next http.HandlerFunc) http.HandlerFunc {
	if len(sessionSecret) == 0 {
		// Fail closed: always require login if session secret isn't configured.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
		})
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			redirectToLogin(w, r)
			return
		}

		userID, ok := ParseSignedSessionValue(cookie.Value, sessionSecret)
		if !ok {
			redirectToLogin(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

func RequireAuthAPI(sessionSecret []byte, next http.HandlerFunc) http.HandlerFunc {
	if len(sessionSecret) == 0 {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userID, ok := ParseSignedSessionValue(cookie.Value, sessionSecret)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

func RequireAuthAPIWithKey(sessionSecret []byte, s store.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// First, allow session cookie auth (browser).
		if len(sessionSecret) > 0 {
			if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
				if userID, ok := ParseSignedSessionValue(cookie.Value, sessionSecret); ok {
					ctx := context.WithValue(r.Context(), userIDKey, userID)
					next(w, r.WithContext(ctx))
					return
				}
			}
		}

		// Fallback: API key auth via Authorization: Bearer <api-key>
		authz := r.Header.Get("Authorization")
		if strings.HasPrefix(authz, "Bearer ") {
			key := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
			if key != "" {
				if userID, ok := validateAPIKey(s, key); ok {
					ctx := context.WithValue(r.Context(), userIDKey, userID)
					next(w, r.WithContext(ctx))
					return
				}
			}
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
}

func RequireAdminHTML(sessionSecret []byte, s store.Store, next http.HandlerFunc) http.HandlerFunc {
	return RequireAuthHTML(sessionSecret, func(w http.ResponseWriter, r *http.Request) {
		if !isAdminRequest(r, s) {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		next(w, r)
	})
}

func RequireAdminAPI(sessionSecret []byte, s store.Store, next http.HandlerFunc) http.HandlerFunc {
	return RequireAuthAPI(sessionSecret, func(w http.ResponseWriter, r *http.Request) {
		if !isAdminRequest(r, s) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func isAdminRequest(r *http.Request, s store.Store) bool {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		return false
	}
	adminUID, err := s.GetAdminUID(r.Context())
	if err != nil {
		return false
	}
	return adminUID == userID
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

