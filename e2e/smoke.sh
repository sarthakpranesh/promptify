#!/usr/bin/env bash
# Pre-release smoke checks for Promptify (REST + MCP). No Go test harness required.
#
# Interactive (default):
#   ./e2e/smoke.sh
#
# Non-interactive flags:
#   ./e2e/smoke.sh --url http://localhost:8080 --api-key "pfy_..."
#   ./e2e/smoke.sh --start-docker --api-key "pfy_..."
#   ./e2e/smoke.sh --start-docker --mongo-uri "mongodb+srv://..." --server-env dev --api-key "pfy_..."

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL=""
API_KEY=""
PROMPTIFY_MONGO_DB_URI=""
PROMPTIFY_SERVER_ENV="dev"
WAIT_SECONDS=90
START_DOCKER=0
STOP_DOCKER=0
INTERACTIVE=0

PASS=0
FAIL=0
REST_LIST_JSON=""
MCP_LIST_JSON=""

usage() {
	cat <<'EOF'
Promptify release smoke test

Run with no arguments for interactive mode (requires gum).

Options:
  --url URL           Base URL for an already-running server
  --api-key KEY       API key from /settings (required)
  --start-docker      Build and start via docker compose (http://localhost:8080)
  --mongo-uri URI     MongoDB URI for docker (default: SQLite in container_data)
  --server-env ENV    dev or prod when using Mongo (default: dev)
  --stop-docker       Stop docker compose after checks (with --start-docker)
  -h, --help          Show this help

Generate an API key from /settings after signing in.
EOF
}

parse_args() {
	while [[ $# -gt 0 ]]; do
		case "$1" in
		--url)
			BASE_URL="$2"
			shift 2
			;;
		--api-key)
			API_KEY="$2"
			shift 2
			;;
		--start-docker)
			START_DOCKER=1
			shift
			;;
		--mongo-uri)
			PROMPTIFY_MONGO_DB_URI="$2"
			shift 2
			;;
		--server-env)
			PROMPTIFY_SERVER_ENV="$2"
			shift 2
			;;
		--stop-docker)
			STOP_DOCKER=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "Unknown option: $1" >&2
			usage >&2
			exit 2
			;;
		esac
	done
}

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Missing required command: $1" >&2
		exit 2
	}
}

interactive_config() {
	require_cmd gum

	echo ""
	gum style --bold --foreground 212 "Promptify smoke test"
	gum style --faint "Pre-release checks for REST and MCP"
	echo ""

	local server_mode
	server_mode="$(gum choose --header "How do you want to run Promptify?" \
		"Server already running locally" \
		"Start with docker compose")"

	case "$server_mode" in
	"Server already running locally")
		START_DOCKER=0
		BASE_URL="$(gum input --header "Server URL" --placeholder "http://localhost:8080" --value "http://localhost:8080")"
		if [[ -z "$BASE_URL" ]]; then
			BASE_URL="http://localhost:8080"
		fi
		;;
	"Start with docker compose")
		START_DOCKER=1
		BASE_URL="http://localhost:8080"

		local db_type
		db_type="$(gum choose --header "Database for docker" "SQLite" "MongoDB")"
		case "$db_type" in
		"SQLite")
			PROMPTIFY_MONGO_DB_URI=""
			;;
		"MongoDB")
			PROMPTIFY_MONGO_DB_URI="$(gum input --header "MongoDB URI" --placeholder "mongodb+srv://user:pass@cluster.mongodb.net/")"
			if [[ -z "$PROMPTIFY_MONGO_DB_URI" ]]; then
				echo "MongoDB URI is required." >&2
				exit 2
			fi
			PROMPTIFY_SERVER_ENV="$(gum choose --header "PROMPTIFY_SERVER_ENV" "dev" "prod")"
			;;
		esac
		;;
	esac

	if [[ -z "$BASE_URL" ]]; then
		echo "Server URL is required." >&2
		exit 2
	fi

	API_KEY="$(gum input --header "API key (from /settings)" --placeholder "pfy_..." --password)"
	if [[ -z "$API_KEY" ]]; then
		echo "API key is required." >&2
		exit 2
	fi
}

if [[ $# -eq 0 ]]; then
	INTERACTIVE=1
else
	parse_args "$@"
fi

if [[ -n "$BASE_URL" ]]; then
	BASE_URL="${BASE_URL%/}"
fi

record_pass() {
	PASS=$((PASS + 1))
	echo "  ok  $*" >&2
}

record_fail() {
	FAIL=$((FAIL + 1))
	echo "  FAIL  $*" >&2
}

curl_api() {
	local method="$1"
	local path="$2"
	shift 2
	curl -sS -X "$method" "${BASE_URL}${path}" "$@"
}

http_status() {
	curl_api "$@" -o /dev/null -w '%{http_code}'
}

http_get() {
	curl_api GET "$1"
}

http_get_auth() {
	curl_api GET "$1" -H "Authorization: Bearer ${API_KEY}"
}

wait_for_server() {
	local deadline=$((SECONDS + WAIT_SECONDS))
	echo "Waiting for ${BASE_URL} (up to ${WAIT_SECONDS}s)..."
	while ((SECONDS < deadline)); do
		if curl -sS -o /dev/null --connect-timeout 2 --max-time 5 "${BASE_URL}/" 2>/dev/null; then
			return 0
		fi
		sleep 2
	done
	record_fail "server did not become reachable at ${BASE_URL}"
	return 1
}

start_docker() {
	echo "Starting Promptify via docker compose..."
	if [[ -n "$PROMPTIFY_MONGO_DB_URI" ]]; then
		echo "  database: MongoDB (${PROMPTIFY_SERVER_ENV}_promptify)"
		(
			cd "$ROOT"
			PROMPTIFY_MONGO_DB_URI="$PROMPTIFY_MONGO_DB_URI" PROMPTIFY_SERVER_ENV="$PROMPTIFY_SERVER_ENV" docker compose up --build -d
		)
	else
		echo "  database: SQLite (container_data volume)"
		(
			cd "$ROOT"
			docker compose up --build -d
		)
	fi
	BASE_URL="http://localhost:8080"
	wait_for_server
}

stop_docker() {
	echo "Stopping docker compose..."
	(
		cd "$ROOT"
		docker compose down
	)
}

validate_json() {
	python3 -c 'import json,sys; json.load(sys.stdin)' >/dev/null 2>&1
}

first_prompt_id_from_rest() {
	python3 -c 'import json,sys; d=json.load(sys.stdin); ps=d.get("prompts") or []; print(ps[0]["id"] if ps else "")'
}

first_prompt_id_from_mcp() {
	python3 -c '
import json, sys
body = json.loads(sys.argv[1])
text = ""
result = body.get("result") or {}
for item in result.get("content") or []:
    if item.get("type") == "text" and item.get("text"):
        text = item["text"]
        break
if not text:
    sys.exit(0)
try:
    prompts = json.loads(text)
except json.JSONDecodeError:
    sys.exit(0)
if isinstance(prompts, list) and prompts:
    print(prompts[0].get("id", ""))
' "$1"
}

check_home_page() {
	echo "→ Public home page" >&2
	local body code
	body="$(http_get /)" || {
		record_fail "GET / request failed"
		return
	}
	code="$(http_status GET /)"
	if [[ "$code" != "200" ]]; then
		record_fail "GET / returned HTTP ${code}, want 200"
		return
	fi
	if ! grep -q 'Promptify' <<<"$body"; then
		record_fail "GET / body missing 'Promptify'"
		return
	fi
	if ! grep -q 'id="email"' <<<"$body" || ! grep -q 'id="password"' <<<"$body"; then
		record_fail "GET / missing username/password login fields"
		return
	fi
	record_pass "GET / (HTTP 200, login page)"
}

check_session_status() {
	echo "→ Session status (unauthenticated)" >&2
	local body code
	body="$(http_get /auth/session)" || {
		record_fail "GET /auth/session failed"
		return
	}
	code="$(http_status GET /auth/session)"
	if [[ "$code" != "200" ]]; then
		record_fail "GET /auth/session returned HTTP ${code}, want 200"
		return
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("authenticated") is False' <<<"$body"; then
		record_fail "GET /auth/session JSON missing authenticated:false"
		return
	fi
	record_pass "GET /auth/session → 200, authenticated:false"
}

check_home_page() {
	echo "→ Public home page" >&2
	local body code
	body="$(http_get /)" || {
		record_fail "GET / request failed"
		return
	}
	code="$(http_status GET /)"
	if [[ "$code" != "200" ]]; then
		record_fail "GET / returned HTTP ${code}, want 200"
		return
	fi
	if ! grep -q 'Promptify' <<<"$body"; then
		record_fail "GET / body missing 'Promptify'"
		return
	fi
	record_pass "GET / (HTTP 200, landing page)"
}

check_api_unauthorized() {
	echo "→ REST auth enforcement" >&2
	local code

	code="$(http_status GET /api/prompts)"
	if [[ "$code" != "401" ]]; then
		record_fail "GET /api/prompts without auth returned HTTP ${code}, want 401"
		return
	fi
	record_pass "GET /api/prompts without auth → 401"

	code="$(http_status GET /api/prompts -H "Authorization: Bearer pfy_invalid_smoke_key")"
	if [[ "$code" != "401" ]]; then
		record_fail "GET /api/prompts with invalid API key returned HTTP ${code}, want 401"
		return
	fi
	record_pass "GET /api/prompts with invalid API key → 401"

	code="$(http_status GET /api/prompts/export)"
	if [[ "$code" != "401" ]]; then
		record_fail "GET /api/prompts/export without auth returned HTTP ${code}, want 401"
		return
	fi
	record_pass "GET /api/prompts/export without auth → 401"
}

check_api_error_paths() {
	echo "→ REST error paths" >&2
	local code

	code="$(http_status GET /api/prompts/bad-id -H "Authorization: Bearer ${API_KEY}")"
	if [[ "$code" != "400" ]]; then
		record_fail "GET /api/prompts/bad-id returned HTTP ${code}, want 400"
		return
	fi
	record_pass "GET /api/prompts/bad-id → 400"

	code="$(http_status GET /api/prompts/999999 -H "Authorization: Bearer ${API_KEY}")"
	if [[ "$code" != "404" ]]; then
		record_fail "GET /api/prompts/999999 returned HTTP ${code}, want 404"
		return
	fi
	record_pass "GET /api/prompts/999999 → 404"
}

check_api_export() {
	echo "→ REST export prompts" >&2
	local body code
	body="$(http_get_auth /api/prompts/export)" || {
		record_fail "GET /api/prompts/export failed"
		return
	}
	code="$(http_status GET /api/prompts/export -H "Authorization: Bearer ${API_KEY}")"
	if [[ "$code" != "200" ]]; then
		record_fail "GET /api/prompts/export returned HTTP ${code}, want 200"
		return
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert "exported_at" in d and "prompts" in d and isinstance(d["prompts"], list)' <<<"$body"; then
		record_fail "GET /api/prompts/export JSON missing exported_at/prompts"
		return
	fi
	record_pass "GET /api/prompts/export → 200 with exported_at + prompts[]"
}

check_api_list_prompts() {
	echo "→ REST list prompts" >&2
	local body code
	body="$(http_get_auth /api/prompts)" || {
		record_fail "GET /api/prompts failed"
		return 1
	}
	code="$(http_status GET /api/prompts -H "Authorization: Bearer ${API_KEY}")"
	if [[ "$code" != "200" ]]; then
		record_fail "GET /api/prompts returned HTTP ${code}, want 200 (check API key)"
		return 1
	fi
	if ! validate_json <<<"$body"; then
		record_fail "GET /api/prompts response is not valid JSON"
		return 1
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert "prompts" in d and isinstance(d["prompts"], list)' <<<"$body"; then
		record_fail 'GET /api/prompts JSON missing "prompts" array'
		return 1
	fi
	record_pass "GET /api/prompts → 200 with prompts[]"
	REST_LIST_JSON="$body"
}

check_api_get_prompt() {
	echo "→ REST get prompt by id" >&2
	local prompt_id body code
	prompt_id="$(first_prompt_id_from_rest <<<"$REST_LIST_JSON")"
	if [[ -z "$prompt_id" ]]; then
		echo "  skip  no prompts in account (create one in the UI to cover GET /api/prompts/{id})" >&2
		return
	fi
	body="$(http_get_auth "/api/prompts/${prompt_id}")" || {
		record_fail "GET /api/prompts/${prompt_id} failed"
		return
	}
	code="$(http_status GET "/api/prompts/${prompt_id}" -H "Authorization: Bearer ${API_KEY}")"
	if [[ "$code" != "200" ]]; then
		record_fail "GET /api/prompts/${prompt_id} returned HTTP ${code}, want 200"
		return
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("id") and "name" in d and "versions" in d and "active" in d' <<<"$body"; then
		record_fail "GET /api/prompts/{id} JSON missing id/name/versions/active"
		return
	fi
	record_pass "GET /api/prompts/${prompt_id} → 200 with prompt detail"
}

check_mcp_unauthorized() {
	echo "→ MCP auth enforcement" >&2
	local code
	code="$(http_status POST /mcp \
		-H 'Content-Type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}')"
	if [[ "$code" != "401" ]]; then
		record_fail "POST /mcp without auth returned HTTP ${code}, want 401"
		return
	fi
	record_pass "POST /mcp without auth → 401"
}

mcp_post() {
	local payload="$1"
	curl_api POST /mcp \
		-H "Authorization: Bearer ${API_KEY}" \
		-H "Content-Type: application/json" \
		-d "$payload"
}

check_mcp_ping() {
	echo "→ MCP ping (authenticated)" >&2
	local body
	body="$(mcp_post '{"jsonrpc":"2.0","id":10,"method":"ping","params":{}}')" || {
		record_fail "MCP ping request failed"
		return
	}
	if ! validate_json <<<"$body"; then
		record_fail "MCP ping response is not valid JSON"
		return
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert "result" in d' <<<"$body"; then
		record_fail "MCP ping JSON missing result"
		return
	fi
	record_pass "MCP ping → JSON result"
}

check_mcp_tools_list() {
	echo "→ MCP tools/list" >&2
	local body
	body="$(mcp_post '{"jsonrpc":"2.0","id":11,"method":"tools/list","params":{}}')" || {
		record_fail "MCP tools/list request failed"
		return
	}
	if ! validate_json <<<"$body"; then
		record_fail "MCP tools/list response is not valid JSON"
		return
	fi
	if ! python3 -c '
import json, sys
d = json.load(sys.stdin)
tools = (d.get("result") or {}).get("tools") or []
names = {t.get("name") for t in tools if isinstance(t, dict)}
assert "list_prompts" in names and "get_prompt" in names
' <<<"$body"; then
		record_fail "MCP tools/list missing list_prompts or get_prompt"
		return
	fi
	record_pass "MCP tools/list → list_prompts + get_prompt registered"
}

check_mcp_initialize() {
	echo "→ MCP initialize" >&2
	local body
	body="$(mcp_post '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"promptify-smoke","version":"1.0.0"}}}')" || {
		record_fail "MCP initialize request failed"
		return
	}
	if ! validate_json <<<"$body"; then
		record_fail "MCP initialize response is not valid JSON"
		return
	fi
	if ! python3 -c 'import json,sys; d=json.load(sys.stdin); assert "result" in d' <<<"$body"; then
		record_fail "MCP initialize JSON missing result"
		return
	fi
	record_pass "MCP initialize → JSON result"
}

check_mcp_list_prompts() {
	echo "→ MCP tools/call list_prompts" >&2
	local body
	body="$(mcp_post '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_prompts","arguments":{}}}')" || {
		record_fail "MCP list_prompts request failed"
		return 1
	}
	if ! validate_json <<<"$body"; then
		record_fail "MCP list_prompts response is not valid JSON"
		return 1
	fi
	if python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("result") else 1)' <<<"$body"; then
		record_pass "MCP list_prompts → result payload"
	else
		record_fail "MCP list_prompts missing result (response: ${body})"
		return 1
	fi
	MCP_LIST_JSON="$body"
}

check_mcp_get_prompt() {
	echo "→ MCP tools/call get_prompt" >&2
	local prompt_id body payload
	prompt_id="$(first_prompt_id_from_mcp "$MCP_LIST_JSON")"
	if [[ -z "$prompt_id" ]]; then
		echo "  skip  no prompts for get_prompt (create one in the UI to cover MCP get_prompt)" >&2
		return
	fi
	payload="$(python3 -c 'import json,sys; print(json.dumps({"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_prompt","arguments":{"id":sys.argv[1]}}}))' "$prompt_id")"
	body="$(mcp_post "$payload")" || {
		record_fail "MCP get_prompt request failed"
		return
	}
	if ! printf '%s' "$body" | python3 -c '
import json, sys
d = json.load(sys.stdin)
result = d.get("result") or {}
content = result.get("content") or []
text = next((c.get("text") for c in content if c.get("type") == "text" and c.get("text")), "")
detail = json.loads(text)
assert detail.get("id") and "name" in detail and "versions" in detail
'; then
		record_fail "MCP get_prompt result missing id/name/versions in tool text"
		return
	fi
	record_pass "MCP get_prompt(id=${prompt_id}) → result with prompt detail"
}

validate_cli_config() {
	if [[ -z "$API_KEY" ]]; then
		echo "--api-key is required (generate one from /settings)." >&2
		exit 2
	fi

	if [[ "$START_DOCKER" -eq 1 ]]; then
		if [[ -z "$BASE_URL" ]]; then
			BASE_URL="http://localhost:8080"
		fi
		return
	fi

	if [[ -z "$BASE_URL" ]]; then
		echo "--url is required when not using --start-docker." >&2
		exit 2
	fi
}

main() {
	require_cmd curl
	require_cmd python3

	if [[ "$INTERACTIVE" -eq 1 ]]; then
		interactive_config
	else
		validate_cli_config
	fi

	if [[ "$START_DOCKER" -eq 1 ]]; then
		start_docker
		if [[ "$STOP_DOCKER" -eq 1 ]]; then
			trap stop_docker EXIT
		fi
	else
		wait_for_server
	fi

	echo ""
	echo "Promptify smoke test → ${BASE_URL}"
	echo ""

	check_home_page
	check_session_status
	check_api_unauthorized
	check_api_error_paths
	if check_api_list_prompts; then
		check_api_get_prompt
	fi
	check_api_export
	check_mcp_unauthorized
	check_mcp_initialize
	check_mcp_ping
	check_mcp_tools_list
	if check_mcp_list_prompts; then
		check_mcp_get_prompt
	fi

	echo ""
	echo "Results: ${PASS} passed, ${FAIL} failed"
	if [[ "$FAIL" -gt 0 ]]; then
		exit 1
	fi
	echo "Smoke test passed."
}

main "$@"
