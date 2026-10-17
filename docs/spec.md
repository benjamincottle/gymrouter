# Spec: Gym Router — constrained public-transport router for bouldering gyms (Sydney)

Status: Approved v1 (2026-10-03). Living document: update the decision log when decisions change.

## 1. Context
Opal app / Google Maps sometimes miss faster routes between home and the gyms we climb at.
Typical misses: tight transfers, walking further to a better service, and entirely different routes.
Feasibility check (2026-10-03) says a constrained app can beat them: few places, a known set of lines
per gym, our own transfer times, and every option checked against live departures.
Also valuable whether or not we beat them: **a live map of where the vehicles are on the route.**

## 2. Goals / non-goals
Goals
- G1: For a chosen gym (or gym → home), rank the best options within that gym's set of lines using live data.
- G2: Show the options on a map with live vehicle positions; the selected trip's vehicles highlighted.
- G3: Personal walking/transfer times, not generic defaults.
- G4: Warn when a connection is at risk, show a countdown to when you need to leave, and follow the trip while travelling.
Non-goals (v1)
- General-purpose trip planning for arbitrary places; non-walking modes (bike/scooter/car);
  fares; accessibility routing; push notifications; multiple users or accounts.

## 3. Data sources (TfNSW Open Data Hub, free API key)
- GTFS static: stops, routes, trips, stop_times, calendar, shapes, transfers/pathways.
- GTFS-realtime: trip updates (delays/cancellations) and vehicle positions, per mode feed.
- Trip Planner API: not used (same engine as Opal). Possible later use for sanity checks.
- The API key is server-side only.

## 4. Places, users, privacy split
- Single user, no accounts; publicly reachable (see §10).
- **Places** are symmetric (name, coordinates, access stops with walk times); any place can be
  origin or destination. Homes and gyms are both places held on the device; a gym also carries its
  **set of lines**.
- **Server config (infrastructure only)**: listen address, data directory, public URL, daily timetable refresh hour,
  realtime polling and budget. Deployed as a mounted file from the private deploy repo (Ansible/compose); the app repo
  ships only an example. Routing and connection-risk defaults are compiled in and overridden per device.
- **Known gyms are built into the app** (`internal/gyms/gyms.toml`, public data): name, address, coordinates, the lines
  that serve the gym end, and optionally measured walks from the door to stops. Every device is offered them, so setup is
  just adding a home. Any other gym can still be added on the device.
- **App repo is public-safe**: no secrets, no real config, no home locations, no recorded data that
  could reveal a home (test fixtures use gym-side or synthetic origins). Secret scanning in CI.
- **Device storage (private data)**: home place(s), **gyms with their line sets and chosen stops**, home
  stops, walking speed, **connection-risk thresholds**
  (start from server defaults; user-tunable), access token. Sent in POST bodies; never stored on the server, except
  the names of the lines requests have named (so a restart can load them; see §5), which aren't tied to any place.
- Multiple homes = more entries in device storage; no code change.
- Initial gyms (9 Degrees): **Lane Cove, Parramatta (Rydalmere), Chatswood** for v1;
  Waterloo and Alexandria were added after the first three. They are built in; others are added in the app (address search, then suggested lines).

## 5. Route model: dynamic routing within each gym's lines
- Each gym has a set of lines that could matter for reaching it (from home, incl. home-side lines),
  e.g. `["bus 370", "train T4", "metro M1"]`. The router finds the best options within that set, so timetable
  changes need no edits. The set lives on the device and is sent with every request.
- GTFS static is filtered at ingest to the union of the line sets the server has been asked about → small
  in-memory timetable. A request naming lines it hasn't loaded makes it reload with the union (a few seconds, once per
  new line). The union is capped, and lines no request has named for two weeks drop out at the day rollover (the known
  gyms' lines always stay). The union is kept in the data directory (`lines.json`: line names and when each was last
  named), so a restart starts with it loaded. The app asks the server to load its gyms' lines in the background as soon
  as it knows them (on opening, after setup, when a gym's lines change), so the first search doesn't wait for a reload.
- A **stop catalogue** (every stop with the lines that serve it) is built from a full-network pass after each timetable
  download. It powers "nearby stops" in setup, which has to work before any lines are chosen.
- Walking transfers between stops on those lines: GTFS transfers/pathways where present, otherwise
  distance × walking factor; **personal overrides win**.
- Access/egress: device-supplied home access stops + walk times; gym access stops from config.
- **The longest walk is a preference, not a wall, and it's measured on foot.** A place with no ticked stops uses every
  stop on the trip's lines within the longest walk (Settings, default 1000 m) **or within 500 m of the nearest stop on
  them**,
  whichever reaches further. Walks are measured along the streets when the street network is there (straight line
  otherwise), so a stop that's close in a straight line but a long way round on foot (across a railway, a river or a
  gully) doesn't crowd out one that's nearer to walk to. Nothing past 3 km in a straight line counts; beyond that the
  place has no stops on those lines and the plan says so. The router chooses among the stops on time, so a station a
  little further beats the closest stop on a poor bus. When even the nearest stop is beyond the longest walk, the plan
  response says so (`stretched_walk`: which end, its nearest stop and the walk to it) and the trip screen says the walk is
  longer than the setting (not once it has been dismissed, see 2026-10-10). Ticked stops
  are unaffected (they work at any distance). Line suggestions find a place's stops by the same
  rule and radius as the planner, so every suggested line is one the planner can reach; setup's nearby-stops list uses
  the rule too.
- **Walks follow real streets, at your pace.** General planners walk through places you can't
  (e.g. through the Johnson factory at Lane Cove) and miss shortcuts that exist (e.g. cutting through the
  plumbing-supply site). Walks to and from stops, and between stops when changing, are routed over a pedestrian network built from OpenStreetMap
  (footpaths, steps, streets; private and no-foot ways excluded). Every walk's time is its route's length at the
  walking speed (5.5 km/h unless set). Where OpenStreetMap is wrong, the fix goes into OpenStreetMap, and the server
  status's "Update now" pulls it in; no walk is timed or stored by hand (see 2026-10-12). A stop ticked for a home or
  gym says which stops to use, not how long the walk is. Until the network has been built
  (first start), walks fall back to straight line × detour and the app says so. Transfers between stops are timed along the
  streets too (cached per stop; pairs that can't be walked within a sensible detour, e.g. across a river, are dropped);
  GTFS pathways inside stations still win.
- The server provisions its own data: timetables (daily), the pedestrian network and the basemap are downloaded on
  first start and refreshed on a schedule, in the background, so a fresh deploy needs nothing but the config and secrets.
- Leg model is mode-generic so bike/scooter legs can be added later.
- Safety net: on every GTFS refresh, report config lines or stops that no longer exist (health + logs).

## 6. Routing & ranking
- **Range RAPTOR** over the filtered timetable with realtime applied (delays, cancellations, skipped stops).
  Returns the best trade-offs between arrival time and number of transfers across several departure times.
- Query modes: **Leave now** (default), **Depart at**, **Arrive by** (lower priority; reverse search, can come last).
- **Connection risk** for each transfer: slack = next departure (realtime) − (arrival (realtime) + personal
  transfer time). Labelled `safe` / `tight` / `at risk` / `missed`. Default thresholds: safe ≥ 3 min, tight 1–3, at risk < 1;
  **user-adjustable in Settings** and sent with each plan request (tuning these is part of how we beat
  generic planners). Options whose
  connection is at risk also show the fallback (the next service).
- **Leave time is per option**: walk times to stops are fixed settings, but each option's leave time =
  first departure − walk to its first stop (− a personal buffer). Options are compared door to door from
  that leave time, and the leave-by countdown uses it.
- Small network → millisecond queries; per-query timeout and size caps still apply.
- Implementation (M2): the range search runs RAPTOR once per distinct leave time in the window (each
  access-stop departure), then keeps the Pareto set over (leave later, arrive earlier, fewer rides).
  That's simpler than true rRAPTOR and fast enough at this network size. Alternatives come from re-running
  with lines excluded (up to two at a time). Options using the same lines and times collapse to the safest
  variant. Each transfer records its slack, risk level and the next service of the onward line.

## 7. Map & live vehicles
- MapLibre GL JS + **self-hosted OpenStreetMap vector extract** (PMTiles file for the Sydney area, served
  by the app, refreshed occasionally). Nothing sent to third parties; OSM attribution shown.
- The map source sits behind a small interface so Google Maps could be swapped in later.
- Overlays: only the option itself: the parts of each line ridden (along GTFS shapes; a train along its own track,
  to the platform it uses) with the stops passed as small
  outlined dots, walking legs as the traced or street route, **the vehicles running your rides** (colour, size, label)
  while they're within about 3 stops of the part you ride, and, during a trip, you.
- Vehicle positions are fetched from the server cache as soon as it says new ones are in (`GET /api/events`, about
  every 15 s while the app is in use), else every 30 s (10 s while the app can't hear the server).

## 8. UI / UX
- Visual design, components and UX conventions: see [DESIGN.md](DESIGN.md).
- Mobile-first responsive PWA (installable); on desktop (1024px and up) the trip and the map sit side by side.
- **Home screen**: one button per gym, plus a direction toggle (to gym / home). Tapping one runs "Leave now".
- **Results**: ranked option cards. Each shows departure, arrival, legs, transfer count, connection-risk
  badges, and a **leave-by countdown** on the top option (auto-refreshing). Time controls for Depart at / Arrive by.
- **Map view**: the selected option + live vehicles (see §7).
- **In-trip mode**: started from an option. Uses browser geolocation (on device; position sent only in
  plan requests) to track progress. Re-checks the remaining legs on each refresh and suggests a switch if
  something slips (missed connection, delay, cancellation). Keeps the screen awake while active.
- **Settings**: homes (pick on map → suggested nearby stops with estimated walk times, editable),
  walking speed (typed, or measured with a short walk), export/import (link + JSON), share setup (link + QR).

## 9. Architecture
- Single Go binary (stdlib `net/http`, `encoding/csv`, `archive/zip`; deps limited to
  `google.golang.org/protobuf` + generated GTFS-rt bindings + a TOML parser).
- Internal packages: `gtfs` (streaming filtered parse, fixture writer), `timetable` (assemble a day from both bundles),
  `lines` (line = mode + short name), `tfnsw` (API client, feed table), `gtfsrt/pb` (generated bindings),
  `realtime` (decode + apply predictions; poller + cache in M3), `raptor` (router), `plan` (window search,
  alternatives, connection risk), then `config`, `api` (HTTP handlers + auth middleware), `tiles` (PMTiles file serving with HTTP range requests).
- Frontend: TypeScript + MapLibre GL JS, built by Vite, embedded via `go:embed`. Minimal deps; no UI framework
  unless needed (decide during build).
- API (JSON; all `/api/*` require the token header except `/healthz`):
  - `GET  /api/defaults` — routing/risk defaults and the known gyms.
  - `POST /api/plan` — {from, to: place (lat/lon + optional access stops), lines, mode, time, prefs} → options.
  - `GET  /api/lines?lines=<list>` — the lines' colours, without loading them.
  - `POST /api/stops/near` — {lat, lon} → nearby stops (all lines serving them) with estimated walk times.
  - `POST /api/suggest-lines` — {from, to: [places]} → a job id; `GET /api/suggest-lines/{job}` → progress, then per destination the candidate lines with how often they appear in the best options.
  - `GET  /api/vehicles?lines=<list>&rides=<list>` — live vehicles running your rides, near the part ridden.
  - `GET  /api/shape?…` — one ride's path and the stops it passes, for drawing.
  - `GET  /api/events` — a stream of events: new live data was applied (the app re-plans), new vehicle positions
    came in (the app fetches them).
  - `GET  /api/status` — detailed health (auth). Implemented API: see `docs/api.md`.
  - `GET  /healthz` — liveness, feed ages, config warnings (no sensitive detail).

## 10. Access, security & privacy (public, no login)
- Public hostname through Traefik with TLS; Traefik rate-limit middleware.
- **Setup-link token**: secret in Ansible Vault → env; server compares in constant time against its hash.
  First device: `docker compose exec app gymrouter setup-link` prints `https://<host>/#setup=<token>`.
  The app saves the token and removes it from the URL. More devices: "Share setup" link/QR.
  Revoke = rotate the secret. Without a token the page shows only the name and the mark (nothing to press), and the API
  answers unauthenticated requests with a plain "404 page not found" (as Go and Traefik do).
- No personal data on the server; private settings travel in POST bodies; request bodies and
  query strings with coordinates are never logged.
- **Key protection**: the server alone polls TfNSW; client requests never trigger upstream calls.
- Hardening: distroless, non-root, read-only root FS, no shell; strict CSP (self + blob for MapLibre
  workers); request size limits; per-query timeouts; dependency updates via Dependabot/Renovate.
- Storage durability: `navigator.storage.persist()`; export link (data in URL fragment, never sent to
  the server) / JSON file, mainly as an iOS safeguard. Targets: Android Chrome + desktop; iOS supported.

## 11. Hosting & ops
- docker compose stack behind Traefik, deployed by Ansible: one app container + one data volume.
- GTFS static: daily download (and on startup if missing) → filter + parse → swap in atomically; keep the
  last good copy if a refresh fails.
- GTFS-realtime: **demand-driven polling** (trip updates ~30 s, vehicle positions ~15 s) only while a
  client with a valid token was active in the last ~10 min; hard ceiling on the upstream call rate.
- No database in v1 (config file + feeds on the volume + device-side personal data).
- Structured logs, `/healthz` with feed staleness. CI/build (GitHub Actions, registry): deferred.

## 12. Testing & validation
- Go unit tests: RAPTOR on small synthetic timetables (transfers, overnight trips, calendar exceptions,
  cancellations, arrive-by); risk classification; config validation; token auth.
- Fixture tests: a trimmed real TfNSW GTFS slice + recorded realtime snapshots → expected options
  for each v1 gym (golden files).
- **Walking-knowledge cases**: for each gym, check that its access-stop walk times reflect the real paths
  (e.g. the Lane Cove factory and shortcut cases).
- **Real usage windows** (test and validation focus): **Sunday morning, arriving around opening time**,
  and **Thursday afternoon, leaving home around 16:00**. Fixtures and golden tests use these windows first.
- **Known-alternative cases**: e.g. T1 → St Leonards → bus (291/287) → Lane Cove. Slower on paper
  (~70 vs ~55 min Thu 16:00) but sometimes fastest in practice; the router must surface it when realtime
  makes it win (delays on the Epping/metro option).
- **Seed real-world cases**: 3–5 logged trips where Opal/Google got it wrong, each as a regression test
  (timetable + realtime snapshot + expected better option).
- Frontend: type-check + lint; a Playwright smoke test (load, plan, map renders) — light.
- Field validation: ~2 weeks of real trips compared against Opal/Google; record wins/losses.

## 13. Risks / to verify in an early spike
Verified 2026-10-03: see `docs/spike-findings.md`. Train trip-ID gap resolved in M2: the unmatched train updates carry
no predictions (other timetable versions) and are ignored; run-number matching covers renumbered trips with times.
- TfNSW GTFS-realtime coverage and format per mode (vehicle positions for buses/trains/metro/light rail;
  TfNSW protobuf extensions).
- Whether trip IDs match between static and realtime feeds (known TfNSW forum pain point).
- Size of the full GTFS bundle; memory use of a streaming, filtered parse.
- Free-tier quota and rate limits (confirm against the current Open Data Hub docs).
- PMTiles extract size/refresh process for the Sydney area.

## 14. Line-set curation
- `POST /api/suggest-lines` (and the `gymrouter suggest-lines` CLI, which shares the code): given two places, loads the
  whole network for a typical weekday and a Sunday, searches home → gym at many departure times, and reports which
  lines appear in the best options and how often, plus the most common itineraries. The app adds the lines that
  appear in the best options for at least 30% of departure times to a gym's built-in lines; the user adds or removes lines and saves them on the gym. Coordinates are used in memory only.
- The full network is loaded for the duration of one request (about 5 s and 150 MB live per day), one request at a time.

## 15. Milestones
1. ✅ **Spike**: fetch GTFS + realtime with the key; check §13 risks; build `suggest-lines`; curate
   line sets for Lane Cove, Rydalmere, Chatswood. Log the 3–5 seed cases where Opal/Google got it wrong.
2. ✅ **Router core**: filtered GTFS model, Range RAPTOR, realtime overlay, connection risk; unit + fixture tests.
3. ✅ **Server**: config, token auth, demand-driven poller, API, `/healthz`, `setup-link` CLI.
4. ✅ **UI**: PWA shell, settings + export/import/share, gym buttons, option cards, countdown.
5. ✅ **Map**: PMTiles serving, MapLibre, shapes, live vehicles with highlighting.
6. ✅ **In-trip mode** and **Arrive by**.
7. ✅ **Deploy**: Dockerfile (distroless), compose example, Traefik labels, CI build/scan/publish.
8. **Self-service setup**: gyms and their lines move to the device; stop catalogue; suggest-lines endpoint and
   review screen; the server needs no gym config.
9. **Field validation**: ~2 weeks of trips compared against Opal/Google.

## Prerequisites (before milestone 1)
- From the user: a TfNSW Open Data Hub account + API key with the GTFS static (complete timetables) and
  GTFS-realtime (trip updates + vehicle positions, all modes) APIs added. The user puts it in a
  gitignored `.env` (`TFNSW_API_KEY=...`) in the repo; it's never pasted into chat or committed.
- From the user: the 3–5 seed cases (date/time, from, to, what Opal/Google said, what was actually better).
- From the user: exact home location, given in chat for `suggest-lines`. Used only as a CLI argument;
  never written to the repo, fixtures, or logs.
- Local tooling: already present (Go 1.27, Node 26, Docker 29, gh). Nothing else needed.
- Not needed until milestone 7: hostname, Traefik entrypoint/cert-resolver names, image registry.

## Decision log
- 2026-10-03: Feasibility = go. Output of the planning phase is this spec, not code.
- 2026-10-03: Live vehicle map in scope; show all vehicles on the gym's lines, selected trip highlighted.
- 2026-10-03: Single user; places model supports multiple homes at no extra cost.
- 2026-10-03: Deploy as a docker compose stack behind Traefik (Ansible-managed homelab); CI deferred.
- 2026-10-03: Routes computed dynamically within each gym's set of lines (not hand-authored).
- 2026-10-03: Stack = Go backend (minimal deps) + TypeScript/MapLibre frontend in one distroless image.
- 2026-10-03: Public, no login; personal data only on the device; server polls and caches TfNSW feeds.
- 2026-10-03: Access gate = setup-link token.
- 2026-10-03: v1 modes = public transport + walking.
- 2026-10-03: Map = self-hosted OSM vector extract (PMTiles); Google tile scraping rejected (breaks Google's terms of service).
- 2026-10-03: Time modes: Leave now, Depart at, Arrive by (Arrive by is lower priority).
- 2026-10-03: v1 extras: connection risk, leave-by countdown, in-trip mode. Push notifications out.
- 2026-10-03: Gyms are config data; v1 = 9 Degrees Lane Cove, Parramatta (Rydalmere), Chatswood.
- 2026-10-03: Line sets: the tool suggests, the user curates.
- 2026-10-03: App repo public-safe; deploy config in a private repo.
- 2026-10-03: Connection-risk thresholds have defaults and are user-adjustable (stored on the device).
- 2026-10-03: Gym-end walking uses curated walk times + optional hand-drawn paths, not street-network
  routing. This is a key source of advantage (Lane Cove examples).
- 2026-10-03: Spike: trains load from the per-mode Sydney Trains bundle (better realtime match); line = mode +
  short name; school buses excluded; poller only fetches the feeds the active gym's lines need (Bronze quota 60k/day, 5/s).
- 2026-10-03: M2 done: router core with realtime overlay (delay propagation, skips, cancellations,
  run-number matching for trains, realtime-only trips), window search, alternatives, connection risk + fallback,
  golden tests on a trimmed real-data fixture (CC BY 4.0, attributed). Shapes loading moved to M5 (map).
- 2026-10-03: M3 done. Config is TOML (public data only; unknown keys rejected). Secrets come from env only
  (`TFNSW_API_KEY`, `GYMROUTER_TOKEN`); `new-token` generates the token and the server keeps only its SHA-256 digest.
  Static feeds use conditional download with atomic replace. Live predictions older than 3 min are ignored
  (timetable used instead). Default daily upstream budget is 40k. `/healthz` is minimal; `/api/status` has the detail.
- 2026-10-03: M4 done. Frontend uses Preact (user confirmed over plain TypeScript): ~4 KB, no transitive deps;
  built by Vite and embedded into the binary. Home setup uses address search through the server (TfNSW stop_finder,
  rate-limited, never logged) or device location, then nearby stops grouped by station. Share/backup links
  are deflate-compressed in the URL fragment (`#z=`) so the QR code stays scannable.
- 2026-10-03: In-station transfers now use GTFS pathways (stairs/escalators/corridors + 60 s platform allowance;
  lifts only as a fallback) instead of straight-line distance. Found in UI testing: Epping train→metro showed 0 min.
- 2026-10-03: M5 done. MapLibre + PMTiles (map file in the data dir, served with range requests behind the token);
  basemap fonts/sprites self-hosted, so the map makes no third-party requests. Leg paths follow GTFS shapes
  (loaded in the background, simplified to 4 m), falling back to stop-to-stop lines. The map shows the gym's
  lines faintly, the chosen option boldly, all vehicles on those lines muted, your own vehicles large and
  labelled, and frames the vehicle for your first ride. MapLibre is lazy-loaded (main bundle stays ~40 KB).
- 2026-10-03: Visual redesign (frontend-design pass): chalk wall + graphite, colour only for line colours and risk;
  Archivo with condensed widths for times; results as a countdown plus option "tape" strips on a shared
  time axis instead of stacked cards.
- 2026-10-03: M6 done. Arrive by searches departures in the 150 min before the deadline and returns options arriving
  by then, latest departure first. In-trip mode: the app tracks the phase from live times (plus GPS when the user
  turns it on), re-plans every 30 s from the vehicle you're on (`on_trip` origin: you can stay on, get off, or walk
  to a nearby stop, and that change is rated), from your position, or from the next stop. It compares the result
  with the committed trips: on track (with lateness), a faster option (≥ 3 min sooner), or a missed connection with
  the next best, offered with one-tap "Switch to this". Screen wake lock while travelling; the active trip
  survives reloads (stored on the device only).
- 2026-10-03: M7 done. Multi-stage Dockerfile (Node builds the frontend, Go cross-compiles from the build
  platform, final image distroless static :nonroot, ~16 MB, built-in `healthcheck` command). Compose example
  matches the homelab pattern (Traefik `websecure`/`letsencrypt` on `traefik_default`, read-only, cap_drop ALL,
  no-new-privileges, uid 65532, 512 MB limit, per-IP rate limit), host `gymrouter.example.com`. GitHub Actions
  mirror the existing repos: CI (Go tests with race, frontend checks, actionlint, PR Docker build), build and
  publish `linux/arm64` to GHCR with a daily base-image digest check, daily govulncheck and npm audit,
  grouped weekly Dependabot. Verified: container healthy ~22 s after first start (timetable download
  included), ~117 MB in use.
- 2026-10-03: Gyms and their line sets move from server config to the device (supersedes "Gyms are config data" and
  "Line sets: the tool suggests, the user curates" as a deploy-time step). Why: a line set depends on gym *and* home,
  so it is as personal as the home; the private deploy config existed only to hold it; and adding a gym or moving house
  should not need a redeploy. Consequences: plan/vehicles/shapes take the line list in the request; the server loads
  timetable data for the union of lines it has been asked about (grows on demand, capped, in memory only); a stop
  catalogue lets setup list nearby stops before any lines exist; `suggest-lines` becomes a one-at-a-time endpoint that
  loads the full network per request (not kept resident); `[[gym]]` in server config is optional presets. Gym-end
  walking stays measured by hand (curated access stops with walk times), now also on the device.
- 2026-10-03: Refines the previous entry after use. The known gyms (9 Degrees Lane Cove, Parramatta, Chatswood) are built
  into the app as public data, with their gym-end lines and room for curated door-to-stop walks, rather than being forced
  through setup: curated data is more precise than anything suggested, and a new device then only needs a home. Setting up
  adds the gyms (all ticked) and finds the lines near home for all of them in one server pass (the full network is loaded once
  per day searched, not once per gym). Custom gyms still work through the editor. Server config shrinks to infrastructure
  (`[server]`, `[realtime]`); `[routing]`, `[risk]` and `[[gym]]` are gone and rejected as unknown keys. The server preloads
  the known gyms' lines at startup.
- 2026-10-03: Walking over real streets (supersedes "never route over a street network"). Why: straight line × 1.3 skews the
  walk to the first stop and from the last one, which dominates short trips, and it is wrong in both directions around rivers,
  rail lines and cul-de-sacs. How: a pedestrian graph built in-process from an OpenStreetMap extract (BBBike's weekly Sydney
  extract, ~57 MB; the Australia-wide file is ~1 GB), with a minimal PBF reader on the existing protobuf dependency, cached as a
  compact file in the data volume and routed with a bounded Dijkstra. Curated walks still win. The server now fetches everything
  it needs itself (the pedestrian network, and the basemap via the pinned `pmtiles` tool shipped in the image) instead of these
  being deploy steps; failures leave the app working on fallbacks and are retried, and `/api/status` reports them.
- 2026-10-03: Transfers between stops use the street network too (not just trip ends). The router takes an optional footpath
  hook; the engine fills it with street walks per stop, in parallel and cached by stop ID so live-data rebuilds don't redo them
  (timetable load went from ~3 s to ~4 s with the full Sydney graph). Whole-network passes (stop catalogue, line suggestions)
  keep straight-line transfers: tens of thousands of stops, and only roughly right is needed there.
- 2026-10-03: Measured walks. Rather than guess or type minutes, any walk from a home or gym to a stop can be timed in the app
  and traced with GPS (Start, walk, "I'm here"), with the screen kept awake. The time is the mean of the last five walks; the
  traced route (simplified to ~80 points) is stored with the stop on the device and drawn on the map in place of the street-map
  route. Poor GPS readings and jumps are discarded; the app warns if the walk started far from the place or ended far from the
  stop. The server only ever sees the resulting number (as before); traces never leave the device. Found while testing: the
  street graph routes Lane Cove's Epping Rd stops the long way round the factory block (~17 min), and the real shortcut
  through the plumbing supplier is about 10 min, which is exactly what this is for.
- 2026-10-04: After field use, the map shows only the trip (supersedes "all vehicles on the gym's lines, the rest muted"
  and the faint network): the sections ridden with their stops, your own vehicles only from ~3 stops before you board to
  ~3 stops after you get off (the server filters by the feed's current stop, else the nearest call), and your position as
  soon as the map opens during a trip (location is on by default in a trip). `GET /api/shapes` is gone. Trips planned with
  Leave at / Arrive by can be started too; until it's about to leave, in-trip checks plan from the trip's own start and time.
- 2026-10-04: Line suggestions run as a server-side job that the app polls (with a progress percentage), because adding all
  five built-in gyms in one request timed out in the field (three then two worked). Splitting per gym was rejected: reading the
  timetable is most of the cost (~12 of ~14 s locally for one gym, ~1 s per extra gym), so it would multiply the work. One job
  at a time, up to 12 destinations, kept 10 minutes after finishing; a dropped poll is retried.
- 2026-10-04: Walk timing moves from home/gym settings into the live trip (supersedes the per-stop timer of 2026-10-03). In
  a trip, "Time this walk" offers the walk you're on (to the first stop, a change, or from the last stop) and any other walk
  of the trip. Timed walks are their own list on the device, keyed by place (a built-in gym by its ref, so re-adding it keeps
  them) and stop or station, and count in both directions. The first timing replaces the default time and its GPS trace is
  drawn from then on; later timings average (last five) or replace, by a setting, with the other offered when saving.
  Plan requests carry a place's timed walks as `walks`, which beat curated and street times and add stops that aren't ticked
  under the home or gym (they weren't usable before, which is likely why saving such a walk seemed not to work). Timed
  changes are sent as transfer times both ways. Older per-stop timings and "Set my time" change times are migrated.
- 2026-10-04: No visible front door. A browser without a token sees a page indistinguishable from a plain-text "404 page not
  found" (no icon, manifest or service worker), and unauthenticated API requests (and non-GETs to the site) get Go's own
  404 instead of 401 JSON, which the app recognises by its plain-text body. The paste-a-setup-link form is gone; a setup link
  is the only way in. The page itself is still served with 200 (the app's HTML must load to read the token from storage).
- 2026-10-04: Two more built-in gyms, ClimbFit Macquarie and ClimbFit St Leonards (lines found as for Waterloo/Alexandria).
  Built-in gyms carry a `brand`, and the gym lists show that brand's small logo (`web/public/brands/`, 64 px PNGs from the
  brands' own sites; ClimbFit's reduced to its teal mountain mark so it reads at 32 px on light and dark).
- 2026-10-04: A highlight colour (Settings), one of the nine 9 Degrees grade colours, for underlines and selection marks
  only (the current screen, the chosen gym's strip, the chosen option's bar, an accent in each Settings icon); structural
  rules stay ink. Black (the ink) is the default. The header loses its rule: the current screen is underlined instead.
  Dates are chosen as a day ("Today (4th)", "Tomorrow (5th)", … about a week) and a 24-hour time in five-minute steps,
  never the browser's locale-dependent picker, and times on another day name it. The map stops re-fitting itself once
  you've moved or zoomed it. "Earlier trips" works from Leave now too.
- 2026-10-04: A started trip shows the option's full description (as under the option list) with a rail down the left in
  each leg's colours (dotted for walking, stops as on the map) and you on it as the blue dot, placed by how far through
  the current step the clock is. It replaces the horizontal progress strip; the summary and switch alerts stay above it.
  Walks are timed from small "Time my walk" links on each walk and change in it (before a trip, changes keep "Set my
  time"). The trip's map shows the same description under it, scrolling on its own, so both fit on one screen.
- 2026-10-04: Walk times only come from recordings made on a trip. "Set my time" (typing a change time) and the minutes typed
  per stop in the home/gym editor are gone: a recording has a route, which is what matters (shortcuts the street map doesn't
  know); for ordinary walks the street routing is right. Ticked stops take the street map's estimate.
- 2026-10-04: Greenfield: no support for data from earlier versions of the app. The settings migrations (old per-stop
  timings, hand-set change times) and other fallbacks for older data and servers are removed; stored data that doesn't
  match the current shape is simply dropped by validation.
- 2026-10-04: Which stop a timed walk belongs to depends on the mode. A train, metro or light-rail platform is keyed by its
  station (any platform counts: they're inside, a minute apart); a bus or coach stop is keyed by itself, including stands that
  belong to a station (the stand across the road is a different walk). The server applies a station-keyed walk or change
  time to the station's rail platforms only. A walk still counts both ways for the same stop.
- 2026-10-04: Round 6 polish. Gyms are listed most used first (each choice counts; the order is fixed while the trip screen
  is open). A started trip's rail begins at home (or the gym's hold) with you there until you leave. The map opens fitted
  to the whole trip. Settings: Auto / Light / Dark appearance (driven by `<html data-theme>`, set before first paint), icon
  buttons to edit and delete, and "Measure my pace": a minute or two of ordinary walking with GPS, counting only the time
  spent moving (readings under 0.5 m/s are standing still), at least 60 s and 100 m, 0.6–2.5 m/s to count as walking.
- 2026-10-04: During a trip, the "you" marker on the description is placed from your location, not the clock: each step has
  a line on the ground (the walk's path, the ride's route shape, or straight between stops) and you're put on the nearest,
  favouring the step the clock expects (40 m per step away) so a nearby part of the route can't steal you. At the meeting
  of two steps you stay on the earlier one (still at home; waiting at the stop). The clock is the fallback when location is
  vaguer than 100 m or you're more than 150 m (or twice the accuracy) off the route. Now / Then still follow the clock and
  live times. Ride shapes are cut where each stop projects onto the line, not at the nearest corner, so a line no longer
  runs past its last stop and back.
- 2026-10-04: Now / Then follow where you are, not the clock (one calculation shared with the marker and the re-checks).
  You're "waiting" within 40 m of the stop you're boarding at, and "on board" once you've moved 120 m along the ride's line;
  progress only goes forward unless you're more than 150 m back from where you were placed; with no usable fix you stay put
  and only a ride you're on carries on by the clock. Without any location the clock decides, as before. Next: matching
  your movement with your vehicle's live position to confirm boarding sooner.
- 2026-10-04: Vehicle matching. While you wait for or ride a vehicle, its live position is polled every 10 s and compared
  with where you were at the same moment (your fixes, interpolated): with it (within 50 m plus your GPS accuracy) at two
  reports between which it moved 50 m or more means you're aboard, sooner and surer than the 120 m along-the-line rule; it
  moving off more than 150 m from you while you stay put means it left without you, and the trip is re-checked straight
  away. Once you're aboard and your own fix goes stale (a tunnel), the vehicle's position stands in for yours.
- 2026-10-04: A landing page instead of the fake 404, and a new mark. A browser without a token sees the wordmark under the
  mark at page scale: the orange line runs in from the screen edge along a row of bolt holes, with a stop every other hole,
  to the foot of a faceted climbing volume bolted into the wall (it draws itself once on load; still under reduced motion).
  Still no controls, and the API's unauthenticated 404s are unchanged. The icon is the same drawing on a graphite tile
  (holes every 12 of its 64 units, the grid the landing page uses too), replacing the faceted hold that read as a nut.
  Setup links, backups and stealth API behaviour are as before; links that would change saved data now ask first.
- 2026-10-05: UI consistency pass, step 1–2 (audit, then direction). The audit found ~17 greys, 18 font sizes, 6 weights,
  22 spacing values, 5 callout styles and 51 computed button styles, no desktop layout, dead-end error states, native
  confirm dialogs, mixed 12/24-hour times and five duration formats. Direction: refine the current look (variant A:
  countdown hero, options board, steps), written down as docs/DESIGN.md. Decided: a two-pane desktop layout (trip beside
  an always-visible map); one callout with four tones taken from the risk colours; one time format per kind (24-hour
  clocks; minutes for predictions, compact only in the board; whole-minute spare time; stopwatch for measured walks);
  in-app confirmation only (inline for one item and End trip, a dialog sheet for whole-device actions); the gym leads the
  trip screen with a reverse button instead of the To the gym / Home toggle; the commit action in a bar pinned to the
  bottom; a darker edge colour for control outlines; details on a second line instead of middle dots; defaults shown one
  way ("Empty uses the default"); End trip moved away from the map's Back. Disabled buttons: a thin greyed outline.
- 2026-10-05: Planning speed. A search works only over the stops trips call at (a few thousand) rather than every stop in
  the feed (~171k): planning allocated ~1.4 GB per request and spent most of its time clearing memory and collecting
  garbage. Searches reuse their working memory. The independent searches (one per leave time; each level of
  alternatives) and the two ends' walks run on all cores, with results combined in a fixed order so the options are
  identical. On a laptop, a plan went from 230 ms to 13 ms on one core and ~5 ms on four. Benchmarks
  (`GYMROUTER_BENCH_DATA`) run over the real timetable and street network.
- 2026-10-05: The loaded lines survive a restart and load ahead of the first search. After a restart (a deploy, a reboot)
  the server had forgotten the lines requests had named, so the first search to each gym waited for the timetable to be
  read again (seconds on the Pi). The union, with each line's last use, is now saved in the data directory (reverses
  "never written to disk": line names aren't sensitive, and the volume is private). The app also sends its gyms' lines
  to `POST /api/lines` whenever they become known or change, which loads them in the background.
- 2026-10-05: On the trip screen, When (and which home) now comes before the gym, and stays on top once a gym is chosen
  (follows on from "the gym leads the trip screen"). Choosing a gym starts the search, so setting the time first means
  no search runs for a trip that wasn't meant. Time edits search once they've been still for half a second.
- 2026-10-05: Trackwork buses come with the train line they replace. TfNSW plans trackwork into the timetable: the trains
  are left out, and the buses are added as replacement-bus routes named afresh for each closure (20T4 and 23T4 for a T4
  weekend, 12CN for the Central Coast & Newcastle line), and those names are on the buses' signs. A gym's lines name
  `train T4`, so before this the buses were never loaded and a trackwork weekend planned as if the closed section had no
  service ("Nothing leaves", or a worse route with no reason given). Now the line code at the end of a bus's name (T1–T9,
  CN, BM, SC, HU, SH; M for the metro, as in 10M and 11M, which isn't in the timetable yet) ties it to its line: loading, routing, vehicles and the realtime feeds treat it as part of that line.
  Matching by the stations a bus serves was rejected: event shuttles (Olympic Park, Bathurst) are replacement-bus routes
  too and would be offered for everyday trips. A bus named like a trackwork bus that can't be tied to a line (a new code, or
  typed as an ordinary bus, which may be how metro buses turn up) and calls at a loaded line's station is logged and listed in `/api/status` (`unknown_trackwork`), so a new code shows up rather than failing
  quietly. Replacement buses are never lines you pick: they're left out of the stop list and manual line entry, and line
  suggestions credit them to the line they replace. The trip screen shows a caution callout for a line whose buses the
  options use, with the names on the buses' signs. Known gaps: few replacement trips have live data (56 of ~4,200 seen),
  so their times and connection risks are mostly timetable; unplanned disruptions (buses called in on the day) aren't in
  the timetable and aren't covered; route shapes are loaded for today's trips, so a future day's bus is drawn straight.
- 2026-10-05: The app's footer shows the server's state: a green, orange or red dot with "Server OK", "Server working,
  needs a look" or "Server can't plan" ("Can't reach the server" when it doesn't answer), then each issue in plain words.
  So a problem the server can see (a failing timetable or street-map download, live data not coming through, trackwork
  buses it can't tie to a line) gets noticed without anyone calling `/api/status`. The server decides the verdict and
  reports issues as codes; the app words them. Only what's actionable and usually absent counts: lines missing today
  don't (weekday-only buses are missing every weekend), nor does one failed poll before the next good one. The app checks
  on opening and every 5 minutes while open, and checking doesn't count as using the app, so it never keeps TfNSW polling
  running by itself.
- 2026-10-05: The highlight colour setting is gone (supersedes "A highlight colour (Settings)"). It wasn't adding
  anything: underlines, the selected row's bar, the route tape and the icons' filled parts are ink, black on the light
  theme and white on the dark one. A saved highlight choice is simply dropped.
- 2026-10-05: The longest walk falls back to a band of stops rather than refusing (see §5). A home or gym more than
  1000 m (the default) from any stop on its lines planned nothing: "no stops on those lines within 1000 m". Ticking stops
  got round it but pins the place to those stops, so lines added later go unused, and timed walks can't start without a
  first trip. Now an end with nothing in range uses the nearest stop on the lines plus any within 500 m further (along the
  streets), up to 3 km, and the trip screen says the walk is longer than the setting. Using only the single nearest stop
  was rejected: it's often the worst choice (a stop on an infrequent bus when a station is a few minutes further), and the
  band costs the router next to nothing. Raising the default was rejected too: it widens every search for everyone to fix
  the rare far place. Line suggestions now use the planner's rule and radius (they used 1200 m against the planner's
  1000 m, so they could suggest a line from a stop the planner then wouldn't use).
- 2026-10-05: The longest walk is measured on foot, and the 500 m band always applies (supersedes the band of the entry
  above, which only applied when nothing was in range, and the straight-line limit). From a home in a gully the bus
  stops were 650 m in a straight line but 22 minutes' walk, and the station 1.2 km in a straight line but 18 minutes. The
  straight-line limit took the bus stops and never the station; once line suggestions (which couldn't reach the station
  either) had added that bus to the gym's lines, the band never ran, and an evening with no bus planned nothing. Now a
  place uses every stop within the longest walk on foot or within 500 m (on foot) of its nearest stop, whichever reaches
  further. The setting is called "Longest walk to a stop", so on foot is what it always claimed to be. The note on the
  trip screen still shows only when even the nearest stop is beyond the setting.
- 2026-10-08: A trackwork bus's line code is T, M or L and a digit, or two letters; light-rail replacement buses (1L1,
  1L2) come with their light-rail line. Follows on from "trackwork buses come with the train line they replace". The
  Bathurst 1000 shuttles (771V8, 772V8, 773V8) call at Bathurst Station, a train station, and their names read as an
  unknown code V8, so the server reported "trackwork buses it can't tie to a line" for an event shuttle. Light-rail
  replacement buses are named the same way as trackwork buses, with the light-rail line's code, so they now join that
  line rather than being reported. Names ending in anything else (V8) are event shuttles, like 8 and 5B. The cost: a
  new code shaped like a letter other than T, M or L and a digit wouldn't be reported. The L names aren't in the
  timetable yet, so, like the metro's 10M, they're unchecked against real data.
- 2026-10-08: Ride shapes are cut, and the stops passed placed, at each call's `shape_dist_traveled` (how far along the
  shape the feed says the stop is) when the feed gives it for the shape and the ride's calls; it supersedes cutting where
  each stop sits beside the line (2026-10-04), which stays only for the trains feed, whose distances are empty. Placing by
  position went wrong wherever a bus runs both ways along a street (loops, out-and-back runs, turning loops): both passes
  are a few metres from the kerb, and the map drew 613 of 2,483 Sydney bus shapes on some leg out to the loop and back,
  up to the whole route (the 680 drew 45 km for a 123 m ride). The feed's stop distances land within 12 m of the stop
  (95th percentile) and in the same unit as the shapes.
- 2026-10-08: A place's band starts at the nearest stop on the trip's lines, as §5 always said; the server had been taking
  the nearest stop on any line it had loaded, which includes lines only someone else's home uses. At Lane Cove the Mars Rd
  stops (258, 285: 423 m on foot) set the band to 1300 m, so the 291's stop on Epping Rd (1430 m round by the streets,
  290 m in a straight line) was left out; the trip still got off there and walked on to another stop. Whether a stop
  counted depended on what other people's lines happened to be loaded. A walk joined from a change and the final walk
  (off at one stop, on foot to another, then to the place) is now drawn through the stop between them, each piece along
  the streets; it was drawn as one straight line, because the street search from the place doesn't reach the stop you
  get off at. Raising the default longest walk to 1500 m was rejected as the fix: it would have caught this stop by
  70 m and widened every search for everyone (see 2026-10-05).
- 2026-10-10: SH is the Southern Highlands Line's trackwork code (`6SH`, Campbelltown to Moss Vale, calls at a T8
  station), so the server stopped reporting it as a bus it can't tie to a line. The report did its job: a new code showed
  up in the footer rather than being silently left out.
- 2026-10-10: On the way to a stop, the time to spare and the re-checks go by the trip's own walk. The time to spare was
  the straight line to the stop × 1.3 at walking speed, and a re-check planned from the GPS position, so the server
  routed the walk over its street map from wherever the phone said you were. Neither knew about a walk you had timed
  and traced: a shortcut the map doesn't have read as "you need to hurry", and could read as a missed service. Now you
  are placed on the walk's own line (your trace if there is one, else the street route) and what's left is that share
  of the walk's time, plus getting back to the line if you're off it; the re-check sends that figure for the stop you're
  heading to along with your position (`walks`), so other stops are still routed from where you are.
- 2026-10-10: The walking pace is learned from timed walks. With no speed set in Settings, walks that haven't been timed
  are planned at the pace the timed, traced ones show: all their traced distance over all their time, so a long walk
  counts for more than a short one, and waits at crossings are in it (it's the door-to-stop pace planning needs, where
  "Measure my pace" counts only the moving stretches). A walk under 100 m or a minute, or one whose trace works out
  slower than 0.6 m/s or faster than 2.5 m/s (a lift, a lost signal), is left out. A speed typed into Settings, or
  taken from "Measure my pace", wins; clearing the field goes back to the learned pace, and to the server's default
  when no walk has been traced. Nothing new is stored: it's worked out from the walks each time.
- 2026-10-10: Each option's tape fills its row on the trip's own scale; the time axis shared between the options goes.
  On the shared axis a 50-minute trip took about half of a 185px strip, so a 3-minute bus was a 5px sliver with no
  room for its name, and which lines a trip takes is the most important thing on the board. The spread the axis showed
  (when each option leaves and arrives against the others) is already in the times either side of the tape. Now rides
  and walks are as wide as their share of that trip, a ride is never narrower than its name (the walks give up the
  room), and walking is a run of dots: between two rides the walk and the wait are one run. Chosen from mockups of four
  alternatives; kept-axis versions (a minimum ride width, names as tags above the tape, a slim axis bar under a fitted
  tape) were rejected as more to read for the same answer. The risk mark now sits evenly between the arrive time and the
  door-to-door time.
- 2026-10-10: A trip under way is checked as it stands, not searched for. Every 30 s the app planned again and looked
  for an option with the same vehicles; none meant "You won't make the planned connection". But a search returns the
  best trips, not every possible one, so a good trip could be missing for reasons that had nothing to do with making
  it. In the field the warning came up on trips that were fine and went away again. Four causes, each reproduced:
  - a later departure with the same arrival beats yours, so the search drops yours (start the 12:14, and the first
    re-check lists the 12:24 instead);
  - you stay "riding" until you've moved along the change (at a same-stop change, until the next vehicle), and once
    the vehicle's time at your stop has passed the server couldn't start a search from it;
  - from the vehicle, the first change used the street map's time whatever you had timed it at;
  - from the vehicle, the first change skipped the change buffer.

  Now the app names the rides still ahead (`keep`) and the server describes exactly that trip with live times
  (`kept`): each change rated as when planning, with your own change times and the buffer, `missed` when there's no
  time left for it; and, before a ride, the time to spare on reaching it. That alone decides whether the trip still
  works. The search still runs alongside, only to offer something faster or something else, and never offers a trip
  with a change there's no time for. The warning says what won't be made ("You won't make the change at North Ryde
  Station: the 288 leaves at 12:34"). Around it:
  - at the stop you get off at, once the ride's time there has come, you're off it (waiting for, or walking to, the
    next), rather than riding until you've moved on;
  - a location that has been quiet for 90 s (a tunnel, an underground platform) isn't used: the clock decides, as
    with no location at all. Before, the last fix at a station entrance kept you "walking to the stop" while you rode;
  - with no location, after the time to leave you're taken to be walking to the stop as planned, not still at home;
  - the trip's live times keep updating when a change in it has gone.

  Two more, decided the same day:
  - a warning that's showing stays until the thing it's about has the "tight" setting to spare again (1 min by
    default). It still appears the moment there's nothing to spare; only clearing waits, so a connection near the
    line doesn't come and go with each live update. Waiting for two checks in a row was rejected: the warning would
    come up to 30 s late, when time matters most;
  - "a faster way" is only offered when its changes are no riskier than those still ahead of you, and both it and
    "next best" say how sure the alternative's changes are. It had gone by arrival time alone, so a trip 4 minutes
    faster on paper could be offered over a safe one while hanging on a 20-second change.

- 2026-10-10: No pull-to-refresh. In the installed app on Android, reloading by pulling down left the page laid out
  one navigation bar taller than the window (the footer off the bottom edge, the page scrolling by that much) until
  the phone was rotated: Chrome's sizing of a reloaded standalone window, not the app's content. The gesture was
  also redundant, since plans and the status refresh themselves. It's turned off; and because that was the only way
  to reload an app left open, the footer now says when the server has moved to a newer version than the page came
  with, and offers Reload. Sizing the frame from a measured height instead was rejected: a guess at which of
  Chrome's numbers is right, for a gesture nothing needs.

- 2026-10-10: The "Longer walk" notice can be dismissed, for good. It's true every time the trip is planned, so after
  the first reading it only pushed the board down. A dismissal is kept in the device's settings (so it's in backups
  and setup links, and a reset clears it), for the walk it was about: a place and a gym's lines, either way round. It
  holds the distance the notice said, and the notice shows again when that changes (the gym's lines or the longest
  walk were edited and the nearest stop is a different one): it's then saying something new. Settings offers "Show
  them again" while there are any. Keeping it only while the trip screen stayed open was the first cut and too
  short: the notice was back after every trip.
- 2026-10-10: Nine more built-in gyms from seven new brands: Nomad Annandale and Gladesville, BlocHaus Marrickville and
  Leichhardt, Beta One Granville, 1UP Chullora, Sandbox Silverwater, SICG St Peters, Skywood Brookvale (lines found as for
  Waterloo/Alexandria; names kept short so the gym rows don't wrap). Each brand has its logo, reduced to its mark where a
  wordmark won't read at 32 px, on a light tile where the mark is dark line work that would vanish in the dark theme.
  The built-in gyms' lines always stay loaded and grew from 42 to 83, so the cap on loaded lines goes from 150 to 200
  to keep about the same room for the lines near people's homes.
- 2026-10-10: The gym chooser starts with nothing ticked (supersedes "adds the gyms (all ticked)" of 2026-10-03). With
  three gyms, all ticked was the quickest way through setup; with sixteen nobody climbs at them all, and a device holds
  12 (as many as one line search takes). At 12 the remaining boxes are disabled and a line says why.
- 2026-10-10: A seventeenth built-in gym, Climber Collective in Jamisontown (Penrith), the first at the western edge of
  the area the map and street network cover. Its lines include the Blue Mountains line, whose trains are the quick way
  out, and the T5 and bus 611: with trackwork on the T1 there was otherwise no way there from the city. The built-in
  gyms now keep 93 lines loaded.
- 2026-10-10: The tape stays in its column. A trip of four rides (home to Climber Collective around trackwork) ran its
  last ride into the arrive time: once the walks were at their narrowest the rides gave up almost nothing, however wide.
  Now the rides wider than their names are trimmed, and when even the names and 13px walks don't fit, the walks narrow
  evenly to as little as one dot; only past that (a 320px screen) is the tape cut at the end of its column.
- 2026-10-10: A dismissed "Longer walk" notice is remembered as a timed walk is: by the place and the stop, not the gym
  (supersedes "a place and a gym's lines", and the distance it held, from earlier today). Dismissing the walk from home
  to a station on a trip to one gym left the notice up for every other gym boarded from that station, though it said
  the same thing each time; a timed walk from home already counts for every gym. `stretched_walk` now names each end's
  nearest stop the way timed walks do (a train, metro or light-rail platform by its station; a bus stop, a stand at a
  station included, by itself), and the dismissal is the place and that stop. The notice shows again when the place's
  nearest stop is another one. Dropping the gym but still matching on the distance was rejected: two gyms' lines can
  call at different platforms of one station, a few metres apart on foot, and "1.4 km" turns into "1.5 km".
- 2026-10-10: The street map can be updated when asked: "Update now" on the Street map row of the server status in
  full, and `POST /api/street-map/update` behind it. The server fetches it every 35 days, which is right for streets
  but long for the one case that matters: a path added to OpenStreetMap so the app stops walking around it. The request
  only wakes the provisioning loop, so there's still one download and one build at a time, and the status says it's
  updating (`walk_updating`). A failed try is now tried again after 6 hours even when the street map in use isn't old
  (before, only an old or missing one was), so a failed update doesn't leave its warning up for a month. BBBike's
  extract is itself rebuilt weekly, so an edit can be up to a week from being there to fetch; showing the extract's own
  date was left out until that turns out to be a guess worth removing. The basemap has no such action: it's a picture,
  and nothing is routed over it.
- 2026-10-10: Climber Collective is no longer built in (supersedes its entry above): sixteen gyms again. The street
  network doesn't reach it. The basemap is cut to a wider box (west to 150.55°E) than the street map (BBBike's Sydney
  extract stops at 150.83°E), so Penrith draws on the map but every walk there is a straight-line estimate; the earlier
  entry's "the map and street network cover" was true of the map only. Widening the street map (Geofabrik's New South
  Wales extract, 269 MB, clipped to the basemap's box) wasn't worth it for a gym nobody here climbs at; BBBike's custom
  extracts are made by hand, one at a time, and can't be fetched on a schedule. A built-in gym has to be inside the
  street map, not just the basemap.
- 2026-10-10: A runner at the end of each gym's row starts the fastest trip there in one tap, without stopping at the
  options; anywhere else on the row still chooses the gym and shows them. Most trips are the next good one to the usual
  gym, and choosing the gym, reading the board and pressing Start trip was three steps for it. "Fastest" is the option
  that arrives soonest among those that can still be caught: none of its changes already missed, and not shown as
  having left (of two arriving together, the one leaving later). It isn't the first to leave, which is what the board
  selects: that one is often a slower way that just happens to go first. It's planned as choosing the gym plans it (the
  same direction too), on the gym's own screen, so when nothing can be caught or the plan fails you're where a tap on
  the row would have put you. The runner is hidden with "Leave at" and "Arrive by": a trip that starts, starts now.
  Chosen from mockups of four treatments (a bare icon, an outlined button, a split row, a labelled "Go" button) and
  three drawings: the bare icon, like the route row's reverse button, with the sprinter and its speed lines.
- 2026-10-10: Which way a trip goes is chosen on the gym list too: a toggle at the end of the When row (the house, an
  arrow, the hold; a tap swaps them). Before, a trip could only be reversed on a chosen gym's screen, so the runner
  was no use for the way home (gym, reverse, back to the list, runner: four taps, against three with Start trip), and
  going back to the list kept the trip reversed with nothing on the list to say so: the next runner would have started
  a trip home unasked. Now the list says which way and the runner starts that way in one tap. The route row's reverse
  button is gone (supersedes the reverse button there): the toggle stays in the When row once a gym is chosen, and two
  controls for one thing, a row apart, was one too many. Chosen from mockups: a second segmented row ("To a gym / To Home") and a
  heading row with the reverse arrows both said it in words but cost a row of height; a pair of icon buttons didn't
  fit beside the When control without shortening its labels; the toggle on the left of the row put it over the logos
  rather than over the runners it steers.
- 2026-10-10: A gym is drawn as a climbing shoe (supersedes the hold, wherever a gym is drawn: the rail's ends, the
  direction toggle, the Gyms heading in Settings). Nobody asked could say what the faceted hold was. Chosen from
  mockups of four treatments (thin outline with all its detail, outline with the sole filled, filled with the detail
  cut out, filled with only a sole line): the outline with the sole filled, which keeps the loop and straps that say
  climbing shoe rather than trainer. At the other drawings' line weight it closed up, so it has finer lines of its
  own. On a dark page the filled sole came out light, which read as the wrong way round, so there the upper is filled
  and the sole is left dark, as thick as the filled one on a light page (the thinner of two soles tried: the thicker
  left too little upper on a dark page). The app icon and the page before sign-in keep the climbing volume.
- 2026-10-10: A train is drawn along its own track, to the platform it uses. Trains are loaded from the Sydney
  Trains bundle (for its realtime trip IDs), whose shapes are one line per route, whichever platform a train uses: a
  ride was drawn from where that line passes the station, and the walk to it ended at the platform, somewhere else.
  At small stations nobody would notice (Hornsby: 17 m at worst); at Central the T4 was drawn 45 m from platform 24
  and the T1 Western began 141 m down the track from platform 18. The complete bundle has the same trips, by trip ID,
  with a shape each that follows the real track and ends within 7 m of its platforms, so a train is drawn along that
  one when it passes within 25 m of both ends of the ride, and along the route's line as before when it doesn't:
  the trip isn't in the complete bundle, or the two bundles disagree about it (the complete bundle's shape stops
  short of some stops on a few trips). On a weekday's T1, T2, T3, T4, T8, T9 and Central Coast trains that is 95% of
  rides, and the worse end of a ride is a median 4 m from its platform (13 m before) and 6 m at the 90th percentile
  (34 m before). Chosen from a mock-up of three treatments at Central and Hornsby: today's line; the route's line
  bent over to the platform over its last few hundred metres, which joined up but cut across platforms and ignored
  platform 24's tunnel; and the complete bundle's line. Bending the line for the rides left over, and routing along
  OpenStreetMap's rails, were both left out: the first until those rides turn out to be noticed, the second because
  the feed already has the answer.
- 2026-10-11: Field review, in a trip and on the map. Each was chosen from mockups taken from the running app, in
  both themes (the rules are in `docs/DESIGN.md`):
  - **A "faster way" notice can be dismissed**, and that way then isn't offered again on the trip (kept with the
    trip, so a reload doesn't bring it back). It's known by the vehicles it takes that yours doesn't, so it's still
    the same suggestion once you're off a ride the two share. Only the fastest way is ever offered: with it dismissed,
    a second, slower "faster way" isn't put up in its place (chosen over offering the next one straight away, which
    would turn one dismissal into a queue of notices). Rejected: no more notices for the rest of the trip, and a
    timed silence after which the same one could return.
  - **Both in-trip notices show the trip they suggest as a row from the list of options** (leave, tape, arrive,
    risk, duration), and their sentences drop what the row says. Chosen over the tape alone under today's sentence,
    and over two tapes, yours against the faster one. The "You won't make…" warning gets the row too, so a suggested
    trip looks the same wherever it's offered.
  - **"Time my walk" starts with a small drawn stopwatch** (16px, the Settings one). Not the ⏱ emoji: each phone
    draws its own, an iPhone's in colour.
  - **You are drawn as what you're doing**, on the rail and on the map: a walker on foot (walking, or waiting at a
    stop), the vehicle's glyph on a ride, and on the rail the house or shoe at either end as before (supersedes the
    arrow on the rail and the plain blue dot on the map). White on the same blue, inside the white border. The rail's
    marker stays 28px (32 and 36 were tried); the map's grows from 20px to 28px to hold the figure and to match it
    (24, 26 and 32 were tried: 24 was too small to read, 26 is a vehicle's size).
  - **The map fits the step you're on once you've set off**, and the whole trip before that. "Set off" is the "you"
    marker's own rule: your location has you past the trip's first step, or without a location the leave time has
    come (chosen over the leave time alone, which is wrong whenever you leave early or late). The step is fitted
    whole, with you in it (chosen over only what's left of it, and over the step plus the next one); waiting at a
    stop it's the ride you're waiting for. The view moves on with each new step unless you've moved the map yourself.
  - **The phone's map can be pulled down over the steps** by a handle on the rule under it (drag or tap; two
    positions), leaving the arrive line and what to do now. It stays as you left it for the rest of the trip. The
    arrive line alone was the other choice.
  - **Stops along a route are 10px** with a 1.5px outline (from 6px and 1px: as wide as the line, they were hard to
    pick out). 8.5px, and bigger stops only where you get on and off, were the other choices.
- 2026-10-11: A countdown of the stops left on a ride, in three places. Chosen from mockups drawn into the running
  app (four ways of showing it: a strip map, a departure-board flap holding the number, the ride's step unfolding
  into its stops, and a tear-off tape with a piece per stop), then from mockups of a 19-stop train and a 17-stop bus:
  - **In the trip's steps, a ride opens into its stops**: every stop still ahead, named and timed, yours last. A tap
    on its stops line opens or closes it, while planning too. On the ride you're on the stops line counts down
    ("10 of 19 stops to go"), and it opens by itself only for the last three stops (so a short ride is open from
    boarding). Your tap wins either way. This keeps the steps as short as they were unless you're looking ahead,
    and brings up the stops that matter by themselves. Considered: opening for the whole of the ride you're on,
    which on a long ride pushes everything after it off the screen; folding the stops in the middle of a long
    ride ("12 more stops"), and showing only the next stop and yours, both unnecessary once a long ride stays closed.
  - **In Now, the strip map**: the ride as the line diagram above a train's doors, you along it, with the next stop
    and the count under it. Chosen over words alone, the flap and the tape. Every stop gets a dot, evenly spaced,
    whatever the ride's length (19 fit a phone comfortably).
  - **On the map**: the stops ahead stay plain dots (numbers on them, and name tags, were the other choices), where
    you get off is the larger white dot from the rail, and the view closes in on what's left of the ride for its
    last three stops, unless you've moved the map yourself (this adjusts the "whole step" zoom above).
  - **What's behind you on a ride is dimmed**, on the rail, the strip and the map.
  - The server now sends each ride's stops with their names and times (`calls`, live when the trip has live data):
    the app was only told how many there were, and where they are for the map. The count goes by where you are on
    the ride's line, and by those times when there's nothing to place you by (a tunnel, no location).
  - **A buzz when it's time to get off is set aside**, undecided (see TODO.md): a page can only vibrate a phone while
    it's open on screen, and not at all on an iPhone; the route that reaches a locked phone is a push notification
    from the server, which is a feature of its own.
- 2026-10-11: The street network comes from Geofabrik's New South Wales extract, clipped to the app's area
  (supersedes BBBike's Sydney extract, and from 2026-10-10 both "widening the street map wasn't worth it" and "a
  built-in gym has to be inside the street map, not just the basemap": they are now one area). BBBike's extract is a
  fixed box that stops at 150.83°E, short of Penrith, and is rebuilt weekly; its custom extracts can't be fetched on
  a schedule. Geofabrik's is rebuilt daily but is the whole state: 269 MB, 36 million nodes.
  - **The area is the basemap's box**, 150.55 to 151.35°E, 34.15 to 33.45°S: wherever the map draws, walks are
    routed. Chosen over BBBike's box with only its western edge moved (Penrith and no more), and over a drawn
    polygon hugging Greater Sydney: ocean and bushland hold so few paths that the box, three times BBBike's in area,
    is 1.92 million nodes against 1.32 million, where a polygon would need code and a shape to keep up for little
    saved.
  - **The server clips it itself, as it reads.** A first pass over the nodes notes which are inside the box (7.6
    million IDs, kept as the differences between neighbours: 8 MB). The ways pass then keeps only walkable ways that
    touch the box, and the last pass gives positions only to nodes inside it, so a way that leaves is cut at its last
    node inside, as it was at the edge of BBBike's extract. Memory goes with the size of the box, not of the state:
    building all of New South Wales the old way would have held about 225 MB before making the graph. Turned down: an
    outside tool such as osmium (the image is distroless and static), Overpass (not for extracts this size), and
    writing a clipped file first (nothing needs it).
  - **Each pass reads only its part of the file.** Geofabrik's files say they are sorted (all the nodes, then the
    ways, then relations), so the node passes stop at the first way and the ways pass starts there: 13 s to build
    against 18 s reading every pass in full, for the same network. A file that doesn't say so is read in full.
  - The download (about 40 s where it was tried) no longer holds the one-big-job-at-a-time lock, only the build does (about 17 s): a
    line search or the stop catalogue isn't kept waiting on a 269 MB download.
  - The cached graph records the area it was clipped to, and one built for another area is rebuilt (staying in use
    meanwhile). The cache's format changed with it, so the first start after this rebuilds it once.
  - Measured on the server as deployed (`GOMEMLIMIT=500MiB`): the build's high-water mark is about 330 MB (265 MB
    before) and memory settles about 35 MB higher than it did. The refresh stays every 35 days, or "Update now".
  - Climber Collective comes back as a built-in gym in a change of its own, once the wider street map is in use.
- 2026-10-11: Climber Collective in Jamisontown is a built-in gym again: seventeen (supersedes its removal on
  2026-10-10). It was taken out only because the street network stopped short of Penrith; the network now covers the
  same area as the map (the entry above), and a walk from Penrith Station to its door follows the streets. Its lines
  are as before, the Blue Mountains line, the T5 and bus 611 among them. A test now holds every built-in gym inside
  the area.
- 2026-10-11: Anywhere on the platform is waiting for the train. Waiting was "within 40 m of the stop", but a
  platform is 167 m long (the median) and the timetable's point for it is seldom its middle: walking up the platform
  turned Now back to "Get to Hornsby Station", and 120 m forward along the track counted as on board from a platform
  long enough to still be standing on. The street network's build now also keeps the outline of every railway
  platform in the area (629, from the same extract, walkable or not), and `/api/shape` sends a train, metro or
  light-rail ride's boarding platform: the outline within 15 m of the stop, or within 30 m when it carries the
  platform's number (Circular Quay's points are that far off theirs). In the app, on the outline or within the
  location's accuracy of its edge (10 to 40 m) is waiting, and being on board by distance needs you off it; a
  vehicle you're seen moving with still says so sooner. With no outline nothing changes. The outline rides with the
  ride's shape, which the app already fetches once a ride, so no plan grows. The cached graph's format changed again
  (version 3): one more rebuild on the first start.
  - Most platforms are one shape in the street map (557). The rest are relations: a shape in several pieces, the
    outer edge and the stairwells cut out of it, where the pieces carry no tags of their own. Left out at first,
    that missed 43 of the timetable's 548 rail platforms, Central's 16 to 21 and all of Chatswood among them, so
    the reader now reads relations and the build keeps the ways a platform relation names, joined end to end into
    its edge (the holes are ignored: in a stairwell you're still on the platform). All 548 now have an outline. It
    costs a pass over the ways' part of the file to find the relations before the ways are read, about 2 s on a
    build of 15 s.
- 2026-10-11: Investigated, not built: placing where you wait along the platform so you step off beside your stairs
  at the destination. The data is there for most train platforms: their outlines (above), where the stairs, lifts and
  escalators are (the timetable feed places them, a median 4 m from the platform's outline, for 89% of platforms) and
  the train's length (the car count is in the trip ID; the metro feed lists its carriages). An 8-car train fills a
  typical platform, so a place on the train carries from one platform to the other, to about a carriage or two.
  What it can't know is where a 4-car train stops on a long platform, and which stairs you'd take is a guess. The
  cost would be in the special cases, and wrong advice is worse than none, so the next step, when wanted, is a
  prototype that only prints its advice for trips we know (TODO.md).
- 2026-10-12: Where to stand, how it will look and when, chosen from mock-ups. Still not built: the prototype that
  prints its advice has to show the advice is right first (TODO.md).
  - It aims for the stairs or escalators your way on takes at the far end, never a lift, and it shows on every train
    ride where the carriage is known. There is no test for whether it's worth showing: "worth it" would need a rule
    of its own, and a wrong guess at it hides advice that was right.
  - In Now, from "Get to the station" until you're on board, and nowhere else. Not on the ride's step and not while
    planning: it's only of use at the platform. It starts at "Get to", not "Wait", because Now only says "Wait" once
    a location puts you on the platform, and underground there's none.
  - The train is a strip heading right, a piece for each carriage, the front one with a driver's cab. Every carriage
    is a hairline-grey outline but yours, a thin outline in the line's colour. Under it, the runner from the gym rows
    and one sentence, "Board the 4th carriage from the front." The runner there starts the fastest trip; here it
    marks the quick way off. The reason ("for the escalators at Central") is left out: short wins on a platform.
  - On the map the platform you board from has a faint fill in the line's colour, and where the spot to stand is
    known the runner sits on it, in the "you" marker's disc at 45% strength. It never reacts to where you are: a
    phone's location on a platform is out by about a carriage, so an "arrived" state would flicker.
  - The carriage comes from the destination platform alone. The spot on the platform you board from is only known
    where the train fills it (Hornsby's 2 and 3 are 77 m longer than an 8-carriage train, and where a train stops
    along them isn't published): elsewhere the strip shows and the map has no marker.
  - Decided after the prototype ran (2026-10-12): where the platform is longer than the train, the train is taken to
    stop in the middle of its room, and the carriage and the spot on the map follow from that. Saying nothing lost
    four rides in ten, and the middle is at least the right end of the train. When two ways on are within seconds of
    each other the best is still the one aimed for.
  - Found the same day: the street map already says where trains stop. Its stopping point for a platform (tagged
    with the platform's timetable ID) lists, a carriage at a time, which way off is beside it, for 553 of the 591
    train and metro stopping points in the area. With that list used first, measuring along the outline second and
    the middle last, the prototype names a carriage for every ride that needs one (153, 20 and 27 of 200). The list
    and measuring differ by a carriage more often than they agree, so the list has to be checked on the ground at
    stations we know before it's trusted. Ways off on the level (ramps, a walk off the end of a terminal platform)
    count as targets; anything that is only a lift doesn't.
  - And the metro says where it stops: its trains report a position while stopped, the same one to the last digit
    train after train, and that position is the train's front. They stop in the middle of the platform, so the
    middle rule is the metro's real rule; where a platform's outline in the map is wrong, the reported position
    stands in for it. Which end of the map's carriage list is the front comes from the direction the track is
    drawn in, which agreed with the stairs every time the stairs could tell (219 of 219).
- 2026-10-12: Where to stand is built, pending field tests. A train or metro ride in a plan carries `board`: the
  carriage, of how many, and the spot on the platform (`internal/stand`). The street network's build keeps the
  map's stopping points with their carriage lists and their track's direction (cache file version 4: one rebuild on
  the first start), and the metro's 40 reported stopping points are built in, collected from the live feed that day.
  The app shows it as decided above. Checked against the prototype on the same 102 plans: the same carriage for 215
  of the 218 rides both answered (one tie between stairs and an escalator at one point, since settled in favour of
  the one the map puts nearer; two were trains of different lengths under one name). It costs about a millisecond a
  ride, and each ride's answer is remembered, so a trip being followed doesn't pay again. Still to check on a
  platform: the map's lists at Hornsby, which of the map and measuring is right where they differ, the metro's
  stopping points, and how far out the middle rule is where there's no list (TODO.md).
- 2026-10-12: A buzz when it's time to get off is dropped. It was set aside on 2026-10-11 to think about; it isn't
  needed.
- 2026-10-12: Timed walks are removed. Timing a walk during a trip, the saved list of timed walks and changes, their
  traced routes on the map, the pace learned from them and the personal change times they sent to the planner all go.
  They were for when walk routing was rough. Now walks follow the street map well, and a missing path is better added
  to OpenStreetMap, where it fixes the walk for good and for everyone, than timed around on one device. It also
  settles the open question of whether a timed walk belongs to a station or a platform: there are none.
  - Every walk is timed one way: its route along the street map at the walking speed.
  - A ticked stop means which stop, not how long. It used to keep the walk time from the moment it was ticked, which
    the planner took as given, so it went stale when the pace or the map changed. Now `access` in a plan request is a
    list of stop IDs and the server times each from the street map (a straight-line estimate for one the map can't
    reach). The same goes for a built-in gym's `[[gym.access]]`.
  - The default walking speed is 5.5 km/h, up from 4.7.
  - "Measure my pace" in Settings stays: it's how the walking speed can be set from a short walk.
  - `walks` on a plan request's place stays, for the one job it still has: during a trip the app says how much walking
    is left to the stop it's heading for. `prefs.transfers` is gone.
  - Greenfield: nothing is migrated. Timed walks and stored stop times already on a device are ignored.
- 2026-10-12: Two small things from use. The "Board the 4th carriage from the front." line under the train strip is at
  the body's weight, not bold, and its runner is drawn lighter to match (1.5px, speed lines 1.2px; the runner on a
  gym's row keeps its 2px): the strip is the emphasis. In the gym chooser, "Add another gym" moves out of the action
  bar to a text link under the list ("Not listed? Add another gym"), and the commit reads "Add gyms" with no count when
  more than one is ticked: three buttons wrapped on phones under about 400 px wide. A bar holds two buttons at most.
- 2026-10-12: A design review of the whole app, and what came of it. The language stays (rules, not cards; ink on
  chalk; colour only for lines and risk; no Material 3). Thirteen changes were mocked on the running app, each beside
  the app as it was, and chosen from the sheet. Taken up (docs/DESIGN.md has the rules):
  - **The main action is filled** (supersedes "no button is ever filled", 2026-10-05). One filled ink button a screen at
    most: Start trip, Save home, Add gyms, Switch to this. The alternative beside it keeps its outline. What that rule
    rejected, an accent-coloured button, stays rejected: the fill is ink.
  - **The countdown says how sure the chosen trip is:** its risk badge and why, under "for the M1 at…". The earliest
    option is still the one chosen first, even when it's at risk: the line says so now. The squares in the rows stay.
  - **Settings' actions are rows:** "Add home" and "Add gym" are the last row of their lists; Set up another device,
    Export backup and Import backup are rows. The only button left on Settings is Reset this device.
  - **A row's bin is ink.** Red arrives with "Delete …?".
  - **Number settings are one line each:** the name, then a short field showing the default. "Empty uses the default"
    is gone from under each; "Use the default (3)" and "Saved." still show when they apply.
  - **"Live" or "Timetable only" sits at the end of the gym's row,** not on a band of its own, and no longer says when
    the plan was updated.
  - **First run asks two questions:** "Where do you start from?" (step 1 of 2), then "Which gyms do you climb at?"
    (step 2 of 2), with no Cancel. The address search says only "Your address isn't stored on the server."
  - **Stops are closed until asked for** in the home and gym editors: "Using every stop within a 1 km walk. Choose
    stops".
  - **The page before sign-in says what to do:** "Open your setup link to start."
  - **The server status and version are on Settings only,** and on the trip screen just when the server isn't OK or
    the app is out of date.
  - **The in-trip location line shows only when something is off.**
  - Left as they are, by choice: the runner on a gym's row and the direction toggle keep their drawings without words.
- 2026-10-12: The smaller things from the design review, tidied in one pass.
  - Text actions are ink when they get you going again ("Reload", "Try my location again") and pencil when they're
    there if you want them; "Choose stops" and "Show 59 more" were ink and are pencil now.
  - Line chips in the gym editor are in their lines' colours before any suggestion: `GET /api/lines?lines=…` gives the
    colours from the stop catalogue, without loading the lines.
  - Selects and tick boxes are drawn as the app's fields are (its chevron, an edge outline, ink when ticked).
  - The map's attribution is a square-cornered panel like the other map controls, not a white pill.
  - "Stored only on this device" is said once in Settings, for homes and gyms together.
  - "Trips put your most used gyms first. Reset the order" is gone from Settings, and with it the way to reset the
    order. The trip screen still lists the most used gyms first.
- 2026-10-12: **The status line is back under the gym's row** (reverses "'Live' or 'Timetable only' sits at the end of
  the gym's row", from the design review above). Beside the gym's name it read as part of the gym. The line says
  "Live data, updated 09:31" (or "Timetable only, …") as before, and the countdown sits under its hairline again.
- 2026-10-12: **Where to board sits above when the train leaves** in Now: the carriages and "Board the 6th carriage
  from the front." come straight under the instruction, then "T1 leaves 19:27 …" and the time to spare.
- 2026-10-12: **The gyms' order can be reset again, from the status** (supersedes "and with it the way to reset the
  order", above: removing the reset was a misreading of "remove 6", which meant leave it off the list of fixes). The
  line in Settings' Gyms section stays gone. The opened server status ends with a row "Gyms: 7 on this device" and a
  quiet "Reset order", shown while any gym has been chosen: out of the way, with the other things only its owner looks
  at.
- 2026-10-12: **The header's underline is gone:** "Gym Router" and the Settings icon no longer carry an ink bar for
  the screen you're on. The Settings icon is still ink on Settings and pencil elsewhere; the screens say which they
  are.
- 2026-10-13: Five things from using it, settled on a sheet of mock-ups before any was built.
  - **Finding a gym's lines shows how far it is, for real.** The figure moved in quarters (a step for each timetable
    read and each gym searched) and sat in small text under a list long enough to hide it. Reading a day's timetable is
    nearly all of the work (about 5 s against 0.7 s to search a gym on it), so the server now says how much of each
    timetable it has read (by bytes of the feed) and counts every departure time searched. The job's status is
    `stage` (`reading`, `searching`) and `progress` (0 to 1) in place of `done`/`of`. The app shows a bar that fills
    along the top of the action bar, pinned with the buttons, over one plain line: "Processing timetables", "Analysing
    trips", then "Adding your gyms"; the same bar sits under "Suggest lines" in the gym editor. Cancel stays live.
    Rejected: a spinner (alive, but not how far); a tape for each gym (the server searches gyms one after another, so
    each could only move in bursts; searching them side by side made the tapes move together and say the same thing
    three times); a dot for each departure time (the tallest, and it scrolls away).
  - **A gym's lines are chips in a row for each kind of transport** (train, metro, light rail, bus, ferry), each row
    led by the kind's drawing and word. A chip is filled in its line's colour when the line is used and an outline
    when it isn't; a tap changes it. Fourteen lines take about 230px where the tick rows took over 800. The share of
    searches is no longer written beside each line: the order says it (most used first), and a chip's title has the
    figure.
  - **A trip says "Live" or "Timetabled" beside Arrive.** The trip screen never said whether its times were live; until
    12 October it said "Location on (±12 m). Checked 09:32." always, and that day's review cut the line to show only
    when something is off. The word comes from each re-check (the plan's `realtime`, which the trip screen ignored).
    No time beside it: a failed re-check has its own line. The planning screen's "Timetable only, updated 09:31" is
    left as it is for now.
  - **Tight is yellow in light, as a fill.** Text needs 4.5:1 on the wall, which made light's Tight (`#865404`) a
    brown as a 12px square. Squares, badges, the caution callout's bar and the server status dot now use
    `--tight-fill` (`#e0a100`, the dark theme's yellow a little stronger); words keep `--tight`, since a yellow is
    1.9:1 on the wall. The Tight badge's text is ink. Dark is unchanged. Rejected: brighter fills for all four levels
    with a deeper red for missed; a square that's full, half or empty; retuning the three hues at text's darkness;
    an amber edge on the yellow square.
  - **A gym without a logo has the shoe** where its logo would be, in ink. Rejected: its initials on an ink tile; a
    colour for the shoe, chosen (a setting for telling two of your own gyms apart, which their names do) or given by
    the app.
- 2026-10-13: **The trip screen says "Live" or "Timetabled" beside "Leave in"** (supersedes "The status line is back
  under the gym's row", 2026-10-12, and settles the wording left alone earlier today): the tag a trip has beside
  Arrive, at the right end of the countdown's label row. The line under the gym's row goes, with the time the plan
  was updated and the hero's hairline, and the countdown moves up about 40px. While a newer plan is slow to arrive
  the tag reads "Updating…" beside a pencil ring. An orange dot was considered and rejected: orange and yellow mean
  something is tight or needs a look, and a refresh is neither. The line stays for what the tag can't say: "Planning…", "Finding
  the fastest trip…", and that walks are estimates.
- 2026-10-13: **A hairline above "Earlier trips" and "Later trips".** With the status line gone, the countdown's
  block ran straight into the links above the options. The countdown now sits between two hairlines: the one under
  the gym's row and this one. Rejected: the heavy rule that starts a section (as above "Now" during a trip), a
  stronger break than this screen has anywhere else, close under the risk line.
- 2026-10-13: **The build images are pinned by digest, and Dependabot looks daily** (supersedes the hardening
  review's choice of 2026-10-05, to fold the `golang` and `node` digests into the scheduled rebuild check). The
  gap: `build.yml` only watched the distroless base, so a Go standard-library fix shipped with the next code
  push; the binary is static, and the fix is in it, not in the base. `golang` and `node` are now named by digest
  in the Dockerfile, as pmtiles was, and Dependabot's docker updates run daily instead of weekly: a Go patch
  release arrives as a pull request, CI builds on it, and merging it publishes the image. What's built only
  changes with a commit, so a build can be repeated. The cost is a merge for each new image, and an order: the
  `go` directive in `go.mod` can't be bumped ahead of the pinned image (the build fails on an older Go), so
  Dependabot's pull request goes first. Rejected: the automatic rebuild within a day (built, then dropped the
  same day: it publishes whatever the tag points at with no CI run first, and a person deploys by hand anyway),
  a blind weekly rebuild, and a different runtime base (the fix isn't there). The scheduled check stays for the
  images still taken by tag, which it reads from the Dockerfile's `FROM` lines: today the distroless base.
- 2026-10-15: **"Live" or "Timetabled" is the trip's, not the server's** (refines the trip screen's tag of
  2026-10-13). The tag said whether the server had fresh live data, so a trip whose first service had no predictions
  still read "Live". It now says whether the ride that matters has live times (`predicted`, or `added`: an extra
  service only live data knows about). On the trip screen that's the highlighted trip's first ride, the one the
  countdown beside it counts to, and it changes as another trip is chosen. During a trip it's the ride you're on, or
  the one you're on your way to (a change included). A trip that's only walking has no tag. Later rides keep saying
  their own state in the steps. The server status's footer still speaks for the server as a whole. Rejected: "Live"
  only when every ride has live times (TfNSW predicts a vehicle once it's about to run, so a two-ride trip would read
  "Timetabled" until well under way), and a third "Partly live" state.
- 2026-10-15: **The server says when live data has changed, and the app asks again at once** (until now each screen
  re-planned every 30 s, at times unrelated to the server's own 30 s fetches: new times reached the screen up to
  30 s late). `GET /api/events` is a stream (server-sent events, read with `fetch` so the token goes in the header,
  never in the URL) that carries one short event whenever new live data has been applied. The trip screen and a
  trip under way re-plan as it arrives; they still re-plan every 60 s without one ("Leave now" moves on with the
  clock), and every 30 s, as before, while the stream isn't connected. The stream is open only while the app is
  visible, and holding it open doesn't count as using the app: the re-plans it sets off do, so a forgotten tab
  doesn't keep the server fetching. Found after waking the server: its first plan is timetabled (the live data had
  gone stale), live data arrives a second or two later, and the screen used to say "Timetabled" until the next 30 s
  refresh. Now that event re-plans it. Rejected: answering the first plan only once fresh live data is in (slower,
  and for nothing when the trip's services have no predictions), and a hint in the plan's reply to ask again soon
  (fixes only the waking, not the 30 s lag). Vehicle positions on the map are still polled (every 10–15 s); they
  could use the same stream later (TODO.md).
- 2026-10-15: **Vehicle positions follow the events stream too** (the follow-up left open with the stream this
  morning). The map and a trip's check for your vehicle polled `/api/vehicles` every 10 s, while the server fetches
  positions every 15 s: two requests in three found nothing new, and a new report waited up to 10 s. The server now
  sends a `vehicles` event after each positions fetch, and both fetch then; without one, every 30 s (every 10 s, as
  before, while the stream isn't connected). The app keeps one stream for everything listening (the trip screen,
  a trip, the map), so opening the map doesn't open a second. Rejected: sending the positions themselves in the
  event (each map wants only the vehicles running its own rides, near the part ridden, which the request says).
- 2026-10-16: **A trip simulator, temporary, for testing at a desk** (field tests take a real trip each, and most of
  what they check is how the in-trip screen follows you, which can be tried without going anywhere). With `?sim` in
  the address, every trip started is simulated, and a panel over the map drives it. It runs on the timetable at a
  speed you choose (paused, 1×, 5×, 20×, 60×, or skipped ahead a minute, five, or to the next step), with a
  simulated you who walks the trip's walks at your pace (or a share of it) and rides its rides, its vehicles where
  the timetable has them, and the location a phone would report (good, poor or none). You can stay put (not set off,
  not walk on, not board) and make any ride late; that's enough for a missed connection, a vehicle that leaves
  without you, a tunnel, and the switches the screen offers then, which the simulated you follows. It stands in at
  four places only: the app's clock (`clock.ts`: what reads the time, and the timers a trip runs on), your location,
  the vehicles' reports, and the plans, which send `sim: true` (the server plans on the timetable alone, and
  takes a `time` with `on_trip`, since the simulator's clock isn't the server's): the trip screen's too while `?sim`
  is on, so a trip is planned on the same timetable it's followed by. Live data and the events stream are left out. A late ride shows on its vehicle and on you, but not in the re-checks, which keep to the timetable:
  injecting delays on the server is the next step if that turns out to matter. A simulation survives a reload,
  paused where it was. To be removed once the field behaviour it stands for has settled: delete `sim.ts`,
  `views/simpanel.tsx` and their few call sites, and the server's `sim` flag; `clock.ts` can stay. Rejected: faking
  the browser's whole clock and location from outside the app (it also speeds up the map's animations and the
  network's timeouts), and replaying recorded trips (worth adding later: it shows real GPS, which this can't).
- 2026-10-17: **The trip screen no longer shows the chosen trip's steps** (the start of a UI overhaul aimed at
  simplifying: keep what works, drop what doesn't earn its place). Under "1 h 3 min door to door, 2 changes, 12 stops"
  the screen listed every step of the highlighted option, the rail and each ride's stops included. Those details
  matter on a trip, where they stay (with you on the rail), not while choosing one: the countdown, the board's tape
  and the map say enough to choose, and the list pushed Start trip a long way down.
- 2026-10-17: **"Door to door" moves under the chosen option's row**, in the 14px pencil line that said "A different
  route, a little slower than the best." for an alternative, which goes: the tape already shows a different route.
  The summary under the board is gone with it, so Start trip follows the board. The line starts where the tape
  does, and the chosen option's fill and ink bar now take in the line as well as the row.
- 2026-10-17: **Where to board moves from Now into the ride's step** (in a trip: the steps, and the steps under the
  phone's map), between where the ride goes and its stops line. In Now it sat between the instruction and when the
  train leaves, two lines of graphic and text at the top of the screen for every walk to a station. In the step it's
  read with the ride it belongs to, and every train ride not yet boarded shows its own, so a change's carriage can be
  read before you get there. It goes once you're on board, as before; the map's platform and spot are unchanged.
  It's quieter there: the train across 80% of the column, and the runner (16px) and "Board the 4th carriage from
  the front." in the step's 14px pencil.
- 2026-10-17: **A trip under way is one screen: the map, with the trip in a panel under it.** Start trip goes straight
  there; the summary screen (the arrive hero, Now, Then, the steps, and "Show on map" to reach the map) is gone. The
  map screen had become the one in use: the map, what to do now and the steps fit one screen. The panel under the map
  holds the arrive line (with the countdown and the Live tag), the warning with its "Switch to this" when there is
  one, what to do now (with the platform it leaves from and the time to spare to the stop), the status line when
  something's off, and the steps; the handle still pulls the map down over the steps. End trip is in its header,
  where "Back to trip" was. Dropped: the leave time before you set off (the steps' first row says it) and Then, the
  next change (the steps show it, with its risk); and with the hero, "3 min later than planned". On desktop the same
  panel is the left pane beside the map. The planning screen's map sheet is unchanged, minus what it had for trips.
- 2026-10-17: **A ride's stops open only when asked** (supersedes, from 2026-10-11, the ride you're on opening by
  itself for its last three stops, and "your tap wins"). Every ride's step starts closed and stays as you leave it;
  open, its stops look as before. The app doesn't decide when you want to see them. Nor does the map close in on what's
  left of a ride for its last three stops any more: it fits the step you're on, whole, to its end.
- 2026-10-17: Around the one trip screen: its header has the app's icon before the title; End trip's question lies
  over the map rather than pushing it down; the map's frame (the rules above and below it, and the handle) is a 2px
  pencil line rather than heavy ink; the line under what to do now names the platform ("T9 leaves 13:27 from platform
  1 (in 15 min)"). The map, once moved by hand, used to fit itself again only when it was next opened; now that it's
  always open, a re-centre control (shown only while the map isn't following) hands the view back to the trip.
- 2026-10-17: **A design review of the whole app, and what came of it.** Every screen was captured from the running
  app (phone and desktop, light and dark) and judged as it is, not only against DESIGN.md. Taken up:
  - **The trip screen says the chosen trip once.** The hero is the countdown and the arrive time only, and only
    leaving now (with a set time the row's leave time is the number). The chosen row's note says where the first ride
    leaves from and when (how late it runs), then the risk badge, why, and the stops; "24 min door to door" went, as
    the row's last column says it. Before, the hero, the row and its note said the trip three times.
  - **The map draws the home and the gym** at the trip's ends, the house and the shoe as on the rail. A trip's line
    began and ended in the streets.
  - **Desktop shows a map before there's a trip**: your home and your gyms, a gym chosen with a click, where the pane
    was a blank wall with a sentence.
  - **Settings says where you are, and how back**: its header reads "Settings" where it says "Gym Router", with
    Back where the cog is, a quiet text link like End trip (it closes an open editor first). The trip
    screen's header keeps the icon and the name, with the cog; setting up shows the icon and the name, and no cog.
    Settings was left by tapping the app's name.
  - **Both maps have one frame**, a 2px pencil rule above and below (`--frame`); the planning sheet's were heavy ink.
  - **Names are set one way** in every list (18px/800/semi): Settings and the gym chooser had them at regular weight.
  - **A segmented control's chosen option has the panel fill** of the board's chosen row, as well as its bar.
  - **Focus the app moves shows its ring only after the keyboard**: a tap that opened the map sheet or a confirm
    left a blue ring on Back or Keep.
  - **Settings' sections have no rules**: each starts with its title and the space above it, and its lists end
    without a closing hairline (there were seven heavy rules down a screen seldom visited). The editors opened from
    it start their sections with a hairline.
  - **Number settings carry their unit in the field** ("5.5 km/h"), not in the name.
  - **"Your services appear here once they're a few stops away" is gone** from the map: it covered the trip's end,
    and the map shows vehicles as they come.
  - **Copy**: "none from Home yet" (was "none near Home yet"); the Gyms section loses its intro.
  - **Editing a home or a built-in gym** shows its name and address as text; only its stops (and a gym's lines)
    change. A home that moves is deleted and added again. Homes now keep the address they were found by (on the
    device, as their coordinates already were).
  - **The trip's steps scroll to your step again once your location first places you** (they went by the clock's
    guess before it, which could be the end of the trip).
  Not taken up: a new bus drawing (bus and train are near alike at small sizes), reordering or folding Settings'
  sections, the grey tape on the gym rows.
- 2026-10-17: **The map's locate button is gone.** MapLibre's own control drew a second blue dot of its own and
  followed it; during a trip the app already draws you (what you're doing, from the same location), and the
  re-centre control hands the view back to the trip. Before a trip, where you are isn't part of planning one.
- 2026-10-17: **The planning map sheet's header matches a trip's**: the app's icon and the title, with Back a quiet
  link at the right (where a trip has End trip), instead of an outlined "‹ Back" button leading it.
- 2026-10-17: **A trip under way has no header**: the map runs to the top of the screen, so it feels like the whole
  screen with the trip pulled up from the bottom. The icon and the trip's title go (the map's ends and the steps say
  where it goes). End trip becomes a chip over the map's top left, drawn like the map's own controls and in line with
  the zoom buttons; its confirm takes its place over the map. Chosen over a link at the end of the steps (hidden
  while the map is pulled down) and one on the arrive line (crowding the countdown and the Live tag). On desktop the
  chip sits over the map pane and the panel starts at the top of the page.
- 2026-10-17: **The trip's panel is a sheet pulled up over the map**: the rule between them goes, the panel's top
  corners are rounded (16px, a new radius, `--r-sheet`) with the map running on behind them, and the handle stays.
- 2026-10-17: **The dark map is coloured as Google Maps' night style** (slate-navy land, deep navy water, slate roads
  with amber-brown motorways, teal parks, warm place names), over Protomaps' dark flavour, which is near black and
  hardly parts water from land. Tried first and rejected: the dark flavour lifted evenly toward white (by 8 to 24%),
  and the light map in either theme (bright against the dark panel). The app's own dark greys stay neutral for now.
- 2026-10-17: **The trip's panel drags to any height, over a map that stays put** (supersedes its two positions,
  2026-10-11). The map fills the screen and never moves or resizes as the panel does; the panel follows the handle to
  wherever it's let go, settling only at its foot (what to do now, no steps) when let go near it. A tap still takes it
  down, or back up as it opened. The map fits the trip above the panel as tall as it is when the map fits (a new
  step, Back to the trip), not while it's dragged. The attribution moves under End trip, out of the panel's way.
- 2026-10-17: **The desktop's map before a gym is chosen takes in the area the app covers**, Blaxland to Manly,
  rather than fitting the gyms tightly. (Gyms drawn as their logos was tried and dropped: the shoe reads better on the
  map.)
  The three findings parked from the codebase review are fixed: the map's vehicle errors are worded by `problem()`;
  an open map is made afresh when the page turns light or dark; and importing a backup with another access token says
  so ("…, and its access token").
- 2026-10-17: **The day and time slide down** under the When row when "Leave at" or "Arrive by" is chosen (and back up
  for "Leave now"), where they appeared at once and pushed the screen down.
- 2026-10-17: **Settings opens with where it's all kept**, above Homes rather than as its intro, and for every
  setting rather than homes and gyms alone: "All settings are stored on this device only." Checked against §4 and §10: everything on Settings (homes, gyms, walking and
  risk settings, appearance, the access token) lives in the browser's storage; requests carry what a plan needs in
  their bodies, which the server neither stores nor logs; the only thing it keeps is the names of the lines requests
  have named (`lines.json`), which aren't tied to any place. Backups and setup links are copies the person makes.
- 2026-10-17: **Settings' sections are evenly and more tightly spaced.** Without their rules the gaps had come apart
  (40 to 60 px from the end of one to the next title): the stack's gap, padding left from the rules, list margins, and
  a 56 px last row each added their part. Now it's one gap (about 28 px seen) everywhere.
