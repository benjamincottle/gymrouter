# Random thought, leave this please

❯ Could you give a very high level (bullet point steps high level) idea of what needs to change apart from gyms.toml in order to add another gym

  Ran 2 shell commands

For the common case, nothing else needs to change: append a [[gym]] block, rebuild and deploy. Devices are offered the new gym in the gym chooser, and they add it themselves. The rest is only needed in some cases:

- Pick the lines: gymrouter suggest-lines finds the lines worth listing for the gym end, from a few varied starting points.
- Curated walks (optional): add [[gym.access]] entries only for stops where the street routing is wrong.
- New brand (only if it isn't 9 Degrees or ClimbFit):
  - Add the logo to web/public/brands/<brand>.png.
  - Add the brand to Brands in internal/gyms/gyms.go:58, or validation rejects it.
- Outside the current area (only if the gym isn't in Sydney):
  - The walking network only covers BBBike's Sydney extract (walk_source).
  - The basemap is cut to mapBBox in internal/engine/provision.go:20 (lon 150.55–151.35, lat −34.15 to −33.45).
  - Both would need widening.
- Tests (optional): TestBuiltInGymsAreValid in internal/gyms/gyms_test.go checks a list of gym IDs. Adding the new one keeps it from being dropped by accident; validation runs either way.
- Docs: the spec's places section describes the built-in gyms in general terms, so it only needs a decision-log line if the change is notable.


# Security and hardening review (2026-10-05)

For review: the unticked items are still open. The baseline is already good (256-bit token compared as a digest,
unauthenticated requests look like a 404, strict CSP and headers, bounded bodies and inputs, query strings and
bodies never logged, distroless non-root read-only container, pinned actions, govulncheck and npm audit
clean today). Roughly in order of value.

- [ ] **Images don't pick up Go security fixes.** `build.yml` rebuilds on a schedule only when the distroless
      digest changes, so a Go standard-library fix (the binary is static, built on `golang:1.27-trixie`)
      only ships with the next code push. `audit.yml` runs govulncheck with `check-latest: true`, so it
      checks the newest Go, not the one the published image was built with: it can't see this gap.
      **Decided (2026-10-05): fold the `golang` and `node` image digests into the scheduled change check**,
      so a Go patch release produces a new image within a day (rather than a blind weekly rebuild).
      Changing the runtime base wouldn't help: the fix lives in the binary, and `distroless/static:nonroot`
      is already the right base (`scratch` and Chainguard `static` are equivalent at best). The bundled
      `pmtiles` binary has its own Go and only updates with upstream releases; the image scan covers it.
- [ ] **Deploy a fixed image, not `:latest`.** `deploy/compose.example.yaml` pulls `:latest`. Deploy by the
      `:<sha>` tag or a digest (Ansible variable), so a deploy is reproducible and can be rolled back;
      optionally check the build provenance with `gh attestation verify` / cosign before rolling out.
- [ ] **Pin the build images by digest.** `node:26-trixie-slim` and `golang:1.27-trixie` are floating tags
      (the pmtiles image is already pinned by digest). Dependabot's docker ecosystem would keep them current.
- [x] **HSTS.** Neither the app nor the compose labels set `Strict-Transport-Security`. Add a Traefik headers
      middleware (`stsSeconds`, `stsIncludeSubdomains`), or set it in `securityHeaders` when
      `server.public_url` is https.
- [ ] **Secrets from files, not the environment.** `GYMROUTER_TOKEN` is unset after start-up, but that
      doesn't hide it: `/proc/1/environ` keeps the start-up environment (and `docker inspect` shows both
      variables). `TFNSW_API_KEY` stays in the environment for the process's life. Support
      `GYMROUTER_TOKEN_FILE` / `TFNSW_API_KEY_FILE` (compose `secrets:` mounted at `/run/secrets`) and
      document that as the way to deploy.
- [ ] **Limit concurrent planning.** `/api/plan`, `/api/stops/near` and `/api/vehicles` have no
      concurrency limit (only suggest-lines and the catalogue are one-at-a-time). Anyone holding the token,
      or a leaked one, can run many plans at once inside the 640 MB / 64-pid container. A small semaphore
      (e.g. 4) answering 429 with `Retry-After` would bound CPU and memory.
- [ ] **Network isolation in compose.** On the shared `traefik_default` network, any other container can
      reach `:8080` directly (skipping Traefik's rate limit and setting its own `X-Forwarded-For`, which
      `trust_proxy` then logs as the client), and this container can reach the others. A dedicated network
      shared only with Traefik closes both. Outbound could also be limited to the TfNSW API,
      `download.bbbike.org` and `build.protomaps.com` at the host firewall.
- [ ] **One shared token, no per-device revocation.** Every device and every settings/backup link (QR,
      share sheet) carries the same long-lived token, so a link that leaks into a chat history works
      forever, and revoking it signs out everything. Worth deciding in `docs/spec.md` whether to keep this
      (simple, single user) or move to per-device tokens issued from a one-time setup code. At least: say
      on the "Set up another device" screen that the link is a password.
- [x] **Upstream text in `/api/status`.** Errors can carry TfNSW's `X-Error-Detail` header and up to 300
      characters of pmtiles output into `Health`. Only token holders see it, but it's unbounded upstream
      text; trimming and length-capping it (as pmtiles output already is) is cheap.
- [ ] **Small ones** (all done but the image scan):
  - [x] `Cross-Origin-Resource-Policy: same-origin` alongside the other headers.
  - [x] `#z=` settings links are inflated in full before the 100 kB check (a 20 kB deflate stream can expand
    ~1000×); stop reading the stream past the limit.
  - [x] GTFS `.txt` files inside the downloaded zips are read without a size cap (`internal/gtfs/load.go`
    `readCSV`); TfNSW is trusted, but a per-file `UncompressedSize64` limit would bound a bad feed.
  - [ ] An image scan (Trivy/Grype) of the published image in `build.yml`, mainly for the bundled pmtiles
    binary, which govulncheck never sees.
  - [x] Local dev: `.env` holding the real API key is mode 0644 (`config.toml` is 0600); `chmod 600 .env`.

# TODO: field reports (2026-10-10)

Branch `field-review`, committed locally step by step. In working order: quick fixes, then the in-trip estimates, the
map, the footer, and last the two that need a decision together (the options board and the connection warnings).

## Server

- [x] **"Some trackwork buses can't be matched"** in the server status. Not like the V8 event buses: `6SH` is a real
      trackwork bus (Campbelltown to Moss Vale, for the Southern Highlands Line) and the code SH wasn't known. It
      was reported because it calls at Campbelltown, a station on a loaded line. SH now means `train SHL`.

## Quick fixes

- [ ] **"timetabled", not "timetable"**, on a ride with no live data.
- [ ] **The house and the hold on the rail line up** with the time on their left and "Leave Home" / "Arrive" on their
      right (both sit a little too high).

## In a trip

- [ ] **Time to spare uses your timed walk.** How long you have to get to the stop is worked out from the street map
      even when the walk has been timed and traced.
- [ ] **Learn the walking pace** from timed, traced walks (an average). A pace set in Settings wins.
- [ ] **Connection warnings.** Look at how they work, find the logic errors, compare with what happened on real trips,
      then adjust. Worked through together.

## Map

- [ ] **You are a blue dot with a white border** during a trip (the size is right).
- [ ] **Once you're on board, only you are shown**, not your vehicle as well.
- [ ] **Smaller vehicle icons with a bus or train in them** rather than the line's name, so they're all one width
      (the line is clear from the map and the trip).

## Trip screen

- [ ] **The options board: rides too thin to label.** The shared time axis shows how the options spread, but a short
      metro, train or bus section can be too thin for its name, the most important thing on it. Mock up alternatives to
      choose from, close to the current design.

## Footer

- [ ] **The server status opens** to show what the status call returned.

# TODO: field-test feedback (2026-10-05)

## Round 9

- [x] **The trip's rail, refined** (choosing a trip, a started trip, and under the map):
  - **Arrive** at the gym's hold (or the house, going home), as at the start, instead of a filled square.
  - **A thicker line** for each ride, about as wide as the stop circles were.
  - **The circles move to the ends of the line:** where you get on, a circle a little wider than the line with a
    small train or bus in it; where you get off, a small white dot inside the line.

## Round 8

- [x] **The trip's rail centred** between the time column and the descriptions: there's a bigger gap on the left now.
- [x] **The same description with its rail when choosing a trip:** the summary under the options looks exactly like
      the started trip's, without the "you" marker.
- [x] **Gyms: reset the order** back to the order they were added (clears the use counts), a small link like
      "Measure my pace".
- [x] **Settings as a cog** in the top right instead of the word.

# Earlier rounds (2026-10-04)

## Round 7

- [x] **Starting a trip opens at the top**, not scrolled down to the trip's steps.
- [x] **More space above the location/checked line** under "Show on map".
- [x] **A bigger "you" marker on the rail:** a blue circle with a white outline and a thin downward arrowhead.
- [x] **The marker follows where you actually are**, placed on the trip's line from your location; the clock is only
      the fallback (no location, or well off the route). (Now / Then still follow the clock and live times.)
- [x] **Now / Then follow where you are too** (basic version): the same "where are you" as the marker. Walking to the
      stop, waiting at it ("Wait for the 52"), on board once you've moved ~120 m along the ride, the last walk. Forward
      only; with no fix you stay put, and only a ride you're on moves on by the clock. Clock-only without any location.
- [x] **Vehicle matching:** your location moving with your vehicle's live position confirms you're on board sooner
      and more surely.
- [x] **Ride lines stop at the stop:** the 52 to 9 Degrees Parramatta ran past its last stop and doubled back.

## Round 6

- [x] **"Gym Router" underline** like the Settings one (it curled up at the ends: the button's rounded corners).
- [x] **Gap above the date/time chooser** the same as the gap below it.
- [x] **Settings: space above "Reset this device"** consistent with the other sections.
- [x] **No rule above Arrive** in a started trip's description.
- [x] **The map opens zoomed to the trip**, filling most of the screen.
- [x] **The "services appear a few stops away" note** can be dismissed, and goes by itself after 5 seconds.
- [x] **A start for the trip's rail:** a house (or the gym's hold) at the top, where you are before you leave.
- [x] **Gyms ordered by use:** as added at first, then the most used at the top.
- [x] **Settings: edit and delete icons** instead of the Edit / Remove links.
- [x] **Settings: light / dark / system** appearance.
- [x] **Measure my walking speed:** a short timed walk from Settings works it out.

## Round 5

- [x] **Walk times only come from recordings made on a trip.** No "Set my time" on changes in the option list, and no
      typed-in minutes per stop in the home/gym editor (stops are still ticked there; their times come from the street
      map or a recording). A recording has a route, which is the point: shortcuts the map doesn't know about.

## Round 4

- [x] **The trip description in a started trip.** The same summary as under the option list, with a bar down the
      left in each leg's colours (dashed for walking) and you on it, moving down as the trip goes. The summary at the
      top and the "you won't make this connection" switch stay; the horizontal progress strip goes.
- [x] **"Time my walk" in the description:** a small link on each walk and change you can time, replacing the big
      "Time this walk" button. (In the option list before a trip, changes keep "Set my time": a walk can only be timed
      while you're doing it.)
- [x] **The same description under the map during a trip**, in place of the horizontal strip, so the map and the
      steps fit on one screen.

## Round 3

- [x] **The map keeps your view.** Live refreshes no longer re-fit the map once you've zoomed or moved it.
- [x] **No rule under the header.** "Gym Router" is underlined when it's the screen you're on, like Settings.
- [x] **Icons for the Settings sections** (homes, gyms, walking and changes, …).
- [x] **A highlight colour** in Settings, for underlines and selection marks: the nine 9 Degrees grade colours
      (green, blue, teal, pink, red, black, purple, white, yellow). Black is the default (the current ink).
- [x] **Door-to-door in the option list always in minutes** ("80m", not "1h20"). The summary stays as it is.
- [x] **"Earlier trips" works with Leave now** too (showing trips that have just gone).
- [x] **Leave now / Leave at / Arrive by above To the gym / Home.**
- [x] **Simpler date and time:** a day list (Today (4th), Tomorrow (5th), then the next few days, about a week)
      and a 24-hour time, instead of the browser's (American-format) date picker. Times on other days say which day.

## Round 2 (before pushing)

- [x] **Unauthenticated visits look like nothing's there.** Without a token the app shows a plain
      "404 page not found" like Go/Traefik's own, and the API answers unauthenticated requests the same way
      (instead of 401 JSON). A setup link is the only way in; the paste-a-link setup form goes.
- [x] **Two more built-in gyms:** ClimbFit Macquarie and ClimbFit St Leonards.
- [x] **A small colour logo of the gym's brand** (9 Degrees, ClimbFit) to the left of each gym in the list.
- [x] **An app icon** next to the "Gym Router" title, and the same mark as the favicon / home-screen icon
      (the current one is a placeholder).
- [x] **"Earlier trips" / "Later trips"** in place of the first/last times above the option list, moving the
      window by 30 minutes.
- [x] **Start a trip from the map** without closing it first.
- [x] **Name the destination in the last walk:** "Walk 5 min to 9 Degrees Parramatta", not "to your destination".
- [x] **A started trip always uses location** (falling back to the timetable if it isn't available).
      Already the case since round 1: location starts with the trip, no tap needed; if it's refused or missing the
      trip follows the timetable and offers "Try my location again".
- [x] **A change icon** in the time column of a change in the trip description.

## Round 1

Worked in this order (quick fixes first, the walk-timing overhaul last). Assumptions are noted where the
feedback left a choice open; say if any are wrong.

## Trip screen

- [x] **Keep the selected option across live refreshes.** Looking at any option but the first, a refresh
      jumped back to the first. (The "same option" key included the leave time, which shifts with live data.)
- [x] **Door-to-door time in the option list**, to the right of the connection-risk square.
- [x] **Number of stops in the selected option's summary** (stops travelled on all rides; also shown per ride).
- [x] **Start a trip from "Leave at" and "Arrive by"**, not only "Leave now". In-trip re-checks then plan
      from the trip's own leave time until it's close, so a later trip isn't reported as missed.
- [x] **Order of choices:** direction (to the gym / home), home, then when (now / leave at / arrive by),
      then the gym, then the plan.
- [x] **Collapse the gym list once one is chosen**, showing only that gym (tap it to choose another).
- [x] **"Gym Router" title goes back to the start** (clears the choices); drop the "Trips" tab.
- [x] **Settings: no double rule above Homes** (the header's line is enough).

## Map

- [x] **Only the trip's own lines, only the sections ridden.** No faint network lines.
- [x] **Stops along the ridden sections** as small dots in the line colour with a white outline.
- [x] **Vehicles only near your part of each trip:** only the vehicles of the trips in the option, shown
      from about 3 stops before you board until about 3 stops after you get off. Other vehicles on the same
      lines are no longer drawn.
- [x] **Your location shows as soon as the map opens during a trip.** Location is on by default in a trip
      (no "Use my location" tap), and the map draws you without pressing the locate button.

## Walk timing overhaul

- [x] **Time walks from the live trip**, not from home/gym settings: the walk to the first stop, each
      change, and the walk from the last stop.
- [x] **Saved per walk and reused whenever that walk comes up again**, for any stop (not only the stops
      ticked under a home or gym; a timed walk to an unticked stop makes that stop usable too).
- [x] **First recording replaces the default** (street-map estimate) for planning, and its GPS trace is
      drawn on the map from then on.
- [x] **Later recordings replace or average** according to a setting (default: average the last 5). The
      save screen offers the other choice too.
- [x] Assumption: a walk is the same either way (home → stop is timed once and also used for stop → home;
      a change A → B also covers B → A, trace reversed).
- [x] Settings lists the timed walks and changes (with remove); the old per-stop "Time it" button goes.
      Existing timings (stop walks, "Set my time" changes) are carried over.

## Setup

- [x] **Adding many built-in gyms timed out** (5 at once failed; 3 then 2 worked): one long request per
      batch. Now a server-side job the app polls, with a progress percentage, so no request runs long.
      (Splitting per gym would have multiplied the work: reading the timetable is most of the cost.)
