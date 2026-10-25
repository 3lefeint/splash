# Backend

Go service: ingest (WebSocket from agents), deduplication, strategy calculation, stint plan, SSE to the web frontend,
REST for parameters and overrides.

Planned layout:

- `cmd/splash` - server entry point
- `cmd/splash-replay` - plays an NDJSON recording against a running backend at adjustable speed
- `internal/strategy` - pure strategy logic without I/O (fuel model, stops left, saving target, stint plan)
- `internal/ingest` - WebSocket handling, dedup by session/car/ET
- `internal/store` - Postgres access (migrations, queries)
- `internal/api` - REST and SSE

Postgres runs on an existing external VM; credentials are provided as environment variables.
