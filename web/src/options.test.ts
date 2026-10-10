import { test } from 'node:test'
import assert from 'node:assert/strict'
import { changesAndStops, position, reselect, rows, stopCount, tape, tripKey } from './options.ts'
import type { Leg, Option } from './types.ts'

const iso = (hhmm: string) => `2026-10-08T${hhmm}:00+11:00`
const stop = (id: string) => ({ id, name: id, lat: -33.8, lon: 151.1 })
const ride = (trip: string, from: string, dep: string, arr: string): Leg => ({
  kind: 'ride', trip_id: trip, line: { mode: 'bus', name: '288' }, from: stop(from), to: stop('Z'), dep: iso(dep), arr: iso(arr),
})
const option = (leave: string, ...rides: Leg[]): Option => ({
  leave_at: iso(leave), arrive: rides[rides.length - 1].arr, duration_s: 0, rides: rides.length, risk: 'safe', lines: [],
  legs: [{ kind: 'walk', to: rides[0].from, dep: iso(leave), arr: rides[0].dep }, ...rides], transfers: [],
})

test('a chosen option is found again when its live times move', () => {
  const before = [option('08:00', ride('t1', 'A', '08:05', '08:30')), option('08:10', ride('t2', 'A', '08:15', '08:40'))]
  const key = tripKey(before[1])
  const after = [option('08:01', ride('t1', 'A', '08:06', '08:31')), option('08:12', ride('t2', 'A', '08:17', '08:42'))]
  assert.equal(reselect(after, key, Date.parse(before[1].leave_at)), 1)
})

test('when the chosen option has gone, the one leaving closest to it is picked', () => {
  const opts = [option('08:00', ride('t1', 'A', '08:05', '08:30')), option('08:20', ride('t3', 'A', '08:25', '08:50'))]
  assert.equal(reselect(opts, 'gone@A', Date.parse(iso('08:18'))), 1)
  assert.equal(reselect([], 'gone@A', 0), 0)
})

test('the same vehicle boarded at a different stop is a different option', () => {
  assert.notEqual(tripKey(option('08:00', ride('t1', 'A', '08:05', '08:30'))), tripKey(option('08:00', ride('t1', 'B', '08:07', '08:30'))))
})

test('stops and changes summary', () => {
  const a = { ...ride('t1', 'A', '08:05', '08:30'), stops: 7 }
  const b = { ...ride('t2', 'B', '08:35', '08:50'), stops: 1 }
  assert.equal(stopCount(option('08:00', a, b)), 8)
  assert.equal(changesAndStops(option('08:00', a, b)), '1 change, 8 stops')
  assert.equal(changesAndStops(option('08:00', b)), 'no changes, 1 stop')
})

test('the description rows and where you are on them', () => {
  const a = ride('t1', 'A', '08:05', '08:30')
  const b = { ...ride('t2', 'B', '08:35', '08:50'), from: stop('B') }
  const o = { ...option('08:00', a, b), transfers: [{ from_leg: 1, to_leg: 2, walk_s: 120, slack_s: 180, risk: 'safe' as const }] }
  const rs = rows(o)
  assert.deepEqual(rs.map((r) => r.kind), ['leg', 'leg', 'change', 'leg', 'arrive'])
  const t = (hhmm: string) => Date.parse(iso(hhmm))
  assert.deepEqual(position(rs, t('07:50')), { row: 0, frac: 0 }, 'before leaving: the top')
  assert.deepEqual(position(rs, t('08:03')), { row: 0, frac: 0.6 }, 'walking to the stop')
  assert.deepEqual(position(rs, t('08:04')), { row: 0, frac: 0.8 })
  assert.deepEqual(position(rs, t('08:30')), { row: 2, frac: 0 }, 'off the first vehicle: the change')
  assert.deepEqual(position(rs, t('08:42')), { row: 3, frac: 7 / 15 })
  assert.deepEqual(position(rs, t('09:30')), { row: 4, frac: 0 }, 'arrived')
})

test('during a trip the rail starts where you set off, and that is where you are until you leave', () => {
  const o = option('08:00', ride('t1', 'A', '08:05', '08:30'))
  const rs = rows(o, true)
  assert.deepEqual(rs.map((r) => r.kind), ['start', 'leg', 'leg', 'arrive'])
  const t = (hhmm: string) => Date.parse(iso(hhmm))
  assert.deepEqual(position(rs, t('07:40')), { row: 0, frac: 0 })
  assert.deepEqual(position(rs, t('08:02')), { row: 1, frac: 0.4 }, 'out the door: on the walk')
})

test('the tape: rides and the walking around them, each with its share of the trip', () => {
  // 08:00 leave, walk 5, ride 25, change 5, ride 15, walk 10: 60 min.
  const a = ride('t1', 'A', '08:05', '08:30')
  const b = ride('t2', 'B', '08:35', '08:50')
  const o = { ...option('08:00', a, b), arrive: iso('09:00') }
  o.legs.push({ kind: 'walk', from: b.to, dep: iso('08:50'), arr: iso('09:00') })
  const shares = (x: Option) => tape(x).map((p) => `${p.kind} ${Math.round(p.share * 60)}`)
  assert.deepEqual(shares(o), ['walk 5', 'ride 25', 'walk 5', 'ride 15', 'walk 10'])
  assert.equal(tape(o).reduce((n, p) => n + p.share, 0), 1)
  // A wait at the stop is part of the walking before the ride; two rides always have a run between them.
  const tight = { ...option('08:00', ride('t1', 'A', '08:10', '08:30'), ride('t2', 'B', '08:30', '08:50')) }
  tight.legs[0].arr = iso('08:04')
  assert.deepEqual(shares({ ...tight, arrive: iso('09:00') }), ['walk 10', 'ride 20', 'walk 0', 'ride 20', 'walk 10'])
  // Straight off the last ride at the door: no walk at the end. On board already: none at the start.
  assert.deepEqual(tape(option('08:05', ride('t1', 'A', '08:05', '08:30'))).map((p) => p.kind), ['ride'])
})
