#!/usr/bin/env bash
set -euo pipefail

if [[ ! -f .env ]]; then
	echo "Missing .env — copy .env.example and set PROMPTIFY_SESSION_SECRET." >&2
	exit 2
fi

docker compose -f docker-compose-local.yml up --build -d

echo "Promptify is running at http://localhost:8080"
echo "Logs: docker compose logs -f"
