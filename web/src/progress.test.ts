import { test } from 'node:test'
import assert from 'node:assert/strict'
import { rows } from './options.ts'
import { locate, onLine, rowLines, shapeKey, type LonLat } from './progress.ts'
import type { Leg, Option } from './types.ts'

// A trip along a straight east–west line: home, walk 300 m to stop A, bus 2 km to stop B, walk 300 m to the gym.
const m = 1 / 111_320
const k = Math.cos((-33.8 * Math.PI) / 180)
const at = (x: number, y = 0): LonLat => [151 + (x * m) / k, -33.8 + y * (1 / 110_540)]
const here = (x: number, y = 0, accuracy = 10) => ({ lon: at(x, y)[0], lat: at(x, y)[1], accuracy })
const stop = (id: string, x: number) => ({ id, name: id, lon: at(x)[0], lat: at(x)[1] })
const iso = (hhmm: string) => `2026-10-08T${hhmm}:00+11:00`
const A = stop('A', 300)
const B = stop('B', 2300)
const legs: Leg[] = [
  { kind: 'walk', to: A, dep: iso('08:00'), arr: iso('08:04') },
  { kind: 'ride', trip_id: 't', line: { mode: 'bus', name: '52' }, from: A, to: B, dep: iso('08:05'), arr: iso('08:15') },
  { kind: 'walk', from: B, dep: iso('08:15'), arr: iso('08:19') },
]
const o: Option = { leave_at: iso('08:00'), arrive: iso('08:19'), duration_s: 0, rides: 1, risk: 'safe', lines: [], legs, transfers: [] }
const rs = rows(o, true) // start, walk, ride, walk, arrive
const lines = rowLines(rs, o, at(0), at(2600), { [shapeKey(legs[1])]: [at(300), at(1300, 40), at(2300)] })

test('distance to a line and how far along it', () => {
  const r = onLine([at(0), at(1000)], here(250, 30))
  assert.ok(Math.abs(r.d - 30) < 1 && Math.abs(r.frac - 0.25) < 0.01, JSON.stringify(r))
  assert.equal(onLine([at(0)], here(0)).d < 1, true)
})

test('you are placed by where you are, not by the clock', () => {
  // Still at home although the clock says you should be on the bus: at home.
  assert.deepEqual(locate(lines, here(0), 2), { row: 0, frac: 0 })
  // Half way to the stop.
  const walking = locate(lines, here(150, 5), 1)!
  assert.equal(walking.row, 1)
  assert.ok(Math.abs(walking.frac - 0.5) < 0.02)
  // At the stop, waiting: the end of the walk, not the start of the ride.
  assert.deepEqual(locate(lines, here(300), 2), { row: 1, frac: 1 })
  // On the bus, part way along its route (which bows north).
  const riding = locate(lines, here(800, 20), 2)!
  assert.equal(riding.row, 2)
  assert.ok(riding.frac > 0.2 && riding.frac < 0.3, String(riding.frac))
  // Arrived.
  assert.equal(locate(lines, here(2600), 3)!.row, 4)
})

test('too vague, or well off the route: no answer (the clock is used)', () => {
  assert.equal(locate(lines, here(150, 0, 250), 1), null)
  assert.equal(locate(lines, here(1000, 2000), 2), null)
})
