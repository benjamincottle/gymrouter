// Pure helpers for lists of options, kept apart from the views so they can be tested.

import type { Option } from './types.ts'

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
