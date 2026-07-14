#!/usr/bin/env bash
# Local dev server for Promptify.
# Clears known config vars, loads .env (if present), then starts the server.
# Commenting a key in .env therefore disables it even if the parent shell exported it.
#
# Usage:
#   ./run-local.sh

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

# Drop inherited config so .env (or absence there) is authoritative.
unset \
	PROMPTIFY_SESSION_SECRET \
	PROMPTIFY_ADMIN_EMAIL \
	PROMPTIFY_ADMIN_PASSWORD \
	PROMPTIFY_LOG_PATH \
	PROMPTIFY_SQLITE_PATH \
	PROMPTIFY_MONGO_DB_URI \
	PROMPTIFY_SERVER_ENV \
	|| true

load_dotenv() {
	local f="$1"
	[[ -f "$f" ]] || return 0
	set -a
	# shellcheck disable=SC1090
	source "$f"
	set +a
}

load_dotenv .env

if ! command -v go >/dev/null 2>&1; then
	echo "Missing required command: go" >&2
	exit 2
fi

if [[ ! -f .env ]]; then
	echo "Warning: .env not found; known config vars are unset." >&2
fi

exec go run ./cmd/server
