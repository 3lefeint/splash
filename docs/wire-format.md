# Wire format

Agents send one JSON object per WebSocket text frame. The same objects, one per line, form the NDJSON recording.
The schema is [`../schema/messages.schema.json`](../schema/messages.schema.json); `ack` is the only backend to agent
message.

Every message carries an envelope: `v`, `type`, `agent_id`, `role`, `session_id`, `seq`, `et`, `payload`.

| type | sent by | content |
|---|---|---|
| `hello` | both | first message after (re)connect, includes `resume_from_seq` |
| `scoring` | both | whole field: positions, laps, pit state, weather |
| `telemetry` | driver | fuel and tyre wear of the driven car |
| `lap` | both | completed lap record (`laps` table); fuel and tyre fields stay empty for opponents |
| `event` | both | pit entry/exit, driver swap, FCY start/end |

Rules:

- `et` is game elapsed time in seconds. Deduplication between the two agents uses `session_id`, `car_id` and `et`/`lap`,
  never wall clock.
- `seq` is strictly increasing per agent and session. The backend answers with `ack` carrying the highest stored `seq`;
  after a reconnect the agent resends everything after the last `ack`.
- A car is identified by `car_id` (plugin slot id, `mID`) plus `vehicle_name`, because slot ids can be reused in
  multiplayer after someone leaves.
- Units: seconds, litres, degrees Celsius. The plugin delivers tyre temperatures in Kelvin and `mLastSector2` as
  S1+S2; the agent converts both. Sector times in all messages are durations, not cumulative.
- `mWear` is 0.0-1.0, fraction of maximum; whether 1.0 means new or worn is not stated in the header and must be
  checked in game.
- Scoring buffers update at 5 Hz, telemetry at 50 Hz; the agent samples telemetry down to 5-10 Hz.
- Game field names and scaling (for example `mWear`) must be verified against the plugin header before the schema is
  frozen (done for the fields above except the `mWear` direction and the mapped buffer names).
