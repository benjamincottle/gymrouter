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
- **Device storage (private data)**: home place(s), **gyms with their line sets and curated access stops**, home
  access stops + walk times, walking speed, personal transfer-time overrides, **connection-risk thresholds**
  (start from server defaults; user-tunable), access token. Sent in POST bodies; never stored on the server.
- Multiple homes = more entries in device storage; no code change.
- Initial gyms (9 Degrees): **Lane Cove, Parramatta (Rydalmere), Chatswood** for v1;
  Waterloo and Alexandria later. They are built in; others are added in the app (address search, then suggested lines).

## 5. Route model: dynamic routing within each gym's lines
- Each gym has a set of lines that could matter for reaching it (from home, incl. home-side lines),
  e.g. `["bus 370", "train T4", "metro M1"]`. The router finds the best options within that set, so timetable
  changes need no edits. The set lives on the device and is sent with every request.
- GTFS static is filtered at ingest to the union of the line sets the server has been asked about → small
  in-memory timetable. The server starts with only the known gyms' lines; a request naming lines it
  hasn't loaded makes it reload with the union (a few seconds, once per new line). The union only grows until restart
  and is capped. The union is never written to disk.
- A **stop catalogue** (every stop with the lines that serve it) is built from a full-network pass after each timetable
  download. It powers "nearby stops" in setup, which has to work before any lines are chosen.
- Walking transfers between stops on those lines: GTFS transfers/pathways where present, otherwise
  distance × walking factor; **personal overrides win**.
- Access/egress: device-supplied home access stops + walk times; gym access stops from config.
- **Walk times at each end are measured, not modelled.** General planners walk through places you can't
  (e.g. through the Johnson factory at Lane Cove) and miss shortcuts that exist (e.g. cutting through the
  plumbing-supply site). We never route over a street network: every gym access stop has a curated walk time,
  and optionally a hand-drawn walking path (GeoJSON line in config) that the map draws instead of a straight line.
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
- Overlays: option legs (vehicle legs along GTFS shapes; walking legs as the curated path if configured,
  otherwise a dashed straight line),
  stops, and **all live vehicles on the selected gym's lines**, with **the selected trip's vehicles
  clearly highlighted** (colour, size, label); the rest muted.
- Vehicle positions refresh by client polling (~10–15 s) from the server cache.

## 8. UI / UX
- Mobile-first responsive PWA (installable); works on desktop.
- **Home screen**: one button per gym, plus a direction toggle (to gym / home). Tapping one runs "Leave now".
- **Results**: ranked option cards. Each shows departure, arrival, legs, transfer count, connection-risk
  badges, and a **leave-by countdown** on the top option (auto-refreshing). Time controls for Depart at / Arrive by.
- **Map view**: the selected option + live vehicles (see §7).
- **In-trip mode**: started from an option. Uses browser geolocation (on device; position sent only in
  plan requests) to track progress. Re-checks the remaining legs on each refresh and suggests a switch if
  something slips (missed connection, delay, cancellation). Keeps the screen awake while active.
- **Settings**: homes (pick on map → suggested nearby stops with estimated walk times, editable),
  walking speed, transfer overrides, export/import (link + JSON), share setup (link + QR).

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
  - `POST /api/stops/near` — {lat, lon} → nearby stops (all lines serving them) with estimated walk times.
  - `POST /api/suggest-lines` — {from, to: [places]} → per destination, candidate lines with how often they appear in the best options.
  - `GET  /api/vehicles?lines=<list>` — live vehicles on those lines (+ trip ids for highlighting).
  - `GET  /api/shapes?lines=<list>` — line shapes for drawing.
  - `GET  /api/status` — detailed health (auth). Implemented API: see `docs/api.md`.
  - `GET  /healthz` — liveness, feed ages, config warnings (no sensitive detail).

## 10. Access, security & privacy (public, no login)
- Public hostname through Traefik with TLS; Traefik rate-limit middleware.
- **Setup-link token**: secret in Ansible Vault → env; server compares in constant time against its hash.
  First device: `docker compose exec app gymrouter setup-link` prints `https://<host>/#setup=<token>`.
  The app saves the token and removes it from the URL. More devices: "Share setup" link/QR.
  Revoke = rotate the secret.
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
