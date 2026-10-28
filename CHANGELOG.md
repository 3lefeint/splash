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
- Windows agent: shared memory reader, lap and pit event detection, spool with resend, WebSocket link and `probe` command
- Live samples are no longer sequenced; pit entries of the same car within 30 s count as one stop
- Web frontend: overview, stint plan, settings and rest view with SSE updates, offline cache and CSV export
- Backend serves the built frontend
- Dockerfile, Kustomize base with test and prod overlays (one namespace, Ingress class `nginx`), GitHub Actions CI with image push and Windows agent build
