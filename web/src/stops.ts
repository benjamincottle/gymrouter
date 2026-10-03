// Grouping of nearby stops for the home editor.

import type { NearStop } from './types.ts'

export interface StopGroup {
  key: string
  name: string
  ids: string[]
  lines: string[]
  walk_s: number
  lat: number // the nearest platform/stand
  lon: number
}

/** Groups platforms/stands of the same station into one row, nearest first. */
export function groupStops(stops: NearStop[]): StopGroup[] {
  const byKey = new Map<string, StopGroup>()
  for (const s of stops) {
    const key = s.station_id || s.id
    const g = byKey.get(key) ?? { key, name: s.station || s.name, ids: [], lines: [], walk_s: s.walk_s, lat: s.lat, lon: s.lon }
    g.ids.push(s.id)
    for (const l of s.lines) if (!g.lines.includes(l)) g.lines.push(l)
    if (s.walk_s < g.walk_s) {
      g.walk_s = s.walk_s
      g.lat = s.lat
      g.lon = s.lon
    }
    byKey.set(key, g)
  }
  return [...byKey.values()].sort((a, b) => a.walk_s - b.walk_s)
}

/** The 3 nearest stops for each line plus every rail/metro/light-rail station; the rest are behind "show more". */
export function relevantGroups(groups: StopGroup[], perLine = 3): StopGroup[] {
  const count = new Map<string, number>()
  return groups.filter((g) => {
    const rail = g.lines.some((l) => /^(train|metro|light-rail|regional-train) /.test(l))
    let keep = rail
    for (const l of g.lines) {
      const n = count.get(l) ?? 0
      if (n < perLine) keep = true
      count.set(l, n + 1)
    }
    return keep
  })
}
