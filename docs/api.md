# API

All `/api/*` endpoints need `Authorization: Bearer <token>`. Without it (or with a wrong one) every path answers
exactly like a page that doesn't exist: `404`, `text/plain`, `404 page not found`. The API's own 404s are JSON. Request bodies are JSON
(`Content-Type: application/json`, max 64 KB, unknown fields rejected). Times are RFC 3339 in Sydney time.
Personal data (home coordinates, walk times) is only sent in request bodies and is never logged or stored.

## `GET /healthz` (public)
`{"ok": true, "service_date": "2026-10-03"}`; returns 503 when the timetable is missing or more than 3 days old.

## `GET /api/defaults`
The gyms the app knows about (built-in public data) and the default preferences:
```json
{"gyms": [{"id": "lanecove", "name": "9 Degrees Lane Cove", "address": "…", "lat": -33.8, "lon": 151.15,
           "lines": ["bus 288", "metro M1", "train T9"],
           "access": [{"stop": "206638", "name": "Mars Rd Stop", "walk_s": 300}]}],
 "defaults": {"walk_speed_mps": 1.3, "min_change_s": 60, "max_walk_m": 1000, "risk": {"safe_s": 180, "tight_s": 60}}}
```
`lines` are the ones that serve the gym end; the device adds the lines near its own home. `access` (optional) holds
measured walks from the gym door.

## `POST /api/plan`
```json
{
  "from": {"lat": -33.7, "lon": 151.08, "access": [{"stop": "207720", "walk_s": 540}]},
  "to":   {"lat": -33.808, "lon": 151.151},
  "lines": ["train T9", "metro M1", "bus 288"],
  "time": "2026-10-08T16:00:00+11:00",
  "window_min": 30,
  "prefs": {
    "walk_speed_mps": 1.4, "min_change_s": 60, "max_walk_m": 800, "leave_buffer_s": 60,
    "risk": {"safe_s": 180, "tight_s": 60},
    "transfers": [{"from": "2121", "to": "2121", "secs": 150}]
  }
}
```
- `lines` (1–60, each `"<mode> <name>"`) is the set to route on, normally the gym's. The server loads timetable
  data for any line it hasn't seen yet, so the first request naming new lines takes a few seconds (unless they were
  loaded ahead with `POST /api/lines`). A line the timetable doesn't have is a 400, and so is going over the cap on the
  total the server will hold. Lines no request has named for two weeks are dropped again at the next day rollover (the
  built-in gyms' lines always stay). The loaded lines are kept in the data directory, so they survive a restart.
  A train line brings the buses that replace its trains during trackwork (`replacement-bus 23T4` for the T4, matched by
  the line code at the end of the bus's name; `10M` for the metro); they aren't listed in `lines`.
- A place is `lat`/`lon`, optionally with curated `access` stops and walk times. Without
  them, it uses every stop on the lines within `max_walk_m` or within 500 m of the nearest stop on them, whichever reaches
  further, measured on foot along the streets when known (straight line otherwise), and none past 3 km in a straight line. Past that the plan is a 400 ("no stops on those lines
  within 3 km"). Either end can be home or gym.
- A place can also carry `walks` (up to 40, `[{"stop": "<stop or station ID>", "walk_s": 540}]`): walks the traveller
  has timed between the place and a stop. They beat any other time for that stop (a station ID covers its rail platforms,
not bus stands that belong to the station)
  and add the stop if it isn't otherwise considered, curated or not. Not allowed with `on_trip`.
- `time` is the earliest time to leave (default: now). Options leaving within `window_min` are returned.
- `transfers` override walking/changing time between stops. A station ID covers its rail (train, metro, light rail)
  platforms; a bus stand that belongs to the station only matches by its own ID.
- `"arrive_by": true` treats `time` as the latest arrival: options arriving by then, latest departure first.
- While travelling, the origin can be the vehicle you're on: `"from": {"on_trip": {"trip_id": "…",
  "from_stop": "<stop where you boarded>"}}`. Planning then starts now, on that vehicle: the first leg is that
  ride, and the change off it is rated like any other. `time` and `arrive_by` don't apply.

Response:
```json
{"service_date": "2026-10-08", "realtime": true, "realtime_at": "…", "walking": "streets",
 "options": [{
   "leave_at": "…", "arrive": "…", "duration_s": 3180, "rides": 3, "risk": "tight", "alternative": false,
   "lines": ["train T9", "metro M1", "bus 288"],
   "legs": [
     {"kind": "walk", "to": {"id": "…", "name": "…", "station": "…", "station_id": "…", "lat": 0, "lon": 0}, "dep": "…", "arr": "…"},
     {"kind": "ride", "from": {…}, "to": {…}, "dep": "…", "arr": "…",
      "line": {"mode": "metro", "name": "M1", "color": "168388", "text_color": "FFFFFF"},
      "trip_id": "…", "headsign": "…", "status": "predicted", "delay_s": 60, "sched_dep": "…", "stops": 6}
   ],
   "transfers": [{"from_leg": 1, "to_leg": 3, "walk_s": 120, "slack_s": 95, "risk": "tight", "fallback_dep": "…"}]
 }],
 "trackwork": [{"line": {"mode": "train", "name": "T4", "color": "005AA3", "text_color": "FFFFFF"}, "buses": ["23T4"]}]}
```
`trackwork` (left out when empty) lists the lines whose trains the options replace with buses, and those buses' names
(the names on their signs).
`stretched_walk` (left out when neither end needed it) is `{"from_m"?: 1430, "to_m"?: …}`: for each end that had no stop
within `max_walk_m` (even its nearest is further), the walk to that nearest stop in metres. An end with timed `walks` never reports it.
`walking` is `streets` when walks to and from stops, and changes between stops, follow real streets and paths (the server's OpenStreetMap-based
network), or `estimate` (straight line × a detour factor) while that network is still being prepared. Curated `access`
walks apply either way. Walk legs carry a `path` (`[[lon, lat], …]`) along the streets when it is known. A walk that
passes a stop on the way (off at one stop, on foot to another and from there to the place) is drawn through that stop,
each piece along the streets where known and straight otherwise. `max_walk_m` is measured on foot (see the place rules above).

`stops` on a ride is the number of stops travelled, counting the one you get off at.
`status` is `scheduled`, `predicted` (live data) or `added` (a realtime-only trip). `risk` is `safe`, `tight`,
`at-risk` or `missed`. `fallback_dep` is the next service of the onward line from the same stop.

## `POST /api/lines`
`{"lines": ["bus 288", "train T9"]}` (up to 150) starts loading the lines in the background and answers
`202 {}` straight away, so that the first search naming them doesn't wait. The app sends all its gyms' lines when it
opens and whenever they change. Lines the timetable doesn't have are skipped; a malformed line, or going over the cap
on the total the server will hold, is a 400. Asking counts as using the lines (see `lines` under `POST /api/plan`).

## `POST /api/stops/near`
`{"lat": …, "lon": …, "radius_m": 800}` → stops near the point (within `radius_m` or 500 m of the nearest, as for a plan) with every line that serves them, nearest first:
`{"stops": [{"id", "name", "station", "lat", "lon", "walk_s", "lines": ["bus 288"]}]}`. Used to set up a home or gym,
before any lines are chosen. Also returns `"walking": "streets"|"estimate"`: with street data, `walk_s` follows the streets
and stops that can't be reached on foot are left out. 503 (with `Retry-After`) for a few seconds after the server starts, while it reads the timetable.

## `POST /api/suggest-lines`, `GET /api/suggest-lines/{job}`
`{"from": {"lat", "lon", "access"?}, "to": [{"lat", "lon", "access"?}, …], "radius_m"?: 1000}` (1 to 12 destinations)
finds, for each destination, the lines that appear in the best options from `from` over the whole network, searched at
10-minute steps on a typical weekday afternoon and a Sunday morning. Each end uses the stops a plan would (`radius_m` is
the longest walk, default the server's `max_walk_m`), so every suggested line is one a plan can reach.

It reads the whole timetable once per day searched (about 15 s, ~300 MB briefly, longer on a small server) however many
destinations there are, which can outlast a proxy's or phone's patience, so it runs as a job: the POST answers
`202 {"job": "<id>"}` straight away (400 for bad input; 429 with `Retry-After` while another search runs, since only one
runs at a time). Poll `GET /api/suggest-lines/{job}` every second or two:
```json
{"state": "running", "done": 3, "of": 12}
{"state": "failed", "done": 1, "of": 12, "error": "no stops within 3 km"}
{"state": "done", "done": 12, "of": 12, "results": [{
  "windows": [{"label": "Weekday afternoon", "date": "2026-10-06", "departures": 19, "typical_s": 1490}],
  "lines": [{"line": "metro M1", "color": "168388", "share": 1.0, "recommended": true}],
  "itineraries": [{"desc": "metro M1 → […] → bus 533", "lines": ["metro M1", "bus 533"], "median_s": 1860,
                   "best_s": 1800, "seen": 13, "of": 19, "window": "Weekday afternoon"}]}]}
```
`done`/`of` count steps (reading a day's timetable; searching one destination on it). `results` are in the order asked.
`share` is the fraction of departure times at which the line appeared in an option; `recommended` means at least 30%.
Finished jobs are kept for 10 minutes, then 404. Coordinates are used in memory only.

## `GET /api/vehicles?lines=<list>[&rides=<list>]`
Live vehicles on the given lines, comma-separated (e.g. `lines=bus 288,train T9`; only fresh data):
`{"vehicles": [{"id", "label", "line": "metro M1", "color", "trip_id", "lat", "lon", "bearing", "status", "ts"}]}`.
With `rides` (up to 8, comma-separated, each `<trip_id>|<from stop_id>|<to stop_id>` from an option's ride legs), only
the vehicles running those rides, and only while they're within 3 stops of the part ridden: on the way to where you
get on, carrying you, or just past where you get off.

## `GET /api/shape?date=YYYY-MM-DD&trip=<trip_id>&from=<stop_id>&to=<stop_id>`
Path of one ride leg (use a plan's `service_date` and the leg's `trip_id` and stop IDs) and the stops it calls at
in between: `{"coordinates": [[lon, lat], …], "stops": [[lon, lat], …]}`. Follows the route shape once shapes have loaded (a few seconds after
startup), with each stop placed on the line; until then, straight lines between the trip's stops.

## `GET /api/map.pmtiles`
The self-hosted basemap (PMTiles), served with HTTP range requests. 404 if not installed (see README).

## `POST /api/geocode`
`{"q": "48 Example St, Suburb"}` → `{"results": [{"name", "type", "lat", "lon"}]}` (up to 6). Looks up an address
through the TfNSW Trip Planner; used once when setting up a home. Limited to one request per second (429 otherwise),
counted against the daily upstream budget; the query is never logged.


## `GET /api/status`
`state` is the verdict at a glance: `ok`; `warning` when the server works but something needs looking at; `error` when
it can't plan (no timetable, or one more than three days old). `issues` (left out when there are none) says what:
`no-timetable`, `old-timetable`, `timetable-refresh` (the last download failed), `live-data` (a realtime feed is failing
while the app is in use), `trackwork` (see `unknown_trackwork`), `street-map`, `basemap` (their download failed).
Lines missing today aren't an issue: weekday-only buses are missing every weekend. The app shows this in its footer.
Asking for the status doesn't count as using the app (it doesn't start realtime polling).

Detailed health: feed ages and errors, upstream requests today, realtime match stats, configured lines missing today
(`missing_lines`; a line replaced by buses all day isn't missing), buses named like trackwork buses that are left out of
searches because they can't be tied to a line (`unknown_trackwork`: a new line code, or typed as an ordinary bus; they
call at a station of a loaded line, so the app needs updating), and
`data`: whether the street network and basemap are ready, their age and any error from the last attempt to fetch them.

Any authenticated API request counts as activity: realtime polling runs while the app was used in the last
`realtime.active_for` (default 10 min).
