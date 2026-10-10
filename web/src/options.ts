// Pure helpers for lists of options, kept apart from the views so they can be tested.

import type { Leg, Option, StopRef, Transfer } from './types.ts'

const ms = (iso: string) => Date.parse(iso)

/** The vehicles an option takes; stays the same as live times change. */
export function tripKey(o: Option): string {
  return o.legs
    .filter((l) => l.kind === 'ride')
    .map((l) => `${l.trip_id ?? ''}@${l.from?.id ?? ''}`)
    .join('|')
}

/** Finds a chosen option again in a refreshed list. */
export function reselect(opts: Option[], trips: string, leave: number): number {
  const i = opts.findIndex((o) => tripKey(o) === trips)
  if (i >= 0 || opts.length === 0) return Math.max(0, i)
  let best = 0
  for (const [j, o] of opts.entries()) {
    if (Math.abs(ms(o.leave_at) - leave) < Math.abs(ms(opts[best].leave_at) - leave)) best = j
  }
  return best
}

/**
 * The option that arrives soonest among those that can still be caught: none of its changes is already missed, and
 * it isn't shown as having left (that starts a minute after its leave time). Of two arriving together, the one that
 * leaves later.
 */
export function fastest(opts: Option[], now: number): Option | undefined {
  let best: Option | undefined
  for (const o of opts) {
    if (o.risk === 'missed' || ms(o.leave_at) <= now - 60_000) continue
    if (!best || ms(o.arrive) < ms(best.arrive) || (ms(o.arrive) === ms(best.arrive) && ms(o.leave_at) > ms(best.leave_at))) best = o
  }
  return best
}

/** Stops travelled over all of an option's rides. */
export function stopCount(o: Option): number {
  return o.legs.reduce((n, l) => n + (l.kind === 'ride' ? (l.stops ?? 0) : 0), 0)
}

/** "no changes, 12 stops" */
export function changesAndStops(o: Option): string {
  const changes = o.rides <= 1 ? 'no changes' : `${o.rides - 1} change${o.rides > 2 ? 's' : ''}`
  const n = stopCount(o)
  return n > 0 ? `${changes}, ${n} stop${n === 1 ? '' : 's'}` : changes
}

/** One step of an option as the description shows it, with the stretch of time it covers. */
export type Row =
  | { kind: 'leg'; leg: Leg; i: number; start: number; end: number }
  | { kind: 'change'; t: Transfer; from?: StopRef; to?: StopRef; modes: [string?, string?]; start: number; end: number }
  | { kind: 'arrive'; start: number; end: number }
  | { kind: 'start'; start: number; end: number } // where you set off from (during a trip)

/**
 * The steps of an option, each with the stretch of time it covers (for placing you on the rail). `withStart` adds the
 * place you set off from as the first step, so before you leave you're shown there.
 */
export function rows(o: Option, withStart = false): Row[] {
  const into = new Map<number, Transfer>(o.transfers.map((t) => [t.to_leg, t]))
  const out: Row[] = withStart ? [{ kind: 'start', start: ms(o.leave_at), end: ms(o.leave_at) }] : []
  for (const [i, leg] of o.legs.entries()) {
    const t = into.get(i)
    const prev = t ? o.legs[t.from_leg] : undefined
    if (t && prev) {
      out.push({ kind: 'change', t, from: prev.to, to: leg.from, modes: [prev.line?.mode, leg.line?.mode], start: ms(prev.arr), end: ms(leg.dep) })
    }
    if (leg.kind === 'walk' && leg.from && leg.to) continue // walking between two rides is part of the change
    out.push({ kind: 'leg', leg, i, start: ms(leg.dep), end: ms(leg.arr) })
  }
  out.push({ kind: 'arrive', start: ms(o.arrive), end: ms(o.arrive) })
  return out
}

/** Which step you're on and how far through it (0..1): the last step that has started. */
export function position(rs: { start: number; end: number }[], now: number): { row: number; frac: number } {
  let row = 0
  for (const [i, r] of rs.entries()) if (r.start <= now) row = i
  const r = rs[row]
  const frac = r.end > r.start ? Math.min(1, Math.max(0, (now - r.start) / (r.end - r.start))) : 0
  return { row, frac: now < r.start ? 0 : frac }
}

/** One piece of an option's tape: a ride, or the walking and waiting before, between or after rides. */
export type TapePiece = { kind: 'ride'; leg: Leg; share: number } | { kind: 'walk'; share: number }

/**
 * An option as the pieces of its tape, each with its share (0..1) of the time from leaving to arriving. Everything
 * that isn't a ride is one run of walking: to the first stop, between two rides (the walk and the wait, however
 * short: two rides never touch), and from the last stop.
 */
export function tape(o: Option): TapePiece[] {
  const total = Math.max(1, ms(o.arrive) - ms(o.leave_at))
  const out: TapePiece[] = []
  let t = ms(o.leave_at)
  for (const l of o.legs) {
    if (l.kind !== 'ride') continue
    if (ms(l.dep) > t || out.length > 0) out.push({ kind: 'walk', share: Math.max(0, ms(l.dep) - t) / total })
    out.push({ kind: 'ride', leg: l, share: Math.max(0, ms(l.arr) - ms(l.dep)) / total })
    t = ms(l.arr)
  }
  if (ms(o.arrive) > t || out.length === 0) out.push({ kind: 'walk', share: Math.max(0, ms(o.arrive) - t) / total })
  return out
}
