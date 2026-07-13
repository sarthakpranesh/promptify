package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"promptify/internal/store"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collUsers          = "users"
	collAdmin          = "admin"
	collPrompts        = "prompts"
	collPromptVersions = "prompt_versions"
)

// Store implements store.Store with MongoDB (e.g. Atlas free tier).
type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

type userDoc struct {
	UID          string    `bson:"_id"`
	Email        string    `bson:"email,omitempty"`
	Name         string    `bson:"name,omitempty"`
	PasswordHash string    `bson:"password_hash,omitempty"`
	APIKeyHash   string    `bson:"api_key_hash,omitempty"`
	CreatedAt    time.Time `bson:"created_at"`
	LastLoginAt  time.Time `bson:"last_login_at,omitempty"`
}

type adminDoc struct {
	ID       string `bson:"_id"`
	UID      string `bson:"uid"`
	Settings string `bson:"settings"`
}

type promptDoc struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	UserID      string        `bson:"user_id"`
	Name        string        `bson:"name"`
	Description string        `bson:"description,omitempty"`
	CreatedAt   time.Time     `bson:"created_at"`
}

type versionDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	PromptID  bson.ObjectID `bson:"prompt_id"`
	Version   int           `bson:"version"`
	Template  string        `bson:"template"`
	IsActive  bool          `bson:"is_active"`
	CreatedAt time.Time     `bson:"created_at"`
}

// Open connects to MongoDB, ensures indexes, and returns a Store.
func Open(ctx context.Context, uri, database string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, err
	}

	s := &Store{
		client: client,
		db:     client.Database(database),
	}
	if err := s.ensureIndexes(ctx); err != nil {
		client.Disconnect(ctx)
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	if err := s.migrateLegacyAdmin(ctx); err != nil {
		return err
	}

	emailIdx := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	promptIdx := []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "name", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
	}
	versionIdx := []mongo.IndexModel{
		{Keys: bson.D{{Key: "prompt_id", Value: 1}, {Key: "version", Value: -1}}},
		{Keys: bson.D{{Key: "prompt_id", Value: 1}, {Key: "is_active", Value: 1}}},
	}

	if _, err := s.db.Collection(collUsers).Indexes().CreateOne(ctx, emailIdx); err != nil {
		return err
	}
	if _, err := s.db.Collection(collPrompts).Indexes().CreateMany(ctx, promptIdx); err != nil {
		return err
	}
	if _, err := s.db.Collection(collPromptVersions).Indexes().CreateMany(ctx, versionIdx); err != nil {
		return err
	}
	return nil
}

// migrateLegacyAdmin moves username/password_hash admin docs to uid+settings.
func (s *Store) migrateLegacyAdmin(ctx context.Context) error {
	var raw bson.M
	err := s.db.Collection(collAdmin).FindOne(ctx, bson.M{}).Decode(&raw)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, ok := raw["uid"]; ok {
		if _, hasSettings := raw["settings"]; !hasSettings {
			_, err := s.db.Collection(collAdmin).UpdateOne(ctx,
				bson.M{"_id": raw["_id"]},
				bson.M{"$set": bson.M{"settings": store.DefaultAdminSettingsJSON}},
			)
			return err
		}
		return nil
	}
	username, _ := raw["username"].(string)
	passwordHash, _ := raw["password_hash"].(string)
	if username == "" {
		_ = s.db.Collection(collAdmin).Drop(ctx)
		return nil
	}

	now := time.Now()
	_, err = s.db.Collection(collUsers).UpdateOne(ctx,
		bson.M{"_id": username},
		bson.M{
			"$set": bson.M{
				"email":         username,
				"name":          username,
				"password_hash": passwordHash,
			},
			"$setOnInsert": bson.M{
				"created_at": now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return err
	}

	_ = s.db.Collection(collAdmin).Drop(ctx)
	_, err = s.db.Collection(collAdmin).InsertOne(ctx, adminDoc{
		ID:       "admin",
		UID:      username,
		Settings: store.DefaultAdminSettingsJSON,
	})
	return err
}

func (s *Store) GetAdminUID(ctx context.Context) (string, error) {
	var doc adminDoc
	err := s.db.Collection(collAdmin).FindOne(ctx, bson.M{}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", store.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if doc.UID == "" {
		return "", store.ErrNotFound
	}
	return doc.UID, nil
}

func (s *Store) InsertAdmin(ctx context.Context, uid, settingsJSON string) error {
	if settingsJSON == "" {
		settingsJSON = store.DefaultAdminSettingsJSON
	}
	_, err := s.db.Collection(collAdmin).InsertOne(ctx, adminDoc{
		ID:       "admin",
		UID:      uid,
		Settings: settingsJSON,
	})
	return err
}

func (s *Store) GetAdminSettings(ctx context.Context) (store.AdminSettings, error) {
	var doc adminDoc
	err := s.db.Collection(collAdmin).FindOne(ctx, bson.M{}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.AdminSettings{}, store.ErrNotFound
	}
	if err != nil {
		return store.AdminSettings{}, err
	}
	var settings store.AdminSettings
	if doc.Settings == "" {
		return store.AdminSettings{}, nil
	}
	if err := json.Unmarshal([]byte(doc.Settings), &settings); err != nil {
		return store.AdminSettings{}, err
	}
	return settings, nil
}

func (s *Store) UpdateAdminSettings(ctx context.Context, settings store.AdminSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	res, err := s.db.Collection(collAdmin).UpdateOne(ctx,
		bson.M{},
		bson.M{"$set": bson.M{"settings": string(raw)}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	n, err := s.db.Collection(collUsers).CountDocuments(ctx, bson.M{})
	return int(n), err
}

func (s *Store) UserExists(ctx context.Context, uid string) (bool, error) {
	n, err := s.db.Collection(collUsers).CountDocuments(ctx, bson.M{"_id": uid})
	return n > 0, err
}

func (s *Store) CreateUser(ctx context.Context, user store.UserRecord) error {
	now := time.Now()
	doc := userDoc{
		UID:          user.UID,
		Email:        user.Email,
		Name:         user.Name,
		PasswordHash: user.PasswordHash,
		CreatedAt:    now,
		LastLoginAt:  user.LastLoginAt,
	}
	_, err := s.db.Collection(collUsers).InsertOne(ctx, doc)
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return store.ErrConflict
	}
	return err
}

func (s *Store) GetUserByUID(ctx context.Context, uid string) (store.UserRecord, error) {
	var doc userDoc
	err := s.db.Collection(collUsers).FindOne(ctx, bson.M{"_id": uid}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.UserRecord{}, store.ErrNotFound
	}
	if err != nil {
		return store.UserRecord{}, err
	}
	return userDocToRecord(doc), nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.UserRecord, error) {
	var doc userDoc
	err := s.db.Collection(collUsers).FindOne(ctx, bson.M{
		"email": bson.M{"$regex": "^" + regexp.QuoteMeta(email) + "$", "$options": "i"},
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return store.UserRecord{}, store.ErrNotFound
	}
	if err != nil {
		return store.UserRecord{}, err
	}
	return userDocToRecord(doc), nil
}

func userDocToRecord(doc userDoc) store.UserRecord {
	return store.UserRecord{
		UID:          doc.UID,
		Email:        doc.Email,
		Name:         doc.Name,
		PasswordHash: doc.PasswordHash,
		APIKeyHash:   doc.APIKeyHash,
		CreatedAt:    doc.CreatedAt,
		LastLoginAt:  doc.LastLoginAt,
	}
}

func (s *Store) UpdateUserCredentials(ctx context.Context, uid, email, name, passwordHash string) error {
	res, err := s.db.Collection(collUsers).UpdateOne(ctx,
		bson.M{"_id": uid},
		bson.M{"$set": bson.M{
			"email":         email,
			"name":          name,
			"password_hash": passwordHash,
		}},
	)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return store.ErrConflict
		}
		return err
	}
	if res.MatchedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) TouchUserLogin(ctx context.Context, uid string, lastLogin time.Time) error {
	res, err := s.db.Collection(collUsers).UpdateOne(ctx,
		bson.M{"_id": uid},
		bson.M{"$set": bson.M{"last_login_at": lastLogin}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUsers(ctx context.Context, emailQuery string, limit int) ([]store.UserListItem, error) {
	if limit <= 0 {
		limit = 10
	}
	emailQuery = strings.TrimSpace(emailQuery)
	filter := bson.M{}
	if emailQuery != "" {
		filter["email"] = bson.M{
			"$regex":   regexp.QuoteMeta(emailQuery),
			"$options": "i",
		}
	}
	opts := options.Find().
		SetSort(bson.D{
			{Key: "last_login_at", Value: -1},
			{Key: "created_at", Value: -1},
		}).
		SetLimit(int64(limit))
	cur, err := s.db.Collection(collUsers).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []store.UserListItem
	for cur.Next(ctx) {
		var doc userDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, store.UserListItem{
			UID:         doc.UID,
			Email:       doc.Email,
			Name:        doc.Name,
			CreatedAt:   doc.CreatedAt,
			LastLoginAt: doc.LastLoginAt,
		})
	}
	return out, cur.Err()
}

func (s *Store) GetUserProfile(ctx context.Context, uid string) (string, string, error) {
	var doc userDoc
	err := s.db.Collection(collUsers).FindOne(ctx, bson.M{"_id": uid}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", "", store.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return doc.Name, doc.Email, nil
}

func (s *Store) SetUserAPIKeyHash(ctx context.Context, uid, hash string) error {
	res, err := s.db.Collection(collUsers).UpdateOne(ctx,
		bson.M{"_id": uid},
		bson.M{"$set": bson.M{"api_key_hash": hash}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetUserAPIKeyHash(ctx context.Context, uid string) (string, bool, error) {
	var doc userDoc
	err := s.db.Collection(collUsers).FindOne(ctx, bson.M{"_id": uid}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if doc.APIKeyHash == "" {
		return "", false, nil
	}
	return doc.APIKeyHash, true, nil
}

func (s *Store) DeleteUserAndData(ctx context.Context, uid string) error {
	if adminUID, err := s.GetAdminUID(ctx); err == nil && adminUID == uid {
		return store.ErrForbidden
	}

	promptIDs, err := s.promptObjectIDsForUser(ctx, uid)
	if err != nil {
		return err
	}
	if len(promptIDs) > 0 {
		if _, err := s.db.Collection(collPromptVersions).DeleteMany(ctx, bson.M{"prompt_id": bson.M{"$in": promptIDs}}); err != nil {
			return err
		}
	}
	if _, err := s.db.Collection(collPrompts).DeleteMany(ctx, bson.M{"user_id": uid}); err != nil {
		return err
	}
	res, err := s.db.Collection(collUsers).DeleteOne(ctx, bson.M{"_id": uid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) AssignLegacyPrompts(ctx context.Context, uid string) error {
	_, err := s.db.Collection(collPrompts).UpdateMany(ctx,
		bson.M{"user_id": "legacy"},
		bson.M{"$set": bson.M{"user_id": uid}},
	)
	return err
}

func (s *Store) ListPromptsForUser(ctx context.Context, uid string) ([]store.PromptSummary, error) {
	cur, err := s.db.Collection(collPrompts).Find(ctx,
		bson.M{"user_id": uid},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var prompts []promptDoc
	if err := cur.All(ctx, &prompts); err != nil {
		return nil, err
	}

	out := make([]store.PromptSummary, 0, len(prompts))
	for _, p := range prompts {
		summary := store.PromptSummary{
			ID:          p.ID.Hex(),
			Name:        p.Name,
			Description: p.Description,
		}
		var active versionDoc
		err := s.db.Collection(collPromptVersions).FindOne(ctx,
			bson.M{"prompt_id": p.ID, "is_active": true},
		).Decode(&active)
		if err == nil {
			summary.Version = active.Version
			summary.Template = active.Template
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Store) GetPromptMeta(ctx context.Context, promptID, uid string) (string, string, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return "", "", err
	}
	var p promptDoc
	err = s.db.Collection(collPrompts).FindOne(ctx, bson.M{"_id": oid, "user_id": uid}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", "", store.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return p.Name, p.Description, nil
}

func (s *Store) ListPromptVersions(ctx context.Context, promptID string) ([]store.VersionRecord, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return nil, err
	}
	cur, err := s.db.Collection(collPromptVersions).Find(ctx,
		bson.M{"prompt_id": oid},
		options.Find().SetSort(bson.D{{Key: "version", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []versionDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]store.VersionRecord, 0, len(docs))
	for _, d := range docs {
		out = append(out, store.VersionRecord{
			Version:   d.Version,
			Template:  d.Template,
			IsActive:  d.IsActive,
			CreatedAt: d.CreatedAt,
		})
	}
	return out, nil
}

func (s *Store) CreatePrompt(ctx context.Context, uid, name, description, template string) (string, error) {
	promptID := bson.NewObjectID()
	now := time.Now()
	_, err := s.db.Collection(collPrompts).InsertOne(ctx, promptDoc{
		ID:          promptID,
		UserID:      uid,
		Name:        name,
		Description: description,
		CreatedAt:   now,
	})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return "", store.ErrConflict
		}
		return "", err
	}
	_, err = s.db.Collection(collPromptVersions).InsertOne(ctx, versionDoc{
		PromptID:  promptID,
		Version:   1,
		Template:  template,
		IsActive:  true,
		CreatedAt: now,
	})
	if err != nil {
		_, _ = s.db.Collection(collPrompts).DeleteOne(ctx, bson.M{"_id": promptID})
		return "", err
	}
	return promptID.Hex(), nil
}

func (s *Store) UpdatePromptDescription(ctx context.Context, promptID, uid, description string) error {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return err
	}
	res, err := s.db.Collection(collPrompts).UpdateOne(ctx,
		bson.M{"_id": oid, "user_id": uid},
		bson.M{"$set": bson.M{"description": description}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetPromptName(ctx context.Context, promptID, uid string) (string, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return "", err
	}
	var p promptDoc
	err = s.db.Collection(collPrompts).FindOne(ctx, bson.M{"_id": oid, "user_id": uid}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", store.ErrNotFound
	}
	return p.Name, err
}

func (s *Store) GetMaxVersion(ctx context.Context, promptID string) (int, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return 0, err
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})
	var v versionDoc
	err = s.db.Collection(collPromptVersions).FindOne(ctx, bson.M{"prompt_id": oid}, opts).Decode(&v)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	return v.Version, err
}

func (s *Store) GetActiveTemplate(ctx context.Context, promptID string) (string, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return "", err
	}
	var v versionDoc
	err = s.db.Collection(collPromptVersions).FindOne(ctx,
		bson.M{"prompt_id": oid, "is_active": true},
	).Decode(&v)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	return v.Template, err
}

func (s *Store) SetAllVersionsInactive(ctx context.Context, promptID string) error {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return err
	}
	_, err = s.db.Collection(collPromptVersions).UpdateMany(ctx,
		bson.M{"prompt_id": oid},
		bson.M{"$set": bson.M{"is_active": false}},
	)
	return err
}

func (s *Store) InsertPromptVersion(ctx context.Context, promptID string, version int, template string, active bool) error {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return err
	}
	_, err = s.db.Collection(collPromptVersions).InsertOne(ctx, versionDoc{
		PromptID:  oid,
		Version:   version,
		Template:  template,
		IsActive:  active,
		CreatedAt: time.Now(),
	})
	return err
}

func (s *Store) DeletePrompt(ctx context.Context, promptID, uid string) error {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return err
	}
	res, err := s.db.Collection(collPrompts).DeleteOne(ctx, bson.M{"_id": oid, "user_id": uid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return store.ErrNotFound
	}
	_, err = s.db.Collection(collPromptVersions).DeleteMany(ctx, bson.M{"prompt_id": oid})
	return err
}

func (s *Store) PromptExistsForUser(ctx context.Context, promptID, uid string) (bool, error) {
	oid, err := parsePromptObjectID(promptID)
	if err != nil {
		return false, err
	}
	n, err := s.db.Collection(collPrompts).CountDocuments(ctx, bson.M{"_id": oid, "user_id": uid})
	return n > 0, err
}

func parsePromptObjectID(promptID string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(promptID)
	if err != nil {
		return bson.ObjectID{}, store.ErrNotFound
	}
	return oid, nil
}

func (s *Store) promptObjectIDsForUser(ctx context.Context, uid string) ([]bson.ObjectID, error) {
	cur, err := s.db.Collection(collPrompts).Find(ctx, bson.M{"user_id": uid})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []promptDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]bson.ObjectID, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids, nil
}
