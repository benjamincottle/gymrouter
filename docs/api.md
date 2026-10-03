# API

All `/api/*` endpoints need `Authorization: Bearer <token>`. Request bodies are JSON
(`Content-Type: application/json`, max 64 KB, unknown fields rejected). Times are RFC 3339 in Sydney time.
Personal data (home coordinates, walk times) is only sent in request bodies and is never logged or stored.

## `GET /healthz` (public)
`{"ok": true, "service_date": "2026-10-03"}`; returns 503 when the timetable is missing or more than 3 days old.

## `GET /api/defaults`
Default preferences, plus any preset gyms the server offers for import (from its optional config):
```json
{"presets": [{"name": "9 Degrees Lane Cove", "address": "…", "lat": -33.8, "lon": 151.15,
              "lines": ["bus 288", "metro M1", "train T9"], "access": [{"stop": "206638", "walk_s": 300}]}],
 "defaults": {"walk_speed_mps": 1.3, "min_change_s": 60, "max_walk_m": 1000, "risk": {"safe_s": 180, "tight_s": 60}}}
```

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
  data for any line it hasn't seen yet, so the first request naming new lines takes a few seconds. There is a cap on
  the total the server will hold (400 Bad Request beyond it).
- A place is `lat`/`lon`, optionally with curated `access` stops and measured walk times. Without
  them, stops within `max_walk_m` are used. Either end can be home or gym.
- `time` is the earliest time to leave (default: now). Options leaving within `window_min` are returned.
- `transfers` override walking/changing time between stops. A station ID covers all its platforms.
- `"arrive_by": true` treats `time` as the latest arrival: options arriving by then, latest departure first.
- While travelling, the origin can be the vehicle you're on: `"from": {"on_trip": {"trip_id": "…",
  "from_stop": "<stop where you boarded>"}}`. Planning then starts now, on that vehicle: the first leg is that
  ride, and the change off it is rated like any other. `time` and `arrive_by` don't apply.

Response:
```json
{"service_date": "2026-10-08", "realtime": true, "realtime_at": "…",
 "options": [{
   "leave_at": "…", "arrive": "…", "duration_s": 3180, "rides": 3, "risk": "tight", "alternative": false,
   "lines": ["train T9", "metro M1", "bus 288"],
   "legs": [
     {"kind": "walk", "to": {"id": "…", "name": "…", "station": "…", "station_id": "…", "lat": 0, "lon": 0}, "dep": "…", "arr": "…"},
     {"kind": "ride", "from": {…}, "to": {…}, "dep": "…", "arr": "…",
      "line": {"mode": "metro", "name": "M1", "color": "168388", "text_color": "FFFFFF"},
      "trip_id": "…", "headsign": "…", "status": "predicted", "delay_s": 60, "sched_dep": "…"}
   ],
   "transfers": [{"from_leg": 1, "to_leg": 3, "walk_s": 120, "slack_s": 95, "risk": "tight", "fallback_dep": "…"}]
 }]}
```
`status` is `scheduled`, `predicted` (live data) or `added` (a realtime-only trip). `risk` is `safe`, `tight`,
`at-risk` or `missed`. `fallback_dep` is the next service of the onward line from the same stop.

## `POST /api/stops/near`
`{"lat": …, "lon": …, "radius_m": 800}` → stops near the point with every line that serves them, nearest first:
`{"stops": [{"id", "name", "station", "lat", "lon", "walk_s", "lines": ["bus 999"]}]}`. Used to set up a home or gym,
before any lines are chosen. 503 (with `Retry-After`) for a few seconds after the server starts, while it reads the timetable.

## `POST /api/suggest-lines`
`{"from": {"lat", "lon", "access"?}, "to": {…}, "radius_m"?: 1200}` → the lines that appear in the best options
from `from` to `to` over the whole network, searched at 10-minute steps on a typical weekday afternoon and a Sunday morning:
```json
{"windows": [{"label": "Weekday afternoon", "date": "2026-10-06", "departures": 19, "typical_s": 1490}],
 "lines": [{"line": "metro M1", "color": "168388", "share": 1.0, "recommended": true}],
 "itineraries": [{"desc": "metro M1 → […] → bus 533", "lines": ["metro M1", "bus 533"], "median_s": 1860,
                  "best_s": 1800, "seen": 13, "of": 19, "window": "Weekday afternoon"}]}
```
`share` is the fraction of departure times at which the line appeared in an option; `recommended` means at least 30%.
It reads the whole timetable (about 15 s, ~300 MB briefly), so only one runs at a time (429 with `Retry-After` otherwise).
400 if either end has no stops nearby. Coordinates are used in memory only.

## `GET /api/vehicles?lines=<list>`
Live vehicles on the given lines, comma-separated (e.g. `lines=bus 288,train T9`; only fresh data):
`{"vehicles": [{"id", "label", "line": "metro M1", "color", "trip_id", "lat", "lon", "bearing", "status", "ts"}]}`.
Match `trip_id` against a ride leg's `trip_id` to highlight the vehicle you'd catch.

## `GET /api/shape?date=YYYY-MM-DD&trip=<trip_id>&from=<stop_id>&to=<stop_id>`
Path of one ride leg (use a plan's `service_date` and the leg's `trip_id` and stop IDs):
`{"coordinates": [[lon, lat], …]}`. Follows the route shape once shapes have loaded (a few seconds after
startup); until then, straight lines between the trip's stops.

## `GET /api/shapes?lines=<list>`
GeoJSON `FeatureCollection` of the given lines (`MultiLineString` per line, properties `line`, `mode`,
`name`, `color`), for drawing the network faintly under a trip. Like `plan`, these load lines the server hasn't seen.

## `GET /api/map.pmtiles`
The self-hosted basemap (PMTiles), served with HTTP range requests. 404 if not installed (see README).

## `POST /api/geocode`
`{"q": "48 Example St, Suburb"}` → `{"results": [{"name", "type", "lat", "lon"}]}` (up to 6). Looks up an address
through the TfNSW Trip Planner; used once when setting up a home. Limited to one request per second (429 otherwise),
counted against the daily upstream budget; the query is never logged.

## `GET /api/status`
Detailed health: feed ages and errors, upstream requests today, realtime match stats, configured lines missing today.

Any authenticated API request counts as activity: realtime polling runs while the app was used in the last
`realtime.active_for` (default 10 min).
