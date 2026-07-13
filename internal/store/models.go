package store

import "time"

// DefaultAdminSettingsJSON is seeded on first admin row.
const DefaultAdminSettingsJSON = `{"allow_registration":false}`

// AdminSettings is server config stored on the admin singleton row.
type AdminSettings struct {
	AllowRegistration bool `json:"allow_registration"`
}

// UserRecord is persisted auth profile data.
type UserRecord struct {
	UID          string
	Email        string
	Name         string
	PasswordHash string
	APIKeyHash   string
	CreatedAt    time.Time
	LastLoginAt  time.Time
}

// UserListItem is a compact row for the admin users table.
type UserListItem struct {
	UID         string
	Email       string
	Name        string
	CreatedAt   time.Time
	LastLoginAt time.Time
}

// PromptSummary is a prompt row with its active version (if any).
type PromptSummary struct {
	ID          string
	Name        string
	Description string
	Version     int
	Template    string
}

// VersionRecord is one prompt version.
type VersionRecord struct {
	Version   int
	Template  string
	IsActive  bool
	CreatedAt time.Time
}
