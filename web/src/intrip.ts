// In-trip tracking: where you are in the chosen option, what to re-plan from, and whether a
// different option is now better. Pure functions, so they can be tested without a browser.

import type { KeepRide, Kept, Leg, Option, PlaceRequest } from './types.ts'
import { stopKey } from './walks.ts'

export interface Position {
  lat: number
  lon: number
  accuracy: number // metres
}

export type Phase =
  | { kind: 'before'; ride: number; waiting?: boolean } // walking to the ride at legs[ride], or waiting for it at the stop
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

/** A location vaguer than this (m) says too little about where you are to plan or estimate from. */
export const USABLE_M = 150

/**
 * Before a ride: can you still reach the stop in time from where you are? Returns the seconds to
 * spare (negative: you'll miss it), or null without a usable position. `leftS` is the walking still to do along
 * the trip's own walk to the stop (progress.ts walkLeftS); without it, straight there with the usual detour.
 */
export function spareToBoard(ride: Leg, pos: Position | null, nowMs: number, walkSpeedMps: number, leftS?: number | null): number | null {
  if (!pos || !ride.from || pos.accuracy > USABLE_M) return null
  const walkS = leftS ?? (distanceM(pos, ride.from) * 1.3) / walkSpeedMps
  return Math.round((t(ride.dep) - nowMs) / 1000 - walkS)
}

/** How long before leaving your position counts as where the trip starts (before that you may be elsewhere). */
export const SETTING_OFF_S = 300

/**
 * Where to re-plan from: the vehicle you're on, your position, or the next stop. Before setting off, the trip's
 * own start: a trip planned for later may be started from somewhere else. `left` is the walking still to do to the
 * stop you're heading for, by the trip's own walk: sent with your position, so the re-check doesn't swap a walk
 * you've timed for the street map's idea of it.
 */
export function replanOrigin(
  o: Option, phase: Phase, pos: Position | null, original: PlaceRequest, nowMs: number, left?: { stop: string; secs: number } | null,
): PlaceRequest | null {
  switch (phase.kind) {
    case 'riding': {
      const l = o.legs[phase.ride]
      return l.trip_id && l.from ? { on_trip: { trip_id: l.trip_id, from_stop: l.from.id } } : null
    }
    case 'before': {
      const first = o.legs.findIndex((l) => l.kind === 'ride')
      const setOff = nowMs >= t(o.leave_at) - SETTING_OFF_S * 1000
      if (pos && pos.accuracy <= USABLE_M && (setOff || phase.ride !== first)) {
        const walks = left ? [{ stop: left.stop, walk_s: Math.min(3600, Math.max(0, Math.round(left.secs))) }] : undefined
        return { lat: pos.lat, lon: pos.lon, ...(walks ? { walks } : {}) }
      }
      if (phase.ride === first) {
        // Still at the start: plan as originally. Past the time to leave with no location to go by, you're taken to
        // be walking to the stop as planned (what's left of the walk is the time until the ride goes), not still at home.
        const l = o.legs[first]
        if (nowMs <= t(o.leave_at) || !l?.from) return original
        const stop = stopKey({ stop: l.from, mode: l.line?.mode })
        const walk_s = Math.min(3600, Math.max(0, Math.round((t(l.dep) - nowMs) / 1000)))
        return { ...original, walks: [...(original.walks ?? []).filter((w) => w.stop !== stop), { stop, walk_s }] }
      }
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

/** The rides still ahead, from leg index `from` on, for the server to check as they stand (PlanRequest.keep). */
export function keepFrom(o: Option, from: number): KeepRide[] {
  return o.legs.slice(from).flatMap((l) => (l.kind === 'ride' && l.trip_id && l.from && l.to ? [{ trip_id: l.trip_id, from: l.from.id, to: l.to.id }] : []))
}

/**
 * Why the trip can't be made as planned: a ride no longer runs ('gone'), you can't reach the next ride in time
 * ('board'), or there's no longer time for a change ('change'). `ride` is the one you won't make; `at`, for a change,
 * the ride you'd be coming off. `unsure` marks a warning that's only still showing (see stillMissed): there's a little to
 * spare again, so it's "may not" rather than "won't".
 */
export type Missed = { why: 'gone' } | { why: 'board'; ride: Leg; unsure?: boolean } | { why: 'change'; ride: Leg; at: Leg; unsure?: boolean }

export type Assessment =
  | { status: 'on-track'; current: Option; lateBy: number }
  | { status: 'better'; current: Option; suggestion: Option; lateBy: number }
  | { status: 'missed'; missed: Missed; current?: Option; suggestion: Option | null }

/** Minimum improvement before suggesting a switch (seconds). */
export const SWITCH_GAIN_S = 180

/** What stops the kept trip being made, if anything. */
function missedIn(kept: Kept | undefined): Missed | null {
  const o = kept?.option
  if (!o) return { why: 'gone' }
  const first = o.legs.find((l) => l.kind === 'ride')
  if ((kept.catch_s ?? 0) < 0 && first) return { why: 'board', ride: first }
  const gone = o.transfers.find((tr) => tr.risk === 'missed')
  return gone ? { why: 'change', ride: o.legs[gone.to_leg], at: o.legs[gone.from_leg] } : null
}

/**
 * A warning already showing stays until the thing it's about has `clearS` to spare again (the "tight" setting), so a
 * connection hovering around nothing to spare doesn't come and go with every live update. Returns the warning to keep
 * showing, or null once it's clear, or no longer the question (you're on that ride, or past that change).
 */
function stillMissed(kept: Kept | undefined, held: Missed | null | undefined, clearS: number): Missed | null {
  const o = kept?.option
  if (!o || !held || held.why === 'gone') return null
  if (held.why === 'board') {
    const first = o.legs.find((l) => l.kind === 'ride')
    return first && first.trip_id === held.ride.trip_id && kept.catch_s !== undefined && kept.catch_s < clearS ? { why: 'board', ride: first, unsure: true } : null
  }
  const tr = o.transfers.find((x) => o.legs[x.to_leg]?.trip_id === held.ride.trip_id && o.legs[x.from_leg]?.trip_id === held.at.trip_id)
  return tr && tr.slack_s < clearS ? { why: 'change', ride: o.legs[tr.to_leg], at: o.legs[tr.from_leg], unsure: true } : null
}

const RISK_RANK = { safe: 0, tight: 1, 'at-risk': 2, missed: 3 }

/**
 * Whether the trip you're on still works, and whether another is now better. `kept` is the server's check of the
 * trip itself (the rides in `committed`, see tripsFrom), which decides whether it works: a search only returns the
 * best trips, and yours can drop out of those while it's still good. `fresh` are the options a search from where you
 * are returns now, for something better or something else; `plannedArrive` is the original arrival time.
 *
 * `hold` is the warning already showing, if any, and the time to spare (s) at which it clears (see stillMissed).
 * A faster way is only offered if its changes are no riskier than those still ahead of you.
 */
export function assess(
  committed: string[], kept: Kept | undefined, fresh: Option[], plannedArrive: string, hold?: { missed: Missed | null; clearS: number },
): Assessment {
  // Another way: not the trip you're on, and not one with a change there's no time for.
  const others = fresh.filter((o) => tripsFrom(o, 0).join('|') !== committed.join('|') && !o.transfers.some((tr) => tr.risk === 'missed'))
  const earliest = (os: Option[]) => os.reduce<Option | null>((b, o) => (!b || t(o.arrive) < t(b.arrive) ? o : b), null)
  const missed = missedIn(kept) ?? stillMissed(kept, hold?.missed, hold?.clearS ?? 0)
  if (missed) return { status: 'missed', missed, current: kept?.option, suggestion: earliest(others) }
  const same = kept!.option!
  const lateBy = Math.round((t(same.arrive) - t(plannedArrive)) / 1000)
  const best = earliest(others.filter((o) => RISK_RANK[o.risk] <= RISK_RANK[same.risk]))
  if (best && t(same.arrive) - t(best.arrive) >= SWITCH_GAIN_S * 1000) {
    return { status: 'better', current: same, suggestion: best, lateBy }
  }
  return { status: 'on-track', current: same, lateBy }
}

/**
 * How sure an option's changes are, to go with offering it: "with no changes", "with a tight change",
 * "with 2 changes, the tightest at risk".
 */
export function changesNote(o: Option): string {
  const n = o.transfers.length
  if (n === 0) return 'with no changes'
  if (n === 1) return `with ${o.risk === 'at-risk' ? 'an' : 'a'} ${o.risk} change`
  return o.risk === 'safe' ? `with ${n} safe changes` : `with ${n} changes, the tightest ${o.risk === 'at-risk' ? 'at risk' : o.risk}`
}

/** Short instruction for the current phase. `destination` names where the trip ends. */
export function instruction(o: Option, phase: Phase, destination = 'your destination'): { now: string; detail?: Leg } {
  switch (phase.kind) {
    case 'before': {
      const l = o.legs[phase.ride]
      const where = l.from?.station || l.from?.name || 'the stop'
      return { now: phase.waiting ? `Wait for the ${l.line?.name ?? ''} at ${where}` : `Get to ${where} for the ${l.line?.name ?? ''}`, detail: l }
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
