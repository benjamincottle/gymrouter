import { test } from 'node:test'
import assert from 'node:assert/strict'
import { advance, doing, nextEvent, vehicleAt, type Scenario, type State, type World } from './sim.ts'
import { rows } from './options.ts'
import { distM } from './boarding.ts'
import { rowLines, type LonLat } from './progress.ts'
import type { Leg, Option } from './types.ts'

const stop = (id: string, lat: number, lon: number) => ({ id, name: id, lat, lon })
const iso = (hhmm: string) => `2026-10-08T${hhmm}:00+11:00`
const ms = (hhmm: string) => Date.parse(iso(hhmm))

const A = stop('A', -33.7, 151.1)
const B = stop('B', -33.77, 151.08)
const C = stop('C', -33.77, 151.081)
const D = stop('D', -33.79, 151.13)
const home: LonLat = [151.091, -33.7] // about 830 m west of A: 10 min at 1.4 m/s
const gym: LonLat = [151.131, -33.79]

const ride = (trip: string, from: typeof A, to: typeof A, dep: string, arr: string, calls?: Leg['calls']): Leg => ({
  kind: 'ride', trip_id: trip, line: { mode: 'train', name: trip.toUpperCase() }, from, to, dep: iso(dep), arr: iso(arr), calls,
})
const walk = (dep: string, arr: string, from?: typeof A, to?: typeof A): Leg => ({ kind: 'walk', from, to, dep: iso(dep), arr: iso(arr) })
const option = (legs: Leg[], arrive: string): Option => ({
  leave_at: legs[0].dep, arrive: iso(arrive), duration_s: 0, rides: legs.filter((l) => l.kind === 'ride').length,
  risk: 'safe', lines: [], legs, transfers: [],
})

// walk 13:35–13:53 → T9 13:54–14:09 → walk → M1 14:12–14:19 → walk to 14:27
const planned = option(
  [walk('13:35', '13:53', undefined, A), ride('t9', A, B, '13:54', '14:09'), walk('14:09', '14:12', B, C),
    ride('m1', C, D, '14:12', '14:19'), walk('14:19', '14:27', D)],
  '14:27',
)

const world = (o: Option): World => {
  const rs = rows(o, true)
  return { option: o, rows: rs, lines: rowLines(rs, o, home, gym, {}), shapes: {}, stops: {}, destination: gym, walkSpeedMps: 1.4 }
}
const start = (scenario: Partial<Scenario> = {}): State => ({
  at: ms('13:33'), you: { kind: 'home' }, pos: home, done: [], scenario: { pace: 1, still: false, gps: 'good', late: {}, ...scenario },
})
const near = (p: LonLat, s: { lat: number; lon: number }, m = 5) => distM({ lon: p[0], lat: p[1] }, s) <= m

test('a vehicle waits at your stop, then runs between the times it calls at its stops', () => {
  const line: LonLat[] = [[151.1, -33.7], [151.1, -33.71], [151.1, -33.72]]
  const leg = ride('t9', A, stop('Z', -33.72, 151.1), '13:54', '14:04', [{ name: 'Y', arr: iso('13:56') }, { name: 'Z', arr: iso('14:04') }])
  const mid: LonLat[] = [[151.1, -33.71]]
  assert.equal(vehicleAt(leg, line, mid, 0, ms('13:48')), null) // not yet near
  assert.deepEqual(vehicleAt(leg, line, mid, 0, ms('13:50')), line[0])
  assert.ok(near(vehicleAt(leg, line, mid, 0, ms('13:55'))!, { lat: -33.705, lon: 151.1 })) // halfway to Y
  assert.ok(near(vehicleAt(leg, line, mid, 0, ms('14:00'))!, { lat: -33.715, lon: 151.1 })) // halfway from Y to Z
  assert.deepEqual(vehicleAt(leg, line, mid, 5 * 60_000, ms('13:58')), line[0]) // five minutes late: still at your stop
  assert.equal(vehicleAt(leg, line, mid, 0, ms('14:06')), null) // gone
})

test('you set off on time, walk to the stop, ride, change and walk to the end', () => {
  const w = world(planned)
  let s = advance(start(), w, ms('13:34'))
  assert.equal(s.you.kind, 'home')
  s = advance(s, w, ms('13:40'))
  assert.equal(s.you.kind, 'walk')
  s = advance(s, w, ms('13:50'))
  assert.equal(s.you.kind, 'wait')
  assert.ok(near(s.pos, A))
  s = advance(s, w, ms('14:00'))
  assert.equal(s.you.kind, 'ride')
  s = advance(s, w, ms('14:11'))
  assert.equal(s.you.kind, 'wait') // across to the M1
  assert.deepEqual(s.done, ['t9'])
  s = advance(s, w, ms('14:15'))
  assert.equal(s.you.kind, 'ride')
  s = advance(s, w, ms('14:30'))
  assert.equal(s.you.kind, 'arrived')
  assert.ok(near(s.pos, { lon: gym[0], lat: gym[1] }))
})

test('walking at half pace you miss the ride, and follow the trip once it changes', () => {
  let s = advance(start({ pace: 0.5 }), world(planned), ms('14:00'))
  assert.equal(s.you.kind, 'wait') // got there at about 13:55
  assert.match(doing(s), /^Missed the T9/)
  // Switched to a later T9: you wait for it and get on.
  const later = option([walk('13:35', '13:53', undefined, A), ride('t9b', A, B, '14:04', '14:19'), walk('14:19', '14:27', B)], '14:27')
  s = advance(s, world(later), ms('14:05'))
  assert.equal(s.you.kind, 'ride')
})

test('staying put, you never set off; a late vehicle takes you later', () => {
  assert.equal(advance(start({ still: true }), world(planned), ms('14:00')).you.kind, 'home')
  const s = advance(start({ late: { t9: 3 } }), world(planned), ms('13:55'))
  assert.equal(s.you.kind, 'wait') // it leaves at 13:57
  assert.equal(advance(s, world(planned), ms('13:58')).you.kind, 'ride')
})

test('the next step to skip to', () => {
  const w = world(planned)
  assert.equal(nextEvent(start(), w), ms('13:35'))
  const s = advance(start(), w, ms('13:50'))
  assert.equal(nextEvent(s, w), ms('13:53'))
})
