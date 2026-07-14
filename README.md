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

## Quick start

**Requirements:** Docker (Compose optional). Image: [`sarthakpranesh/promptify`](https://hub.docker.com/r/sarthakpranesh/promptify). This is for a quick try; for daily use, read [Configuration](#configuration).

With docker run:
```bash
docker run -d \
  --name promptify \
  --restart unless-stopped \
  -p 8080:8080 \
  sarthakpranesh/promptify:latest
```

With compose
```bash
services:
  promptify:
    image: sarthakpranesh/promptify:latest
    container_name: promptify
    restart: unless-stopped
    ports:
      - "8080:8080"
    # Uncomment when you device to daily drive
    # env_file:
    #   - .env
    # volumes:
    #   - ./container_data:/app/data
```

Open [http://localhost:8080](http://localhost:8080). The default admin credentials are `admin@promptify.com` and `admin`.

## Configuration

Refer [.env.example](.env.example) and [docker-compose.yml](./docker-compose.yml). Just copy and modify values as per your needs.

Volumes
Data persists under `./container_data` and the compose file already maps it to a local folder.

ENV Reference Table
| Variable                   | Required | Description                                                                 |
| -------------------------- | -------- | --------------------------------------------------------------------------- |
| `PROMPTIFY_SESSION_SECRET` | Yes      | Long random string for signed login cookies. Keep it stable across restarts, unless you want to logout all your users. |
| `PROMPTIFY_ADMIN_EMAIL`    | No       | Admin email (default `admin@promptify.com`) , automatically gets updated to latest values from ENV on container restarts                                |
| `PROMPTIFY_ADMIN_PASSWORD` | No       | Admin password (default `admin`), automatically gets updated to latest values from ENV on container restarts                                            |
| `PROMPTIFY_LOG_PATH`       | No       | Path for the process log (default `data/promptify.log`)                     |
| `PROMPTIFY_MONGO_DB_URI`             | No       | If set, use MongoDB instead of SQLite                                       |
| `PROMPTIFY_SERVER_ENV`               | No       | With Mongo: `dev` or `prod` (selects DB name). Default `dev`                |
| `PROMPTIFY_SQLITE_PATH`    | No       | SQLite file path (default `data/database.db`)                               |

## Using Promptify

### Web app

For users
1. Sign in at `/` (or register if the admin enabled registration).
2. Create and edit prompts on the dashboard.
3. Open Settings to generate an API key for tools and automations.

For admins
Everything a non admin user can do and below
1. Open the Server console to:
   - Toggle self-registration
   - Add or remove users (the admin account cannot be removed)
   - Download the process log

### REST API

After you create an API key in Settings:

```bash
curl -H "Authorization: Bearer <API_KEY>" \
  http://<your promptify url>:8080/api/prompts
```

### MCP

MCP endpoint: `http://<your promptify url>:8080/mcp`.

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

## Contributing

Want to change code, fix bugs, or improve docs? See [CONTRIBUTING.md](CONTRIBUTING.md) for the tech stack, project layout, and local development workflow.

<p align="left">
  With love from India 🇮🇳
</p>
