package validate

import (
	"fmt"
	"net/mail"
	"strings"
)

// Email normalizes and validates a bare email address (no display name).
// Returns the address lowercased.
func Email(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("email is required")
	}
	if strings.ContainsAny(raw, "<> \t") {
		return "", fmt.Errorf("invalid email address")
	}

	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", fmt.Errorf("invalid email address")
	}
	if !strings.EqualFold(addr.Address, raw) {
		return "", fmt.Errorf("invalid email address")
	}

	email := strings.ToLower(addr.Address)
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || !strings.Contains(domain, ".") {
		return "", fmt.Errorf("invalid email address")
	}
	return email, nil
}
