// API types (see docs/api.md).

/** A gym the app knows about (public data shipped with the server). */
export interface KnownGym {
  id: string
  name: string
  brand?: string // whose logo to show: /brands/<brand>.png
  address?: string
  lat: number
  lon: number
  lines: string[] // the lines that serve the gym end
  access?: { stop: string; name: string; walk_s: number }[] // measured walks from the door
}

export interface Defaults {
  walk_speed_mps: number
  min_change_s: number
  max_walk_m: number
  risk: { safe_s: number; tight_s: number }
}

export interface DefaultsResponse {
  gyms: KnownGym[]
  defaults: Defaults
}

export interface StopRef {
  id: string
  name: string
  station?: string
  station_id?: string
  lat: number
  lon: number
}

export interface Line {
  mode: string
  name: string
  color?: string
  text_color?: string
}

export interface Leg {
  kind: 'walk' | 'ride'
  from?: StopRef
  to?: StopRef
  dep: string
  arr: string
  line?: Line
  trip_id?: string
  headsign?: string
  status?: 'scheduled' | 'predicted' | 'added'
  delay_s?: number
  sched_dep?: string
  stops?: number // ride legs: stops travelled, counting the one you get off at
  path?: [number, number][] // a walk along the streets, [lon, lat]
}

export type Risk = 'safe' | 'tight' | 'at-risk' | 'missed'

export interface Transfer {
  from_leg: number
  to_leg: number
  walk_s: number
  slack_s: number
  risk: Risk
  fallback_dep?: string
}

export interface Option {
  leave_at: string
  arrive: string
  duration_s: number
  rides: number
  risk: Risk
  alternative?: boolean
  lines: string[]
  legs: Leg[]
  transfers: Transfer[]
}

export interface PlanResponse {
  service_date: string
  realtime: boolean
  realtime_at?: string
  walking: 'streets' | 'estimate' // how walks to and from stops were timed
  options: Option[]
  trackwork?: Trackwork[] // lines that buses stand in for during the search
  /** Set when an end had no stop within the longest walk, so the nearest ones were used: the walk to the nearest (m). */
  stretched_walk?: { from_m?: number; to_m?: number }
  kept?: Kept // the answer to a request's `keep`
}

/** One ride of the trip being followed: a vehicle from one of its stops to a later one. */
export interface KeepRide {
  trip_id: string
  from: string
  to: string
}

/**
 * The trip being followed, checked as it stands now rather than searched for. `option` is that trip with its live
 * times and each change rated ("missed" when there's no longer time for it); it's absent when the trip can't be made at
 * all (a ride cancelled, or no longer calling there). `catch_s`, before the first ride, is the time to spare on
 * reaching it (negative: too late); absent on board.
 */
export interface Kept {
  option?: Option
  catch_s?: number
}

/** A line with trackwork: the buses replacing its trains, by the names on their signs ("23T4"). */
export interface Trackwork {
  line: Line
  buses: string[]
}

export interface NearStop extends StopRef {
  walk_s: number
  lines: string[]
}

export interface GeocodeResult {
  name: string
  type: string
  lat: number
  lon: number
}

export interface PlaceRequest {
  lat?: number
  lon?: number
  access?: { stop: string; walk_s: number }[]
  walks?: { stop: string; walk_s: number }[] // timed walks (stop or station IDs); beat everything else
  on_trip?: { trip_id: string; from_stop: string }
}

export interface PlanRequest {
  from: PlaceRequest
  to: PlaceRequest
  lines: string[] // the lines to route on (a gym's set)
  time?: string
  arrive_by?: boolean
  window_min?: number
  keep?: KeepRide[] // during a trip: the rides still ahead, to be checked as they stand (PlanResponse.kept)
  prefs: {
    walk_speed_mps?: number
    min_change_s?: number
    max_walk_m?: number
    leave_buffer_s?: number
    risk?: { safe_s: number; tight_s: number }
    transfers?: { from: string; to: string; secs: number }[]
  }
}

export interface Vehicle {
  id: string
  label?: string
  line: string
  color?: string
  trip_id?: string
  lat: number
  lon: number
  bearing?: number
  status?: string
  ts: string
}

export interface SuggestedLine {
  line: string
  color?: string
  share: number // fraction of departure times at which the line shows up in the best options
  recommended: boolean
}

export interface SuggestedItinerary {
  desc: string
  lines: string[]
  median_s: number
  best_s: number
  seen: number
  of: number
  window: string
}

export interface SuggestResult {
  windows: { label: string; date: string; departures: number; typical_s?: number }[]
  lines: SuggestedLine[]
  itineraries: SuggestedItinerary[]
}

/** One result per destination, in the order asked. */
export interface SuggestResponse {
  results: SuggestResult[]
}

/** GET /api/status: the verdict and what needs looking at (the footer's line), then the detail it opens to. */
export interface ServerStatus {
  state: 'ok' | 'warning' | 'error'
  issues?: string[]
  version: string // the commit the server (and this app, built into it) came from
  service_date?: string // the day its timetable is loaded for
  static_age_s?: number // since the timetable was downloaded
  static_error?: string
  polling_active?: boolean // live data is being fetched (only while the app is in use)
  upstream_requests_today?: number
  feeds?: { name: string; trip_updates_age_s?: number; vehicles_age_s?: number; error?: string }[]
  missing_lines?: string[] // loaded lines with no services today
  unknown_trackwork?: string[]
  realtime?: { updates: number; matched: number; matched_by_run: number; added: number; cancelled: number; empty: number; unmatched: number }
  data?: { walk_ready: boolean; walk_age_s?: number; walk_error?: string; map_ready: boolean; map_age_s?: number; map_error?: string }
}
