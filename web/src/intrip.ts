// In-trip tracking: where you are in the chosen option, what to re-plan from, and whether a
// different option is now better. Pure functions, so they can be tested without a browser.

import type { Leg, Option, PlaceRequest } from './types.ts'

export interface Position {
  lat: number
  lon: number
  accuracy: number // metres
}

export type Phase =
  | { kind: 'before'; ride: number } // walking to / waiting for the ride at legs[ride]
  | { kind: 'riding'; ride: number } // on the vehicle of legs[ride]
  | { kind: 'final-walk' } // off the last vehicle, walking to the destination
  | { kind: 'arrived' }

const t = (iso: string) => Date.parse(iso)

/** Works out the phase from the clock (using live times from the latest plan). */
export function phaseAt(o: Option, nowMs: number): Phase {
  for (const [i, l] of o.legs.entries()) {
    if (l.kind !== 'ride') continue
    if (nowMs < t(l.dep)) return { kind: 'before', ride: i }
    if (nowMs < t(l.arr)) return { kind: 'riding', ride: i }
  }
  return nowMs < t(o.arrive) ? { kind: 'final-walk' } : { kind: 'arrived' }
}

export function distanceM(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const r = Math.PI / 180
  const dLat = (b.lat - a.lat) * r
  const dLon = (b.lon - a.lon) * r
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * r) * Math.cos(b.lat * r) * Math.sin(dLon / 2) ** 2
  return 2 * 6371000 * Math.asin(Math.min(1, Math.sqrt(h)))
}

/**
 * Before a ride: can you still reach the stop in time from where you are? Returns the seconds to
 * spare (negative: you'll miss it), or null without a usable position.
 */
export function spareToBoard(ride: Leg, pos: Position | null, nowMs: number, walkSpeedMps: number): number | null {
  if (!pos || !ride.from || pos.accuracy > 150) return null
  const walkS = (distanceM(pos, ride.from) * 1.3) / walkSpeedMps
  return Math.round((t(ride.dep) - nowMs) / 1000 - walkS)
}

/** How long before leaving your position counts as where the trip starts (before that you may be elsewhere). */
export const SETTING_OFF_S = 300

/**
 * Where to re-plan from: the vehicle you're on, your position, or the next stop. Before setting off, the trip's
 * own start: a trip planned for later may be started from somewhere else.
 */
export function replanOrigin(o: Option, phase: Phase, pos: Position | null, original: PlaceRequest, nowMs: number): PlaceRequest | null {
  switch (phase.kind) {
    case 'riding': {
      const l = o.legs[phase.ride]
      return l.trip_id && l.from ? { on_trip: { trip_id: l.trip_id, from_stop: l.from.id } } : null
    }
    case 'before': {
      const first = o.legs.findIndex((l) => l.kind === 'ride')
      const setOff = nowMs >= t(o.leave_at) - SETTING_OFF_S * 1000
      if (pos && pos.accuracy <= 150 && (setOff || phase.ride !== first)) return { lat: pos.lat, lon: pos.lon }
      if (phase.ride === first) return original // still at the start: plan as originally
      const s = o.legs[phase.ride].from
      return s ? { lat: s.lat, lon: s.lon, access: [{ stop: s.id, walk_s: 0 }] } : null
    }
    default:
      return null // nothing left to plan
  }
}

/**
 * The time to re-plan from: now, or (for a trip planned for later) a little before it leaves, so the planned
 * departure is inside the search window rather than reported as missed.
 */
export function replanTime(o: Option, phase: Phase, nowMs: number): string | undefined {
  if (phase.kind !== 'before') return undefined
  const from = t(o.leave_at) - SETTING_OFF_S * 1000
  return from > nowMs ? new Date(from).toISOString() : undefined
}

/** The trips still ahead of you in an option, from leg index `from` on. */
export function tripsFrom(o: Option, from: number): string[] {
  return o.legs.slice(from).filter((l) => l.kind === 'ride').map((l) => l.trip_id ?? '')
}

export type Assessment =
  | { status: 'on-track'; current: Option; lateBy: number }
  | { status: 'better'; current: Option; suggestion: Option; lateBy: number }
  | { status: 'missed'; suggestion: Option | null }

/** Minimum improvement before suggesting a switch (seconds). */
export const SWITCH_GAIN_S = 180

/**
 * Compares the fresh options with the trips you're committed to. `committed` are the trip IDs still
 * ahead (see tripsFrom); `plannedArrive` is the original arrival time.
 */
export function assess(committed: string[], fresh: Option[], plannedArrive: string): Assessment {
  const same = fresh.find((o) => {
    const ids = tripsFrom(o, 0)
    return ids.length === committed.length && ids.every((id, i) => id === committed[i])
  })
  const best = fresh.reduce<Option | null>((b, o) => (!b || t(o.arrive) < t(b.arrive) ? o : b), null)
  if (!same) return { status: 'missed', suggestion: best }
  const lateBy = Math.round((t(same.arrive) - t(plannedArrive)) / 1000)
  if (best && best !== same && t(same.arrive) - t(best.arrive) >= SWITCH_GAIN_S * 1000) {
    return { status: 'better', current: same, suggestion: best, lateBy }
  }
  return { status: 'on-track', current: same, lateBy }
}

/** Short instruction for the current phase. `destination` names where the trip ends. */
export function instruction(o: Option, phase: Phase, destination = 'your destination'): { now: string; detail?: Leg } {
  switch (phase.kind) {
    case 'before': {
      const l = o.legs[phase.ride]
      return { now: `Get to ${l.from?.station || l.from?.name || 'the stop'} for the ${l.line?.name ?? ''}`, detail: l }
    }
    case 'riding': {
      const l = o.legs[phase.ride]
      return { now: `On the ${l.line?.name ?? ''}: get off at ${l.to?.station || l.to?.name || 'your stop'}`, detail: l }
    }
    case 'final-walk':
      return { now: `Walk to ${destination}` }
    case 'arrived':
      return { now: "You've arrived" }
  }
}
