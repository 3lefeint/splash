#!/bin/sh
# Looks at the dashboard without the game: a made-up endurance race against a throw-away backend.
#
#   scripts/simulate.sh [minutes] [speed]
#   SIM_TRACK="P1 24h Nurburgring" scripts/simulate.sh     plays on a track known to another backend
#
# minutes is the length of the race in game minutes (default 120), speed how much faster than real time it runs
# (default 20, so two hours take six minutes). The backend listens on $SPLASH_LISTEN (default :8052) with the
# in-memory store and ends with the script. With SIM_TRACK the outline and the corner names of that track are taken
# from the backend at $SIM_SOURCE (default http://127.0.0.1:8050) and the cars drive on it; the demo backend records
# the outline again from the first clean lap, so the map appears after about one lap. Needs python3 with the websockets package (PYTHON names another
# interpreter), and Go and Node for the backend.
set -eu
minutes=${1:-120}
speed=${2:-20}
root=$(cd "$(dirname "$0")/.." && pwd)
listen=${SPLASH_LISTEN:-:8052}
source=${SIM_SOURCE:-http://127.0.0.1:8050}
track=${SIM_TRACK:-}
port=${listen##*:}
work=$(mktemp -d)
backend=
trap '[ -z "$backend" ] || kill "$backend" 2>/dev/null || true; rm -rf "$work"' EXIT INT TERM

trackfile=
if [ -n "$track" ]; then
  q=$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$track")
  curl -sf "$source/api/track-map?track=$q" > "$work/map.json" || { echo "no map for '$track' at $source"; exit 1; }
  curl -sf "$source/api/track-corners?track=$q" > "$work/corners.json" || echo '[]' > "$work/corners.json"
  python3 -c 'import json, sys; m = json.load(open(sys.argv[1])); m["corners"] = json.load(open(sys.argv[2])); json.dump(m, open(sys.argv[3], "w"))' \
    "$work/map.json" "$work/corners.json" "$work/track.json"
  trackfile=$work/track.json
fi

echo "building the backend with the frontend ..."
(cd "$root/web" && { [ -d node_modules ] || npm ci; } && npm run build) > "$work/web.log" 2>&1 || { cat "$work/web.log"; exit 1; }
(cd "$root/backend" && GOTOOLCHAIN=local go build -o "$work/splash" ./cmd/splash)
SPLASH_LISTEN=$listen SPLASH_AGENT_TOKEN=dev "$work/splash" > "$work/backend.log" 2>&1 &
backend=$!
until curl -s -o /dev/null "http://127.0.0.1:$port/api/state"; do
  kill -0 "$backend" 2>/dev/null || { cat "$work/backend.log"; exit 1; }
  sleep 1
done
# pit parameters and a stint plan, as a team would have entered them
api="http://127.0.0.1:$port/api"
name=${track:-Demo Ring}
q=$(python3 -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1]))' "$name")
curl -s -o /dev/null -X PUT "$api/params?track=$q&car=%231%20DEMO%20TEAM" -H 'Content-Type: application/json' \
  -d '{"track":"'"$name"'","car":"#1 DEMO TEAM","pit_through_green":22,"pit_through_fcy":10,"refuel_rate":3.4,"tyre_change":8,"driver_swap":6,"parallel_service":false,"fuel_effect":0,"deg_per_lap":0,"save_cost":0,"reserve":3}'
curl -s -o /dev/null -X PUT "$api/stint-plan" -H 'Content-Type: application/json' \
  -d '[{"driver":"Demo Driver A","duration":2400},{"driver":"Demo Driver B","duration":2400},{"driver":"Demo Driver C","duration":2400},{"driver":"Demo Driver A","duration":2400},{"driver":"Demo Driver B","duration":2400},{"driver":"Demo Driver C","duration":2400}]'
[ -z "$trackfile" ] || curl -s -o /dev/null -X PUT "$api/track-corners?track=$q" -H 'Content-Type: application/json' --data-binary @"$work/corners.json"
echo "dashboard: http://localhost:$port/  (Ctrl+C ends the demo)"
(cd "$root/agent/src" && ${PYTHON:-python3} -m splash_agent simulate --url "ws://127.0.0.1:$port/v1/agent/ws" --token dev --minutes "$minutes" --speed "$speed" ${trackfile:+--track-file "$trackfile"})
echo "the race is over; the dashboard keeps the last state until you press Ctrl+C"
wait "$backend"
