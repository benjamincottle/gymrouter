// API types (see docs/api.md).

export interface Gym {
  id: string
  name: string
  address?: string
  lat: number
  lon: number
  lines: string[]
}

export interface Defaults {
  walk_speed_mps: number
  min_change_s: number
  max_walk_m: number
  risk: { safe_s: number; tight_s: number }
}

export interface GymsResponse {
  gyms: Gym[]
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
  options: Option[]
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
  gym?: string
  lat?: number
  lon?: number
  access?: { stop: string; walk_s: number }[]
  on_trip?: { trip_id: string; from_stop: string }
}

export interface PlanRequest {
  from: PlaceRequest
  to: PlaceRequest
  time?: string
  arrive_by?: boolean
  window_min?: number
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
