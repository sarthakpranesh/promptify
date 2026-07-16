package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"promptify/internal/store"
	mongostore "promptify/internal/store/mongo"
	sqlitestore "promptify/internal/store/sqlite"
)

// Open returns the configured Store implementation.
//
// Defaults to SQLite. If PROMPTIFY_MONGO_DB_URI is set, connects to MongoDB instead.
//
// Environment:
//   - PROMPTIFY_MONGO_DB_URI: MongoDB connection string (Atlas, etc.); when set, uses Mongo
//   - PROMPTIFY_SERVER_ENV: dev or prod (default dev); selects dev_promptify or prod_promptify when using Mongo
//   - PROMPTIFY_SQLITE_PATH: SQLite file path (default data/database.db)
func Open(ctx context.Context) (store.Store, error) {
	if uri := strings.TrimSpace(os.Getenv("PROMPTIFY_MONGO_DB_URI")); uri != "" {
		dbName, err := mongoDatabaseName()
		if err != nil {
			return nil, err
		}
		log.Printf("database: mongodb (%s)", dbName)
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return mongostore.Open(ctx, uri, dbName)
	}

	path := strings.TrimSpace(os.Getenv("PROMPTIFY_SQLITE_PATH"))
	if path == "" {
		path = "data/database.db"
	}
	log.Printf("database: sqlite (%s)", path)
	return sqlitestore.Open(path)
}

func mongoDatabaseName() (string, error) {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("PROMPTIFY_SERVER_ENV")))
	if env == "" {
		env = "dev"
	}
	switch env {
	case "dev":
		return "dev_promptify", nil
	case "prod":
		return "prod_promptify", nil
	default:
		return "", fmt.Errorf("invalid PROMPTIFY_SERVER_ENV %q: must be dev or prod", env)
	}
}
