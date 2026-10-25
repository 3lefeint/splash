#!/bin/sh
# Looks at the dashboard without the game: plays a raw capture through the agent logic into a throw-away backend.
#
#   scripts/demo.sh capture.ndjson.gz [speed]
#
# The capture is written by the agent with raw_file set (see docs/development.md). speed is the playback speed, 1 is
# real time (default 4). The backend listens on $SPLASH_LISTEN (default :8052) with the in-memory store and ends with
# the script. Needs python3 with the websockets package for the agent logic (PYTHON names another interpreter), and
# Go and Node for the backend.
set -eu
capture=${1:?usage: scripts/demo.sh capture.ndjson.gz [speed]}
speed=${2:-4}
root=$(cd "$(dirname "$0")/.." && pwd)
listen=${SPLASH_LISTEN:-:8052}
work=$(mktemp -d)
backend=
trap '[ -z "$backend" ] || kill "$backend" 2>/dev/null || true; rm -rf "$work"' EXIT INT TERM

cat > "$work/agent.toml" <<TOML
[server]
url = "ws://127.0.0.1${listen}/v1/agent/ws"
token = "dev"

[agent]
id = "demo"
role = "driver"
spool_dir = "$work/spool"

[poll]
telemetry_hz = 5
scoring_hz = 2
TOML

echo "converting the capture with the agent logic ..."
(cd "$root/agent/src" && ${PYTHON:-python3} -m splash_agent replay-raw "$capture" --config "$work/agent.toml" --out "$work/demo.ndjson")

echo "building the replay tool ..."
(cd "$root/backend" && GOTOOLCHAIN=local go build -o "$work/splash-replay" ./cmd/splash-replay)

echo "building the backend with the frontend ..."
(cd "$root/web" && { [ -d node_modules ] || npm ci; } && npm run build) > "$work/web.log" 2>&1 || { cat "$work/web.log"; exit 1; }
(cd "$root/backend" && GOTOOLCHAIN=local go build -o "$work/splash" ./cmd/splash)
SPLASH_LISTEN=$listen SPLASH_AGENT_TOKEN=dev "$work/splash" > "$work/backend.log" 2>&1 &
backend=$!
port=${listen##*:}
until curl -s -o /dev/null "http://127.0.0.1:$port/api/state"; do
  kill -0 "$backend" 2>/dev/null || { cat "$work/backend.log"; exit 1; }
  sleep 1
done
echo "dashboard: http://localhost:$port/  (Ctrl+C ends the demo)"
"$work/splash-replay" -file "$work/demo.ndjson" -speed "$speed" -url "ws://127.0.0.1:$port/v1/agent/ws" -token dev
echo "replay finished; the dashboard keeps the last state until you press Ctrl+C"
wait "$backend"
