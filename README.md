# Promptify

Self-hostable prompt bank for individuals and teams. Create, version, and reuse prompts from a web UI, REST API, or MCP-compatible clients (for example Cursor).

Each account has its own prompts, settings, and API keys. An admin account can manage users and server options.

## Features

- Web UI to create and version prompts (with `{{variables}}` in templates)
- Multi-user accounts; each user’s prompts stay private to them
- Optional self-registration (admin can turn this on or off)
- API keys for REST and MCP access
- SQLite by default (single file), or MongoDB if you prefer it
- Docker Compose for a one-command deploy
- Tags: share a prompt with everyone on the server by adding the `public` tag; group prompts by use case or any custom tags you define `(upcoming)`
- Full-text search and filter by tag in the UI and MCP `(upcoming)`
- Allow admins to reset user passowrds `(upcoming)`

## Quick start (Docker)

**Requirements:** Docker and Docker Compose.

```bash
cp .env.example .env
# Set a strong secret, e.g.:
#   openssl rand -hex 32
# Put the value in PROMPTIFY_SESSION_SECRET inside .env

./build-and-start.sh
# same as: docker compose up --build -d
```

Open [http://localhost:8080](http://localhost:8080).

Default admin (change these via env before first launch in production):

| Setting  | Default               |
| -------- | --------------------- |
| Email    | `admin@promptify.com` |
| Password | `admin`               |

Data persists under `./container_data`. Logs: `docker compose logs -f`.

### Stop / update

```bash
docker compose down
docker compose up --build -d
```

## Configuration

Copy `.env.example` → `.env`. Compose loads this file automatically.

| Variable                   | Required | Description                                                                 |
| -------------------------- | -------- | --------------------------------------------------------------------------- |
| `PROMPTIFY_SESSION_SECRET` | Yes      | Long random string for signed login cookies. Keep it stable across restarts |
| `PROMPTIFY_ADMIN_EMAIL`    | No       | Admin email (default `admin@promptify.com`)                                 |
| `PROMPTIFY_ADMIN_PASSWORD` | No       | Admin password (default `admin`)                                            |
| `PROMPTIFY_LOG_PATH`       | No       | Path for the process log (default `data/promptify.log`)                     |
| `PROMPTIFY_MONGO_DB_URI`             | No       | If set, use MongoDB instead of SQLite                                       |
| `PROMPTIFY_SERVER_ENV`               | No       | With Mongo: `dev` or `prod` (selects DB name). Default `dev`                |
| `PROMPTIFY_SQLITE_PATH`    | No       | SQLite file path (default `data/database.db`)                               |

### Admin account

On first start, Promptify creates the admin user from `PROMPTIFY_ADMIN_EMAIL` / `PROMPTIFY_ADMIN_PASSWORD`.

Later starts keep the same admin identity. If you change those env values and restart, the admin email/password update to match.

**Production tip:** set a strong admin password and a unique `PROMPTIFY_SESSION_SECRET` before the first launch.

## Using Promptify

### Web app

1. Sign in at `/auth/login` (or register if the admin enabled registration).
2. Create and edit prompts on the dashboard.
3. Open **Settings** to generate an API key for tools and automations.
4. Admins see a **Server** console to:
   - Toggle self-registration
   - Add or remove users (the admin account cannot be removed)
   - Download the process log

### REST API

After you create an API key in Settings:

```bash
curl -H "Authorization: Bearer <API_KEY>" \
  http://localhost:8080/api/prompts
```

### MCP (Cursor and other clients)

MCP endpoint: `http://localhost:8080/mcp` (or your public URL + `/mcp`).

Example Cursor MCP config:

```json
{
  "mcpServers": {
    "promptify": {
      "url": "http://localhost:8080/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_PROMPTIFY_API_KEY"
      }
    }
  }
}
```

## Troubleshooting

| Problem                         | What to try                                                                                                      |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Cannot sign in                  | Defaults are `admin@promptify.com` / `admin` unless you changed env. Login expects a valid email address         |
| `401` on `/api/*` or `/mcp`     | Use `Authorization: Bearer <API_KEY>` from Settings for the same user                                            |
| Logged out after container restart | Keep `PROMPTIFY_SESSION_SECRET` unchanged so existing cookies still verify                                    |
| Lost admin access               | Set `PROMPTIFY_ADMIN_EMAIL` / `PROMPTIFY_ADMIN_PASSWORD` and restart; credentials sync onto the admin account    |

## Run without Docker

If you prefer running the binary/source directly (for example during contribution work), see [CONTRIBUTING.md](CONTRIBUTING.md).

```bash
./run-local.sh
# or: PROMPTIFY_SESSION_SECRET="..." go run ./cmd/server
```

Then open [http://localhost:8080](http://localhost:8080).

## Contributing

Want to change code, fix bugs, or improve docs? See [CONTRIBUTING.md](CONTRIBUTING.md) for the tech stack, project layout, and local development workflow.

<p align="left">
  With love from India 🇮🇳
</p>
