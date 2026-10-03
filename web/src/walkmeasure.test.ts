import { test } from 'node:test'
import assert from 'node:assert/strict'
import { addFix, distanceM, finish, meanSecs, mmss, simplifyTrace, walkedM, withWalk, type Recording } from './walkmeasure.ts'
import { sanitize, walkTraces, type Settings } from './settings.ts'

const T0 = 1_700_000_000_000
// A walker heading north at ~1.3 m/s (1e-5 degrees of latitude is ~1.1 m): one reading a second.
const at = (i: number, lonOff = 0) => ({ lat: -33.8 + i * 0.0000117, lon: 151.15 + lonOff, accuracy: 8, t: T0 + i * 1000 })

test('usable readings are kept; poor accuracy and jumps are not', () => {
  const rec: Recording = { startedAt: T0, fixes: [] }
  assert.equal(addFix(rec, at(1)), true)
  assert.equal(addFix(rec, { ...at(2), accuracy: 120 }), false, 'poor accuracy')
  assert.equal(addFix(rec, { ...at(2), lat: -33.79 }), false, 'a 1 km jump in a second')
  assert.equal(addFix(rec, { ...at(1), t: T0 + 500 }), false, 'out of order')
  assert.equal(addFix(rec, at(3)), true)
  assert.equal(rec.fixes.length, 2)
  assert.ok(Math.abs(walkedM(rec) - distanceM(rec.fixes[0], rec.fixes[1])) < 0.001)
})

test('a finished walk reports time, distance, a short trace, and how far off the ends were', () => {
  const rec: Recording = { startedAt: T0, fixes: [] }
  for (let i = 1; i <= 120; i++) addFix(rec, at(i))
  const w = finish(rec, T0 + 125_000, { lat: -33.8, lon: 151.15 }, { lat: -33.8 + 121 * 0.0000117, lon: 151.15 })
  assert.equal(w.secs, 125)
  assert.ok(w.trace.length >= 2 && w.trace.length <= 80, `trace points ${w.trace.length}`)
  assert.ok(w.startedNearM !== undefined && w.endedNearM !== undefined)
  assert.ok(w.distanceM > 150 && w.distanceM < 180, `walked ${w.distanceM} m`)
  assert.ok(w.startedNearM! < 5 && w.endedNearM! < 5, 'the readings start and end at the places given')
})

test('a straight line collapses to its ends; a corner is kept', () => {
  const line = Array.from({ length: 50 }, (_, i) => [151.15 + i * 0.00002, -33.8] as [number, number])
  assert.equal(simplifyTrace(line).length, 2)
  const corner = [...Array.from({ length: 20 }, (_, i) => [151.15 + i * 0.00002, -33.8] as [number, number]),
    ...Array.from({ length: 20 }, (_, i) => [151.15 + 19 * 0.00002, -33.8 + (i + 1) * 0.00002] as [number, number])]
  const s = simplifyTrace(corner)
  assert.ok(s.length >= 3 && s.length <= 5, `corner kept: ${s.length}`)
  const wiggly = Array.from({ length: 500 }, (_, i) => [151.15 + i * 0.00001, -33.8 + Math.sin(i / 3) * 0.0001] as [number, number])
  assert.ok(simplifyTrace(wiggly, 80).length <= 80)
  assert.deepEqual(simplifyTrace(line)[0], [151.15, -33.8])
})

test('walks average, newest few only; replacing starts over', () => {
  const stop = { stop: '1', name: 'Epping Rd', walk_s: 1000 }
  const walk = (secs: number) => ({ secs, distanceM: 600, trace: [[151.15, -33.8], [151.151, -33.801]] as [number, number][] })
  let a = withWalk(stop, walk(600), false)
  assert.deepEqual([a.times, a.walk_s], [[600], 600])
  a = withWalk(a, walk(660), false)
  assert.deepEqual([a.times, a.walk_s], [[600, 660], 630])
  for (const s of [610, 620, 630, 640]) a = withWalk(a, walk(s), false)
  assert.equal(a.times?.length, 5, 'only the most recent five count')
  assert.equal(a.times?.[0], 660)
  assert.deepEqual(withWalk(a, walk(500), true).times, [500])
  // Without GPS the earlier trace stays.
  const noGps = withWalk(a, { secs: 700, distanceM: 0, trace: [] }, false)
  assert.deepEqual(noGps.trace, a.trace)
  assert.equal(meanSecs([100, 200]), 150)
  assert.equal(mmss(585), '9:45')
})

const base: Settings = { version: 1, homes: [], gyms: [], transfers: [] }

test('measured walks survive sanitize; junk in them does not', () => {
  const place = { id: 'g', name: 'Gym', lat: -33.8, lon: 151.15 }
  const trace = [[151.15, -33.8], [151.151, -33.801]]
  const s = sanitize({
    ...base,
    gyms: [{ ...place, lines: ['bus 288'], access: [
      { stop: '1', name: 'A', walk_s: 600, times: [590, 610, 'x', -4, 99999], trace },
      { stop: '2', name: 'B', walk_s: 300, trace: [[1, 2]] },
      { stop: '3', name: 'C', walk_s: 300, trace: [[500, 2], [1, 2]] },
    ] }],
  })
  const a = s.gyms[0].access
  assert.deepEqual(a[0].times, [590, 610])
  assert.deepEqual(a[0].trace, trace)
  assert.equal(a[1].trace, undefined, 'a one-point trace is not a route')
  assert.equal(a[2].trace, undefined, 'out-of-range coordinates are dropped')
  assert.deepEqual(sanitize(s), s)
})

test('traces for the map: forward from the start place, reversed into the end place', () => {
  const home = { id: 'h', name: 'Home', lat: 0, lon: 0, access: [{ stop: 'a', name: 'A', walk_s: 1, trace: [[1, 1], [2, 2]] as [number, number][] }] }
  const gym = { id: 'g', name: 'Gym', lat: 0, lon: 0, access: [{ stop: 'b', name: 'B', walk_s: 1, trace: [[3, 3], [4, 4]] as [number, number][] }] }
  const t = walkTraces(home, gym)
  assert.deepEqual(t.from, { a: [[1, 1], [2, 2]] })
  assert.deepEqual(t.to, { b: [[4, 4], [3, 3]] })
  assert.deepEqual(gym.access[0].trace, [[3, 3], [4, 4]], 'the stored trace is not reversed in place')
})
