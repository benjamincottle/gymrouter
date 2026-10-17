# TODO: from the codebase review before the UI overhaul (2026-10-17)

Small things found reading the app end to end; fixed after the overhaul (2026-10-17).

- [x] **The map's vehicle errors show the server's own words.** `web/src/map/mapview.tsx` puts `e.message` from a
      failed `/api/vehicles` straight into its callout. Every other error goes through `problem()` (`web/src/api.ts`),
      as DESIGN.md's Errors section says: never a raw server string.
- [x] **An open map keeps the theme it opened with.** The map's style (light or dark basemap, and the colours read
      from the CSS tokens: walks, outlines, the "you" blue) is set once when the map is made, so switching Light/Dark
      in Settings (or the system switching under Auto) leaves an open map in the old theme until it's opened again.
      On desktop, where the map pane stays open, that's the whole session.
- [x] **Importing a backup doesn't say it changes the access token.** A backup file whose token differs from the
      device's replaces it, but the confirm sheet only counts homes and gyms ("It replaces the 1 home and 3 gyms on
      this device"). A link with a different token says so ("…and its access token", `importQuestion` in
      `web/src/settings.ts`); the file import should too.

# TODO: live data (2026-10-15)

- [x] **Vehicle positions over the events stream.** Plans re-plan when `GET /api/events` says live data changed;
      the map's vehicles and a trip's check for your vehicle were still polled every 10 s. The server now sends a
      `vehicles` event after each positions fetch (every 15 s while the app is in use) and both fetch then, so a
      vehicle moves as soon as its report is in and no request is wasted between reports. One stream serves the
      whole app (`web/src/live.ts`).
- [x] **Check the stream through Traefik on p4**: that it stays open past a minute (Traefik 3's entry points have a
      60 s read timeout by default). Checked 2026-10-15 against the live app (Traefik 3.7.14): open for 200 s over
      HTTP/2 and 100 s over HTTP/1.1, an event about every 30 s and a keep-alive every 20 s, never dropped. The read
      timeout doesn't reach a request that's already been read, so nothing in the homelab needs changing.

# Security and hardening review (2026-10-05)

For review: the unticked items are still open. The baseline is already good (256-bit token compared as a digest,
unauthenticated requests look like a 404, strict CSP and headers, bounded bodies and inputs, query strings and
bodies never logged, distroless non-root read-only container, pinned actions, govulncheck and npm audit
clean today). Roughly in order of value.

- [x] **Images don't pick up Go security fixes.** `build.yml` rebuilt on a schedule only when the distroless
      digest changed, so a Go standard-library fix (the binary is static, built on `golang:1.27-trixie`)
      only shipped with the next code push. Decided 2026-10-05: fold the `golang` and `node` image digests
      into the scheduled check. Superseded 2026-10-13 by pinning them (below): Dependabot now looks daily
      and a Go patch release arrives as a pull request, which rebuilds the image when it's merged. The
      scheduled check reads the floating tags from the Dockerfile, which leaves the distroless base.
      Changing the runtime base wouldn't help: the fix lives in the binary, and `distroless/static:nonroot`
      is already the right base (`scratch` and Chainguard `static` are equivalent at best). The bundled
      `pmtiles` binary has its own Go and only updates with upstream releases; the image scan covers it.
- [ ] **Deploy a fixed image, not `:latest`.** `deploy/compose.example.yaml` pulls `:latest`. Deploy by the
      `:<sha>` tag or a digest (Ansible variable), so a deploy is reproducible and can be rolled back;
      optionally check the build provenance with `gh attestation verify` / cosign before rolling out.
- [x] **Pin the build images by digest.** `node:26-trixie-slim` and `golang:1.27-trixie` were floating tags
      (the pmtiles image was already pinned by digest). Both are pinned now, and Dependabot's docker
      ecosystem keeps them current, daily rather than weekly.
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
      `download.geofabrik.de` and `build.protomaps.com` at the host firewall.
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

# TODO: field review (2026-10-11)

Branch `field-review-2`, committed locally. In a trip and Map are done but for the stops countdown; each was chosen
from mockups taken from the running app (the alternatives are in the spec's decision log, 2026-10-11). Server and
Investigate are recorded only, not started.

## In a trip

- [x] **A "faster way" notice can be dismissed.** That way then isn't offered again on the trip, through re-checks and
      a reload: it's known by the vehicles it takes that yours doesn't. Only the fastest way is ever offered, so with
      it dismissed nothing shows until a different way is the fastest.
- [x] **The notice shows the trip it suggests as a row from the list of trips**: leave, tape, arrive, risk, door to
      door, from where you are now. Its sentence drops what the row says ("6 min sooner, with no changes."). The "You
      won't make…" warning shows its next best the same way ("Next best, with no changes:").
- [x] **A stopwatch starts each "Time my walk" link.** Drawn (the Settings one, 16px, in the link's ink), not the ⏱
      emoji, which every phone draws its own way.
- [x] **A countdown of the stops left on a ride**, in three places (chosen from mockups; the spec's decision log,
      2026-10-11, has the alternatives):
  - **In the steps**, a ride opens into its stops: every stop ahead, named and timed, yours last. A tap on its stops
    line opens or closes it (while planning too). On the ride you're on that line counts down ("10 of 19 stops to
    go"), and the ride opens by itself for the last three stops. Your tap wins either way.
  - **In Now**, the ride as a strip map with you along it, the next stop and the count under it. With one stop to
    go the instruction becomes "Get off at the next stop".
  - **On the map**, where you get off is a larger white dot, and the view closes in on what's left of the ride for
    its last three stops, unless you've moved the map.
  - What's behind you on a ride is dimmed, on the rail, the strip and the map.
  - The server sends each ride's stops with their names and live times (`calls`).
- [x] **Anywhere on the platform is waiting for the train.** Wandering up the platform turned Now back to "Get to
      Hornsby Station": you only counted as waiting within 40 m of the platform's point in the timetable, which is
      often near one end (74 m from the middle of Hornsby's platform 1, 152 m from its far end). The other way, more
      than 120 m forward along the track counted as on board while you were still standing on a long platform.
      Now the server keeps each platform's outline from the street map (629 in the area) and sends a train's
      boarding platform with its shape; on it, or within the location's accuracy of its edge, you're waiting, and
      you're not on board by distance until you're off it. All 548 of the timetable's rail platforms in the area have
      an outline: 505 are drawn in the street map as one shape, and the rest (Central's 16 to 21, Chatswood, North
      Sydney, Auburn, the north-west metro) as a shape in several pieces with the stairwells cut out, which the
      reader now reads too.

## Map

- [x] **Once you've set off, the map fits the step you're on**: the walk, the ride from where you get on to where you
      get off, the change, with you in it. Waiting at a stop, it's the ride you're waiting for. Before that it's the
      whole trip, as it was. "Set off" is the "you" marker's own rule: your location has you past the trip's first
      step, or with no location the leave time has come. The view moves on with each new step, unless you've moved
      the map yourself.
- [x] **You are drawn as what you're doing**, on the map and on the trip's rail: a walker on foot (walking, or waiting
      at a stop), a train, bus or ferry on a ride, white on the blue inside the white border. 28px on both (the map's
      was a 20px dot). At either end of the rail you're still the house or the shoe.
- [x] **The map can be pulled down over the steps** by a handle on the rule under it: drag it, or tap it. The arrive
      line and what to do now stay under it. Drag it back up for the steps. It stays as you left it for the rest of
      the trip.
- [x] **Stops along a route are 10px across** with a heavier outline (they were 6px, as wide as the line).

## Server

- [x] **The walking paths come from Geofabrik** instead of BBBike: its New South Wales extract, rebuilt daily (BBBike's
      Sydney one was weekly, and stopped at 150.83°E, short of Penrith). The server downloads the whole state
      (269 MB) every 35 days or when asked, keeps the streets inside the app's area and throws the rest away. The
      area is the basemap's box, so wherever the map draws, walks are routed: west 12 km past Penrith, north to
      Berowra and Palm Beach, south to Waterfall and Campbelltown. 1.92 M nodes (1.32 M before), a 57 MB cache.
- [x] **Climber Collective is back as a built-in gym** (Jamisontown): it was taken out only because the street map
      didn't reach it. Seventeen gyms again, and a test holds every built-in gym inside the app's area.

## Investigate

- [x] **Platform start points placed along the platform** to line up with the fastest exit at the destination
      (i.e. dynamic). Built 2026-10-12, pending the field tests listed below: a train or metro ride in a plan says
      which carriage to board and where on the platform it stops (`internal/stand`), Now shows it as a strip from
      "Get to the station" until you're on board, and the map picks out the platform and marks the spot. What
      follows is how it got there. Investigated 2026-10-11: feasible for most train platforms, to about a carriage or two, not to
      the metre. What it needs exists:
  - each platform's shape and length: the server now keeps an outline for every one of the timetable's 548 rail
    platforms in the area (median length 167 m);
  - where the stairs, escalators and lifts are: the timetable feed places them (`stops.txt` nodes such as "STR To
    P1", joined to platforms in `pathways.txt`) for 89% of platforms, a median 4 m from the platform's shape;
  - how long the train is: the trip ID carries the car count, and the metro feed lists its carriages;
  - an 8-car train (163 m) fills a typical platform, so the part of the train beside your stairs at the destination
    carries over to the platform you board from.
  What limits it: where a 4-car train stops on a long platform isn't published; which stairs you'd take is a guess
  (the shortest walk on); location on a platform is too poor for live correction, so it's "where to stand", said
  once. A guess at the cost: 400 to 600 lines on the server with many special cases, a few milliseconds a plan, and
  wrong advice is worse than none.
  **Next, when asked:** a time-boxed prototype that only prints its advice for trips we know, with no interface;
  build it for real only if that advice turns out right.
  **How it would look, chosen from mock-ups (2026-10-12), not built:**
  - In Now, from "Get to the station" until you're on board, and nowhere else (not on the ride's step, not while
    planning): the train as a strip heading right, a piece for each carriage, the front one with a raked driver's
    cab. Every carriage is a hairline-grey outline but yours, which is a thin outline in the line's colour. Under it
    the runner from the gym rows and one sentence with no reason given: "Board the 4th carriage from the front."
  - On the map: the platform you board from has a faint fill in the line's colour. Where the spot to stand is known,
    the runner sits on it in the "you" marker's disc at 45% strength. It never reacts to where you are: no "you're
    there", and your own marker simply passes over it.
  - The carriage can be named from the destination platform alone. The spot can only be named where the train fills
    the platform you board from: Hornsby's 2 and 3 are 77 m longer than an 8-carriage train, and where a train stops
    along them isn't published. There, the strip shows and the map has no marker.
  - Nothing shows when the carriage isn't known (a short train, a platform with no stairs in the data).
  - It shows whenever the carriage is known: every train ride, with no test for whether it's worth it.
  - It only ever aims for stairs or escalators, never a lift.
  **The prototype (2026-10-12):** a script outside the repo that only prints. Over 102 real plans between a neutral
  start at Hornsby station and the 17 built-in gyms, both ways, it met 220 different train and metro rides, getting
  off at 70 platforms.
  - How it decides: the way on is the quickest way by the timetable's station pathways (lifts left out) from the
    platform to the next platform, or out of the station and on to the next stop or the place. The carriage is where
    that way off sits along the platform's outline, from the end the train leaves by, in 20.4 m carriages (22 m on
    the metro). A platform longer than the train is tried with the train stopped at each end of its room: if the two
    answers differ, it says nothing. The spot to wait needs the boarding platform within 10 m of the train's length.
  - What came out: a carriage for 111 rides (the way on is stairs for 86, escalators for 12, a way off on the level
    for 13), and a spot to wait for 64 of those. It can't say for 89, always because the platform is longer than the
    train: the long platforms built for intercity trains (Hornsby 2 to 4, Strathfield, Epping, Lidcombe, Granville,
    Penrith, Central 14) and most of the metro. Nothing to say for 20 (the next train is at the same platform or
    across it).
  - Not checked on the ground. The two answers that could be tested hold (Redfern's Lawson St stairs at the city
    end, Central's main concourse at the north end of 16 to 23).
  - Decided since (2026-10-12): on a platform longer than the train, don't stay quiet: take the train as stopped
    in the middle of its room, for the carriage and for the spot on the map alike. It's the right end of the train
    at least. And a close call is no reason to hold back: pick the best way on even when the second best is within
    seconds (31 answers, beside a different carriage for 25).
  - Where trains stop turned out to be in data we already download. The street map (the same Geofabrik extract)
    marks a stopping point on the track at each platform, tagged with the platform's timetable ID (`ref`), and on 553
    of the 591 train and metro stopping points in the area it carries `exit:carriages:forward`: one entry a
    carriage, saying which way off is beside it (Hornsby 3: `no|no|lift|steps|no|no|no|no`). 477 lists are for 8
    carriages, 30 for 6, 26 for 10. Which end is the front has to be worked out (the tag follows the track's
    direction in the map): the prototype turns the list whichever way fits the measured stairs and lifts better.
  - Second run, with that list used first, measuring second and the middle last: every ride that needs a carriage
    gets one. 153 from the map, 20 measured, 27 by the middle (platforms with no list, or a list for another length
    of train: Penrith, Eastwood, Epping, Central 14, Strathfield 1, part of the metro), 20 with nothing to say. The
    spot to wait comes from the boarding platform's list for 180 of the 200.
  - The map and measuring disagree more often than not: the same carriage for 48 of the 153, one out for 87, two out
    for 18. The map is the likelier to be right (recorded per staircase; measuring depends on where an outline ends
    and where the timetable puts the top of the stairs), but it's volunteers' work and unchecked by us. **To check
    on the ground**, the map's Hornsby, front carriage first: platform 1 leaving for the city, stairs and lift at
    the front; platform 3 leaving towards Strathfield, lift at the 3rd and steps at the 4th; arriving, platform 1
    stairs and lift at the back, platform 2 stairs at the 7th and lift at the 8th, platform 4 stairs at the 6th and
    lift at the 7th.
  - Ways off on the level count as targets: the ramps at Strathfield, the walk off the end of Central 14, the links
    to the concourse at Sydenham and Gadigal (the map lists them as `flat` or `ramp`). A way off that is only a
    lift is skipped whatever kind of path the timetable calls it (Meadowbank's "LFT Railway Rd" is a "walkway").
  - The metro says where it stops. Its trains report "stopped at" with a position in the live feed. Ten minutes of
    collecting at one poll every 10 s, on top of an earlier sparser sample, gave a position for 40 of its 42
    platforms; at 39 a second train reported the same position to the last digit (at Barangaroo 1 a third train
    was 10 m further on). The two without are rarely used (Sydenham 2, Tallawong 3). That position is the train's
    front: at 34 of the 40 it's where the front of a train stopped in the middle of the platform would be (3.4 m
    out on average, spread 5.5 m), so the middle rule is the metro's real rule. The other six (Epping 5, Martin
    Place 3 and 4, Central 26 and 27, Sydenham 1) are platforms whose outlines in the map are wrong: there the
    reported position stands in for the outline.
  - Which end of the map's list is the front comes from the track: the list runs front carriage first for a train
    going the way the track is drawn in the map (`:forward`). Wherever the stairs could settle it themselves, the
    track agreed, 219 times out of 219. Fitting the list to the stairs alone flips on near-symmetric platforms
    (Macquarie Park went from the 5th carriage to the 2nd), so the track's direction has to be read.
  - Why the map and measuring differ: probably the timetable marks the top of the stairs, by the concourse, and the
    map where they land on the platform. On the metro, where the two name different carriages the map's is the one
    further from the middle of the train 42 times out of 56. So the map's lists are the ones to trust, and
    measuring is good to a carriage where there's no list. Not confirmed on the ground.
  - Fourth run, with all of that: 225 rides; the carriage comes from the map's list for 152, from the metro's
    reported stop for 28, from measuring for 11 and from the middle rule for 12 (trains only: Penrith, Eastwood,
    Epping, Central 14, Strathfield 1); 22 have nothing to say. On the trains the map and measuring name the same
    carriage for 46 of 142, one out for 79, two out for 17.
  - Dead ends: ordinary trains' live positions sit on a fixed point per platform, too coarse. The carriage tables in
    the Sydney Trains bundle (`vehicle_boardings.txt`) say which carriages reach a platform shorter than the train,
    not where a train stops on a longer one; still worth using, so it never names a carriage whose doors stay shut.
  - **Pending field tests** (the approach looks solid; these are what only standing on a platform can settle):
    the map's lists at Hornsby, platforms 1 to 4 (above); at a station where the map and measuring name different
    carriages, which one the stairs are really beside; on the metro, that the front of the train stops where the
    collected position says; and on a long platform with no list (Epping, Eastwood, Strathfield 1), how far out the
    middle rule is.
  - As built: the street network's build keeps these stopping points, their lists and the direction of the track
    through each, and the metro's 40 stopping points are a table built into the server. Having the server learn
    them from the live feed it already polls instead would keep them current without a release; not done.

## Walks

- [x] **Timed walks are removed** (2026-10-12). Walk routing is good now and a missing path can be added to
      OpenStreetMap, so timing walks by hand isn't needed: "Time my walk", the saved list, their traces on the map
      and the pace learned from them are gone. Every walk is its route along the street map at the walking speed
      (default now 5.5 km/h; "Measure my pace" stays). A ticked stop says which stop, not how long: its time comes
      from the street map each time. This also settles whether a timed walk belongs to a station or a platform,
      raised over Central on 2026-10-10: there are none.

# TODO: field reports (2026-10-10)

Branch `field-review`, committed locally step by step. In working order: quick fixes, then the in-trip estimates, the
map, the footer, and last the two that need a decision together (the options board and the connection warnings).

## Server

- [x] **"Some trackwork buses can't be matched"** in the server status. Not like the V8 event buses: `6SH` is a real
      trackwork bus (Campbelltown to Moss Vale, for the Southern Highlands Line) and the code SH wasn't known. It
      was reported because it calls at Campbelltown, a station on a loaded line. SH now means `train SHL`.

## Quick fixes

- [x] **"timetabled", not "timetable"**, on a ride with no live data.
- [x] **The house and the hold on the rail line up** with the time on their left and "Leave Home" / "Arrive" on their
      right (both sit a little too high).

## In a trip

- [x] **Time to spare uses your timed walk.** How long you have to get to the stop is worked out from the street map
      even when the walk has been timed and traced. It was the straight line to the stop × 1.3; and the 30-second
      re-check planned from your GPS position over the street map, which could call a service missed that your own
      route still catches. Both now go by the trip's own walk: where you are along your trace (or the street route),
      and that share of its time.
- [x] **Learn the walking pace** from timed, traced walks (an average). A pace set in Settings wins. All the traced
      distance over all the time, crossings included; the Walking speed field says what an empty field uses.
- [x] **Connection warnings.** They came up on trips that were fine, and sometimes went away again. Every 30 s the app
      planned again and looked for an option with the same vehicles; none meant "You won't make the planned
      connection". But a search only lists the best trips. Reproduced against the server: a later departure with the
      same arrival hides yours; just off a vehicle you still counted as riding, and the server couldn't start from a
      stop the vehicle had passed; from the vehicle, a timed change and the change buffer were both forgotten.
      Now the server checks the trip itself: the app names the rides still ahead and gets that trip back with live
      times and each change rated. Only that decides whether it still works; the search is just for something better.
      The warning says what won't be made. Also: you're off a ride once you're at its stop and its time has come; a
      location quiet for 90 s (a tunnel) isn't used; with no location you're taken to be walking as planned.
      A warning that's showing stays until there's the "tight" setting to spare again, so it doesn't come and go.
      "A faster way" is only offered when its changes are no riskier than yours, and says how sure they are.

## Map

- [x] **You are a blue dot with a white border** during a trip (the size is right). The border was there but white on
      a pale map: it now has a soft dark edge, and is a touch wider (3px).
- [x] **Once you're on board, only you are shown**, not your vehicle as well. The vehicle you're riding goes; ones you
      have yet to catch stay. With no fresh fix of your own (a tunnel) your dot follows the vehicle's reports.
- [x] **Smaller vehicle icons with a bus or train in them** rather than the line's name, so they're all one width
      (the line is clear from the map and the trip). 25px across, from 34.

## Trip screen

- [x] **The options board: rides too thin to label.** The shared time axis showed how the options spread, but a short
      metro, train or bus section could be too thin for its name, the most important thing on it. Chosen from mockups
      (option D, smaller dots): each trip fills the full width on its own scale, no shared axis. Rides and walks are as
      wide as their share of the trip; a ride is never narrower than its name, the room taken from the walks. Walking is
      small even dots, and between two rides the dots just fill the space.
- [x] **The risk mark sits evenly** between the arrive time and the door-to-door time.

## Footer

- [x] **The server status opens** to show what the status call returned: a row for each thing in plain words (problems
      in their status colour), and under them the reply itself as JSON. Opening it asks the server again.

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
