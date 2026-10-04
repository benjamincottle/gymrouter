import { test } from 'node:test'
import assert from 'node:assert/strict'
import { changeTimes, existing, findChange, legTrace, placeKey, placeWalks, record, sanitizeWalks, segments, type TimedWalk } from './walks.ts'
import type { Leg, Option, StopRef } from './types.ts'
import type { LonLat } from './walkmeasure.ts'

const iso = (hhmm: string) => `2026-10-08T${hhmm}:00+11:00`
const stop = (id: string, name: string, station_id?: string): StopRef => ({ id, name, station: station_id ? name : undefined, station_id, lat: -33.8, lon: 151.1 })
const EPP = stop('epp-p1', 'Epping Station', 'epp')
const EPP2 = stop('epp-p5', 'Epping Station', 'epp')
const BUS = stop('bus-1', 'Epping Rd at Rivett Rd')
const MARS = stop('mars', 'Mars Rd')
const home = { key: 'home:h1', name: 'Home', lat: -33.77, lon: 151.08 }
const gym = { key: 'gym:lane-cove', name: 'Lane Cove', lat: -33.81, lon: 151.15 }

const legs: Leg[] = [
  { kind: 'walk', to: EPP, dep: iso('08:00'), arr: iso('08:09') },
  { kind: 'ride', trip_id: 'm1', from: EPP, to: EPP2, dep: iso('08:10'), arr: iso('08:20') },
  { kind: 'walk', from: EPP2, to: BUS, dep: iso('08:20'), arr: iso('08:24') },
  { kind: 'ride', trip_id: 'b', from: BUS, to: MARS, dep: iso('08:26'), arr: iso('08:35') },
  { kind: 'walk', from: MARS, dep: iso('08:35'), arr: iso('08:40') },
]
const o: Option = {
  leave_at: iso('08:00'), arrive: iso('08:40'), duration_s: 2400, rides: 2, risk: 'safe', lines: [], legs,
  transfers: [{ from_leg: 1, to_leg: 3, walk_s: 240, slack_s: 120, risk: 'safe' }],
}
const walk = (secs: number, trace: LonLat[] = []) => ({ secs, distanceM: 300, trace })
const T: LonLat[] = [[1, 1], [2, 2], [3, 3]]

test("an option's walks: to the first stop, the change, from the last stop", () => {
  const s = segments(o, home, gym)
  assert.deepEqual(s.map((x) => [x.kind, x.leg, x.label, x.estimateS]), [
    ['access', 0, 'Home to Epping Station', 540],
    ['change', 3, 'Epping Station to Epping Rd at Rivett Rd', 240],
    ['access', 4, 'Mars Rd to Lane Cove', 300],
  ])
  assert.equal(segments(o).length, 1, 'without the places only the change can be timed')
})

test('the first timing replaces the default; later ones average or replace', () => {
  const [toStop, change, fromStop] = segments(o, home, gym)
  let w = record([], toStop, walk(500, T), 'average')
  assert.deepEqual(w, [{ kind: 'access', place: 'home:h1', stop: ['epp'], label: 'Home – Epping Station', times: [500], trace: T }])
  assert.deepEqual(placeWalks(w, 'home:h1'), [{ stop: 'epp', walk_s: 500 }])
  w = record(w, toStop, walk(600), 'average')
  assert.deepEqual(w[0].times, [500, 600])
  assert.deepEqual(w[0].trace, T, 'a walk without GPS keeps the traced route')
  w = record(w, toStop, walk(450), 'replace')
  assert.deepEqual(w[0].times, [450])
  for (const t of [1, 2, 3, 4, 5, 6]) w = record(w, toStop, walk(400 + t), 'average')
  assert.equal(w[0].times.length, 5, 'the last five')

  // From the last stop to the gym is stored as gym → stop, trace reversed.
  w = record(w, fromStop, walk(320, T), 'average')
  const g = w.find((x) => x.kind === 'access' && x.place === 'gym:lane-cove')!
  assert.deepEqual([g.label, g.trace], ['Lane Cove – Mars Rd', [[3, 3], [2, 2], [1, 1]]])
  assert.deepEqual(legTrace(w, o, 4, home.key, gym.key), T, 'drawn in the direction walked')

  // A change, timed the other way round on the trip home, updates the same walk.
  w = record(w, change, walk(200, T), 'average')
  const back = { ...change, stops: [BUS, EPP] as [StopRef, StopRef] }
  assert.equal(existing(w, back), w[w.length - 1])
  w = record(w, back, walk(220, [[9, 9], [8, 8]]), 'average')
  const c = w.filter((x) => x.kind === 'change')
  assert.equal(c.length, 1)
  assert.deepEqual([c[0].times, c[0].trace], [[200, 220], [[8, 8], [9, 9]]])
  assert.deepEqual(changeTimes(w), [{ from: 'epp', to: 'bus-1', secs: 210 }, { from: 'bus-1', to: 'epp', secs: 210 }])
  assert.deepEqual(legTrace(w, o, 2), [[8, 8], [9, 9]])
  assert.equal(findChange(w, EPP2, BUS)?.reversed, false, 'any platform of the station matches')
})

test('place keys survive a built-in gym being removed and added again', () => {
  assert.equal(placeKey('gym', { id: 'x1', ref: 'lane-cove' }), placeKey('gym', { id: 'y2', ref: 'lane-cove' }))
  assert.equal(placeKey('home', { id: 'h1' }), 'home:h1')
})

test('stored walks are checked', () => {
  const good: TimedWalk = { kind: 'change', from: ['a'], to: ['b'], label: 'A – B', times: [100] }
  assert.deepEqual(sanitizeWalks([good, { ...good, times: [] }, { ...good, kind: 'x' }, { ...good, from: [] },
    { ...good, trace: [[500, 1], [1, 1]] }, null, 'x']), [good, good])
  assert.deepEqual(sanitizeWalks('nope'), [])
})
