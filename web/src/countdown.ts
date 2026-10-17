// The stops left on the ride you're on: how many are behind you, counted from where you are on the ride's line or,
// with nothing to place you by, from the times it calls at them. Pure functions, so they can be tested without a browser.

import { onLine, type LonLat } from './progress.ts'
import type { Call } from './types.ts'

/** How far through a ride you are, in stops. */
export interface RideProgress {
  leg: number // the ride's place in the option's legs
  total: number // its stops after the one you got on at; the last is yours
  passed: number // how many of them are behind you (never the last: you're still on board)
  along: number // 0..1 of the way from the last stop behind you to the next one
  frac: number // 0..1 of the way along the ride's line
}

/**
 * Where each call sits along a ride's line, 0..1 (the last, where you get off, is its end). `stops` are the calls
 * before the last, placed on the line (see /api/shape); null if they aren't the ones the ride calls at.
 */
export function callPlaces(line: LonLat[], stops: LonLat[], calls: Call[]): number[] | null {
  if (calls.length === 0 || stops.length !== calls.length - 1 || line.length < 2) return null
  let last = 0
  const out = stops.map(([lon, lat]) => (last = Math.max(last, onLine(line, { lat, lon }).frac))) // in order along the line
  return [...out, 1]
}

/** A stop is behind you once you're this far (of the whole line) past it: a location wobbling at a stop hasn't left it. */
const PAST = 0.002

/**
 * The stops behind you on a ride and how far you are towards the next. `frac` is where you are along its line (0..1),
 * from your location or your vehicle's, and `places` where each call sits on it; without either, the calls' times
 * decide (`depMs` is when the ride left the stop you got on at).
 */
export function stopsPassed(calls: Call[], places: number[] | null, frac: number | null, depMs: number, nowMs: number): { passed: number; along: number } {
  const n = calls.length
  const span = (from: number, to: number, at: number) => (to > from ? Math.min(1, Math.max(0, (at - from) / (to - from))) : 0)
  if (places && frac !== null) {
    let passed = 0
    while (passed < n - 1 && frac > places[passed] + PAST) passed++
    return { passed, along: span(passed > 0 ? places[passed - 1] : 0, places[passed], frac) }
  }
  const times = calls.map((c) => (c.arr ? Date.parse(c.arr) : NaN))
  let passed = 0
  let from = depMs
  for (let k = 0; k < n - 1; k++) {
    if (Number.isNaN(times[k])) continue // a stop it skips: it's behind you once the next one is
    if (times[k] > nowMs) break
    passed = k + 1
    from = times[k]
  }
  const next = times.slice(passed).find((t) => !Number.isNaN(t))
  return { passed, along: next === undefined ? 0 : span(from, next, nowMs) }
}

/** "3 stops to go", "1 stop to go". */
export const stopsToGo = (n: number) => `${n} stop${n === 1 ? '' : 's'} to go`
