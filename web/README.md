# Web

Svelte 5 + Vite PWA for desktop, tablet and phone, one responsive dark layout. Live updates come from the backend's SSE
stream (`/api/stream`), changes go through the REST endpoints.

Views (hash routes):

- **Overview** (`#/dashboard`): countdown to the next stop, fuel and consumption, stops to go and saving target,
  driver and tyre wear, class opponents with gap trend, current stint against the plan.
- **Stint plan** (`#/plan`): the recomputed plan with wall clock times, editing of the planned stints (also a generator
  for equal stints), CSV export and print.
- **Settings** (`#/settings`): training parameters per track and car, manual values (overrides) for fuel and
  consumption, language and tyre value direction.
- **Rest** (`#/rest`): for a resting driver: countdown to the own next stint, the time to be online as spotter and the
  next own stints.

The last ready state is kept in the browser, so a reload without a backend shows the last known state. The service
worker caches the app and the last loaded stint plan (network first); the live stream is never cached. Texts are in
German or English (`src/lib/i18n.ts`), the choice is stored per browser.

The tyre wear value of the game (`mWear`) is a fraction of the maximum; its direction is not documented. The overview
shows the raw value as a percentage and the setting "Tyre value 1.0 means new" flips it once checked with a new set.

## Development

    npm install
    npm run dev      # proxies /api to http://localhost:8080 (SPLASH_BACKEND changes the target)
    npm test         # unit tests
    npm run check    # type check
    npm run build    # builds into ../backend/internal/webui/dist, which the backend embeds

`npm run build` has to run before building the backend binary that should serve the app; without it the backend
answers 503 on `/`. Build output is not committed (only `dist/.gitkeep`).
