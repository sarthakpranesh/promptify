# Project TODO

## Done

### Core app
- [x] Go server with chi router (`cmd/server/main.go`)
- [x] Server-rendered web UI (dashboard, create/edit prompt, settings)
- [x] Prompt CRUD with version history (create version on template change, one active version)
- [x] Prompt variables parsed from `{{placeholders}}` in active template
- [x] Refactored prompts to use numeric IDs (not names) in routes/API

### Auth & users
- [x] Single admin login (email + password from env)
- [x] Admin credentials bcrypt-hashed in `admin` table; re-sync on env change
- [x] Signed session cookies for browser routes (`HttpOnly`, `SameSite=Lax`)
- [x] Require `PROMPTIFY_SESSION_SECRET` at startup (no silent dev fallback)
- [x] Per-user data scoping by admin username
- [x] Legacy prompt migration (`userId='legacy'` → first bootstrapped admin)
- [x] API key generation from `/settings` (hashed at rest)
- [x] Bearer auth for REST/MCP (session cookie or API key)
- [x] Session status endpoint (`GET /auth/session`)
- [x] Account deletion (`POST /api/account/delete`)

### API & MCP
- [x] REST: list/get prompts, export all prompts (`/api/prompts`, `/api/prompts/{id}`, `/api/prompts/export`)
- [x] MCP Streamable HTTP at `/mcp` (stateless)
- [x] MCP tools: `list_prompts`, `get_prompt(id)`
- [x] MCP server instructions for user-facing prompt list formatting

### Database
- [x] `Store` interface for prompt/user/auth operations (`internal/store/store.go`)
- [x] SQLite implementation + schema migrations (`internal/store/sqlite`)
- [x] MongoDB implementation + indexes (`internal/store/mongo`)
- [x] Backend selection via env: SQLite default, Mongo when `PROMPTIFY_MONGO_DB_URI` is set (`internal/db/db.go`)
- [x] Handlers/middleware depend on `store.Store` (not `*sql.DB`)

### Docker & ops
- [x] Dockerfile + docker-compose (SQLite volume at `./container_data`)
- [x] Multi-arch Docker build/push script (`build-and-push.sh`)
- [x] Local build-and-start helper (`build-and-start.sh`)
- [x] Pre-release smoke test script (`e2e/smoke.sh`; REST + MCP, optional Docker/Mongo)

### Docs
- [x] README: local setup, auth model, API/MCP checks, SQLite + Mongo config, troubleshooting

## Pending

- [ ] Getting production ready
  - [ ] Harden auth cookie for production:
    - [x] Set `Secure` on session cookies (with trusted proxy awareness if needed).
  - [ ] Add CSRF protection for state-changing cookie-auth endpoints.
  - [ ] Harden HTTP server:
    - [ ] Replace bare `ListenAndServe` with `http.Server` timeouts and graceful shutdown.
