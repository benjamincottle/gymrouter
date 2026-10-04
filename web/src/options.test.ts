import { test } from 'node:test'
import assert from 'node:assert/strict'
import { changesAndStops, reselect, stopCount, tripKey } from './options.ts'
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
  assert.equal(changesAndStops(option('08:00', ride('t1', 'A', '08:05', '08:30'))), 'no changes') // older server
})
