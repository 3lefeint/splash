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
- `internal/api` - REST and SSE (planned)

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
