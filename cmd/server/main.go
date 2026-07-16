package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mark3labs/mcp-go/server"

	"promptify/internal/applog"
	"promptify/internal/auth"
	"promptify/internal/db"
	"promptify/internal/handlers"
)

func main() {
	// build logger
	resolvedLog, logCloser, err := applog.Setup()
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer logCloser.Close()

	// open database
	database, err := db.Open(context.Background())
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// build server session secret
	sessionSecret := []byte(strings.TrimSpace(os.Getenv("PROMPTIFY_SESSION_SECRET")))
	if len(sessionSecret) == 0 {
		log.Fatal("PROMPTIFY_SESSION_SECRET is required; set a long random value (see README)")
	}

	// bootstrap the admin user
	if err := auth.BootstrapAdmin(context.Background(), database); err != nil {
		log.Fatalf("failed to bootstrap admin: %v", err)
	}

	authHandler := handlers.NewAuthHandler(database, sessionSecret)
	adminHandler := handlers.NewAdminHandler(database, sessionSecret, resolvedLog)
	h := handlers.NewPromptHandler(database)

	// build the router
	r := chi.NewRouter()

	// Frontend auth pages.
	r.Get("/", authHandler.HomePage)
	r.Get("/register", authHandler.RegisterPage)
	r.Get("/auth/session", http.HandlerFunc(authHandler.SessionStatus))
	r.Post("/auth/login", http.HandlerFunc(authHandler.Login))
	r.Post("/auth/register", http.HandlerFunc(authHandler.Register))
	r.Get("/logout", authHandler.Logout)

	// Serve frontend static assets.
	r.Handle("/static/*", http.StripPrefix("/static", http.FileServer(http.Dir("web/static"))))

	// Prompt pages + REST API (scoped per user).
	r.Get("/dashboard", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.ListPrompts)))
	r.Get("/api/prompts", handlers.RequireAuthAPIWithKey(sessionSecret, database, http.HandlerFunc(h.ListPromptsAPI)))
	r.Get("/api/prompts/{id}", handlers.RequireAuthAPIWithKey(sessionSecret, database, http.HandlerFunc(h.GetPromptAPI)))
	r.Get("/api/prompts/export", handlers.RequireAuthAPIWithKey(sessionSecret, database, http.HandlerFunc(h.ExportPromptsAPI)))
	r.Post("/api/account/api-key", handlers.RequireAuthAPI(sessionSecret, http.HandlerFunc(authHandler.GenerateAPIKey)))
	r.Post("/api/account/delete", handlers.RequireAuthAPI(sessionSecret, http.HandlerFunc(authHandler.DeleteAccount)))

	r.Get("/prompts/new", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.NewPromptPage)))
	r.Post("/prompts", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.CreatePrompt)))

	r.Get("/prompts/{id}", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.GetPrompt)))
	r.Post("/prompts/{id}", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.UpdatePrompt)))
	r.Post("/prompts/{id}/delete", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.DeletePrompt)))
	r.Get("/settings", handlers.RequireAuthHTML(sessionSecret, http.HandlerFunc(h.SettingsPage)))

	// Admin server console.
	r.Get("/server", handlers.RequireAdminHTML(sessionSecret, database, http.HandlerFunc(adminHandler.ServerPage)))
	r.Get("/api/admin/settings/registration", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.GetRegistrationSetting)))
	r.Post("/api/admin/settings/registration", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.SetRegistrationSetting)))
	r.Get("/api/admin/users", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.ListUsersAPI)))
	r.Post("/api/admin/users", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.CreateUserAPI)))
	r.Delete("/api/admin/users", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.DeleteUserAPI)))
	r.Get("/api/admin/logs/download", handlers.RequireAdminAPI(sessionSecret, database, http.HandlerFunc(adminHandler.DownloadLogs)))

	mcpServer := server.NewMCPServer(
		"Promptify",
		"0.0.1",
		server.WithToolCapabilities(true),
		server.WithInstructions(`Promptify — user-owned prompt bank (templates, descriptions, variables, version history).
TOOLS
- list_prompts: Call first for discovery. JSON per prompt: id (internal only), name, description, active version, variables parsed from the active template.
- get_prompt(id): Full active template plus full version history. Use to run a prompt or inspect past versions.

USER-FACING LISTS (after list_prompts)
- Do not paste raw JSON. Turn each array entry into a small, uniform block so every prompt looks the same.
- Use this exact pattern for every prompt (same two labels every time; use an em dash — when a field is empty):
  **{name} (version {N})**
  Description: {text or —}
  Variables: {comma-separated names from the JSON array, or — if empty}
- Separate one prompt from the next with one blank line (one empty line between blocks).
- Keep ids out of the visible list unless the user asks; use id only for get_prompt.

WORKFLOW
1. Might a saved prompt fit? → list_prompts; choose by name/description.
2. Running or need exact template? → get_prompt(id); follow template and placeholders unless the user wants otherwise.
3. No fit → continue without Promptify (prompts are edited in the web app).

TRANSPORT
Streamable HTTP at /mcp on the Promptify host.`),
	)
	handlers.RegisterEmbeddedPromptifyMCPTools(mcpServer, h)

	streamable := server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))
	mcpHTTP := handlers.APIKeyBearerMiddleware(database, streamable)
	r.Handle("/mcp", mcpHTTP)
	r.Handle("/mcp/", mcpHTTP)

	log.Println("Server running on :8080 (MCP Streamable HTTP at /mcp)")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
