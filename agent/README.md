# Agent

Windows agent (Python) that runs on each race PC. It reads the rF2 Shared Memory Map Plugin buffers read-only at a low
poll rate and streams them to the backend over WebSocket.

- Scoring (whole field) is read at `scoring_hz` (default 2), telemetry of the car this PC drives at `telemetry_hz`
  (default 5). Both are limited to 10 Hz.
- Telemetry is sent whenever this PC is in a car (`mInRealtime`), so the same agent works for the driver and the
  spotter PC at the driver change.
- The agent derives laps and pit events from the scoring updates of all cars. They are durable: written to a spool file
  and resent after a connection drop until the backend acknowledged them. Scoring and telemetry are live samples and
  are never resent.
- The session id is built from track, session number, event start time and server name, so both PCs get the same id.

The agent logs a status line every 30 seconds: session, number of cars, frames read, backend connection, queued
messages and a note when the game time does not move (pause, menu or game closed).

Message format: see [`../docs/wire-format.md`](../docs/wire-format.md).

## Setup (Windows)

1. Enable `rFactor2SharedMemoryMapPlugin64.dll` in rFactor 2.
2. `pip install .` in this directory (Python 3.11 or newer).
3. Copy `agent.example.toml` to `agent.toml` and set the server URL, the token and an `id` for this PC.
4. `splash-agent run --config agent.toml`

With the Cloudflare Access in front of the backend, give the agent its own service token in `agent.toml`
(`server.cf_client_id` and `server.cf_client_secret`, both or neither); the handshake then also sends
`CF-Access-Client-Id` and `CF-Access-Client-Secret`. The agent never logs them. Do not run it with debug logging for the
`websockets` library: that prints every handshake header, the tokens included.

The WebSocket reconnects by itself after any loss, also one by Cloudflare or the tunnel (a cut before the HTTP answer
included): the pause starts at one second, doubles up to 30 s, carries jitter and starts over after a connection that
worked. Then the spool backlog is resent from the last sequence number the backend acknowledged. Every attempt writes one
log line with the reason and the pause; no token appears in it.

`splash-agent probe` prints the telemetry of all vehicles once (fuel, capacity, tyre wear). Use it in a session with
other cars to see whether the plugin delivers fuel and tyre data for remote vehicles.

With `record_file` set, every message is also written to an NDJSON file (about 2-3 GB per 24 hours) that
`splash-replay` can play back.

## Yellow flags

The agent passes on the three sector flags it already reads and, per vehicle, `sector`, `flag` (only when not 0) and
`under_yellow` (only when true), taken from the scoring buffer it reads anyway. The sector is sent for our own car always
(11 bytes per message) and for the other cars only while a sector flag is up (about 11 bytes per car, 440 bytes for 40
cars). Nothing is read, polled or opened additionally. The backend must be updated first: it refuses unknown fields.

## Logging

One line per event goes to the console (stderr) of the command window, with a timestamp. The level is `log.level` in
`agent.toml` (`DEBUG`, `INFO`, `WARNING`, `ERROR`, default `INFO`); `--log-level LEVEL` on the command line overrides it
(`splash-agent --log-level DEBUG run` or `splash-agent run --log-level DEBUG`). There is no log file yet: when one is
wanted, return a file handler from `extra_handlers()` in `logsetup.py`.

No line contains the Splash token, the Cloudflare secret or a URL with credentials: every line passes a filter that
blanks registered secrets and lookalikes, the start line shows only the host and port of the backend and whether a
Cloudflare client id is set. The `websockets` library prints all handshake headers, tokens included, at DEBUG; its logger
is therefore kept at WARNING whatever the agent level is.

| level | message | when |
|---|---|---|
| INFO | `splash-agent <version> starting: server=<scheme://host:port> agent=<id> role=<role> cloudflare_client_id=<set/not set> log_level=<level> spool=<dir>` | start |
| INFO | `waiting for rFactor 2 and the shared memory plugin (<why>)`, `plugin buffers found` | the plugin buffers are missing, then there (once per change) |
| INFO | `pit menu buffer not available, trying again every 10 s`, `pit menu buffer opened` | optional buffer |
| INFO | `session changed: <new> (was <old>)` | the game session changed |
| INFO | `connected, backend is at seq <n>, <m> messages to resend` | connected |
| INFO | `backlog resent: <n> messages up to seq <s>, all acknowledged` | the spool was resent and acknowledged |
| INFO | `status: <session>, <cars> cars, scoring <n>, telemetry <n>, backend connected/not connected, pending <n>` | every 30 s |
| INFO | `shutting down (interrupted)`, `stopped` | end |
| WARNING | `<reason>; reconnecting in <s> s (attempt <n>)` | the connection was lost; one line per attempt, with the next delay |
| WARNING | `<n> snapshots of the plugin buffers could not be read consistently in the last 30 s` | failed reads |
| WARNING | `<n> messages are waiting for the backend to acknowledge them (spool grows while it is away)` | 500 or more pending |
| ERROR | `backend rejected the connection (HTTP <code>); check the tokens; reconnecting in <s> s (attempt <n>)` | 401/403 and the like |
| ERROR | `unrecoverable failure, the agent stops` (with traceback) | anything uncaught |
| DEBUG | `handshake ok (HTTP 101)`, `sent <type> seq <n>, <bytes> bytes` | every message sent |
| DEBUG | `samples per second: scoring <x>, telemetry <y>` | every 30 s |
| DEBUG | `spool: <type> seq <n>, <m> pending`, `raw scoring buffer: ...`, `raw telemetry buffer: ...` | spool and buffer details |

The agent does not measure a clock offset to the backend, so there is no line for it.

Example lines:

    2026-10-28 13:21:03,402 INFO splash_agent.link: connected, backend is at seq 0, 2 messages to resend
    2026-10-28 13:21:03,402 WARNING splash_agent.link: connection lost: received 1000 (OK); then sent 1000 (OK); reconnecting in 0.0 s (attempt 1)
    2026-10-28 13:21:03,668 ERROR splash_agent.link: backend rejected the connection (HTTP 401); check the tokens; reconnecting in 0.0 s (attempt 1)
    2026-10-28 13:21:03,455 DEBUG splash_agent.link: sent lap seq 1, 177 bytes

## Development

    pip install -e ".[dev]"
    pytest

The tests run on any OS: plugin buffers are built in memory and the connection is tested against a stand-in for the
backend socket. Only `reader.open_windows_buffer` needs Windows and a running game; it has not been run against a real
game yet.

The ctypes layout in `src/splash_agent/structs.py` was compared field by field with the plugin header (pack 4, 32-bit
`long`, 64-bit build); `tests/test_structs.py` pins the sizes and offsets the agent relies on.
