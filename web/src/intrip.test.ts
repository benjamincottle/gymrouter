import { test } from 'node:test'
import assert from 'node:assert/strict'
import { assess, instruction, phaseAt, replanOrigin, replanTime, spareToBoard, tripsFrom } from './intrip.ts'
import type { Leg, Option } from './types.ts'

const stop = (id: string, lat: number, lon: number, station?: string) => ({ id, name: station ? `${station}, Platform 1` : id, station, lat, lon })
const iso = (hhmm: string) => `2026-10-08T${hhmm}:00+11:00`
const ms = (hhmm: string) => Date.parse(iso(hhmm))

const A = stop('A', -33.7, 151.1, 'Central Station')
const B = stop('B', -33.77, 151.08, 'Epping Station')
const C = stop('C', -33.77, 151.081, 'Epping Station')
const D = stop('D', -33.79, 151.13)

const ride = (trip: string, name: string, from: typeof A, to: typeof A, dep: string, arr: string): Leg => ({
  kind: 'ride', trip_id: trip, line: { mode: 'train', name }, from, to, dep: iso(dep), arr: iso(arr),
})
const walk = (dep: string, arr: string, from?: typeof A, to?: typeof A): Leg => ({ kind: 'walk', from, to, dep: iso(dep), arr: iso(arr) })

const option = (legs: Leg[], arrive: string): Option => ({
  leave_at: legs[0].dep, arrive: iso(arrive), duration_s: 0, rides: legs.filter((l) => l.kind === 'ride').length,
  risk: 'safe', lines: [], legs, transfers: [],
})

// walk 13:35–13:53 → T9 13:54–14:09 → change → M1 14:12–14:19 → walk to 14:27
const planned = option(
  [walk('13:35', '13:53', undefined, A), ride('t9', 'T9', A, B, '13:54', '14:09'), walk('14:09', '14:12', B, C),
    ride('m1', 'M1', C, D, '14:12', '14:19'), walk('14:19', '14:27', D)],
  '14:27',
)

test('phase follows the clock', () => {
  assert.deepEqual(phaseAt(planned, ms('13:40')), { kind: 'before', ride: 1 })
  assert.deepEqual(phaseAt(planned, ms('14:00')), { kind: 'riding', ride: 1 })
  assert.deepEqual(phaseAt(planned, ms('14:10')), { kind: 'before', ride: 3 })
  assert.deepEqual(phaseAt(planned, ms('14:15')), { kind: 'riding', ride: 3 })
  assert.deepEqual(phaseAt(planned, ms('14:20')), { kind: 'final-walk' })
  assert.deepEqual(phaseAt(planned, ms('14:30')), { kind: 'arrived' })
})

test('spare time to reach the stop uses distance and walking speed', () => {
  const near = { lat: A.lat, lon: A.lon + 0.001, accuracy: 20 } // ~93 m away → ~93 s walking
  const spare = spareToBoard(planned.legs[1], near, ms('13:52'), 1.3)!
  assert.ok(spare > 0 && spare < 120, `spare ${spare}`)
  const far = { lat: A.lat + 0.02, lon: A.lon, accuracy: 20 } // ~2.2 km away
  assert.ok(spareToBoard(planned.legs[1], far, ms('13:52'), 1.3)! < 0)
  assert.equal(spareToBoard(planned.legs[1], { ...near, accuracy: 500 }, ms('13:52'), 1.3), null)
  assert.equal(spareToBoard(planned.legs[1], null, ms('13:52'), 1.3), null)
  // With the walking left along the trip's own walk, that's what counts, not the straight line.
  assert.equal(spareToBoard(planned.legs[1], far, ms('13:52'), 1.3, 45), 75)
})

test('re-plan origin depends on the phase', () => {
  const home = { lat: -33.70, lon: 151.08 }
  const now = ms('13:40')
  const here = { lat: -33.8, lon: 151.1, accuracy: 10 }
  assert.deepEqual(replanOrigin(planned, { kind: 'riding', ride: 1 }, null, home, now), { on_trip: { trip_id: 't9', from_stop: 'A' } })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, null, home, now), home)
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, here, home, now), { lat: -33.8, lon: 151.1 })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 3 }, null, home, now), { lat: C.lat, lon: C.lon, access: [{ stop: 'C', walk_s: 0 }] })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 3 }, here, home, now), { lat: -33.8, lon: 151.1 })
  assert.equal(replanOrigin(planned, { kind: 'arrived' }, null, home, now), null)
  // Walking to a stop: the walking left by the trip's own (timed) walk goes with your position.
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, here, home, now, { stop: 'central', secs: 200.4 }), {
    lat: -33.8, lon: 151.1, walks: [{ stop: 'central', walk_s: 200 }],
  })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, null, home, now, { stop: 'central', secs: 200 }), home)
})

test('a trip started well before it leaves is re-planned from its own start and time', () => {
  const home = { lat: -33.70, lon: 151.08 }
  const early = ms('11:00')
  const elsewhere = { lat: -33.9, lon: 151.2, accuracy: 10 }
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, elsewhere, home, early), home)
  assert.equal(replanTime(planned, { kind: 'before', ride: 1 }, early), new Date(ms('13:30')).toISOString())
  assert.equal(replanTime(planned, { kind: 'before', ride: 1 }, ms('13:32')), undefined)
  assert.equal(replanTime(planned, { kind: 'riding', ride: 1 }, early), undefined)
})

test('assessment: on track, running late, better option, missed connection', () => {
  const committed = tripsFrom(planned, 1)
  assert.deepEqual(committed, ['t9', 'm1'])

  const same = option(planned.legs.slice(1), '14:29') // same trips, 2 min late
  let a = assess(committed, [same], planned.arrive)
  assert.equal(a.status, 'on-track')
  assert.equal(a.status === 'on-track' && a.lateBy, 120)

  const faster = option([ride('t9', 'T9', A, B, '13:54', '14:09'), ride('bus', '288', B, D, '14:10', '14:20')], '14:22')
  a = assess(committed, [same, faster], planned.arrive)
  assert.equal(a.status, 'better')
  assert.equal(a.status === 'better' && a.suggestion, faster)

  const slightlyFaster = option(faster.legs, '14:28') // only 1 min better: not worth switching
  assert.equal(assess(committed, [same, slightlyFaster], planned.arrive).status, 'on-track')

  const later = option([ride('t9', 'T9', A, B, '13:54', '14:09'), ride('m1-next', 'M1', C, D, '14:22', '14:29')], '14:37')
  a = assess(committed, [later], planned.arrive)
  assert.equal(a.status, 'missed')
  assert.equal(a.status === 'missed' && a.suggestion, later)
  assert.deepEqual(assess(committed, [], planned.arrive), { status: 'missed', suggestion: null })
})

test('instructions', () => {
  assert.equal(instruction(planned, { kind: 'before', ride: 1 }).now, 'Get to Central Station for the T9')
  assert.equal(instruction(planned, { kind: 'riding', ride: 1 }).now, 'On the T9: get off at Epping Station')
  assert.equal(instruction(planned, { kind: 'arrived' }).now, "You've arrived")
  assert.equal(instruction(planned, { kind: 'final-walk' }, '9 Degrees Parramatta').now, 'Walk to 9 Degrees Parramatta')
})
