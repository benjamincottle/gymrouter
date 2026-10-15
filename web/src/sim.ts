// The trip simulator: temporary, for trying a trip at a desk instead of on the street (docs/spec.md, 2026-10-16).
// Opened with ?sim in the address, every trip started is simulated. It runs on the timetable at a speed you choose,
// with a simulated you who walks the trip's walks and rides its rides, its vehicles, and the location a phone would
// report. It stands in at four places: the clock (clock.ts), your location (geolocation()), the vehicles' reports
// (vehicles()) and the re-checks, which ask the server for the timetable alone at the simulated time (`sim`).

import * as clock from './clock.ts'
import { callPlaces } from './countdown.ts'
import { statusTime as at } from './format.ts'
import { lengthM, onLine, shapeKey, splitAt, walkTo, type LonLat } from './progress.ts'
import type { Row } from './options.ts'
import type { Leg, Option, Vehicle } from './types.ts'

export type Gps = 'good' | 'poor' | 'off'

/** What you can make happen. */
export interface Scenario {
  pace: number // your walking speed, as a share of the trip's
  still: boolean // you stay where you are: you don't set off, walk on or board
  gps: Gps
  late: Record<string, number> // minutes a vehicle runs late, by trip ID
}

/** What the simulated you is doing. */
export type You =
  | { kind: 'home' }
  | { kind: 'walk'; path: LonLat[]; walked: number; trip?: string } // to the ride with this trip ID; none: to the end
  | { kind: 'wait'; leg: Leg; since: number } // at its stop since then
  | { kind: 'ride'; leg: Leg }
  | { kind: 'arrived' }

export interface State {
  at: number // the simulated time this is for
  you: You
  pos: LonLat
  done: string[] // the trip IDs ridden
  scenario: Scenario
}

/** The trip as the screen has it (views/intrip.tsx passes it on each render). */
export interface World {
  option: Option
  rows: Row[]
  lines: LonLat[][] // each row's line on the ground (progress.rowLines)
  shapes: Record<string, LonLat[]> // each ride's route, by shapeKey
  stops: Record<string, LonLat[]> // the stops along it, before the last
  destination: LonLat
  walkSpeedMps: number
}

/** A vehicle shows this long before it leaves your stop (it waits there: only the part ridden is known). */
const SHOWN_BEFORE_MS = 5 * 60_000
/** Vehicles report where they are this often. */
const REPORT_MS = 15_000
const STEP_MS = 1000

const late = (sc: Scenario, trip?: string) => (trip ? (sc.late[trip] ?? 0) * 60_000 : 0)
const pt = (s: { lat: number; lon: number }): LonLat => [s.lon, s.lat]

/** The point `frac` (0..1) of the way along a line. */
function along(line: LonLat[], frac: number): LonLat {
  return line.length < 2 ? line[0] : splitAt(line, frac)[1][0]
}

/**
 * Where a ride's vehicle is at time t, `lateMs` behind its timetable: waiting at your stop before it leaves, then
 * along the ride's line between the times it calls at its stops. Null when it isn't near the part ridden.
 */
export function vehicleAt(leg: Leg, line: LonLat[] | undefined, stops: LonLat[] | undefined, lateMs: number, t: number): LonLat | null {
  if (!leg.from || !leg.to) return null
  const path = line && line.length > 1 ? line : [pt(leg.from), pt(leg.to)]
  const dep = Date.parse(leg.dep) + lateMs
  const arr = Date.parse(leg.arr) + lateMs
  if (t < dep - SHOWN_BEFORE_MS || t > arr + 60_000) return null
  const knots: [number, number][] = [[dep, 0]]
  const places = line && stops && leg.calls ? callPlaces(path, stops, leg.calls) : null
  if (places && leg.calls) {
    leg.calls.forEach((c, k) => {
      if (c.arr && k < places.length - 1) knots.push([Date.parse(c.arr) + lateMs, places[k]])
    })
  }
  knots.push([arr, 1])
  if (t <= dep) return path[0]
  for (let k = 1; k < knots.length; k++) {
    const [t0, f0] = knots[k - 1]
    const [t1, f1] = knots[k]
    if (t <= t1) return along(path, t1 > t0 ? f0 + ((f1 - f0) * (t - t0)) / (t1 - t0) : f1)
  }
  return path[path.length - 1]
}

/** The next ride not yet ridden, and its place in the legs. */
export function nextRide(o: Option, done: string[]): { leg: Leg; i: number } | null {
  const i = o.legs.findIndex((l) => l.kind === 'ride' && l.trip_id && l.from && l.to && !done.includes(l.trip_id))
  return i < 0 ? null : { leg: o.legs[i], i }
}

/** A walk from where you are to the next ride's stop (or to the end): along the trip's own walk, from its nearest point. */
function walkOn(s: State, w: World): You {
  const next = nextRide(w.option, s.done)
  let line: LonLat[] | undefined
  let target: LonLat
  if (next) {
    const r = walkTo(w.rows, w.option, next.i)
    line = r ? w.lines[r.row] : undefined
    target = pt(next.leg.from!)
  } else {
    const end = w.rows.findIndex((r) => r.kind === 'arrive')
    const last = w.rows[end - 1]
    line = last?.kind === 'leg' && last.leg.kind === 'walk' ? w.lines[end - 1] : undefined
    target = w.destination
  }
  const rest = line && line.length > 1 ? splitAt(line, onLine(line, { lon: s.pos[0], lat: s.pos[1] }).frac)[1] : [target]
  return { kind: 'walk', path: [s.pos, ...rest], walked: 0, trip: next?.leg.trip_id }
}

/** One step of up to a second: what you do between s.at and t. */
function step(s: State, w: World, t: number): State {
  const dt = t - s.at
  const sc = s.scenario
  const next = nextRide(w.option, s.done)
  const you = s.you
  s = { ...s, at: t }
  switch (you.kind) {
    case 'home':
      if (!sc.still && t >= Date.parse(w.option.leave_at)) s.you = walkOn(s, w)
      return s
    case 'walk': {
      if (you.trip !== next?.leg.trip_id) return { ...s, you: walkOn(s, w) } // the trip changed under you: on to its next ride
      if (sc.still) return s
      const walked = you.walked + (w.walkSpeedMps * sc.pace * dt) / 1000
      const len = lengthM(you.path)
      if (walked < len) return { ...s, you: { ...you, walked }, pos: along(you.path, walked / len) }
      s.pos = you.path[you.path.length - 1]
      s.you = next ? { kind: 'wait', leg: next.leg, since: t } : { kind: 'arrived' }
      return s
    }
    case 'wait': {
      if (you.leg.trip_id !== next?.leg.trip_id) return { ...s, you: walkOn(s, w) }
      const dep = Date.parse(you.leg.dep) + late(sc, you.leg.trip_id)
      // On board if you were there when it left; otherwise you're still at the stop, and it's gone.
      if (!sc.still && t >= dep && you.since <= dep) s.you = { kind: 'ride', leg: you.leg }
      return s
    }
    case 'ride': {
      const k = shapeKey(you.leg)
      const lateMs = late(sc, you.leg.trip_id)
      s.pos = vehicleAt(you.leg, w.shapes[k], w.stops[k], lateMs, t) ?? s.pos
      if (t < Date.parse(you.leg.arr) + lateMs) return s
      s.pos = pt(you.leg.to!)
      s.done = [...s.done, you.leg.trip_id!]
      s.you = walkOn(s, w)
      return s
    }
    case 'arrived':
      return s
  }
}

/** You, moved on to time t a second at a time. */
export function advance(s: State, w: World | null, t: number): State {
  if (!w) return { ...s, at: Math.max(s.at, t) }
  while (s.at < t) s = step(s, w, Math.min(t, s.at + STEP_MS))
  return s
}

/** The next moment worth skipping to: setting off, or a minute before a ride leaves or reaches your stop. */
export function nextEvent(s: State, w: World): number | null {
  const times: number[] = []
  if (s.you.kind === 'home') times.push(Date.parse(w.option.leave_at))
  for (const l of w.option.legs) {
    if (l.kind !== 'ride' || !l.trip_id || s.done.includes(l.trip_id)) continue
    const lateMs = late(s.scenario, l.trip_id)
    times.push(Date.parse(l.dep) + lateMs - 60_000, Date.parse(l.arr) + lateMs - 60_000)
  }
  const ahead = times.filter((t) => t > s.at + 5000)
  return ahead.length ? Math.min(...ahead) : null
}

/** What the simulated you is doing, in a line. */
export function doing(s: State): string {
  const y = s.you
  const name = (l: Leg) => l.line?.name ?? 'service'
  switch (y.kind) {
    case 'home':
      return s.scenario.still ? 'At the start, staying put' : 'At the start'
    case 'walk': {
      const left = Math.max(0, Math.round(lengthM(y.path) - y.walked))
      return `Walking, ${left} m to ${y.trip ? 'the stop' : 'the end'}`
    }
    case 'wait': {
      const dep = Date.parse(y.leg.dep) + late(s.scenario, y.leg.trip_id)
      return s.at > dep && y.since > dep ? `Missed the ${name(y.leg)} (left ${at(dep)})` : `Waiting for the ${name(y.leg)}, leaving ${at(dep)}`
    }
    case 'ride':
      return `On the ${name(y.leg)}, at ${y.leg.to?.name} ${at(Date.parse(y.leg.arr) + late(s.scenario, y.leg.trip_id))}`
    case 'arrived':
      return 'Arrived'
  }
}

// --- the simulation that's running ---

const KEY = 'gymrouter.sim'
const FIX = { good: { accuracy: 8, noise: 4 }, poor: { accuracy: 60, noise: 30 } }

let state: State | null = null
let world: World | null = null
let loop: ReturnType<typeof setInterval> | undefined
let lastFix = 0
let lastSaved = 0
const watchers = new Map<number, PositionCallback>()
let nextWatch = 1
const listeners = new Set<() => void>()

/** True when the address asks for the simulator (?sim): trips started then are simulated. */
export const wanted = () => new URLSearchParams(window.location.search).has('sim')

/** The simulation running, if any. */
export const current = () => state

/** Starts simulating a trip from just before it sets off (or now, if that's later), paused. */
export function start(origin: LonLat, leaveAt: string) {
  const at = Math.max(Date.now(), Date.parse(leaveAt) - 2 * 60_000)
  state = { at, you: { kind: 'home' }, pos: origin, done: [], scenario: { pace: 1, still: false, gps: 'good', late: {} } }
  lastFix = 0
  clock.setSim(at, 0)
  run()
}

/** Picks up a simulated trip after a reload, paused where it was; a fresh one if nothing was saved. */
export function resume(origin: LonLat, leaveAt: string) {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) ?? 'null') as State | null
    if (saved?.you && saved.scenario) {
      state = saved
      clock.setSim(saved.at, 0)
      run()
      return
    }
  } catch {
    /* start again */
  }
  start(origin, leaveAt)
}

/** Ends the simulation: the clock is the real one again. */
export function end() {
  state = null
  world = null
  clearInterval(loop)
  loop = undefined
  clock.setSim(null)
  try {
    localStorage.removeItem(KEY)
  } catch {
    /* nothing kept */
  }
  tell()
}

/** The trip as the screen has it now. */
export function see(w: World) {
  world = w
}

export function setScenario(patch: Partial<Scenario>) {
  if (!state) return
  state = { ...state, scenario: { ...state.scenario, ...patch } }
  tell()
}

/** Skips to the next moment worth seeing. */
export function skipToNext() {
  if (!state || !world) return
  const t = nextEvent(state, world)
  if (t !== null) clock.skip(t - clock.now())
}

export function onChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
const tell = () => listeners.forEach((l) => l())

function run() {
  clearInterval(loop)
  loop = setInterval(tick, 100)
  tell()
}

function tick() {
  if (!state) return
  const t = clock.now()
  if (t <= state.at) return // paused
  state = advance(state, world, t)
  // A location about once a (simulated) second, and never more than 40 s apart however fast it runs.
  const gps = state.scenario.gps
  if (gps !== 'off' && state.at - lastFix >= 1000) {
    lastFix = state.at
    const { accuracy, noise } = FIX[gps]
    const [lon, lat] = jitter(state.pos, noise)
    const p = { coords: { latitude: lat, longitude: lon, accuracy }, timestamp: state.at } as unknown as GeolocationPosition
    watchers.forEach((w) => w(p))
  }
  if (Date.now() - lastSaved > 1000) {
    lastSaved = Date.now()
    try {
      localStorage.setItem(KEY, JSON.stringify(state))
    } catch {
      /* a reload starts it again */
    }
  }
  tell()
}

/** A point moved by about `m` metres in a random direction (normally distributed). */
function jitter([lon, lat]: LonLat, m: number): LonLat {
  const g = () => Math.sqrt(-2 * Math.log(1 - Math.random())) * Math.cos(2 * Math.PI * Math.random())
  return [lon + (g() * m) / (111_320 * Math.cos((lat * Math.PI) / 180)), lat + (g() * m) / 110_540]
}

/** The location the app follows: the simulated you while a trip is simulated, otherwise the browser's. */
export function geolocation(): Geolocation | undefined {
  if (!state) return navigator.geolocation
  return {
    watchPosition: (ok: PositionCallback) => {
      const id = nextWatch++
      watchers.set(id, ok)
      return id
    },
    clearWatch: (id: number) => void watchers.delete(id),
    getCurrentPosition: () => undefined,
  } as Geolocation
}

/** The simulated vehicles running the given rides (<trip>|<from stop>|<to stop>), as of their last report. */
export function vehicles(rides: string[]): Vehicle[] {
  if (!state) return []
  const s = state
  const ts = Math.floor(s.at / REPORT_MS) * REPORT_MS
  const legs = [...(world?.option.legs ?? []), ...(s.you.kind === 'ride' || s.you.kind === 'wait' ? [s.you.leg] : [])]
  const out: Vehicle[] = []
  for (const key of new Set(rides)) {
    const leg = legs.find((l) => l.kind === 'ride' && shapeKey(l) === key)
    if (!leg?.trip_id || !leg.line) continue
    const p = vehicleAt(leg, world?.shapes[key], world?.stops[key], late(s.scenario, leg.trip_id), ts)
    if (!p) continue
    out.push({
      id: `sim ${leg.trip_id}`, line: `${leg.line.mode} ${leg.line.name}`, color: leg.line.color, trip_id: leg.trip_id,
      lon: p[0], lat: p[1], ts: new Date(ts).toISOString(),
    })
  }
  return out
}
