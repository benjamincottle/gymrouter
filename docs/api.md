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
           "access": [{"stop": "206638", "name": "Mars Rd Stop"}]}],
 "defaults": {"walk_speed_mps": 1.528, "min_change_s": 60, "max_walk_m": 1000, "risk": {"safe_s": 180, "tight_s": 60}}}
```
`lines` are the ones that serve the gym end; the device adds the lines near its own home. `access` (optional) names
the stops to use at the gym end, when it shouldn't be every stop within the walking limit.

## `POST /api/plan`
```json
{
  "from": {"lat": -33.7, "lon": 151.08, "access": ["2077301", "2077303"]},
  "to":   {"lat": -33.808, "lon": 151.151},
  "lines": ["train T9", "metro M1", "bus 288"],
  "time": "2026-10-08T16:00:00+11:00",
  "window_min": 30,
  "prefs": {
    "walk_speed_mps": 1.4, "min_change_s": 60, "max_walk_m": 800, "leave_buffer_s": 60,
    "risk": {"safe_s": 180, "tight_s": 60}
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
- A place is `lat`/`lon`, optionally with `access`: the IDs of the stops to use (up to 20), each walked to along the
  streets at the walking speed (a straight-line estimate for one the street network can't reach, or while there is no
  network). Without
  them, it uses every stop on the lines within `max_walk_m` or within 500 m of the nearest stop on them, whichever reaches
  further, measured on foot along the streets when known (straight line otherwise), and none past 3 km in a straight line. Past that the plan is a 400 ("no stops on those lines
  within 3 km"). Either end can be home or gym.
- A place can also carry `walks` (up to 40, `[{"stop": "<stop or station ID>", "walk_s": 540}]`): how long the walk
  from here to a stop takes, when the app knows better than the street map from this point. During a trip the app
  sends its position with one: the walking left to the stop it's heading for, by the trip's own walk (and 0 for the
  stop it's waiting at). A walk beats any other time for that stop (a station ID covers its rail platforms, not bus
  stands that belong to the station) and adds the stop if it isn't otherwise considered. Not allowed with `on_trip`.
- `time` is the earliest time to leave (default: now). Options leaving within `window_min` are returned.
- `"arrive_by": true` treats `time` as the latest arrival: options arriving by then, latest departure first.
- While travelling, the origin can be the vehicle you're on: `"from": {"on_trip": {"trip_id": "…",
  "from_stop": "<stop where you boarded>"}}`. Planning then starts now, on that vehicle: the first leg is that
  ride, and the change off it is rated like any other. `time` and `arrive_by` don't apply.
- While travelling, `"keep": [{"trip_id": "…", "from": "<stop ID>", "to": "<stop ID>"}, …]` (up to 8, in order) names
  the rides still ahead on the trip being followed. The answer's `kept` says how that trip stands now, whatever the
  search returns (a search only lists the best trips, and a good trip can drop out of them). With `on_trip`, the first
  ride kept must be that trip from `from_stop`, and the stop it's kept to stays reachable for the search once the
  vehicle's time there has passed. Not allowed with `arrive_by`.

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
      "trip_id": "…", "headsign": "…", "status": "predicted", "delay_s": 60, "sched_dep": "…", "stops": 6,
      "calls": [{"name": "…", "station": "…", "arr": "…"}],
      "board": {"carriage": 4, "of": 8, "spot": [151.0986, -33.7034]}}
   ],
   "transfers": [{"from_leg": 1, "to_leg": 3, "walk_s": 120, "slack_s": 95, "risk": "tight", "fallback_dep": "…"}]
 }],
 "trackwork": [{"line": {"mode": "train", "name": "T4", "color": "005AA3", "text_color": "FFFFFF"}, "buses": ["23T4"]}]}
```
`trackwork` (left out when empty) lists the lines whose trains the options replace with buses, and those buses' names
(the names on their signs).
`stretched_walk` (left out when neither end needed it) is `{"from_m"?: 1430, "from_stop"?: "212110", "to_m"?: …, "to_stop"?: …}`:
for each end that had no stop within `max_walk_m` (even its nearest is further), the walk to that nearest stop in metres,
and the stop by the ID `walks` would name it by (a train, metro or light-rail platform by its station's ID; any other
stop by its own). An end with `walks` never reports it.
`walking` is `streets` when walks to and from stops, and changes between stops, follow real streets and paths (the server's OpenStreetMap-based
network), or `estimate` (straight line × a detour factor) while that network is still being prepared. Walk legs carry a `path` (`[[lon, lat], …]`) along the streets when it is known. A walk that
passes a stop on the way (off at one stop, on foot to another and from there to the place) is drawn through that stop,
each piece along the streets where known and straight otherwise. `max_walk_m` is measured on foot (see the place rules above).

`kept` (only with `keep`) is `{"option": {…}, "catch_s": 95}`. `option` is the kept trip as an option like any other,
with live times; each change is rated with the traveller's own change times and the change buffer, and is `missed`
when there's no longer time for it (the option is still returned). It's left out when the trip can't be made at all: a
ride is cancelled, isn't in the timetable, or doesn't call at those stops, or the trip doesn't reach the places asked
for. `catch_s`, when not on a vehicle, is the time to spare on reaching the first ride from `from` at `time` (negative:
too late); it's left out with `on_trip`. Still on the first ride after its time at the stop kept to, the change off it
has what's left from now, less the walk.

`stops` on a ride is the number of stops travelled, counting the one you get off at.
`board` (a train or metro ride, when it's known) is where to board so as to step off beside the way on: the
`carriage`, counted from the front, `of` how many, and the `spot` on the platform where that carriage stops (`[lon,
lat]`, left out when the street map has no outline for the platform). The way on is the stairs or escalators the trip
takes at the far end, to its next platform or out of the station, never a lift. The carriage comes from the street
map's record of which carriage stops beside which way off; without one it's measured along the platform, from where
the metro's trains report stopping or, for other trains, with the train in the middle of the room it has. It's left
out when the next ride leaves from the same platform or the one across it, when the train's length isn't known, and
while the street network is still being prepared.
`calls` are those stops in order, ending with the one you get off at: each one's `name` (and its `station`, for a
platform) and when the ride gets there (`arr`, by live data when the trip has it; left out for a stop the trip skips).
They're in the same order as the stops `/api/shape` places on the ride's line.
`status` is `scheduled`, `predicted` (live data) or `added` (a realtime-only trip). `risk` is `safe`, `tight`,
`at-risk` or `missed`. `fallback_dep` is the next service of the onward line from the same stop.

## `POST /api/lines`
`{"lines": ["bus 288", "train T9"]}` (up to 150) starts loading the lines in the background and answers
`202 {}` straight away, so that the first search naming them doesn't wait. The app sends all its gyms' lines when it
opens and whenever they change. Lines the timetable doesn't have are skipped; a malformed line, or going over the cap
on the total the server will hold, is a 400. Asking counts as using the lines (see `lines` under `POST /api/plan`).

## `GET /api/lines?lines=<list>`
`{"colors": {"train T1": "F99D1C", "bus 288": "00B5EF"}}`: the route colour (hex, no `#`) of each line named, from
the stop catalogue, so a line's chip can be drawn in its colour before any search has used it. It loads nothing and
doesn't count as using the lines. A line without a colour, one the timetable doesn't have, or any line while the
catalogue is still being built, is left out. A malformed line is a 400.

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
{"state": "running", "stage": "reading", "progress": 0.214}
{"state": "failed", "stage": "reading", "progress": 0.389, "error": "no stops within 3 km"}
{"state": "done", "stage": "searching", "progress": 1, "results": [{
  "windows": [{"label": "Weekday afternoon", "date": "2026-10-06", "departures": 19, "typical_s": 1490}],
  "lines": [{"line": "metro M1", "color": "168388", "share": 1.0, "recommended": true}],
  "itineraries": [{"desc": "metro M1 → […] → bus 533", "lines": ["metro M1", "bus 533"], "median_s": 1860,
                   "best_s": 1800, "seen": 13, "of": 19, "window": "Weekday afternoon"}]}]}
```
`stage` is what it's doing: `reading` a day's timetable, then `searching` each destination on it, for each of the two
days. `progress` is how much of the whole is done, 0 to 1: reading counts by how much of the feed has been read (it's
nearly all of the work), searching by departure times. Neither is set before the job starts work. `results` are in the
order asked.
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
in between: `{"coordinates": [[lon, lat], …], "stops": [[lon, lat], …]}`. For a train, metro or light-rail ride it
also carries `"platform": [[lon, lat], …]`, the outline of the platform it's boarded from (closed: the last point is
the first), when the street map has one at the stop (within 15 m, or 30 m when it carries the platform's number): anywhere on it is where you wait. Follows the route shape once shapes have loaded (a few seconds after
startup), with each stop placed on the line; until then, straight lines between the trip's stops. A train follows
its own track to the platform it uses, where the complete bundle has the trip; otherwise its route's line.

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
Lines missing today aren't an issue: weekday-only buses are missing every weekend. The app shows this in its footer,
which opens to the rest of the reply.
`version` is the commit the server was built from, shortened to seven characters (`dev` when the build didn't record
it, a trailing `+` when built from uncommitted changes); the footer shows it too.
Asking for the status doesn't count as using the app (it doesn't start realtime polling).

Detailed health: feed ages and errors, upstream requests today, realtime match stats, configured lines missing today
(`missing_lines`; a line replaced by buses all day isn't missing), buses named like trackwork buses that are left out of
searches because they can't be tied to a line (`unknown_trackwork`: a new line code, or typed as an ordinary bus; they
call at a station of a loaded line, so the app needs updating), and
`data`: whether the street network and basemap are ready, their age and any error from the last attempt to fetch them.
`data.walk_updating` is there (and true) while the street network is being downloaded and rebuilt, or is about to be.

## `POST /api/street-map/update`
No body. Asks the server to download the street map and rebuild the street network now, instead of when it's next due
(every 35 days). `202 {}` at once: the work happens in the background, the network in use stays in use until the new
one is ready, and the status says how it's going (`data.walk_updating`, then a new `walk_age_s`, or `walk_error`).
Asking while one is under way changes nothing. A failed update is tried again after 6 hours, like any other.
`409` when the server doesn't download a street map (`data.auto_download` off, or no `walk_source`).
Geofabrik rebuilds its extract daily, so an edit to OpenStreetMap is usually there to fetch the next day.

Any authenticated API request counts as activity: realtime polling runs while the app was used in the last
`realtime.active_for` (default 10 min).
