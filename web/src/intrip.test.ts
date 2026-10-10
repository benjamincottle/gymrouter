import { test } from 'node:test'
import assert from 'node:assert/strict'
import { assess, changesNote, instruction, keepFrom, phaseAt, replanOrigin, replanTime, spareToBoard, tripsFrom, type Missed } from './intrip.ts'
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
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, null, home, ms('13:30')), home)
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, here, home, now), { lat: -33.8, lon: 151.1 })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 3 }, null, home, now), { lat: C.lat, lon: C.lon, access: [{ stop: 'C', walk_s: 0 }] })
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 3 }, here, home, now), { lat: -33.8, lon: 151.1 })
  assert.equal(replanOrigin(planned, { kind: 'arrived' }, null, home, now), null)
  // Walking to a stop: the walking left by the trip's own (timed) walk goes with your position.
  assert.deepEqual(replanOrigin(planned, { kind: 'before', ride: 1 }, here, home, now, { stop: 'central', secs: 200.4 }), {
    lat: -33.8, lon: 151.1, walks: [{ stop: 'central', walk_s: 200 }],
  })
  // Past the time to leave with no location: taken to be walking as planned, so what's left is the time until the
  // ride goes (14 min to the 13:54), for its station, alongside the home's own walks.
  const timed = { ...home, walks: [{ stop: 'other', walk_s: 300 }, { stop: 'central', walk_s: 900 }] }
  assert.deepEqual(replanOrigin({ ...planned, legs: [planned.legs[0], { ...planned.legs[1], from: { ...A, station_id: 'central' } }, ...planned.legs.slice(2)] },
    { kind: 'before', ride: 1 }, null, timed, now, { stop: 'central', secs: 200 }),
  { ...home, walks: [{ stop: 'other', walk_s: 300 }, { stop: 'central', walk_s: 840 }] })
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

test('assessment: the trip is judged by the check of the trip itself, not by whether a search still lists it', () => {
  const committed = tripsFrom(planned, 1)
  assert.deepEqual(committed, ['t9', 'm1'])
  assert.deepEqual(keepFrom(planned, 1), [{ trip_id: 't9', from: 'A', to: 'B' }, { trip_id: 'm1', from: 'C', to: 'D' }])
  assert.deepEqual(keepFrom(planned, 2), [{ trip_id: 'm1', from: 'C', to: 'D' }])

  const same = option(planned.legs.slice(1), '14:29') // same trips, 2 min late
  let a = assess(committed, { option: same }, [], planned.arrive)
  assert.equal(a.status, 'on-track', 'on track though the search no longer lists it')
  assert.equal(a.status === 'on-track' && a.lateBy, 120)
  assert.equal(assess(committed, { option: same, catch_s: 40 }, [same], planned.arrive).status, 'on-track', 'itself is never "a faster way"')

  const faster = option([ride('t9', 'T9', A, B, '13:54', '14:09'), ride('bus', '288', B, D, '14:10', '14:20')], '14:22')
  a = assess(committed, { option: same }, [same, faster], planned.arrive)
  assert.equal(a.status, 'better')
  assert.equal(a.status === 'better' && a.suggestion, faster)
  const slightlyFaster = option(faster.legs, '14:28') // only 1 min better: not worth switching
  assert.equal(assess(committed, { option: same }, [same, slightlyFaster], planned.arrive).status, 'on-track')
  // A way with a change there's no time for is no suggestion.
  const hopeless = { ...faster, transfers: [{ from_leg: 0, to_leg: 1, walk_s: 60, slack_s: -20, risk: 'missed' as const }] }
  assert.equal(assess(committed, { option: same }, [hopeless], planned.arrive).status, 'on-track')

  // Can't reach the next ride in time.
  const later = option([ride('t9', 'T9', A, B, '13:54', '14:09'), ride('m1-next', 'M1', C, D, '14:22', '14:29')], '14:37')
  a = assess(committed, { option: same, catch_s: -30 }, [later], planned.arrive)
  assert.deepEqual(a.status === 'missed' && [a.missed.why, a.missed.why === 'board' && a.missed.ride.trip_id, a.suggestion, a.current], ['board', 't9', later, same])
  // No longer time for a change: the live times are still worth having.
  const broken = { ...same, transfers: [{ from_leg: 0, to_leg: 2, walk_s: 180, slack_s: -45, risk: 'missed' as const }] }
  a = assess(committed, { option: broken }, [later, hopeless], planned.arrive)
  assert.deepEqual(a.status === 'missed' && [a.missed.why, a.missed.why === 'change' && a.missed.ride.trip_id, a.missed.why === 'change' && a.missed.at.trip_id, a.suggestion, a.current],
    ['change', 'm1', 't9', later, broken])
  // A ride cancelled, or the server said nothing about the trip.
  assert.deepEqual(assess(committed, {}, [], planned.arrive), { status: 'missed', missed: { why: 'gone' }, current: undefined, suggestion: null })
  assert.equal(assess(committed, undefined, [later], planned.arrive).status, 'missed')
})

test('a warning that is showing stays until there is the tight setting to spare again', () => {
  const committed = tripsFrom(planned, 1)
  const withChange = (slack_s: number, catch_s?: number) => ({
    option: { ...option(planned.legs.slice(1), '14:27'), transfers: [{ from_leg: 0, to_leg: 2, walk_s: 180, slack_s, risk: slack_s < 0 ? 'missed' as const : slack_s < 60 ? 'at-risk' as const : 'tight' as const }] },
    ...(catch_s === undefined ? {} : { catch_s }),
  })
  const status = (kept: ReturnType<typeof withChange>, missed: Missed | null) => assess(committed, kept, [], planned.arrive, { missed, clearS: 60 }).status
  // Nothing showing: 20 s to spare is on track (at risk, but not a warning).
  assert.equal(status(withChange(20), null), 'on-track')
  // It went to -10 s and the warning showed; back at +20 s it stays, at +60 s it clears.
  const shown = assess(committed, withChange(-10), [], planned.arrive, { missed: null, clearS: 60 })
  assert.ok(shown.status === 'missed' && shown.missed.why === 'change')
  const held = shown.status === 'missed' ? shown.missed : null
  assert.equal(status(withChange(20), held), 'missed')
  const still = assess(committed, withChange(20), [], planned.arrive, { missed: held, clearS: 60 })
  assert.ok(still.status === 'missed' && still.missed.why === 'change' && still.missed.unsure, 'held: "may not", not "won\'t"')
  assert.ok(shown.status === 'missed' && shown.missed.why === 'change' && !shown.missed.unsure)
  assert.equal(status(withChange(59), held), 'missed')
  assert.equal(status(withChange(60), held), 'on-track')
  // The same for getting to a ride: shown at -5 s, held at +30 s, clear at +60 s, and moot once you're on it.
  const late = assess(committed, withChange(200, -5), [], planned.arrive)
  assert.ok(late.status === 'missed' && late.missed.why === 'board')
  const heldBoard = late.status === 'missed' ? late.missed : null
  assert.equal(status(withChange(200, 30), heldBoard), 'missed')
  assert.equal(status(withChange(200, 60), heldBoard), 'on-track')
  assert.equal(status(withChange(200), heldBoard), 'on-track', 'on board: you made it')
  // A warning about a change that's now behind you, or a service that's running again, doesn't linger.
  const past = { option: option(planned.legs.slice(3), '14:27') }
  assert.equal(assess(['m1'], past, [], planned.arrive, { missed: held, clearS: 60 }).status, 'on-track')
  assert.equal(status(withChange(20), { why: 'gone' }), 'on-track')
})

test('a faster way is no riskier than the trip you are on, and says how sure its changes are', () => {
  const committed = tripsFrom(planned, 1)
  const mine = (risk: 'safe' | 'tight') => ({ option: { ...option(planned.legs.slice(1), '14:29'), risk, transfers: [{ from_leg: 0, to_leg: 2, walk_s: 180, slack_s: 200, risk }] } })
  const other = (risk: 'safe' | 'tight' | 'at-risk', arrive: string, n = 1) => ({
    ...option([ride('t9', 'T9', A, B, '13:54', '14:09'), ride(`bus-${risk}-${arrive}`, '288', B, D, '14:10', '14:20')], arrive), risk,
    transfers: Array.from({ length: n }, () => ({ from_leg: 0, to_leg: 1, walk_s: 60, slack_s: 10, risk })),
  })
  const risky = other('at-risk', '14:20')
  const steady = other('safe', '14:24')
  assert.equal(assess(committed, mine('safe'), [risky], planned.arrive).status, 'on-track', 'faster, but riskier than yours')
  let a = assess(committed, mine('safe'), [risky, steady], planned.arrive)
  assert.equal(a.status === 'better' && a.suggestion, steady, 'the fastest that is no riskier')
  a = assess(committed, mine('tight'), [other('tight', '14:21'), steady], planned.arrive)
  assert.equal(a.status === 'better' && a.suggestion.arrive, iso('14:21'), 'as risky as yours is allowed')
  // When yours is gone, the next best is the soonest whatever its risk: it's said, not hidden.
  a = assess(committed, {}, [risky, steady], planned.arrive)
  assert.equal(a.status === 'missed' && a.suggestion, risky)
  assert.equal(changesNote(risky), 'with an at-risk change')
  assert.equal(changesNote(steady), 'with a safe change')
  assert.equal(changesNote(other('tight', '14:21', 2)), 'with 2 changes, the tightest tight')
  assert.equal(changesNote(other('at-risk', '14:21', 2)), 'with 2 changes, the tightest at risk')
  assert.equal(changesNote(other('safe', '14:21', 3)), 'with 3 safe changes')
  assert.equal(changesNote(option([ride('b', '291', A, D, '13:54', '14:20')], '14:25')), 'with no changes')
})

test('instructions', () => {
  assert.equal(instruction(planned, { kind: 'before', ride: 1 }).now, 'Get to Central Station for the T9')
  assert.equal(instruction(planned, { kind: 'riding', ride: 1 }).now, 'On the T9: get off at Epping Station')
  assert.equal(instruction(planned, { kind: 'arrived' }).now, "You've arrived")
  assert.equal(instruction(planned, { kind: 'final-walk' }, '9 Degrees Parramatta').now, 'Walk to 9 Degrees Parramatta')
})
