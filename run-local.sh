#!/usr/bin/env bash
# Interactive local dev server for Promptify (SQLite or MongoDB).
# Loads .env (if present) for defaults; still prompts for every value.
#
# Usage:
#   ./run-local.sh

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

load_dotenv() {
	local f="$1"
	[[ -f "$f" ]] || return 0
	set -a
	# shellcheck disable=SC1090
	source "$f"
	set +a
}

load_dotenv .env

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required command: $1" >&2
		exit 2
	}
}

require_cmd gum
require_cmd go

random_secret() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 24
		return
	fi
	head -c 32 /dev/urandom | base64 | tr -dc 'a-zA-Z0-9' | head -c 32
	echo
}

db_choice="$(gum choose --header "Database" "SQLite" "MongoDB")"

default_secret="${PROMPTIFY_SESSION_SECRET:-}"
if [[ -z "$default_secret" || "$default_secret" == "replace-with-long-random-string" ]]; then
	default_secret="$(random_secret)"
fi
session_secret="$(gum input \
	--header "PROMPTIFY_SESSION_SECRET" \
	--placeholder "session signing secret" \
	--value "$default_secret")"
[[ -z "$session_secret" ]] && session_secret="$default_secret"

default_admin_email="${PROMPTIFY_ADMIN_EMAIL:-admin@promptify.com}"
admin_email="$(gum input \
	--header "PROMPTIFY_ADMIN_EMAIL (default: admin@promptify.com)" \
	--placeholder "admin@promptify.com" \
	--value "$default_admin_email")"
[[ -z "$admin_email" ]] && admin_email="admin@promptify.com"

default_admin_password="${PROMPTIFY_ADMIN_PASSWORD:-admin}"
admin_password="$(gum input \
	--header "PROMPTIFY_ADMIN_PASSWORD (default: admin)" \
	--placeholder "admin" \
	--password \
	--value "$default_admin_password")"
[[ -z "$admin_password" ]] && admin_password="admin"

common_env=(
	PROMPTIFY_SESSION_SECRET="$session_secret"
	PROMPTIFY_ADMIN_EMAIL="$admin_email"
	PROMPTIFY_ADMIN_PASSWORD="$admin_password"
)

case "$db_choice" in
"SQLite")
	exec env "${common_env[@]}" go run ./cmd/server
	;;
"MongoDB")
	if [[ -z "${MONGO_DB_URI:-}" ]]; then
		MONGO_DB_URI="$(gum input --header "MONGO_DB_URI" --placeholder "mongodb://...")"
	else
		MONGO_DB_URI="$(gum input \
			--header "MONGO_DB_URI" \
			--placeholder "mongodb://..." \
			--value "$MONGO_DB_URI")"
	fi
	if [[ -z "$MONGO_DB_URI" ]]; then
		echo "MONGO_DB_URI is required." >&2
		exit 2
	fi
	server_env_default="${SERVER_ENV:-dev}"
	if [[ "$server_env_default" != "dev" && "$server_env_default" != "prod" ]]; then
		server_env_default="dev"
	fi
	server_env="$(gum choose --header "SERVER_ENV" --selected "$server_env_default" "dev" "prod")"
	exec env \
		"${common_env[@]}" \
		MONGO_DB_URI="$MONGO_DB_URI" \
		SERVER_ENV="$server_env" \
		go run ./cmd/server
	;;
esac
