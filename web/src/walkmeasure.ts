// Measuring your walking pace: a short walk, read with GPS and timed only while you're moving.
// Pure logic; the screen that drives it is views/pacetest.tsx.

export interface Fix {
  lat: number
  lon: number
  accuracy: number // metres
  t: number // ms since the epoch
}

export interface Recording {
  startedAt: number
  fixes: Fix[]
}

/** GPS readings worse than this are noise (indoors, between buildings), not a place on the path. */
export const MAX_ACCURACY_M = 40
/** Faster than this between two readings isn't walking (it's a GPS jump). */
export const MAX_SPEED_MPS = 4

const R = 6371000

export function distanceM(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const p = Math.PI / 180
  const dLat = (b.lat - a.lat) * p
  const dLon = (b.lon - a.lon) * p
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * p) * Math.cos(b.lat * p) * Math.sin(dLon / 2) ** 2
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)))
}

/** Adds a reading if it's usable. Returns whether it was kept. */
export function addFix(rec: Recording, fix: Fix): boolean {
  if (!(fix.accuracy <= MAX_ACCURACY_M) || fix.t < rec.startedAt - 1000) return false
  const last = rec.fixes[rec.fixes.length - 1]
  if (last) {
    const dt = (fix.t - last.t) / 1000
    if (dt <= 0) return false
    if (distanceM(last, fix) / dt > MAX_SPEED_MPS && distanceM(last, fix) > 30) return false
  }
  rec.fixes.push(fix)
  return true
}

export function mmss(secs: number): string {
  const m = Math.floor(secs / 60)
  const s = Math.abs(Math.round(secs)) % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/** Slower than this between two readings is standing still (lights, a crossing), not walking. */
export const MOVING_MPS = 0.5
/** A pace test needs at least this much walking to mean anything. */
export const PACE_MIN_S = 60
export const PACE_MIN_M = 100

export interface Pace {
  mps: number // metres per second, while moving
  movingS: number
  metres: number
}

/**
 * Walking pace from a recording: distance over time, counting only the stretches between readings where you were
 * moving, so stops at crossings don't drag it down. Null if there isn't enough walking, or it isn't walking pace.
 */
export function pace(rec: Recording): Pace | null {
  let metres = 0
  let secs = 0
  for (let i = 1; i < rec.fixes.length; i++) {
    const a = rec.fixes[i - 1]
    const b = rec.fixes[i]
    const dt = (b.t - a.t) / 1000
    const d = distanceM(a, b)
    if (dt > 0 && d / dt >= MOVING_MPS) {
      metres += d
      secs += dt
    }
  }
  if (secs < PACE_MIN_S || metres < PACE_MIN_M) return null
  const mps = metres / secs
  return mps >= 0.6 && mps <= 2.5 ? { mps, movingS: Math.round(secs), metres: Math.round(metres) } : null
}
