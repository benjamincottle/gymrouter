import { test } from 'node:test'
import assert from 'node:assert/strict'
import { addFix, mmss, pace, type Recording } from './walkmeasure.ts'

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
})

test('a measured time reads as a stopwatch', () => {
  assert.equal(mmss(585), '9:45')
})

test('walking pace counts only the time spent moving', () => {
  // 1.4 m/s north for 90 s, a 30 s wait at a crossing, then 30 s more: pace is 1.4 m/s, not slowed by the wait.
  const rec: Recording = { startedAt: 0, fixes: [] }
  const lat0 = -33.8
  const m = 1 / 111_320 // degrees of latitude per metre (roughly)
  let y = 0
  let t = 0
  const at = () => rec.fixes.push({ lat: lat0 + y * m, lon: 151.1, accuracy: 8, t: t * 1000 })
  for (; t <= 90; t += 5, y += 7) at()
  y -= 7
  for (let w = 0; w < 6; w++) { t += 5; at() }
  for (let k = 0; k < 6; k++) { t += 5; y += 7; at() }
  const p = pace(rec)!
  assert.ok(Math.abs(p.mps - 1.4) < 0.03, `pace ${p.mps}`)
  assert.equal(p.movingS, 120)
  // Too short, or not walking.
  assert.equal(pace({ startedAt: 0, fixes: rec.fixes.slice(0, 6) }), null)
  const drive: Recording = { startedAt: 0, fixes: Array.from({ length: 30 }, (_, i) => ({ lat: lat0 + i * 50 * m, lon: 151.1, accuracy: 8, t: i * 5000 })) }
  assert.equal(pace(drive), null)
})
