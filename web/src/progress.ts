// Where you are on a trip's description, from your location rather than the clock: each step has a line on the ground
// (the walk's path, the ride's route, or straight between stops), and you're placed on the one you're nearest.

import type { Row } from './options.ts'
import type { Leg, Option, StopRef } from './types.ts'

export type LonLat = [number, number]

/** The key for a ride's route shape (see /api/shape). */
export const shapeKey = (l: Leg) => `${l.trip_id}|${l.from?.id}|${l.to?.id}`

const pt = (s: { lat: number; lon: number }): LonLat => [s.lon, s.lat]

/** Each step's line on the ground, in order with the rows. `shapes` holds the routes of the rides fetched so far. */
export function rowLines(rs: Row[], o: Option, start: LonLat, end: LonLat, shapes: Record<string, LonLat[]>): LonLat[][] {
  return rs.map((r): LonLat[] => {
    switch (r.kind) {
      case 'start':
        return [start]
      case 'arrive':
        return [end]
      case 'change': {
        const walk = o.legs.slice(r.t.from_leg + 1, r.t.to_leg).find((l) => l.kind === 'walk')
        if (walk?.path && walk.path.length > 1) return walk.path
        return [r.from, r.to].filter((s): s is StopRef => s !== undefined).map(pt)
      }
      case 'leg': {
        const l = r.leg
        const a = l.from ? pt(l.from) : start
        const b = l.to ? pt(l.to) : end
        if (l.kind === 'walk') return l.path && l.path.length > 1 ? l.path : [a, b]
        return shapes[shapeKey(l)] ?? [a, b]
      }
    }
  })
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
