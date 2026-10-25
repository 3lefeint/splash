# Backend

Go service: ingest (WebSocket from agents), deduplication, strategy calculation, stint plan, SSE to the web frontend,
REST for parameters and overrides.

Planned layout:

- `cmd/splash` - server entry point
- `cmd/splash-replay` - plays an NDJSON recording against a running backend at adjustable speed
- `internal/strategy` - pure strategy logic without I/O (fuel model, stops left, saving target, stint plan, gap trend)
- `internal/wire` - decoding and validation of agent messages
- `internal/ingest` - WebSocket handling, token check, acknowledgements, resend handling
- `internal/live` - latest scoring and telemetry sample in memory
- `internal/store` - laps, stops and agent progress (Postgres and in-memory implementation, goose migrations)
- `internal/replay` - playback of recordings
- `internal/engine` - combines live state, laps, stops, parameters and overrides into the state shown to the team
- `internal/api` - REST endpoints and the SSE stream

Postgres runs on an existing external VM. Configuration is done with environment variables:

| variable | meaning |
|---|---|
| `SPLASH_DATABASE_URL` | Postgres URL; without it an in-memory store is used |
| `SPLASH_AGENT_TOKEN` | shared secret for agent connections; without it they are not authenticated |
| `SPLASH_LISTEN` | listen address, default `:8080` |

Agents connect to `/v1/agent/ws` with `Authorization: Bearer <token>`; `/healthz` answers `ok`.

Replay a recording against a running backend:

    go run ./cmd/splash-replay -file ../testdata/sample.ndjson -speed 10

Tests: `go test ./...`. The Postgres tests run when `SPLASH_TEST_DATABASE_URL` points to a database; they work in a
throwaway schema.

## API

Read and write endpoints have no authentication; only the agent socket uses the token.

| endpoint | meaning |
|---|---|
| `GET /api/state` | the complete computed state (fuel model, stops, saving target, stint plan, class opponents) |
| `GET /api/stream` | the same state as server-sent events (`event: state`), at most once per second and only when it changed |
| `GET /api/params`, `GET/PUT /api/params?track=&car=` | training parameters per track and car; the set matching the running session is used, defaults apply otherwise |
| `GET/PUT /api/stint-plan` | the global stint plan, a list of `{driver, duration}`, written as a whole |
| `GET /api/overrides`, `PUT/DELETE /api/overrides/{key}` | manual values for `fuel` and `fuel_per_lap`; an override ends by itself once the live value matches it again |
