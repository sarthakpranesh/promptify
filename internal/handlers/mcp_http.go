package handlers

import (
	"context"
	"net/http"
	"strings"

	"promptify/internal/store"
)

// BearerTokenMiddleware returns an http.Handler that requires Authorization: Bearer <token>
// when token is non-empty. Use to protect the remote MCP HTTP endpoint.
func BearerTokenMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Bearer realm="promptify-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// APIKeyBearerMiddleware protects MCP with per-user API keys.
// It expects Authorization: Bearer <api-key> where api-key encodes uid.
func APIKeyBearerMiddleware(s store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="promptify-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		key := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		userID, ok := validateAPIKey(s, key)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="promptify-mcp"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
