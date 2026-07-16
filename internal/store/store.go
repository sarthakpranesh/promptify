package store

import (
	"context"
	"time"
)

// Store abstracts prompt and user persistence. Implementations: SQLite, MongoDB.
type Store interface {
	Close() error

	// Admin singleton: uid pointer + settings JSON (no password).
	GetAdminUID(ctx context.Context) (uid string, err error)
	InsertAdmin(ctx context.Context, uid string, settingsJSON string) error
	GetAdminSettings(ctx context.Context) (AdminSettings, error)
	UpdateAdminSettings(ctx context.Context, settings AdminSettings) error

	// Users: registered, login history, API keys.
	CountUsers(ctx context.Context) (int, error)
	UserExists(ctx context.Context, uid string) (bool, error)
	CreateUser(ctx context.Context, user UserRecord) error
	GetUserByUID(ctx context.Context, uid string) (UserRecord, error)
	GetUserByEmail(ctx context.Context, email string) (UserRecord, error)
	UpdateUserCredentials(ctx context.Context, uid, email, name, passwordHash string) error
	TouchUserLogin(ctx context.Context, uid string, lastLogin time.Time) error
	ListUsers(ctx context.Context, emailQuery string, limit int) ([]UserListItem, error)
	GetUserProfile(ctx context.Context, uid string) (name, email string, err error)
	SetUserAPIKeyHash(ctx context.Context, uid, hash string) error
	GetUserAPIKeyHash(ctx context.Context, uid string) (hash string, ok bool, err error)
	DeleteUserAndData(ctx context.Context, uid string) error

	// Prompts: user-owned, versioned, templates.
	ListPromptsForUser(ctx context.Context, uid string) ([]PromptSummary, error)
	GetPromptMeta(ctx context.Context, promptID, uid string) (name, description string, err error)
	ListPromptVersions(ctx context.Context, promptID string) ([]VersionRecord, error)
	CreatePrompt(ctx context.Context, uid, name, description, template string) (string, error)
	UpdatePromptDescription(ctx context.Context, promptID, uid, description string) error
	GetPromptName(ctx context.Context, promptID, uid string) (string, error)
	GetMaxVersion(ctx context.Context, promptID string) (int, error)
	GetActiveTemplate(ctx context.Context, promptID string) (string, error)
	SetAllVersionsInactive(ctx context.Context, promptID string) error
	InsertPromptVersion(ctx context.Context, promptID string, version int, template string, active bool) error
	DeletePrompt(ctx context.Context, promptID, uid string) error
	PromptExistsForUser(ctx context.Context, promptID, uid string) (bool, error)
}
