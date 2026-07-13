package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"promptify/internal/store"
)

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateAPIKeyForUID(uid string) (string, error) {
	uidEnc := base64.RawURLEncoding.EncodeToString([]byte(uid))

	secretBytes := make([]byte, 24)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", err
	}
	secretEnc := base64.RawURLEncoding.EncodeToString(secretBytes)

	// Includes uid (encoded) directly in the key payload.
	return "pfy_" + uidEnc + "_" + secretEnc, nil
}

func parseUIDFromAPIKey(apiKey string) (string, bool) {
	// Secret is base64url and may contain '_' — only split uid from secret once.
	parts := strings.SplitN(apiKey, "_", 3)
	if len(parts) != 3 || parts[0] != "pfy" {
		return "", false
	}

	uidBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(uidBytes) == 0 {
		return "", false
	}

	return string(uidBytes), true
}

func validateAPIKey(s store.Store, apiKey string) (string, bool) {
	uid, ok := parseUIDFromAPIKey(apiKey)
	if !ok {
		return "", false
	}

	storedHash, found, err := s.GetUserAPIKeyHash(context.Background(), uid)
	if err != nil || !found || storedHash == "" {
		return "", false
	}

	if hashAPIKey(apiKey) != storedHash {
		return "", false
	}

	return uid, true
}

func (h *AuthHandler) GenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	apiKey, err := generateAPIKeyForUID(userID)
	if err != nil {
		http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}

	if err := h.Store.SetUserAPIKeyHash(r.Context(), userID, hashAPIKey(apiKey)); err != nil {
		http.Error(w, "Failed to store API key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"api_key": apiKey,
	})
}

