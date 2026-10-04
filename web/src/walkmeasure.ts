// Measuring a walk: time it and trace it with GPS (saving it is walks.ts).
// Pure logic; the screen that drives it is views/walktimer.tsx.

export type LonLat = [number, number]

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
/** Shorter than this isn't a real measurement (a mistaken tap). */
export const MIN_WALK_S = 15
/** A trace is kept to about this many points: enough to draw the route, small enough to sync in a link. */
export const MAX_TRACE_POINTS = 80
/** The average uses the most recent walks, so it follows changes (a new gate, roadworks). */
export const MAX_SAMPLES = 5

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

/** Metres walked along the readings so far. */
export function walkedM(rec: Recording): number {
  let d = 0
  for (let i = 1; i < rec.fixes.length; i++) d += distanceM(rec.fixes[i - 1], rec.fixes[i])
  return d
}

// Douglas–Peucker on a local flat approximation (metres), which is plenty over a few hundred metres.
function simplifyOnce(pts: LonLat[], tolM: number): LonLat[] {
  if (pts.length < 3) return pts
  const lat0 = pts[0][1]
  const kx = 111320 * Math.cos((lat0 * Math.PI) / 180)
  const ky = 110540
  const xy = pts.map(([lon, lat]) => [lon * kx, lat * ky] as const)
  const keep = new Array<boolean>(pts.length).fill(false)
  keep[0] = keep[pts.length - 1] = true
  const stack: [number, number][] = [[0, pts.length - 1]]
  while (stack.length) {
    const [a, b] = stack.pop()!
    let worst = -1
    let wd = tolM
    const [ax, ay] = xy[a]
    const [bx, by] = xy[b]
    const dx = bx - ax
    const dy = by - ay
    const len2 = dx * dx + dy * dy
    for (let i = a + 1; i < b; i++) {
      const [px, py] = xy[i]
      const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / len2))
      const d = Math.hypot(px - (ax + t * dx), py - (ay + t * dy))
      if (d > wd) {
        wd = d
        worst = i
      }
    }
    if (worst >= 0) {
      keep[worst] = true
      stack.push([a, worst], [worst, b])
    }
  }
  return pts.filter((_, i) => keep[i])
}

/** Reduces a trace to at most `max` points, loosening the tolerance until it fits. Endpoints are kept. */
export function simplifyTrace(pts: LonLat[], max = MAX_TRACE_POINTS): LonLat[] {
  let tol = 2
  let out = simplifyOnce(pts, tol)
  while (out.length > max && tol < 200) {
    tol *= 1.5
    out = simplifyOnce(pts, tol)
  }
  // ~1 m precision is plenty and keeps the stored JSON short.
  return out.map(([lon, lat]) => [Math.round(lon * 1e5) / 1e5, Math.round(lat * 1e5) / 1e5] as LonLat)
}

export interface Walk {
  secs: number
  distanceM: number
  trace: LonLat[] // empty without GPS
  startedNearM?: number // how far the first reading was from where the walk should start
  endedNearM?: number // how far the last reading was from the stop
}

/** Turns a finished recording into a measurement. `from` and `to` are where the walk should start and end. */
export function finish(rec: Recording, endedAt: number, from?: { lat: number; lon: number }, to?: { lat: number; lon: number }): Walk {
  const secs = Math.round((endedAt - rec.startedAt) / 1000)
  const trace = simplifyTrace(rec.fixes.map((f) => [f.lon, f.lat] as LonLat))
  const first = rec.fixes[0]
  const last = rec.fixes[rec.fixes.length - 1]
  return {
    secs,
    distanceM: Math.round(walkedM(rec)),
    trace: trace.length >= 2 ? trace : [],
    startedNearM: first && from ? Math.round(distanceM(first, from)) : undefined,
    endedNearM: last && to ? Math.round(distanceM(last, to)) : undefined,
  }
}

/** The walking time a set of measurements stands for: the mean of the most recent ones. */
export function meanSecs(times: number[]): number {
  return Math.round(times.reduce((a, b) => a + b, 0) / times.length)
}

export function mmss(secs: number): string {
  const m = Math.floor(secs / 60)
  const s = Math.abs(Math.round(secs)) % 60
  return `${m}:${String(s).padStart(2, '0')}`
}
