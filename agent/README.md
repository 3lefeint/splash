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

Message format: see [`../docs/wire-format.md`](../docs/wire-format.md).

## Setup (Windows)

1. Enable `rFactor2SharedMemoryMapPlugin64.dll` in rFactor 2.
2. `pip install .` in this directory (Python 3.11 or newer).
3. Copy `agent.example.toml` to `agent.toml` and set the server URL, the token and an `id` for this PC.
4. `splash-agent run --config agent.toml`

`splash-agent probe` prints the telemetry of all vehicles once (fuel, capacity, tyre wear). Use it in a session with
other cars to see whether the plugin delivers fuel and tyre data for remote vehicles.

With `record_file` set, every message is also written to an NDJSON file (about 2-3 GB per 24 hours) that
`splash-replay` can play back.

## Development

    pip install -e ".[dev]"
    pytest

The tests run on any OS: plugin buffers are built in memory and the connection is tested against a stand-in for the
backend socket. Only `reader.open_windows_buffer` needs Windows and a running game; it has not been run against a real
game yet.

The ctypes layout in `src/splash_agent/structs.py` was compared field by field with the plugin header (pack 4, 32-bit
`long`, 64-bit build); `tests/test_structs.py` pins the sizes and offsets the agent relies on.
