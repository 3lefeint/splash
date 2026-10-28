# Wire format

Agents send one JSON object per WebSocket text frame. The same objects, one per line, form the NDJSON recording.
The schema is [`../schema/messages.schema.json`](../schema/messages.schema.json); `ack` is the only backend to agent
message.

Every message carries an envelope: `v`, `type`, `agent_id`, `role`, `session_id`, `seq`, `et`, `payload`.

| type | sent by | content |
|---|---|---|
| `hello` | both | first message after (re)connect, includes `resume_from_seq` |
| `scoring` | both | whole field: places, positions on the map, laps, pit state, weather, flags |
| `telemetry` | driver | fuel and tyre wear of the driven car |
| `lap` | both | completed lap record (`laps` table); fuel and tyre fields stay empty for opponents |
| `event` | both | pit entry/exit, driver swap, FCY start/end |

Rules:

- `et` is game elapsed time in seconds. Deduplication between the two agents uses `session_id`, `car_id` and `et`/`lap`,
  never wall clock.
- Only `lap` and `event` messages are durable. They are numbered with a strictly increasing `seq` per agent and session,
  starting at 1; `hello` always has `seq` 0. `scoring` and `telemetry` are live samples: they are not sequenced, carry the
  last durable `seq`, and are never resent.
- The backend answers every message with `ack` carrying the highest durable `seq` it has stored. After a reconnect the
  agent sends `hello`, takes the `ack` as the backend's position and resends every durable message after it.
- Two agents report the same pit stop with slightly different `et`. Pit entries of the same car within 30 s are one stop.
- A car is identified by `car_id` (plugin slot id, `mID`) plus `vehicle_name`, because slot ids can be reused in
  multiplayer after someone leaves.
- Units: seconds, litres, degrees Celsius. The plugin delivers tyre temperatures in Kelvin and `mLastSector2` as
  S1+S2; the agent converts both. Sector times in all messages are durations, not cumulative.
- `mWear` is 0.0-1.0, fraction of maximum; whether 1.0 means new or worn is not stated in the header and must be
  checked in game.
- Scoring buffers update at 5 Hz, telemetry at 50 Hz; the agent samples telemetry down to 5-10 Hz.
- Game field names and scaling (for example `mWear`) must be verified against the plugin header before the schema is
  frozen (done for the fields above except the `mWear` direction and the mapped buffer names).

## Yellow flags in `scoring`

`sector_flags` (three raw values) and `yellow_state` are sent with every sample. A sector with a local yellow reports 1,
a clear one 11 (seen in a capture, not documented by the plugin). Per vehicle the agent may add `sector` (1 to 3, the
plugin's mSector 1, 2, 0), `flag` (raw mFlag, only when not 0) and `under_yellow` (raw mUnderYellow, only when true).
`sector` is sent for the player's own car always and for every other car only while a sector flag is up, so a green
session costs 11 bytes per message and a flagged one about 11 bytes per car. All three are optional: an older agent
sends none of them and the backend then marks no probable cause. The backend uses only `sector`; `flag` and `under_yellow` are decoded but not used, because the game leaves them unset. The backend decodes strictly, so the
backend has to be updated before an agent that sends these fields.
