package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"promptify/internal/store"
	"promptify/internal/validate"
)

const (
	defaultAdminEmail    = "admin@promptify.com"
	defaultAdminPassword = "admin"
)

// BootstrapAdmin ensures a sole admin user exists and stays synced with env.
//
// First launch (admin table empty): create user from PROMPTIFY_ADMIN_* and
// store a stable UID in admin.
// Later launches: keep that UID; sync email/password/name from env onto users.
func BootstrapAdmin(ctx context.Context, s store.Store) error {
	rawEmail := strings.TrimSpace(os.Getenv("PROMPTIFY_ADMIN_EMAIL"))
	password := os.Getenv("PROMPTIFY_ADMIN_PASSWORD")
	if password == "" {
		password = defaultAdminPassword
	}

	var email string
	if rawEmail == "" {
		email = defaultAdminEmail
	} else {
		normalized, err := validate.Email(rawEmail)
		if err != nil {
			return fmt.Errorf("PROMPTIFY_ADMIN_EMAIL: %w", err)
		}
		email = normalized
	}

	adminUID, err := s.GetAdminUID(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return createFirstAdmin(ctx, s, email, password)
	}
	if err != nil {
		return fmt.Errorf("read admin uid: %w", err)
	}

	return syncAdminUser(ctx, s, adminUID, email, password)
}

func createFirstAdmin(ctx context.Context, s store.Store, email, password string) error {
	userCount, err := s.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}

	uid, err := newStableUID()
	if err != nil {
		return fmt.Errorf("generate admin uid: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	if err := s.CreateUser(ctx, store.UserRecord{
		UID:          uid,
		Email:        email,
		Name:         email,
		PasswordHash: string(hash),
	}); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}

	if err := s.InsertAdmin(ctx, uid, store.DefaultAdminSettingsJSON); err != nil {
		return fmt.Errorf("insert admin pointer: %w", err)
	}

	if userCount == 0 {
		if err := s.AssignLegacyPrompts(ctx, uid); err != nil {
			return fmt.Errorf("assign legacy prompts: %w", err)
		}
	}

	log.Println("admin user created from environment")
	return nil
}

func syncAdminUser(ctx context.Context, s store.Store, uid, email, password string) error {
	user, err := s.GetUserByUID(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		if err := s.CreateUser(ctx, store.UserRecord{
			UID:          uid,
			Email:        email,
			Name:         email,
			PasswordHash: string(hash),
		}); err != nil {
			return fmt.Errorf("recreate missing admin user: %w", err)
		}
		log.Println("admin user recreated from environment (same uid)")
		return nil
	}
	if err != nil {
		return fmt.Errorf("load admin user: %w", err)
	}

	emailChanged := !strings.EqualFold(user.Email, email)
	passChanged := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil
	if !emailChanged && !passChanged {
		return nil
	}

	hash := user.PasswordHash
	if passChanged {
		b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}
		hash = string(b)
	}

	name := user.Name
	if user.Name == "" || strings.EqualFold(user.Name, user.Email) {
		name = email
	}

	if err := s.UpdateUserCredentials(ctx, uid, email, name, hash); err != nil {
		return fmt.Errorf("sync admin credentials: %w", err)
	}
	log.Println("admin credentials synced from environment")
	return nil
}

func newStableUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
