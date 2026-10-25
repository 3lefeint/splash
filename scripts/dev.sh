#!/usr/bin/env bash
# Builds the frontend and runs the backend from source for local testing.
# Uses the in-memory store unless SPLASH_DATABASE_URL is set. Agents connect to ws://<host>:8080/v1/agent/ws.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"

cd "$root/web"
[ -d node_modules ] || npm ci
npm run build

cd "$root/backend"
export SPLASH_LISTEN="${SPLASH_LISTEN:-:8080}"
export SPLASH_AGENT_TOKEN="${SPLASH_AGENT_TOKEN:-dev}"
GOTOOLCHAIN=local exec go run ./cmd/splash
