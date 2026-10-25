# Agent

Windows agent (Python) that runs on each race PC. It reads the rF2 Shared Memory Map Plugin buffers read-only at a low
poll rate (5-10 Hz), writes every sample to a local NDJSON recording and streams it to the backend over WebSocket.

- Driver PC: telemetry of the driven car plus scoring.
- Spotter PC: scoring of the whole field (redundancy at driver change).
- The NDJSON file doubles as send buffer: after a connection drop the agent resends from the last acknowledged `seq`.

Message format: see [`../schema`](../schema).
