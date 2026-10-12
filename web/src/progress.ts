// Where you are on a trip's description, from your location rather than the clock: each step has a line on the ground
// (the walk's path, the ride's route, or straight between stops), and you're placed on the one you're nearest.

import type { Row } from './options.ts'
import type { Phase } from './intrip.ts'
import type { Leg, Option, StopRef } from './types.ts'

export type LonLat = [number, number]

/** The key for a ride's route shape (see /api/shape). */
export const shapeKey = (l: Leg) => `${l.trip_id}|${l.from?.id}|${l.to?.id}`

const pt = (s: { lat: number; lon: number }): LonLat => [s.lon, s.lat]

/**
 * Each step's line on the ground, in order with the rows. `shapes` holds the routes of the rides fetched so far.
 */
export function rowLines(rs: Row[], o: Option, start: LonLat, end: LonLat, shapes: Record<string, LonLat[]>): LonLat[][] {
  return rs.map((r): LonLat[] => {
    switch (r.kind) {
      case 'start':
        return [start]
      case 'arrive':
        return [end]
      case 'change': {
        const i = o.legs.findIndex((l, j) => j > r.t.from_leg && j < r.t.to_leg && l.kind === 'walk')
        const path = i < 0 ? undefined : o.legs[i].path
        if (path && path.length > 1) return path
        return [r.from, r.to].filter((s): s is StopRef => s !== undefined).map(pt)
      }
      case 'leg': {
        const l = r.leg
        const a = l.from ? pt(l.from) : start
        const b = l.to ? pt(l.to) : end
        if (l.kind === 'walk') {
          const path = l.path
          return path && path.length > 1 ? path : [a, b]
        }
        return shapes[shapeKey(l)] ?? [a, b]
      }
    }
  })
}

/**
 * The step you walk to reach the ride at legs[ride] (the walk to the first stop, or a change) and how long the trip
 * allows for it, in seconds. `bufferS` is the leave buffer, which the first walk's
 * times include.
 */
export function walkTo(rs: Row[], o: Option, ride: number, bufferS = 0): { row: number; secs: number } | null {
  for (const [row, r] of rs.entries()) {
    if (r.kind === 'change' && r.t.to_leg === ride) return { row, secs: r.t.walk_s }
    if (r.kind === 'leg' && r.leg.kind === 'walk' && o.legs.findIndex((l, j) => j > r.i && l.kind === 'ride') === ride) {
      return { row, secs: Math.max(0, Math.round((r.end - r.start) / 1000) - (r.i === 0 ? bufferS : 0)) }
    }
  }
  return null
}

/** Off a walk's line by more than your location's accuracy (or this), you still have to get back to it. */
const ON_WALK_M = 50

/**
 * Seconds of walking left to the end of a walk from where you are: the part of its line still ahead at the pace the
 * walk takes as a whole (`secs`), plus getting back to the line, straight and at `walkSpeedMps`, if you're off it.
 */
export function walkLeftS(line: LonLat[], secs: number, pos: { lat: number; lon: number; accuracy: number }, walkSpeedMps: number): number {
  const { d, frac } = onLine(line, pos)
  const off = Math.max(0, d - Math.min(pos.accuracy, ON_WALK_M))
  return Math.round(secs * (1 - frac) + (off * 1.3) / walkSpeedMps)
}

/** Distance (m) from p to a line, and how far along the line (0..1) the nearest point is. */
export function onLine(line: LonLat[], p: { lat: number; lon: number }): { d: number; frac: number } {
  const k = Math.cos((p.lat * Math.PI) / 180) * 111_320
  const xy = (q: LonLat) => [(q[0] - p.lon) * k, (q[1] - p.lat) * 110_540] as const // metres, p at the origin
  if (line.length === 0) return { d: Infinity, frac: 0 }
  if (line.length === 1) return { d: Math.hypot(...xy(line[0])), frac: 0 }
  let total = 0
  let best = { d: Infinity, at: 0 }
  for (let i = 0; i < line.length - 1; i++) {
    const [ax, ay] = xy(line[i])
    const [bx, by] = xy(line[i + 1])
    const dx = bx - ax
    const dy = by - ay
    const len = Math.hypot(dx, dy)
    const t = len > 0 ? Math.max(0, Math.min(1, -(ax * dx + ay * dy) / (len * len))) : 0
    const d = Math.hypot(ax + t * dx, ay + t * dy)
    if (d < best.d) best = { d, at: total + t * len }
    total += len
  }
  return { d: best.d, frac: total > 0 ? best.at / total : 0 }
}

/** A line cut in two at `frac` (0..1) of its length: the part behind that point and the part ahead, each ending or starting there. */
export function splitAt(line: LonLat[], frac: number): [LonLat[], LonLat[]] {
  if (line.length < 2) return [line, line]
  const k = Math.cos((line[0][1] * Math.PI) / 180) * 111_320
  const len = (a: LonLat, b: LonLat) => Math.hypot((b[0] - a[0]) * k, (b[1] - a[1]) * 110_540)
  let total = 0
  for (let i = 1; i < line.length; i++) total += len(line[i - 1], line[i])
  let left = Math.min(1, Math.max(0, frac)) * total
  for (let i = 1; i < line.length; i++) {
    const d = len(line[i - 1], line[i])
    if (left <= d) {
      const t = d > 0 ? left / d : 0
      const cut: LonLat = [line[i - 1][0] + (line[i][0] - line[i - 1][0]) * t, line[i - 1][1] + (line[i][1] - line[i - 1][1]) * t]
      return [[...line.slice(0, i), cut], [cut, ...line.slice(i)]]
    }
    left -= d
  }
  return [line, [line[line.length - 1]]]
}

/** Steps away from where the clock expects you count as this much further (m), so a nearby part of the route can't steal you. */
const STEP_PENALTY_M = 40
/** Within this of a step's start, you're still at the end of the one before (waiting at the stop, not yet on the bus). */
const BOUNDARY = 0.02

/**
 * Which step you're on, and how far along it, from your location; null if the location is too vague or you're well
 * away from the route (the clock is used then). `expected` is the step the clock puts you on.
 */
export function locate(lines: LonLat[][], p: { lat: number; lon: number; accuracy: number }, expected: number): { row: number; frac: number } | null {
  if (p.accuracy > 100 || lines.length === 0) return null
  let best: { row: number; frac: number; d: number; score: number } | null = null
  for (const [row, line] of lines.entries()) {
    const { d, frac } = onLine(line, p)
    const score = d + STEP_PENALTY_M * Math.abs(row - expected)
    if (!best || score < best.score) best = { row, frac, d, score }
  }
  if (!best || best.d > Math.max(150, p.accuracy * 2)) return null
  // At the very end of the last walk, you've arrived.
  const next = lines[best.row + 1]
  if (best.frac >= 1 - BOUNDARY && next?.length === 1 && onLine(next, p).d <= best.d + 10) return { row: best.row + 1, frac: 0 }
  // On the line where one step meets the next, you're still at the earlier one until you've moved along.
  if (best.frac <= BOUNDARY && best.row > 0) {
    const prev = lines[best.row - 1]
    const end = prev[prev.length - 1]
    if (onLine([end], p).d <= best.d + 10) return { row: best.row - 1, frac: prev.length > 1 ? 1 : 0 }
  }
  return { row: best.row, frac: best.frac }
}

// --- Where you are, over time ---

export interface At {
  row: number
  frac: number
}

const behind = (a: At, b: At) => a.row < b.row || (a.row === b.row && a.frac < b.frac)

/** Further back than this from where you were last placed, the location wins: you really have gone back. */
const BACK_M = 150

/** The length of a line in metres. */
export function lengthM(line: LonLat[]): number {
  let total = 0
  for (let i = 1; i < line.length; i++) {
    const [a, b] = [line[i - 1], line[i]]
    const k = Math.cos((a[1] * Math.PI) / 180) * 111_320
    total += Math.hypot((b[0] - a[0]) * k, (b[1] - a[1]) * 110_540)
  }
  return total
}

/**
 * Moves you on along the steps. With a usable location you're placed by it, but only forward (GPS wobbles at a stop
 * shouldn't send you back a step) unless you're clearly back down the route. Without one (a tunnel, indoors) you stay
 * where you were last seen; only a ride you're on carries on by the clock. `clock` is where the timetable puts you.
 */
export function advance(prev: At | null, lines: LonLat[][], pos: { lat: number; lon: number; accuracy: number } | null, clock: At): At {
  const seen = pos ? locate(lines, pos, prev?.row ?? clock.row) : null
  if (!prev) return seen ?? clock
  if (seen) {
    if (!behind(seen, prev)) return seen
    return onLine(lines[prev.row], pos!).d > BACK_M ? seen : prev
  }
  if (clock.row === prev.row && clock.frac > prev.frac && lines[prev.row].length > 1) return { row: prev.row, frac: clock.frac }
  return prev
}

/** On a ride's line, you're on board once you've moved this far along it (until then you're waiting at the stop). */
export const BOARDED_M = 120
/** This close to the stop you're going to board at, you're waiting there. */
const AT_STOP_M = 40

/**
 * Whether a position is on a shape (a platform's outline, closed when its last point is its first): inside it, or
 * within `marginM` of its edge.
 */
export function inShape(outline: LonLat[], p: { lat: number; lon: number }, marginM: number): boolean {
  if (outline.length < 2) return false
  if (onLine(outline, p).d <= marginM) return true
  const first = outline[0]
  const last = outline[outline.length - 1]
  if (outline.length < 4 || first[0] !== last[0] || first[1] !== last[1]) return false
  // A ray east from p: crossing the outline an odd number of times means p is inside.
  let inside = false
  for (let i = 0; i + 1 < outline.length; i++) {
    const [ax, ay] = outline[i]
    const [bx, by] = outline[i + 1]
    if (ay > p.lat !== by > p.lat && ax + ((p.lat - ay) / (by - ay)) * (bx - ax) > p.lon) inside = !inside
  }
  return inside
}

/** How near a platform's edge still counts as on it: the location's accuracy, within these (m). */
const PLATFORM_EDGE_M: [number, number] = [10, 40]

/**
 * What you're doing, from where you are on the steps: walking to a ride or waiting for it, on it, or the last walk.
 * `platform` gives the outline of the platform legs[ride] is boarded from, when it's known (a train's): a platform is
 * far longer than the point the timetable gives for it, and anywhere on it you're waiting.
 */
export function phaseOf(
  o: Option, rs: Row[], at: At, lines: LonLat[][], pos: { lat: number; lon: number; accuracy: number } | null,
  aboard: (ride: number) => boolean = () => false, // seen moving with the vehicle of legs[ride] (boarding.ts)
  nowMs = 0, // to tell when a ride has reached your stop
  platform: (ride: number) => LonLat[] | undefined = () => undefined,
): Phase {
  const ph = phaseFromSteps(o, rs, at, lines, pos, nowMs, platform)
  return ph.kind === 'before' && aboard(ph.ride) ? { kind: 'riding', ride: ph.ride } : ph
}

function phaseFromSteps(
  o: Option, rs: Row[], at: At, lines: LonLat[][], pos: { lat: number; lon: number; accuracy: number } | null, nowMs: number,
  platform: (ride: number) => LonLat[] | undefined,
): Phase {
  const r = rs[at.row]
  const nextRide = (from: number) => o.legs.findIndex((l, j) => j >= from && l.kind === 'ride')
  const near = (s?: { lat: number; lon: number }) => !!(pos && s && onLine([[s.lon, s.lat]], pos).d <= Math.max(AT_STOP_M, pos.accuracy))
  const onPlatform = (ride: number) => {
    const outline = platform(ride)
    return !!(pos && outline && inShape(outline, pos, Math.min(PLATFORM_EDGE_M[1], Math.max(PLATFORM_EDGE_M[0], pos.accuracy))))
  }
  const waiting = (ride: number) => near(o.legs[ride].from) || onPlatform(ride)
  const before = (ride: number): Phase => (ride < 0 ? { kind: 'final-walk' } : { kind: 'before', ride, waiting: waiting(ride) })
  switch (r.kind) {
    case 'start':
      return before(nextRide(0))
    case 'arrive':
      return { kind: 'arrived' }
    case 'change':
      return before(r.t.to_leg)
    case 'leg':
      if (r.leg.kind === 'walk') return before(nextRide(r.i + 1))
      // At the stop you get off at, once the ride's time there has come: you're off it, on to the next thing (you
      // stay on this step until you've moved along the next, but you're no longer riding).
      if (nowMs >= Date.parse(r.leg.arr) && near(r.leg.to)) return before(nextRide(r.i + 1))
      // On the ride's line: aboard once you've moved along it; until then, at the stop waiting. A long platform runs
      // further along the track than that: still on it, you're still waiting (your vehicle, once you're seen moving
      // with it, says you're aboard sooner: see phaseOf).
      if (onPlatform(r.i)) return { kind: 'before', ride: r.i, waiting: true }
      return at.frac * lengthM(lines[at.row]) >= BOARDED_M ? { kind: 'riding', ride: r.i } : { kind: 'before', ride: r.i, waiting: true }
  }
}
