# Backend

Go service: ingest (WebSocket from agents), deduplication, strategy calculation, stint plan, SSE to the web frontend,
REST for parameters and overrides.

Planned layout:

- `cmd/splash` - server entry point
- `cmd/splash-replay` - plays an NDJSON recording against a running backend at adjustable speed
- `internal/strategy` - pure strategy logic without I/O (fuel model, stops left, saving target, stint plan, gap trend)
- `internal/wire` - decoding and validation of agent messages
- `internal/ingest` - WebSocket handling, agent token check and team lookup, acknowledgements, resend handling
- `internal/live` - latest scoring and telemetry sample in memory
- `internal/store` - laps, stops and agent progress (Postgres and in-memory implementation, goose migrations)
- `internal/replay` - playback of recordings
- `internal/teams` - the runtime of each team (live state, engine, command hub, ingest), created on demand
- `internal/engine` - combines live state, laps, stops, parameters and overrides into the state shown to the team
- `internal/api` - REST endpoints and the SSE stream
- `internal/webui` - the built frontend from `web/`, embedded into the binary

Postgres runs on an existing external VM. Configuration is done with environment variables:

| variable | meaning |
|---|---|
| `SPLASH_DATABASE_URL` | Postgres URL; without it an in-memory store is used |
| `SPLASH_DB_MAX_CONNS` | size of the database connection pool, default 10 |
| `SPLASH_ACCESS_TEAM_DOMAIN` | Cloudflare Access team domain, e.g. `team.example.com`; with `SPLASH_ACCESS_AUDIENCE` it switches browser authentication on; the prod deployment requires both |
| `SPLASH_ACCESS_AUDIENCE` | audience tag of the Access application; both must be set together |
| `SPLASH_ADMIN_EMAILS` | comma separated e-mail addresses that become administrators at start (new ones join the team `default`) |
| `SPLASH_RATE_WRITES_PER_MIN` | write requests a minute per identity, default 120, 0 = unlimited |
| `SPLASH_RATE_CONNECTS_PER_MIN` | new SSE streams per identity and agent connections per agent a minute, default 30, 0 = unlimited |
| `SPLASH_SSE_HEARTBEAT_SECONDS` | keep-alive interval of idle SSE streams, default 15, 0 = default |
| `SPLASH_AGENT_MIN_VERSION` | the oldest agent version that counts as up to date, e.g. `0.2.0`; older agents are marked as outdated in the agent list. Empty marks none |
| `SPLASH_WS_PING_SECONDS` | how often an agent connection is pinged, default 20 |
| `SPLASH_WS_DEAD_AFTER_SECONDS` | an agent connection whose ping stays unanswered this long is closed, default 30 |
| `SPLASH_LOG_LEVEL` | `debug`, `info`, `warn` or `error`; default `info`; anything else stops the start |
| `SPLASH_DB_SLOW_MS` | database calls slower than this are logged as warnings, default 500 |
| `SPLASH_AGENT_TOKEN` | shared secret for agent connections, mapped to the team `default`; without it agent connections are not authenticated, unless the Access variables are set: then only registered agent tokens are accepted |
| `SPLASH_LISTEN` | listen address, default `:8080` |

The backend no longer migrates the database at start. Run `go run ./cmd/splash migrate` (or `splash migrate`) with
`SPLASH_DATABASE_URL` set to the owner role first; `scripts/dev.sh` does this when the variable is set. The backend
itself then runs with a role that only has data rights, see `deploy/README.md`.

## Teams

Several teams can use one backend. Everything except track data (outlines, pit lanes, corner names) belongs to a team:
parameters, stint plan, overrides, pit setups, sessions with their laps, stops and weather. Each team has its own
command hub, and each agent of a team has its own live state, engine and SSE stream, because members train
independently, in the same or in different sessions. The agent is the one whose token the connection used; the shared
dev token is agent 0. The migration creates the team `default` and assigns all existing data to it.

An agent's team comes from its token: the handshake looks up the SHA-256 hash of `Authorization: Bearer <token>` in
the table `agents` (revoked tokens are rejected, `last_seen` is updated), never from the message. `SPLASH_AGENT_TOKEN`
stays as a fallback that maps to the team `default`. Tokens are stored and compared only as hashes.

With the Access variables set (see below) the agent socket also requires a valid `Cf-Access-Jwt-Assertion`: Cloudflare
adds it after it has checked the agent's service token (`CF-Access-Client-Id` / `CF-Access-Client-Secret`). A handshake
without a valid one (signature, issuer, audience, expiry) gets 401 before the Splash token is looked at; the Splash token
is required in addition. The token of a service has no e-mail address; the `common_name` claim is only logged at debug
level. The same token is refused with 403 on the browser and admin paths. With the Access variables set, a Splash token that is not registered in `agents` is accepted only if it equals a
non-empty `SPLASH_AGENT_TOKEN`; without that variable it is rejected with 401. Without the Access variables the agent
socket behaves as before.

### Connection loss

Cloudflare and the tunnel can cut idle or long connections, so both clients recover on their own. An idle SSE stream
gets a keep-alive message every `SPLASH_SSE_HEARTBEAT_SECONDS`: an SSE comment (`: ping`) plus a named event `ping`
whose data is the interval in milliseconds, which the page uses to notice a connection that went silent without an
error. The server pings every agent connection and closes it when a ping stays unanswered for
`SPLASH_WS_DEAD_AFTER_SECONDS`; the agent resumes from the last acknowledged sample when it reconnects.

### Browser identity

With `SPLASH_ACCESS_TEAM_DOMAIN` and `SPLASH_ACCESS_AUDIENCE` set, every `/api/` request must carry the header
`Cf-Access-Jwt-Assertion`. The token is checked completely: RS256 signature against the keys at
`https://<team domain>/cdn-cgi/access/certs` (cached for an hour, refreshed when a token names an unknown key, at most
every 30 s), issuer, audience and expiry. A missing or invalid token gives 401. The e-mail address of the token is
looked up in `team_members`; an unknown address gives 403. An address belongs to exactly one team (it is the primary
key). Without the two variables authentication is off: a warning is logged, every request belongs to the team
`default` and nobody is an administrator.

`GET /api/me` returns `{email, team_id, team, is_admin}`. Members with `is_admin` can use the administration page
(`#/admin`) and the API below it:

| endpoint | meaning |
|---|---|
| `GET/POST /api/admin/teams` | list teams, add a team `{name}` |
| `GET/PUT/DELETE /api/admin/members` | list members, add or move a member `{email, team_id, is_admin}`, remove `?email=` |
| `GET/POST /api/admin/agents` | list agents, create one `{name, team_id, email}`; the reply carries the token, shown only once. `email` is optional and must be a member of the team |
| `PUT /api/admin/agents/{id}` | tie an agent to a member of its team `{email}`, or release it with an empty address |
| `POST /api/admin/agents/{id}/revoke` | make an agent token stop working |
| `GET/POST /api/admin/cars` | the team cars of a team (`?team_id=`) with their vehicle names and the names seen in sessions that belong to none; add a car `{team_id, name}` |
| `DELETE /api/admin/cars/{id}?team_id=` | delete a team car; its parameters and set-ups stay stored under its name |
| `PUT /api/admin/cars/{id}/names`, `DELETE /api/admin/car-names?team_id=&name=` | assign a vehicle name of the game to a team car `{team_id, vehicle_name}` (it moves if it belonged to another car), or take it away |

The first administrator is named in `SPLASH_ADMIN_EMAILS`; the addresses are applied at every start. An administrator
cannot remove or demote themselves.

Every write request (any method but GET, HEAD and OPTIONS) is appended to `audit_log` with time, e-mail, team, method
and path, before the handler runs. Request bodies, query strings and tokens are not stored. Write requests and new SSE
streams are limited per e-mail address, new agent connections per agent; a request over the limit gets 429 with
`Retry-After`. The limits are in memory and per process.

The frontend is served on `/`; build it with `npm run build` in `web/` first. Agents connect to `/v1/agent/ws` with `Authorization: Bearer <token>`; `/healthz` answers `ok`.

Replay a recording against a running backend:

    go run ./cmd/splash-replay -file ../testdata/sample.ndjson -speed 10

Tests: `go test ./...`. The Postgres tests run when `SPLASH_TEST_DATABASE_URL` points to a database; they work in a
throwaway schema.

## Yellow flags

The state carries `yellow_sectors` (the sectors 1 to 3 with a local yellow, `[]` when none), independent of `fcy`
(full course yellow). The sector flag only says that there is a yellow. Cars carry `probable_cause` in `field` and in
the rivals list: a car in a flagged sector that is not in the pits and moves slower than about 11 km/h between two
scoring samples (derived from its lap distance, nothing extra is sent); our own car never does. The cause is a
candidate: another slow car in the same sector looks the same. The page works out the distance and the corner from
these cars. The per-car flag fields of the game stay unset, so they are not used. Lap marking does not use sector
flags.

## Logging

The backend writes one structured line per event (`log/slog`, `key=value`) to stderr. The level is set with
`SPLASH_LOG_LEVEL` (default `info`). Every value passes a scrubber that blanks e-mail addresses, bearer tokens, JWTs and
the password part of URLs, and the call sites log ids and reason codes instead: no secret, token (not even a part),
password or e-mail address appears at any level. Identities in lines are a short hash (`ShortID`), teams and agents are
ids. Floods of the same INFO line (failed logins, rate limit hits) are held to one line per minute and reason, the next
line carries `suppressed=N`.

| level | message | keys | when |
|---|---|---|---|
| INFO | `starting` | `listen store access_auth agent_shared_token admins_from_env db_max_conns sse_heartbeat_s ws_ping_s ws_dead_after_s rate_writes_per_min rate_connects_per_min log_level` | the configuration, once |
| INFO | `migrations applied` | | `splash migrate` finished |
| INFO | `administrators set from the environment` | `count` | `SPLASH_ADMIN_EMAILS` was applied |
| INFO | `shutting down`, `stopped` | | the process ends |
| INFO | `agent connected` | `team agent role` | the first message of an agent connection |
| INFO | `agent disconnected` | `team agent reason seconds messages stored resent_duplicates` | the connection ended; `reason` says why |
| INFO | `session started`, `session changed` | `team session` / `team from to` | the game session of a team (there is no explicit end event; a session ends when the next one starts) |
| INFO | `backlog resent` | `team agent stored duplicates` | the agent has resent its spool after a reconnect |
| INFO | `authentication failed` | `path` (`browser`/`agent`), `reason` | `missing malformed algorithm unknown_key bad_signature wrong_issuer wrong_audience no_expiry expired not_yet_valid no_email unknown_member`, agent: the same with `access_` in front, `unknown_token`, `revoked_agent` |
| INFO | `admin action` | `action admin admin_team ...` | `team_created member_set member_removed agent_created agent_revoked` with ids |
| INFO | `rate limited` | `kind identity` | `write`, `stream` or `agent_connect` limit hit |
| INFO | `track map recorded` | `track length` | |
| WARN | `SPLASH_DATABASE_URL not set: using in-memory store, ...` and the two other missing-configuration lines | | at start |
| WARN | `access keys fetch failed, will retry with a later request` | `err` | the key set of the Access team could not be fetched |
| WARN | `slow database call` | `statement ms threshold_ms` | above `SPLASH_DB_SLOW_MS` |
| WARN | `samples dropped or deduplicated` | `team older_live_samples resent_duplicates interval_seconds` | per minute, only when there were some |
| WARN | `stream client gone with unsent data` | `team err` | writing to an SSE client failed |
| WARN | `invalid message`, `agent connection dead, closing` | `team agent err` | |
| ERROR | `request failed` | `method route status ms team err` | every answer with a status of 500 or more |
| ERROR | `panic recovered` | `method route panic stack team` | a handler panicked; the answer is a 500 |
| ERROR | `database call failed` | `statement ms err` | |
| ERROR | `audit write failed`, `identify failed`, `verify access token`, `agent lookup failed`, `state could not be computed`, `store failed`, `save session` and the other failed saves | `team err` | |
| DEBUG | `request` | `method route status ms team` | one per request; `route` is the matched pattern, so ids in the path and the query stay out |
| DEBUG | `stream connected`, `stream heartbeat`, `stream closed` | `team` | SSE |
| DEBUG | `frame` | `team type bytes seq` | every agent message |
| DEBUG | `access token verified`, `access token rejected` | `email_claim common_name_claim` / `reason` | JWT checks |
| DEBUG | `access keys cache hit`, `access keys refreshed` | `keys` | key cache |
| DEBUG | `database call` | `statement ms` | every statement, never its arguments |
| DEBUG | `state computed` | `ms` | engine run |
| DEBUG | `ping answered`, `agent token accepted`, `websocket handshake failed` | | agent connection details |

Example lines:

    time=2026-10-28T13:20:49.841Z level=INFO msg="admin action" action=member_set admin=373a64e5 admin_team=1 member=2305d09f team_id=1 is_admin=false
    time=2026-10-28T13:20:49.846Z level=INFO msg="agent disconnected" team=1 agent=driver-pc reason="closed by agent" seconds=0 messages=2 stored=1 resent_duplicates=0
    time=2026-10-28T13:20:49.639Z level=WARN msg="slow database call" statement="UPDATE agents SET last_seen = now() WHERE id = $1" ms=40 threshold_ms=20
    time=2026-10-28T13:20:50.699Z level=ERROR msg="request failed" method=GET route="GET /api/state" status=500 ms=0 team=0
    time=2026-10-28T13:20:49.839Z level=DEBUG msg="access token rejected" reason=expired

## API

Read and write endpoints have no authentication; only the agent socket uses the token.

| endpoint | meaning |
|---|---|
| `GET /api/agents` | the non-revoked agents of the team with `mine`, `active`, last sample and session, for choosing whose state to look at; no addresses |
| `GET /api/state` | the complete computed state (fuel model, stops, saving target, stint plan, class opponents) of one agent: `?agent=<id>` names it, without it the member's own agent is used, the one of their address with the newest sample, and a member without an agent gets an empty state with `agent_id` -1. Without authentication one agent with a live sample in the last 30 seconds is used, the lowest id if several (404 for an agent that has not connected since the backend started) |
| `GET /api/stream` | the same state as server-sent events (`event: state`), at most once per second and only when it changed; `?agent=` as for the state |
| `GET /api/params`, `GET/PUT /api/params?track=&car=` | training parameters per track and car; the set matching the running session is used, defaults apply otherwise. `tyre_change` is 17 s (`DefaultTyreChange`) where no set exists or the value is 0, which counts as unset; every stop plan and rejoin calculation uses that resolved value. The lap time model settings `fit_min_laps` (3 to 20, default 5), `fit_slow_percent` (2 to 30, default 8), `fit_skip_fcy` and `fit_skip_wet` (default true) are part of the set: unset values read as the defaults, values out of range are answered with 400 |
| `GET/PUT /api/stint-plan` | the stint plan of the team, a list of `{driver, duration}`, written as a whole |
| `GET /api/overrides`, `PUT/DELETE /api/overrides/{key}` | manual values for `fuel` and `fuel_per_lap` of one agent (`?agent=` as for the state; writing without an agent is answered with 409); an override ends by itself once the live value matches it again |
| `GET /api/sessions`, `GET /api/sessions/{id}/export?kind=` | the stored sessions with their counts, CSV export of laps, stops or conditions |
| `GET /api/sessions/{id}/laps` | the laps of the driven cars of a session with their conditions, the agent that delivered them and whether they count for the models, for the lap table |
| `PUT /api/laps/excluded` | leave a lap out of the models, take it in, or let it follow its session `{session_id, car_id, vehicle_name, lap, excluded: true\|false\|null}`; only the driver of the lap (a member whose agent delivered it) or an administrator |
| `PUT /api/sessions/{id}/excluded` | leave a whole session out of the models, or take it in `{excluded}`; an administrator, or a member whose agents delivered all the laps of the session |

## Lap time model

`fit` in the state is the lap time model fitted from the clean laps of the car. Which laps count is set in the
parameters of the track and car (`fit_min_laps`, `fit_slow_percent`, `fit_skip_fcy`, `fit_skip_wet`, see the API
table) and read at every calculation. A lap is left out as `missing_data` (no time or fuel), `in_lap`, `out_lap`, `fcy`
(with `fit_skip_fcy`), `wet` (with `fit_skip_wet`) or `too_slow` (more than `fit_slow_percent` over the median of the
other laps, from 3 laps on) or `manual` (left out by a person in the lap table). `min_laps` is the number of clean laps
needed. While there are too few laps (`reason` `few_laps`) `excluded` lists every left-out lap of the running session
with its reason; otherwise it is `[]`, never null.

### Learning from earlier sessions

The lap time model and the fuel model do not start from nothing: besides the running session they use the laps of the
driven car from all earlier sessions of the team on the same track. The car counts with all vehicle names of its team
car (a new livery keeps its history). Laps and sessions that a person left out are not used. The tyre age of a lap is
worked out inside its own session, so the laps of different sessions do not mix up their tyre sets.

- Fuel model: the laps of earlier sessions are fed in first, so fuel per lap and lap time are known from the first lap.
  The model keeps one value per driver (the driver name the game reports); a driver's own value is used once they have
  three valid laps, before that the average of the drivers with data.
- Lap time model: the laps of the current driver count first; when the model cannot be fitted with them for lack of laps
  (`fit_min_laps`), the laps of all drivers of the team are used. `fit.history_laps` says how many laps come from earlier
  sessions and `fit.scope` whose they are (`driver` or `team`).
- The laps of earlier sessions are read again every 15 seconds, so a lap left out counts from the next read on.
- The per-section tyre wear statistics still use the running session only.

## Pit stops

`pits` in the state lists the stops of the driven car (`lane`, `stopped`, `work`, `through`, ...) and `suggest` holds
parameter values derived from them. The pit lane loss (`pit_through`) is the net time against racing past: `through`
(pit lane time minus standing time) minus `race_part`, the time a car at race pace needs for the same stretch
(distance from pit entry to pit exit along the lap times the lap time over the track length); `net` is the difference
and the median of the positive `net` values is the suggestion. A stop without a net value carries `net_missing`: `in_progress`, `stand_time`, `track_length`, `lap_time`, `lane_unknown`, `entry_dist`, `exit_dist` or `stretch`. The distance is learned by the pit lane tracker from
the lap distance of the cars in the scoring samples when they enter and leave the pit lane (`entry_dist`, `exit_dist`
in the stored pit lane, averaged around the lap so the start line may lie inside the pit lane). While the distance, the
track length or the pace is unknown, `race_part` and `net` are `null` and there is no suggestion. The tyre change time
is not suggested: it is the parameter `tyre_change` (default 17 s).

The pit service always runs in sequence: the stand time is refuelling plus the tyre change plus the driver change where
the stop has one (`pitLoss`, `serviceTime`). The old `parallel_service` parameter has no effect any more: an older client
may still send it and it is accepted and dropped, and the unused database column stays in place.

## Rejoin point

`strategy.rejoin` says what a stop now costs (`loss`, `position`, `lost`) and `lap_dist`, the point of the lap where we
would sit among the cars afterwards: our lap distance minus what a car at race pace covers in `loss` seconds (track
length over lap time), wrapped around the lap. It is `null` while our lap distance, the track length or the lap time is
unknown; the track map then draws no marker. The marker moves with the car and with the time lost.

The entry distance is taken at the first sample inside the pit lane; if that sample has no lap distance, the last
distance seen before it stands in (the car was at most one sample earlier). An exit sample without a lap distance
waits for the next sample outside the lane. The mean is taken around the lap, so an entry before the start line and an
exit after it give a stretch across the line (for example entry 20400 m, exit 319 m on a 20832 m track: 751 m). A stored
lane that has points and boxes but no entry distance gets one on its next pass and keeps what it has.
