package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"promptify/internal/store"

	_ "modernc.org/sqlite"
)

// Store implements store.Store with SQLite.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and runs migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) GetAdminUID(ctx context.Context) (string, error) {
	var uid string
	err := s.db.QueryRowContext(ctx, `SELECT uid FROM admin LIMIT 1`).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if uid == "" {
		return "", store.ErrNotFound
	}
	return uid, nil
}

func (s *Store) InsertAdmin(ctx context.Context, uid, settingsJSON string) error {
	if settingsJSON == "" {
		settingsJSON = store.DefaultAdminSettingsJSON
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin (uid, settings) VALUES (?, ?)`,
		uid, settingsJSON,
	)
	return err
}

func (s *Store) GetAdminSettings(ctx context.Context) (store.AdminSettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT settings FROM admin LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AdminSettings{}, store.ErrNotFound
	}
	if err != nil {
		return store.AdminSettings{}, err
	}
	var settings store.AdminSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return store.AdminSettings{}, err
	}
	return settings, nil
}

func (s *Store) UpdateAdminSettings(ctx context.Context, settings store.AdminSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admin SET settings = ?`, string(raw))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func (s *Store) UserExists(ctx context.Context, uid string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM users WHERE uid = ? LIMIT 1", uid).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) CreateUser(ctx context.Context, user store.UserRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (uid, email, name, password_hash, created_at, last_login_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, user.UID, user.Email, user.Name, user.PasswordHash, nullTime(user.LastLoginAt))
	if err != nil && isUniqueConstraintErr(err) {
		return store.ErrConflict
	}
	return err
}

func (s *Store) GetUserByUID(ctx context.Context, uid string) (store.UserRecord, error) {
	var u store.UserRecord
	var created, lastLogin sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT uid, COALESCE(email,''), COALESCE(name,''), COALESCE(password_hash,''),
			COALESCE(api_key_hash,''), created_at, last_login_at
		FROM users WHERE uid = ?
	`, uid).Scan(&u.UID, &u.Email, &u.Name, &u.PasswordHash, &u.APIKeyHash, &created, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return store.UserRecord{}, store.ErrNotFound
	}
	if err != nil {
		return store.UserRecord{}, err
	}
	if created.Valid {
		u.CreatedAt = created.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.UserRecord, error) {
	var u store.UserRecord
	var created, lastLogin sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT uid, COALESCE(email,''), COALESCE(name,''), COALESCE(password_hash,''),
			COALESCE(api_key_hash,''), created_at, last_login_at
		FROM users WHERE lower(email) = lower(?) LIMIT 1
	`, email).Scan(&u.UID, &u.Email, &u.Name, &u.PasswordHash, &u.APIKeyHash, &created, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return store.UserRecord{}, store.ErrNotFound
	}
	if err != nil {
		return store.UserRecord{}, err
	}
	if created.Valid {
		u.CreatedAt = created.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	return u, nil
}

func (s *Store) UpdateUserCredentials(ctx context.Context, uid, email, name, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET email = ?, name = ?, password_hash = ? WHERE uid = ?
	`, email, name, passwordHash, uid)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return store.ErrConflict
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) TouchUserLogin(ctx context.Context, uid string, lastLogin time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE uid = ?`, lastLogin, uid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context, emailQuery string, limit int) ([]store.UserListItem, error) {
	if limit <= 0 {
		limit = 10
	}
	emailQuery = strings.TrimSpace(emailQuery)
	rows, err := s.db.QueryContext(ctx, `
		SELECT uid, COALESCE(email,''), COALESCE(name,''), created_at, last_login_at
		FROM users
		WHERE (? = '' OR lower(email) LIKE '%' || lower(?) || '%')
		ORDER BY
			CASE WHEN last_login_at IS NULL THEN 1 ELSE 0 END,
			last_login_at DESC,
			created_at DESC
		LIMIT ?
	`, emailQuery, emailQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.UserListItem
	for rows.Next() {
		var u store.UserListItem
		var created, lastLogin sql.NullTime
		if err := rows.Scan(&u.UID, &u.Email, &u.Name, &created, &lastLogin); err != nil {
			return nil, err
		}
		if created.Valid {
			u.CreatedAt = created.Time
		}
		if lastLogin.Valid {
			u.LastLoginAt = lastLogin.Time
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) GetUserProfile(ctx context.Context, uid string) (string, string, error) {
	var name, email sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT name, email FROM users WHERE uid = ?", uid).Scan(&name, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", store.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return name.String, email.String, nil
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}

func (s *Store) SetUserAPIKeyHash(ctx context.Context, uid, hash string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET api_key_hash = ? WHERE uid = ?", hash, uid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetUserAPIKeyHash(ctx context.Context, uid string) (string, bool, error) {
	var h sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT api_key_hash FROM users WHERE uid = ?", uid).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !h.Valid || h.String == "" {
		return "", false, nil
	}
	return h.String, true, nil
}

func (s *Store) DeleteUserAndData(ctx context.Context, uid string) error {
	if adminUID, err := s.GetAdminUID(ctx); err == nil && adminUID == uid {
		return store.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM prompt_versions WHERE prompt_id IN (SELECT id FROM prompts WHERE userId = ?)", uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM prompts WHERE userId = ?", uid); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM users WHERE uid = ?", uid)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) ListPromptsForUser(ctx context.Context, uid string) ([]store.PromptSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.description, COALESCE(pv.version, 0), pv.template
		FROM prompts p
		LEFT JOIN prompt_versions pv ON pv.prompt_id = p.id AND pv.is_active = 1
		WHERE p.userId = ?
		ORDER BY p.created_at DESC
	`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.PromptSummary
	for rows.Next() {
		var id int64
		var p store.PromptSummary
		var desc, tmpl sql.NullString
		if err := rows.Scan(&id, &p.Name, &desc, &p.Version, &tmpl); err != nil {
			continue
		}
		p.ID = formatPromptID(id)
		if desc.Valid {
			p.Description = desc.String
		}
		if tmpl.Valid {
			p.Template = tmpl.String
		}
		out = append(out, p)
	}
	if out == nil {
		out = []store.PromptSummary{}
	}
	return out, rows.Err()
}

func (s *Store) GetPromptMeta(ctx context.Context, promptID, uid string) (string, string, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return "", "", err
	}
	var name string
	var description sql.NullString
	err = s.db.QueryRowContext(ctx,
		"SELECT name, description FROM prompts WHERE id = ? AND userId = ?",
		id, uid,
	).Scan(&name, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", store.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	desc := ""
	if description.Valid {
		desc = description.String
	}
	return name, desc, nil
}

func (s *Store) ListPromptVersions(ctx context.Context, promptID string) ([]store.VersionRecord, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT version, template, is_active, created_at
		FROM prompt_versions WHERE prompt_id = ?
		ORDER BY version DESC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.VersionRecord
	for rows.Next() {
		var v store.VersionRecord
		if err := rows.Scan(&v.Version, &v.Template, &v.IsActive, &v.CreatedAt); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) CreatePrompt(ctx context.Context, uid, name, description, template string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		"INSERT INTO prompts (userId, name, description) VALUES (?, ?, ?)",
		uid, name, description,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return "", store.ErrConflict
		}
		return "", err
	}

	promptID, _ := result.LastInsertId()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO prompt_versions (prompt_id, version, template, is_active)
		VALUES (?, 1, ?, 1)
	`, promptID, template)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return formatPromptID(promptID), nil
}

func (s *Store) UpdatePromptDescription(ctx context.Context, promptID, uid, description string) error {
	id, err := parsePromptID(promptID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE prompts SET description = ? WHERE id = ? AND userId = ?",
		description, id, uid,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetPromptName(ctx context.Context, promptID, uid string) (string, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return "", err
	}
	var name string
	err = s.db.QueryRowContext(ctx,
		"SELECT name FROM prompts WHERE id = ? AND userId = ?",
		id, uid,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	return name, err
}

func (s *Store) GetMaxVersion(ctx context.Context, promptID string) (int, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return 0, err
	}
	var v int
	err = s.db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM prompt_versions WHERE prompt_id = ?",
		id,
	).Scan(&v)
	return v, err
}

func (s *Store) GetActiveTemplate(ctx context.Context, promptID string) (string, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return "", err
	}
	var tmpl string
	err = s.db.QueryRowContext(ctx,
		"SELECT template FROM prompt_versions WHERE prompt_id = ? AND is_active = 1",
		id,
	).Scan(&tmpl)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tmpl, err
}

func (s *Store) SetAllVersionsInactive(ctx context.Context, promptID string) error {
	id, err := parsePromptID(promptID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"UPDATE prompt_versions SET is_active = 0 WHERE prompt_id = ?",
		id,
	)
	return err
}

func (s *Store) InsertPromptVersion(ctx context.Context, promptID string, version int, template string, active bool) error {
	id, err := parsePromptID(promptID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO prompt_versions (prompt_id, version, template, is_active)
		VALUES (?, ?, ?, ?)
	`, id, version, template, active)
	return err
}

func (s *Store) DeletePrompt(ctx context.Context, promptID, uid string) error {
	id, err := parsePromptID(promptID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var found int64
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM prompts WHERE id = ? AND userId = ?",
		id, uid,
	).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM prompt_versions WHERE prompt_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM prompts WHERE id = ? AND userId = ?", id, uid); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PromptExistsForUser(ctx context.Context, promptID, uid string) (bool, error) {
	id, err := parsePromptID(promptID)
	if err != nil {
		return false, err
	}
	var found int64
	err = s.db.QueryRowContext(ctx,
		"SELECT id FROM prompts WHERE id = ? AND userId = ?",
		id, uid,
	).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func parsePromptID(promptID string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(promptID), 10, 64)
	if err != nil || id <= 0 {
		return 0, store.ErrNotFound
	}
	return id, nil
}

func formatPromptID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (s *Store) migrate() error {
	if err := createUsersTable(s.db); err != nil {
		return err
	}
	if err := migrateAdminTable(s.db); err != nil {
		return err
	}
	versionTable := `
	CREATE TABLE IF NOT EXISTS prompt_versions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		prompt_id INTEGER,
		version INTEGER,
		template TEXT,
		is_active BOOLEAN,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := s.db.Exec(versionTable); err != nil {
		return err
	}
	return ensurePromptsTable(s.db)
}

func migrateAdminTable(db *sql.DB) error {
	var exists bool
	if err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sqlite_master WHERE type='table' AND name='admin'
		)
	`).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		_, err := db.Exec(`
			CREATE TABLE admin (
				uid TEXT PRIMARY KEY,
				settings TEXT NOT NULL DEFAULT '{"allow_registration":false}'
			);
		`)
		return err
	}

	hasUID, err := hasColumn(db, "admin", "uid")
	if err != nil {
		return err
	}
	hasUsername, err := hasColumn(db, "admin", "username")
	if err != nil {
		return err
	}

	// Already on new shape.
	if hasUID && !hasUsername {
		hasSettings, err := hasColumn(db, "admin", "settings")
		if err != nil {
			return err
		}
		if !hasSettings {
			if _, err := db.Exec(`ALTER TABLE admin ADD COLUMN settings TEXT NOT NULL DEFAULT '{"allow_registration":false}'`); err != nil {
				return err
			}
		}
		return nil
	}

	// Legacy admin(username, password_hash) → admin(uid, settings) + users.password_hash.
	if hasUsername {
		return migrateLegacyAdmin(db)
	}

	// Unexpected shape: recreate empty new table.
	if _, err := db.Exec(`DROP TABLE IF EXISTS admin`); err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE TABLE admin (
			uid TEXT PRIMARY KEY,
			settings TEXT NOT NULL DEFAULT '{"allow_registration":false}'
		);
	`)
	return err
}

func migrateLegacyAdmin(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var username, passwordHash sql.NullString
	err = tx.QueryRow(`SELECT username, password_hash FROM admin LIMIT 1`).Scan(&username, &passwordHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if _, err := tx.Exec(`DROP TABLE admin`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE admin (
			uid TEXT PRIMARY KEY,
			settings TEXT NOT NULL DEFAULT '{"allow_registration":false}'
		);
	`); err != nil {
		return err
	}

	if username.Valid && username.String != "" {
		uid := username.String
		email := username.String
		hash := ""
		if passwordHash.Valid {
			hash = passwordHash.String
		}

		var exists int
		_ = tx.QueryRow(`SELECT 1 FROM users WHERE uid = ? LIMIT 1`, uid).Scan(&exists)
		if exists == 1 {
			if _, err := tx.Exec(`
				UPDATE users SET email = ?, name = COALESCE(NULLIF(name,''), ?), password_hash = ?
				WHERE uid = ?
			`, email, email, hash, uid); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`
				INSERT INTO users (uid, email, name, password_hash, created_at)
				VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
			`, uid, email, email, hash); err != nil {
				return err
			}
		}

		if _, err := tx.Exec(`
			INSERT INTO admin (uid, settings) VALUES (?, ?)
		`, uid, store.DefaultAdminSettingsJSON); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func createUsersTable(db *sql.DB) error {
	userTable := `
	CREATE TABLE IF NOT EXISTS users (
		uid TEXT PRIMARY KEY,
		email TEXT,
		name TEXT,
		password_hash TEXT,
		api_key_hash TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login_at DATETIME
	);`
	if _, err := db.Exec(userTable); err != nil {
		return err
	}

	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"api_key_hash", `ALTER TABLE users ADD COLUMN api_key_hash TEXT`},
		{"password_hash", `ALTER TABLE users ADD COLUMN password_hash TEXT`},
	} {
		ok, err := hasColumn(db, "users", col.name)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := db.Exec(col.ddl); err != nil {
				return err
			}
		}
	}

	// Enforce unique emails at the database level (app also normalizes case).
	_, _ = db.Exec(`DROP INDEX IF EXISTS idx_users_email`)
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email)`); err != nil {
		return err
	}
	return nil
}

func ensurePromptsTable(db *sql.DB) error {
	var exists bool
	if err := db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sqlite_master WHERE type='table' AND name='prompts'
		)
	`).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		promptTable := `
		CREATE TABLE IF NOT EXISTS prompts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			userId TEXT NOT NULL,
			name TEXT,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(userId, name)
		);`
		_, err := db.Exec(promptTable)
		return err
	}

	hasUserID, err := hasColumn(db, "prompts", "userId")
	if err != nil {
		return err
	}
	if hasUserID {
		return nil
	}

	log.Println("Migrating prompts table to add userId column...")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE prompts RENAME TO prompts_old;`); err != nil {
		return err
	}
	promptTable := `
	CREATE TABLE IF NOT EXISTS prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		userId TEXT NOT NULL,
		name TEXT,
		description TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(userId, name)
	);`
	if _, err := tx.Exec(promptTable); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO prompts (id, userId, name, description, created_at)
		SELECT id, 'legacy' AS userId, name, description, created_at
		FROM prompts_old;
	`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE prompts_old;`); err != nil {
		return err
	}
	return tx.Commit()
}

func hasColumn(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + tableName + `);`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid         int
			name        string
			typ         string
			notnull     int
			dfltValue   sql.NullString
			primary_key int
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &primary_key); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}
