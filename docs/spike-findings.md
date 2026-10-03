# Spike findings (milestone 1), 2026-10-03

Checks the spec's §13 risks against the real TfNSW data. Home-specific results (line sets near home)
are deliberately not recorded here; they go in the private deploy config.

## API access and limits
- Auth header: `Authorization: apikey <token>`. Requests without it get a 401.
- Bronze (free) plan: **60,000 requests/day, 5 requests/second**. Going over either returns a 403
  (`X-Error-Detail: Account Over Rate Limit` / `... Over Quota Limit`).
- Budget: continuous polling of 5 trip-update feeds every 30 s plus 5 vehicle-position feeds every 15 s is
  ~43k requests/day, which fits but leaves little headroom. With demand-driven polling (spec §11) real usage is a small fraction of that.
  The poller should also fetch only the feeds for modes in the active gym's line set, and back off on a 403.

## GTFS static
- Complete bundle `GET /v1/publictransport/timetables/complete/gtfs`: **297 MB zip, 1.48 GB unzipped**
  (`shapes.txt` 1.0 GB, `stop_times.txt` 424 MB). Rebuilt daily (Last-Modified ~04:45 Sydney time). Supports ETag.
- Files: agency, stops, routes, trips, stop_times, calendar, calendar_dates, shapes, notes, levels,
  pathways. **No `transfers.txt`**, so walking transfers must be computed (as planned) or taken from pathways.
- TfNSW route types matter: 2 Sydney Trains/intercity (`T1`, `T9`, `CCN`…), 401 metro, 900 light rail,
  4 ferry, 700 bus, **712 school bus (exclude)**, **714 replacement bus (temporary trackwork buses)**.
  Route short names are not unique across types (e.g. `286` exists as a normal bus and a school bus), so
  a line is identified by **mode + short name** (e.g. `bus 288`, `train T9`).
- Loading one service day of the whole network (all modes, ~48k trips, ~5.9k patterns) with a streaming
  parse: **3.8 s, ~117 MB live heap**. Filtering to the gyms' line sets will cut that a lot.
  Shapes were not loaded; they need a separate filtered pass (only shapes of trips on the line sets).

## GTFS-realtime
| Mode | Trip updates | Vehicle positions | Sample size (TU) |
|---|---|---|---|
| Sydney Trains (incl. intercity) | `/v2/gtfs/realtime/sydneytrains` | `/v2/gtfs/vehiclepos/sydneytrains` | 200 KB |
| Metro | `/v2/gtfs/realtime/metro` | `/v2/gtfs/vehiclepos/metro` | 26 KB |
| Buses | `/v1/gtfs/realtime/buses` | `/v1/gtfs/vehiclepos/buses` | **4.3 MB** |
| Parramatta Light Rail (L4) | `/v1/gtfs/realtime/lightrail/parramatta` | `/v1/gtfs/vehiclepos/lightrail/parramatta` | 16 KB |
| Sydney Ferries | `/v1/gtfs/realtime/ferries/sydneyferries` | `/v1/gtfs/vehiclepos/ferries/sydneyferries` | 38 KB |

- All feeds decode with the standard `gtfs-realtime.proto`. TfNSW extensions (e.g. field 1007) show up as
  unknown fields and can be ignored.
- The bus trip-update feed is large (4.3 MB per fetch; ~12 GB/day if polled every 30 s nonstop), which is
  another reason for demand-driven polling.

### Trip ID matching (static ↔ realtime), Saturday midday sample
| Feed | Matches complete bundle |
|---|---|
| Buses TU / VP | 3060/3061, 1443/1444 |
| Metro TU / VP | 30/30, 12/12 |
| Light rail TU / VP | 24/24, 6/6 |
| Trains TU / VP | **205/341**, **128/195** |

- Trains match better against the per-mode bundle `GET /v1/gtfs/schedule/sydneytrains` (9.7 MB):
  **255/341**. It's a superset of the complete bundle for train trips, so **load trains from the per-mode bundle**.
- The remaining 86 unmatched train updates: 45 are on `RTTA_REV`/`RTTA_DEF` routes (apparently runs added or
  changed on the day; exact meaning not confirmed), and the rest carry a timetable version in their trip ID
  (`…1309…`) that isn't in either bundle.
  Milestone 2 should match leftovers by route + run number + start time, or show them as realtime-only vehicles
  on the map without using them for routing.

## Map tiles
- Protomaps daily planet build (`build.protomaps.com/YYYYMMDD.pmtiles`), Greater Sydney extract
  (bbox `150.55,-34.15,151.35,-33.45`, all zooms): **63 MB**, made in ~4 s with `pmtiles extract`.

## suggest-lines
- `gymrouter suggest-lines --from <gym|lat,lon> --to <gym|lat,lon> [--date --start --end --step]`
  runs RAPTOR over the whole network for each departure time. It then re-runs with lines banned (up to
  two at a time) to surface alternative routes within `--slack` of the best, groups the results by lines and
  interchanges, and counts how often each line appears.
- Example (gym → gym, Lane Cove → Chatswood, Tue 17:00): the best options combine `bus 288`/`bus 533` →
  `metro M1` (North Ryde → Chatswood) → `bus 283`/`281`, or `bus 258` direct to Chatswood.
- The generic walking defaults (1.3 m/s, 1.3× straight-line detour) are crude: they over- or under-estimate
  real walks, which confirms that personal walk and transfer times are needed (spec §5).

## Changes for the next milestones
- Load trains from the per-mode Sydney Trains bundle; everything else from the complete bundle.
- Line identity = mode + short name; exclude school buses; flag replacement buses in the UI.
- Poller: only feeds for modes in use, honour 403 rate/quota errors with backoff, use conditional GET where supported.
