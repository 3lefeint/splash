# Changelog

## [Unreleased]

### Added

- Repository structure for agent, backend, web and deploy
- Wire format schema for agent messages
- MIT license
- Strategy core: fuel consumption model, laps to stop, stops remaining, saving target, stint plan rescheduling and opponent gap trend
- Backend ingest: WebSocket endpoint for agents with token check and acknowledgements
- Postgres storage for laps and pit stops with migrations, in-memory store for tests
- Replay tool `splash-replay`
- REST endpoints for parameters, stint plan and overrides
- SSE stream with the complete computed state
- State engine combining fuel model, stops, stint plan and class opponents
