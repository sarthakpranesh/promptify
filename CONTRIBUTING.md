# Contributing to Promptify

Thanks for helping improve Promptify. This guide covers development setup, architecture, and how to land changes. For install and day-to-day use, see [README.md](README.md).

## Ways to contribute

- Bug reports and feature ideas (clear repro steps help a lot)
- Documentation improvements
- Code: fixes, tests, small focused features

Prefer small, reviewable changes over large unrelated refactors.

## Development setup

**Requirements:**

- Go `1.23+`
- Optional: [gum](https://github.com/charmbracelet/gum) for interactive helpers (`./run-local.sh`, `./e2e/smoke.sh`)
- Optional: Docker / Docker Compose for containerized runs and smoke tests

```bash
cp .env.example .env
# set PROMPTIFY_SESSION_SECRET (long random string)

./run-local.sh
# or:
PROMPTIFY_SESSION_SECRET="dev-secret" go run ./cmd/server
```

Server listens on `http://localhost:8080`. Default admin: `admin@promptify.com` / `admin`.

Compose (same as end users):

```bash
./build-and-start.sh
```

## Tech stack

- Go `1.23`
- Router: [chi](https://github.com/go-chi/chi)
- Storage: SQLite (default, `data/database.db`) or MongoDB when `MONGO_DB_URI` is set
- Auth: email + password; sole admin UID stored separately from user credentials
- MCP: Streamable HTTP at `/mcp`
- UI: server-rendered HTML under `web/templates`

## Project layout

| Path                         | Role                                              |
| ---------------------------- | ------------------------------------------------- |
| `cmd/server/main.go`         | Bootstrap, routes, middleware, MCP wiring         |
| `internal/auth`              | Admin bootstrap from environment (create-or-sync) |
| `internal/store`             | `Store` interface (swap backends)                 |
| `internal/store/sqlite`      | SQLite implementation + migrations                |
| `internal/store/mongo`       | MongoDB implementation                            |
| `internal/db`                | Opens SQLite by default, or Mongo when URI is set |
| `internal/handlers`          | HTML, REST, auth middleware, MCP handlers         |
| `internal/applog`            | stderr + `promptify.log` tee                      |
| `web/templates`              | Server-rendered pages                             |
| `e2e/smoke.sh`               | Pre-release REST + MCP smoke checks               |

## Architecture notes

### Auth model

- Browser routes use a signed session cookie after `/auth/login` (or `/auth/register` when enabled).
- API + MCP accept:
  - Session cookie (browser), or
  - Bearer user API key from `/settings` (hashed at rest).
- Prompt data is scoped to each user’s UID.
- **Server** console (`/server`) is admin-only (`session uid == admin.uid`).

### Admin bootstrap (hybrid)

- The `admin` record stores the sole admin **UID** plus server **settings** JSON (for example `allow_registration`). It does not store a password.
- **First launch** (empty admin): create a user from `PROMPTIFY_ADMIN_*` with a stable UID, write that UID into `admin`.
- **Later launches**: keep the UID; if email/password differ from env, update the `users` row. UID never changes.
- Change admin email/password by updating env and restarting.

### Storage selection

```bash
# SQLite (default); optional path override
export PROMPTIFY_SQLITE_PATH="data/database.db"

# MongoDB
export MONGO_DB_URI="mongodb://..."
export SERVER_ENV=dev   # selects dev_promptify or prod_promptify
```

Handlers and middleware depend on `store.Store`, not a concrete DB driver.

## Testing

```bash
# Against a running server (interactive; needs gum + API key from /settings)
./e2e/smoke.sh

# Non-interactive examples
./e2e/smoke.sh --url http://localhost:8080 --api-key "pfy_..."
./e2e/smoke.sh --start-docker --api-key "pfy_..."
./e2e/smoke.sh --start-docker --mongo-uri "mongodb://..." --server-env dev --api-key "pfy_..."
```

Keep the smoke script green for changes that touch REST, MCP, auth, or storage backends.

## Pull requests

1. Branch from the default branch; keep the diff focused on one concern.
2. Match existing Go style and package boundaries (`store` interface, handlers, etc.).
3. Update README only for user-facing behavior; keep contributor/tech detail here.
4. Describe **why** in the PR body; note how you tested (smoke flags, manual UI steps).

## Release / image notes

- Local image via Compose: see `Dockerfile` and `docker-compose.yml`.
- Multi-arch Hub publish: `./build-and-push.sh` (adjust tag/`PLATFORMS` as needed).

## Questions

Open an issue if something in this guide is wrong or missing. Prefer fixing the docs in the same PR when you discover gaps while building.
