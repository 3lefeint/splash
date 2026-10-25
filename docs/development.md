# Development loop

No image, CI run or exe is needed to try a change.

## Backend and frontend

    scripts/dev.sh

builds the frontend, embeds it and runs the backend on `:8080` with the in-memory store (data is lost on exit). Set
`SPLASH_DATABASE_URL` to use Postgres, `SPLASH_AGENT_TOKEN` to change the agent token (default `dev`) and
`SPLASH_LISTEN` for another port. For frontend work, `npm run dev` in `web/` serves it with hot reload and proxies the
API to the backend.

## Agent on the race PC (from source)

Install Python 3.11 or newer once, then in the folder that holds a copy of `agent/`:

    py -m pip install websockets
    set PYTHONPATH=src
    py -m splash_agent run --config agent.toml

A change to the agent is a file copy. `url` in `agent.toml` points to the backend, for example
`ws://<host>:8080/v1/agent/ws` with the token from `SPLASH_AGENT_TOKEN`. Start the game and load a session first, then
the agent.

## Debugging with a raw capture

Set `raw_file = "capture.ndjson.gz"` in the `[agent]` section and drive a short session (about 230 MB per hour
with a few cars, more with a full field). Copy the file to the
development machine and run it through the agent logic offline:

    py -m splash_agent replay-raw capture.ndjson.gz --config agent.toml --out replay-out.ndjson

The output has the same messages the agent would have sent. A fix in the agent logic can be checked against the same
drive as often as needed without the game.

## Looking at the dashboard without the game

    scripts/demo.sh capture.ndjson.gz [speed]

converts a raw capture with the current agent logic, plays it into a throw-away backend (in-memory store, port 8052 or
`SPLASH_LISTEN`) and prints the address of the dashboard. A change to the frontend, the strategy or the agent logic can
so be looked at on a real drive without the game or the race PC. `PYTHON` names an interpreter that has the
`websockets` package.

## A made-up race

    scripts/simulate.sh [minutes] [speed]

starts a throw-away backend like `scripts/demo.sh` and plays an invented endurance race into it: a field of cars on an
invented track with pit stops, driver changes, a full course yellow, rain, a penalty, damage and a car off the track.
The driven car answers pit setups and pit requests. It also fills in pit parameters and a stint plan. Use it to look
at every part of the dashboard, or `python -m splash_agent simulate --url ... --token ...` against another backend.
