// Timed walks: walks you've timed during a trip, kept on this device and used whenever the same walk comes up again.
// A walk is either between a place (home or gym) and a stop, or a change between two stops. Either way it counts in
// both directions: the walk from home to the station is the walk from the station home.
// Pure logic; the screens are views/intrip.tsx (timing) and views/settings.tsx (the list).

import type { Option, PlaceRequest, StopRef } from './types.ts'
import { meanSecs, MAX_SAMPLES, type LonLat, type Walk } from './walkmeasure.ts'

interface Timed {
  label: string // e.g. "Home – Epping Station"
  times: number[] // seconds, most recent last (at most MAX_SAMPLES)
  trace?: LonLat[] // the route last walked with GPS, in the direction described below
}

/** Between a place and a stop; the trace runs from the place to the stop. */
export interface AccessWalk extends Timed {
  kind: 'access'
  place: string // see placeKey
  stop: string[] // stop or station IDs it covers
}

/** A change between two stops; the trace runs from `from` to `to`. */
export interface ChangeWalk extends Timed {
  kind: 'change'
  from: string[]
  to: string[]
}

export type TimedWalk = AccessWalk | ChangeWalk

/** What happens when a walk that already has a time is timed again. */
export type Retime = 'average' | 'replace'

export const MAX_WALKS = 100

/** Identifies a home or gym across edits; a built-in gym by what it was added from, so removing and re-adding keeps its walks. */
export function placeKey(kind: 'home' | 'gym', p: { id: string; ref?: string }): string {
  return kind === 'home' ? `home:${p.id}` : `gym:${p.ref ?? p.id}`
}

/** A stop as a walk reaches it: the stop, and the mode of the ride you get on or off there. */
export interface At {
  stop: Pick<StopRef, 'id' | 'station_id'>
  mode?: string // e.g. "train", "bus"
}

const RAIL = ['train', 'regional-train', 'metro', 'light-rail']

/**
 * A stop as walks know it. A rail platform is its station (any platform will do: they're inside, a minute apart);
 * anything else, a bus stand included, is its own stop (the stand across the road is a different walk).
 */
export function stopKey(at: At): string {
  return at.mode && RAIL.includes(at.mode) && at.stop.station_id ? at.stop.station_id : at.stop.id
}

const covers = (ids: string[], at: At) => ids.includes(stopKey(at))

export function findAccess(walks: TimedWalk[], place: string, at: At): AccessWalk | undefined {
  return walks.find((w): w is AccessWalk => w.kind === 'access' && w.place === place && covers(w.stop, at))
}

/** The change walk between two stops, in either direction; `reversed` means it was timed from `b` to `a`. */
export function findChange(walks: TimedWalk[], a: At, b: At): { walk: ChangeWalk; reversed: boolean } | undefined {
  for (const w of walks) {
    if (w.kind !== 'change') continue
    if (covers(w.from, a) && covers(w.to, b)) return { walk: w, reversed: false }
    if (covers(w.from, b) && covers(w.to, a)) return { walk: w, reversed: true }
  }
  return undefined
}

export const walkSecs = (w: Timed) => meanSecs(w.times)

// --- The walks in a trip ---

export interface PlaceRef {
  key: string
  name: string
  lat: number
  lon: number
}

/** One walk in an option that can be timed, described in the direction it's walked on this trip. */
export interface Segment {
  kind: 'access' | 'change'
  leg: number // the option's walk leg, or for a change the ride you change onto
  label: string // "Home to Epping Station"
  from: { name: string; lat: number; lon: number }
  to: { name: string; lat: number; lon: number }
  estimateS: number // what this trip assumed
  // what identifies it: access walks by place and stop; changes by the two stops
  place?: string
  stop?: At
  outbound?: boolean // access: walking from the place to the stop (else from the stop to the place)
  stops?: [At, At]
}

const stopName = (s: Pick<StopRef, 'name' | 'station'>) => s.station || s.name
/** A rail platform by its station; a bus stand by its own name ("West Ryde Station, Victoria Rd, Stand F"). */
const atName = (s: StopRef, mode?: string) => (mode && RAIL.includes(mode) ? stopName(s) : s.name)
const legSecs = (o: Option, i: number) => Math.round((Date.parse(o.legs[i].arr) - Date.parse(o.legs[i].dep)) / 1000)
const modeOf = (o: Option, i: number) => o.legs[i]?.line?.mode

/** Where a walk leg starts and ends, with the modes of the rides either side of it. */
function ends(o: Option, i: number): { from?: At; to?: At } {
  const l = o.legs[i]
  return {
    from: l.from && { stop: l.from, mode: modeOf(o, i - 1) },
    to: l.to && { stop: l.to, mode: modeOf(o, i + 1) },
  }
}

/** The walks of an option you could time: to the first stop, each change, and from the last stop. */
export function segments(o: Option, start?: PlaceRef, end?: PlaceRef): Segment[] {
  const out: Segment[] = []
  const first = o.legs[0]
  if (start && first?.kind === 'walk' && !first.from && first.to) {
    const s = first.to
    const name = atName(s, modeOf(o, 1))
    out.push({
      kind: 'access', leg: 0, label: `${start.name} to ${name}`, from: start, to: { name, lat: s.lat, lon: s.lon },
      estimateS: legSecs(o, 0), place: start.key, stop: ends(o, 0).to, outbound: true,
    })
  }
  for (const t of o.transfers) {
    const a = o.legs[t.from_leg]?.to
    const b = o.legs[t.to_leg]?.from
    if (!a || !b) continue
    const an = atName(a, modeOf(o, t.from_leg))
    const bn = atName(b, modeOf(o, t.to_leg))
    out.push({
      kind: 'change', leg: t.to_leg, label: an === bn ? `Change at ${an}` : `${an} to ${bn}`,
      from: { name: an, lat: a.lat, lon: a.lon }, to: { name: bn, lat: b.lat, lon: b.lon },
      estimateS: t.walk_s, stops: [{ stop: a, mode: modeOf(o, t.from_leg) }, { stop: b, mode: modeOf(o, t.to_leg) }],
    })
  }
  const li = o.legs.length - 1
  const last = o.legs[li]
  if (end && li > 0 && last?.kind === 'walk' && last.from && !last.to) {
    const s = last.from
    const name = atName(s, modeOf(o, li - 1))
    out.push({
      kind: 'access', leg: li, label: `${name} to ${end.name}`, from: { name, lat: s.lat, lon: s.lon }, to: end,
      estimateS: legSecs(o, li), place: end.key, stop: ends(o, li).from, outbound: false,
    })
  }
  return out
}

/** The walk already timed for a segment, if any. */
export function existing(walks: TimedWalk[], seg: Segment): TimedWalk | undefined {
  if (seg.kind === 'access') return findAccess(walks, seg.place!, seg.stop!)
  return findChange(walks, seg.stops![0], seg.stops![1])?.walk
}

/**
 * Saves a timed walk. The first time replaces the default; after that `retime` says whether it's averaged with the
 * earlier walks or replaces them. The newest GPS trace is the one kept.
 */
export function record(walks: TimedWalk[], seg: Segment, w: Walk, retime: Retime): TimedWalk[] {
  const old = existing(walks, seg)
  const times = (old && retime === 'average' ? [...old.times, w.secs] : [w.secs]).slice(-MAX_SAMPLES)
  let trace: LonLat[] | undefined = w.trace.length >= 2 ? w.trace : old?.trace
  let next: TimedWalk
  if (seg.kind === 'access') {
    // Stored from the place to the stop.
    if (w.trace.length >= 2 && !seg.outbound) trace = [...w.trace].reverse()
    const label = seg.outbound ? `${seg.from.name} – ${seg.to.name}` : `${seg.to.name} – ${seg.from.name}`
    next = { kind: 'access', place: seg.place!, stop: (old as AccessWalk | undefined)?.stop ?? [stopKey(seg.stop!)], label, times, trace }
  } else {
    const [a, b] = seg.stops!
    const found = findChange(walks, a, b)
    if (found?.reversed && w.trace.length >= 2) trace = [...w.trace].reverse()
    next = found
      ? { ...found.walk, times, trace }
      : { kind: 'change', from: [stopKey(a)], to: [stopKey(b)], label: seg.from.name === seg.to.name ? `Change at ${seg.from.name}` : `${seg.from.name} – ${seg.to.name}`, times, trace }
  }
  if (!next.trace) delete next.trace
  return [...walks.filter((x) => x !== old), next].slice(-MAX_WALKS)
}

// --- Using them ---

/** The walks to send with a place in a plan request. */
export function placeWalks(walks: TimedWalk[], place: string): NonNullable<PlaceRequest['walks']> {
  const out: NonNullable<PlaceRequest['walks']> = []
  for (const w of walks) {
    if (w.kind === 'access' && w.place === place) for (const stop of w.stop) out.push({ stop, walk_s: walkSecs(w) })
  }
  return out.slice(0, 40)
}

/** Timed changes as the planner's transfer times, both ways. */
export function changeTimes(walks: TimedWalk[]): { from: string; to: string; secs: number }[] {
  const out: { from: string; to: string; secs: number }[] = []
  for (const w of walks) {
    if (w.kind !== 'change') continue
    for (const a of w.from) {
      for (const b of w.to) {
        out.push({ from: a, to: b, secs: walkSecs(w) })
        if (a !== b) out.push({ from: b, to: a, secs: walkSecs(w) })
      }
    }
  }
  return out.slice(0, 100)
}

/** The traced route for one of an option's walk legs, in the direction walked, if there is one. */
export function legTrace(walks: TimedWalk[], o: Option, i: number, start?: string, end?: string): LonLat[] | undefined {
  const l = o.legs[i]
  if (l.kind !== 'walk') return undefined
  let t: LonLat[] | undefined
  const { from, to } = ends(o, i)
  if (from && to) {
    const f = findChange(walks, from, to)
    t = f?.walk.trace && (f.reversed ? [...f.walk.trace].reverse() : f.walk.trace)
  } else if (to && start) {
    t = findAccess(walks, start, to)?.trace
  } else if (from && end) {
    const tr = findAccess(walks, end, from)?.trace
    t = tr && [...tr].reverse()
  }
  return t && t.length > 1 ? t : undefined
}

// --- Stored data ---

const isNum = (v: unknown, min: number, max: number): v is number => typeof v === 'number' && Number.isFinite(v) && v >= min && v <= max
const ids = (v: unknown): string[] | null =>
  Array.isArray(v) && v.length > 0 && v.length <= 12 && v.every((x) => typeof x === 'string' && x.length > 0 && x.length <= 64) ? v : null

function cleanTrace(v: unknown): LonLat[] | undefined {
  if (!Array.isArray(v) || v.length < 2 || v.length > 120) return undefined
  const ok = v.every((p) => Array.isArray(p) && p.length === 2 && isNum(p[0], -180, 180) && isNum(p[1], -90, 90))
  return ok ? (v as LonLat[]) : undefined
}

function cleanTimes(v: unknown): number[] {
  return Array.isArray(v) ? v.filter((t): t is number => isNum(t, 1, 3600)).slice(-MAX_SAMPLES).map(Math.round) : []
}

/** Validates untrusted walks (storage, a backup or a link), dropping anything malformed. */
export function sanitizeWalks(input: unknown): TimedWalk[] {
  if (!Array.isArray(input)) return []
  const out: TimedWalk[] = []
  for (const r of input.slice(0, MAX_WALKS) as Record<string, unknown>[]) {
    if (!r || typeof r !== 'object' || typeof r.label !== 'string' || r.label.length > 160) continue
    const times = cleanTimes(r.times)
    if (times.length === 0) continue
    const trace = cleanTrace(r.trace)
    const base = { label: r.label, times, ...(trace ? { trace } : {}) }
    if (r.kind === 'access' && typeof r.place === 'string' && r.place.length <= 60 && ids(r.stop)) {
      out.push({ kind: 'access', place: r.place, stop: ids(r.stop)!, ...base })
    } else if (r.kind === 'change' && ids(r.from) && ids(r.to)) {
      out.push({ kind: 'change', from: ids(r.from)!, to: ids(r.to)!, ...base })
    }
  }
  return out
}
