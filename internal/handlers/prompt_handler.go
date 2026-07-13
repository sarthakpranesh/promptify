package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"promptify/internal/store"
)

type PromptHandler struct {
	Store store.Store
}

func NewPromptHandler(s store.Store) *PromptHandler {
	return &PromptHandler{Store: s}
}

func variablesFromTemplate(tmpl string) []string {
	if tmpl == "" {
		return []string{}
	}
	v := extractTemplateVariables(tmpl)
	if v == nil {
		return []string{}
	}
	return v
}

func summariesToPrompts(summaries []store.PromptSummary) []Prompt {
	prompts := make([]Prompt, 0, len(summaries))
	for _, s := range summaries {
		prompts = append(prompts, Prompt{
			ID:          s.ID,
			Name:        s.Name,
			Description: s.Description,
			Version:     s.Version,
			Variables:   variablesFromTemplate(s.Template),
		})
	}
	return prompts
}

func (h *PromptHandler) LoadPromptListForUser(userID string) (PromptListResponse, error) {
	summaries, err := h.Store.ListPromptsForUser(context.Background(), userID)
	if err != nil {
		return PromptListResponse{}, err
	}
	return PromptListResponse{Prompts: summariesToPrompts(summaries)}, nil
}

func (h *PromptHandler) LoadPromptDetailForUser(promptID, userID string) (PromptDetail, error) {
	ctx := context.Background()
	name, description, err := h.Store.GetPromptMeta(ctx, promptID, userID)
	if err != nil {
		return PromptDetail{}, err
	}

	versions, err := h.Store.ListPromptVersions(ctx, promptID)
	if err != nil {
		return PromptDetail{}, err
	}

	detail := PromptDetail{ID: promptID, Name: name, Description: description}
	for _, rec := range versions {
		v := Version{
			Version:             rec.Version,
			Template:            rec.Template,
			IsActive:            rec.IsActive,
			Variables:           extractTemplateVariables(rec.Template),
			HighlightedTemplate: highlightTemplateVariables(rec.Template),
			CreatedAt:           rec.CreatedAt.Format("Jan 02, 2006 15:04"),
		}
		if v.IsActive {
			detail.Active = v
			detail.Variables = v.Variables
		}
		detail.Versions = append(detail.Versions, v)
	}

	sort.Slice(detail.Versions, func(i, j int) bool {
		return detail.Versions[i].Version > detail.Versions[j].Version
	})

	return detail, nil
}

// GetPromptDetailForUserByID is the single prompt-fetch wrapper for all interfaces
// (web, REST API, MCP). It enforces valid ID shape and user ownership.
func (h *PromptHandler) GetPromptDetailForUserByID(promptID, userID string) (PromptDetail, error) {
	if strings.TrimSpace(promptID) == "" {
		return PromptDetail{}, errors.New("invalid prompt id")
	}
	return h.LoadPromptDetailForUser(promptID, userID)
}

func (h *PromptHandler) ListPrompts(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.LoadPromptListForUser(userID)
	if err != nil {
		http.Error(w, "Failed to load prompts", http.StatusInternalServerError)
		return
	}
	currentUser, err := h.loadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.ParseFiles("web/templates/layout.html", "web/templates/index.html"))
	tmpl.ExecuteTemplate(w, "layout", struct {
		Prompts     []Prompt
		CurrentUser CurrentUser
	}{
		Prompts:     list.Prompts,
		CurrentUser: currentUser,
	})
}

func (h *PromptHandler) ListPromptsAPI(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.LoadPromptListForUser(userID)
	if err != nil {
		http.Error(w, "Failed to load prompts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (h *PromptHandler) NewPromptPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	currentUser, err := h.loadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.ParseFiles("web/templates/layout.html", "web/templates/new.html"))
	tmpl.ExecuteTemplate(w, "layout", struct {
		CurrentUser CurrentUser
	}{
		CurrentUser: currentUser,
	})
}

func (h *PromptHandler) CreatePrompt(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := normalizeFreeText(r.FormValue("name"))
	description := normalizeFreeText(r.FormValue("description"))
	promptTemplate := strings.TrimSpace(r.FormValue("prompt"))

	if err := validatePromptInput(name, description, promptTemplate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	promptID, err := h.Store.CreatePrompt(r.Context(), userID, name, description, promptTemplate)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			http.Error(w, "A prompt with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create prompt", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/prompts/%s", promptID), http.StatusSeeOther)
}

func (h *PromptHandler) GetPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := parsePromptID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid prompt id", http.StatusBadRequest)
		return
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	detail, err := h.GetPromptDetailForUserByID(id, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Prompt not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load prompt", http.StatusInternalServerError)
		return
	}

	currentUser, err := h.loadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.ParseFiles("web/templates/layout.html", "web/templates/prompt.html"))
	tmpl.ExecuteTemplate(w, "layout", struct {
		PromptDetail
		CurrentUser CurrentUser
	}{
		PromptDetail: detail,
		CurrentUser:  currentUser,
	})
}

func (h *PromptHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	currentUser, err := h.loadCurrentUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to load user", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.ParseFiles("web/templates/layout.html", "web/templates/settings.html"))
	tmpl.ExecuteTemplate(w, "layout", struct {
		CurrentUser CurrentUser
		BaseURL     string
	}{
		CurrentUser: currentUser,
		BaseURL:     requestBaseURL(r),
	})
}

func (h *PromptHandler) GetPromptAPI(w http.ResponseWriter, r *http.Request) {
	promptID, err := parsePromptID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid prompt id", http.StatusBadRequest)
		return
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	detail, err := h.GetPromptDetailForUserByID(promptID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Prompt not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load prompt", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detail)
}

func (h *PromptHandler) ExportPromptsAPI(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	list, err := h.LoadPromptListForUser(userID)
	if err != nil {
		http.Error(w, "Failed to load prompts", http.StatusInternalServerError)
		return
	}

	exportedPrompts := make([]PromptDetail, 0, len(list.Prompts))
	for _, prompt := range list.Prompts {
		detail, err := h.LoadPromptDetailForUser(prompt.ID, userID)
		if err != nil {
			http.Error(w, "Failed to export prompts", http.StatusInternalServerError)
			return
		}
		exportedPrompts = append(exportedPrompts, detail)
	}

	fileTimestamp := time.Now().UTC().Format("20060102-150405")
	fileName := fmt.Sprintf("promptify-prompts-%s.json", fileTimestamp)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	json.NewEncoder(w).Encode(struct {
		ExportedAt string         `json:"exported_at"`
		Prompts    []PromptDetail `json:"prompts"`
	}{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Prompts:    exportedPrompts,
	})
}

func (h *PromptHandler) UpdatePrompt(w http.ResponseWriter, r *http.Request) {
	promptID, err := parsePromptID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid prompt id", http.StatusBadRequest)
		return
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	newDescription := normalizeFreeText(r.FormValue("description"))
	newTemplate := strings.TrimSpace(r.FormValue("prompt"))
	ctx := r.Context()

	name, err := h.Store.GetPromptName(ctx, promptID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Prompt not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load prompt", http.StatusInternalServerError)
		return
	}

	if err := validatePromptInput(name, newDescription, newTemplate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.Store.UpdatePromptDescription(ctx, promptID, userID, newDescription); err != nil {
		http.Error(w, "Failed to update prompt", http.StatusInternalServerError)
		return
	}

	currentVersion, err := h.Store.GetMaxVersion(ctx, promptID)
	if err != nil {
		http.Error(w, "Failed to update prompt", http.StatusInternalServerError)
		return
	}

	currentTemplate, err := h.Store.GetActiveTemplate(ctx, promptID)
	if err != nil {
		http.Error(w, "Failed to update prompt", http.StatusInternalServerError)
		return
	}

	if currentTemplate != newTemplate {
		if err := h.Store.SetAllVersionsInactive(ctx, promptID); err != nil {
			http.Error(w, "Failed to save new version", http.StatusInternalServerError)
			return
		}
		if err := h.Store.InsertPromptVersion(ctx, promptID, currentVersion+1, newTemplate, true); err != nil {
			http.Error(w, "Failed to save new version", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/prompts/%s", promptID), http.StatusSeeOther)
}

func (h *PromptHandler) DeletePrompt(w http.ResponseWriter, r *http.Request) {
	promptID, err := parsePromptID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid prompt id", http.StatusBadRequest)
		return
	}

	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.Store.DeletePrompt(r.Context(), promptID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Prompt not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to delete prompt", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

var disallowedCharsRegex = regexp.MustCompile(`[^a-zA-Z0-9\s\-_]`)

func normalizeFreeText(in string) string {
	noSpecials := disallowedCharsRegex.ReplaceAllString(in, "")
	return strings.TrimSpace(noSpecials)
}

func validatePromptInput(name string, description string, promptTemplate string) error {
	if len(strings.TrimSpace(name)) < 3 {
		return errors.New("name must be at least 3 characters long")
	}
	if description != "" && strings.TrimSpace(description) == "" {
		return errors.New("description contains only invalid characters")
	}

	words := strings.Fields(promptTemplate)
	if len(words) < 3 {
		return errors.New("prompt template must contain at least 3 words")
	}
	return nil
}

func parsePromptID(rawID string) (string, error) {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return "", errors.New("invalid prompt id")
	}
	if isSQLitePromptID(id) || isMongoObjectID(id) {
		return id, nil
	}
	return "", errors.New("invalid prompt id")
}

func isSQLitePromptID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0
}

var mongoObjectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

func isMongoObjectID(id string) bool {
	return mongoObjectIDPattern.MatchString(id)
}

func (h *PromptHandler) loadCurrentUser(ctx context.Context, userID string) (CurrentUser, error) {
	name, email, err := h.Store.GetUserProfile(ctx, userID)
	if err != nil {
		return CurrentUser{}, err
	}

	fullName := strings.TrimSpace(name)
	emailVal := strings.TrimSpace(email)

	display := firstNameFromProfile(fullName, emailVal)
	if fullName == "" {
		fullName = "Not provided"
	}
	if emailVal == "" {
		emailVal = "Not provided"
	}

	isAdmin := false
	if adminUID, err := h.Store.GetAdminUID(ctx); err == nil {
		isAdmin = adminUID == userID
	}

	return CurrentUser{
		FullName:    fullName,
		Email:       emailVal,
		DisplayName: trimToRunes(display, 28),
		IsAdmin:     isAdmin,
	}, nil
}

func firstNameFromProfile(fullName string, email string) string {
	if fullName != "" {
		parts := strings.Fields(fullName)
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
	}
	if email != "" {
		local := strings.Split(email, "@")
		if len(local) > 0 && local[0] != "" {
			return local[0]
		}
	}
	return "Account"
}

func trimToRunes(in string, max int) string {
	if max <= 0 || in == "" {
		return ""
	}
	if utf8.RuneCountInString(in) <= max {
		return in
	}

	r := []rune(in)
	return string(r[:max])
}
