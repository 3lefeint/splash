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
- Dockerfile, Kustomize base with staging and prod overlays (one namespace, Ingress class `nginx`), GitHub Actions CI with image push and Windows agent build
- Agent option `raw_file` and `replay-raw` command to run a captured session through the agent offline
- `scripts/dev.sh` runs frontend and backend from source; development loop documented
- Track map: the backend records the outline from the first clean lap, the agent sends car positions, the overview and a map page show all cars with switches for opponents, classes and single cars
- Tyre wear by track section: the backend attributes the wear of the driven car to 50 sections per lap, the map can paint it per lap or as the change since the tyre set was new, and a panel shows wear per wheel, the trend and the wear of recent laps
- Overview tiles can be moved and resized on a 12 column grid (edit mode, saved in the browser); phones keep one column
- Agent logs a status line every 30 s (session, cars, frames, backend connection, game time standing still)
- Tyre temperatures (tread, inner layer, pressure, brakes, engine) and weather with a history of track and air temperature as overview tiles
- Pit stops: measured time in the pit lane, standing in the box, until the work was done and fuel flow, with suggested parameters that can be applied in one click (refuel rate, driver change and the pit lane loss against racing past, shown per stop as gross, race pace part and net, or the reason why there is no net value; no tyre change suggestion); the tyre change counts 17 s unless a value is stored per track and car; the service always runs in sequence (tyres, then fuel, plus the driver change), there is no parallel service setting
- Pit menu: the agent reads the selected row of the game pit menu (fuel, tyres, ...); the pit setups use it
- Pit setups: named lists of pit menu rows (max fuel, medium tyres, ...) that a button sends to the game of the driver through the plugin
- Track map: while the outline is recorded the map shows the lap driven so far and how much of it is covered
- Tyre temperatures tile shows the car from above, nose up, with the tread of each tyre coloured by temperature; the working range is set per compound name in the options; the text inside the tyres is larger and no text is shown beside the car
- Explanatory texts of the tiles are shown in a popup behind an "i" button in the top right corner of the tile
- Sessions are stored with start, track, car and, per lap of the driven car, weather, tyre and engine temperatures and who drove (player or AI); a sessions page lists them with CSV exports of laps, stops and conditions
- The driver tile shows when the AI drives the car
- Rivals tile: gap, pace difference, stint, fuel and forecast stop of every car, where we would rejoin after a stop now and the best lap to stop; every car carries its fuel and tyre wear in the scoring message
- Lap time model: fuel effect and tyre degradation fitted from the clean laps, applicable as parameters; minimum clean laps, slow limit and the exclusion of full course yellow and wet laps are settings per track and car, and while there are too few laps the panel lists the excluded laps with their reasons
- Notes tile: wet track on dry tyres, rising rain, full course yellow, tyre temperatures against the ranges, rivals that stop soon and a better moment to stop
- Stint plan: stints behind the finish are cut and the needed ones are scaled; the next two stay in the list greyed out as reserve
- Weather history is stored per session, so a restart of the backend no longer loses it
- Agents tile and notes: which agent is connected, its version and the age of its data; a lost agent is reported
- Announcements: beeps or spoken messages for the stop, driver change in 5 and 1 minutes, full course yellow, damage, penalties, wet track and lost agents, with a banner under the header; the options show which speech voice is used, let you pick one and say when the browser has none
- Pit request button that toggles the pit request of the car through the plugin
- Damage, penalties and overheating of the driven car are read from the game and reported in the notes
- Advice on wet tyres: prepare, switch now, or slicks possible again, from track wetness and rain
- Rivals show the most worn tyre; the scoring message carries the wear of every tyre of every car and the penalties
- Track map: time gained or lost against the car ahead, the car behind or our best lap in each of 50 sections, from the section times of every car
- Track map: fuel use, speed and braking along the lap, the pit lane with its boxes, where we would rejoin after a stop with the cars that would pass us (the marker sits where we would be among the cars: our lap distance minus what the time lost covers at race pace), cars coloured by their pace against ours and cars off the track
- The agent sends the brake pedal, the lateral position and the track edge
- Race simulator (`splash-agent simulate`, `scripts/simulate.sh`) that plays an invented endurance race with pit stops, rain, yellow flags, penalties and damage into a backend; with `SIM_TRACK` it drives on the outline and corners of a track known to another backend
- The lap time model leaves out laps on a wet track and warns when it fits the laps poorly
- Demo script that plays a raw capture into a throw-away backend, to look at the dashboard without the game
- Header shows the track and car of the session in the middle and the time left next to the connection status; the layout button is a gear at the left of the header on the dashboard
- Tyre wear of the driven car in each tyre of the tyre temperature tile, with a vertical bar beside each tyre that runs from green over orange to red
- Overview tiles can be hidden in the layout editor and shown again from a list below the grid; the driver tile no longer shows tyre wear
- Tracks page for the known tracks: mark each corner on the map from where it starts to where it ends, shown as a coloured stretch with a name that can be dragged and a line that docks at the nearest points of name and stretch, with no session needed; the map page shows the corners and an overview tile shows the corner the car is in
- Tracks page: the corner list is ordered by position on the track; zoom and move the map with the mouse wheel, buttons and dragging, and change the width of the columns by dragging the bars between them
- Browser identity from the Cloudflare Access token (`SPLASH_ACCESS_TEAM_DOMAIN`, `SPLASH_ACCESS_AUDIENCE`): signature, issuer, audience and expiry are checked, the e-mail address maps to a team through `team_members`, unknown addresses get 403; without the variables authentication is off (the prod deployment requires them, staging and dev do not)
- Administration page and API for teams, members and agents; an agent token is shown once and only its hash is stored; first administrators from `SPLASH_ADMIN_EMAILS`
- Agent socket: when the Access variables are set, a handshake needs a valid `Cf-Access-Jwt-Assertion` (no e-mail required) in addition to the Splash token; the agent can send `CF-Access-Client-Id` and `CF-Access-Client-Secret` (`server.cf_client_id`, `server.cf_client_secret`)
- Reconnects: the SSE stream sends a configurable keep-alive (`SPLASH_SSE_HEARTBEAT_SECONDS`), the server closes agent connections that do not answer pings (`SPLASH_WS_PING_SECONDS`, `SPLASH_WS_DEAD_AFTER_SECONDS`), the page reconnects with backoff and jitter, shows the last state as reconnecting and asks for a new login when the session has expired
- Logging: the backend writes structured lines by level (`SPLASH_LOG_LEVEL`, `SPLASH_DB_SLOW_MS`): start summary, agent connections, sessions, failed logins, admin actions, rate limit hits, slow and failed database calls, failed requests and recovered panics, debug lines for requests, frames and token checks; the agent logs to the console with a level from `log.level` or `--log-level`, and the `websockets` library can no longer print handshake headers; no secrets, tokens or e-mail addresses are logged
- Yellow flags: the sector flag only says there is a yellow; cars almost standing still in a flagged sector (not in the pits) are marked as probable causes on the map and in the rivals list, and an alert line shows the nearest one ahead with distance and corner ("Yellow in 340 m, before <corner>") or "Yellow, cause unknown"; two independent settings for the alert line and the markers; the agent adds the sector of the cars to the scoring message (the backend must be updated first)
- Audit log of all write requests (time, e-mail, team, method, path)
- Rate limits per identity on write requests, SSE streams and agent connections (`SPLASH_RATE_WRITES_PER_MIN`, `SPLASH_RATE_CONNECTS_PER_MIN`), answered with 429 and `Retry-After`
- Release flow with two tag series: `v*` builds the backend image, `agent-v*` publishes the Windows agent as a GitHub release with a SHA-256 checksum
- Each lap stores the agent whose token delivered it (`laps.agent_id`), taken from the token and not from the agent id in the message
- Live state, engine, SSE stream and overrides exist per agent instead of per team, so members who train at the same time, also in different sessions, no longer disturb each other; `/api/state`, `/api/stream` and `/api/overrides` take `?agent=<id>` and otherwise show the agent of the member's own address (an agent can be tied to a member in the admin API, `PUT /api/admin/agents/{id}`, or when it is created) or an empty state if the member has none, and one active agent when authentication is off; `GET /api/agents` lists the agents of the team for choosing; the state carries `agent_id` and `agent_name`

### Changed

- Teams: parameters, stint plan, overrides, pit setups and sessions with their laps, stops and weather are kept per team; live state, engine, command hub and SSE stream run per team; track data is shared; existing data moves to the team `default`
- Agent tokens are looked up by hash in the table `agents` and decide the team; `SPLASH_AGENT_TOKEN` stays as a fallback for the team `default` (with the Access variables set, only when it is non-empty and matches)
- Migrations no longer run at backend start; `splash migrate` applies them, deployed as an init container with its own database secret
- The backend connects with a pool of 10 connections, configurable with `SPLASH_DB_MAX_CONNS`
- The environments are called `splash` (prod) and `splash-stg` (staging): overlay `deploy/overlays/stg`, suffix `-stg`, Secrets `splash-stg-*`, database `splash_stg`
- Prod and staging ingresses use the class `public`; the staging environment has no LAN host

### Fixed

- The page no longer crashes for a team without data or with a single car: lists in the state are sent as `[]` instead of `null` (rivals, rival tyre wear, rejoin lists, weather history, stint plan entries, pit lane boxes) and the page treats a missing list as empty
- Agent keeps reconnecting after a connection that was cut before the HTTP answer, and starts its reconnect pause over after a connection that worked; the pause carries jitter
- Agent marks laps as full course yellow only for game phase 6 or yellow flag states 1 to 3, not for sector flags
- Agent no longer creates an empty shared memory area when it starts before the game; it opens the plugin buffers read-only and waits
- Agent detects a tyre change from a rise of the wear value, also for slightly worn tyres
- Agent starts a new session id when the race time jumps back, so a restarted race on the same track no longer joins the old data
