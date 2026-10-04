// Are you on your vehicle? Your location moving with the vehicle's live position is strong evidence; the vehicle
// leaving the stop while you stay put means you've missed it. Pure logic; views/intrip.tsx feeds it.

export interface Fix {
  lat: number
  lon: number
  accuracy: number
  t: number // ms since the epoch
}

/** A live report of the vehicle's position. */
export interface Sighting {
  lat: number
  lon: number
  t: number
}

const R = 6371000
export function distM(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const p = Math.PI / 180
  const h = Math.sin(((b.lat - a.lat) * p) / 2) ** 2 + Math.cos(a.lat * p) * Math.cos(b.lat * p) * Math.sin(((b.lon - a.lon) * p) / 2) ** 2
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)))
}

/** Your fixes this close in time to a report can stand for where you were then. */
const NEAR_IN_TIME_MS = 20_000
/** Between two reports the vehicle must have moved this far: one parked at the stop proves nothing. */
const MOVED_M = 50
/** Within this of the vehicle (plus your GPS accuracy) you're with it. */
const WITH_M = 50
/** The vehicle this far from you, while you've stayed put, has gone without you. */
const GONE_M = 150
const STAYED_M = 30

/** Where you were at time t: between the fixes either side of it (or the nearest one, if close enough in time). */
export function whereAt(history: Fix[], t: number): Fix | null {
  let before: Fix | null = null
  let after: Fix | null = null
  for (const f of history) {
    if (f.t <= t && (!before || f.t > before.t)) before = f
    if (f.t >= t && (!after || f.t < after.t)) after = f
  }
  if (before && after && after.t - before.t <= 2 * NEAR_IN_TIME_MS) {
    const k = after.t === before.t ? 0 : (t - before.t) / (after.t - before.t)
    return {
      lat: before.lat + k * (after.lat - before.lat), lon: before.lon + k * (after.lon - before.lon),
      accuracy: Math.max(before.accuracy, after.accuracy), t,
    }
  }
  const near = [before, after].filter((f): f is Fix => !!f && Math.abs(f.t - t) <= NEAR_IN_TIME_MS)
  return near.sort((a, b) => Math.abs(a.t - t) - Math.abs(b.t - t))[0] ?? null
}

export type Boarding = 'aboard' | 'left-without-you' | null

/**
 * From your recent fixes and your vehicle's recent reports: 'aboard' if you were with it at two reports between which
 * it moved; 'left-without-you' if it moved off and away from you while you stayed where you were; otherwise null.
 */
export function boarding(history: Fix[], sightings: Sighting[]): Boarding {
  const seen = [...sightings].sort((a, b) => a.t - b.t)
  for (let i = seen.length - 1; i > 0; i--) {
    const s2 = seen[i]
    const s1 = seen.slice(0, i).reverse().find((s) => distM(s, s2) >= MOVED_M)
    if (!s1) continue
    const me1 = whereAt(history, s1.t)
    const me2 = whereAt(history, s2.t)
    if (!me1 || !me2) continue
    const with1 = distM(me1, s1) <= WITH_M + me1.accuracy
    const with2 = distM(me2, s2) <= WITH_M + me2.accuracy
    if (with1 && with2) return 'aboard'
    if (distM(me2, s2) > GONE_M + me2.accuracy && distM(me1, me2) < STAYED_M && distM(me2, s2) > distM(me1, s1)) return 'left-without-you'
    return null
  }
  return null
}
