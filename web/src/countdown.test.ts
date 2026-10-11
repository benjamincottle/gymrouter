import { test } from 'node:test'
import assert from 'node:assert/strict'
import { callPlaces, stopsPassed, stopsToGo } from './countdown.ts'
import { splitAt, type LonLat } from './progress.ts'
import type { Call } from './types.ts'

const iso = (hhmm: string) => `2026-10-11T${hhmm}:00+11:00`
const ms = (hhmm: string) => Date.parse(iso(hhmm))
// A ride along a straight street: four stops after the one you get on at, the last yours.
const line: LonLat[] = [[151.0, -33.8], [151.04, -33.8]]
const stops: LonLat[] = [[151.01, -33.8], [151.02, -33.8], [151.03, -33.8]]
const calls: Call[] = [{ name: 'A', arr: iso('14:02') }, { name: 'B', arr: iso('14:04') }, { name: 'C', arr: iso('14:06') }, { name: 'D', arr: iso('14:08') }]

test('each call has its place along the line, the last at its end', () => {
  const p = callPlaces(line, stops, calls)!
  assert.deepEqual(p.map((x) => Math.round(x * 100)), [25, 50, 75, 100])
  assert.equal(callPlaces(line, stops.slice(1), calls), null, 'not the stops this ride calls at')
  assert.equal(callPlaces(line, [], []), null)
})

test('stops are behind you once you are past them on the line', () => {
  const p = callPlaces(line, stops, calls)!
  const at = (frac: number) => stopsPassed(calls, p, frac, ms('14:00'), ms('14:00'))
  assert.deepEqual(at(0), { passed: 0, along: 0 })
  assert.equal(at(0.25).passed, 0, 'at a stop, you have not passed it')
  assert.equal(at(0.3).passed, 1)
  assert.equal(Math.round(at(0.375).along * 10), 5, 'half way from A to B')
  assert.equal(at(0.8).passed, 3)
  assert.equal(at(1).passed, 3, 'the last stop is never behind you: you are still on board')
})

test('with nothing to place you by, the times decide', () => {
  const at = (hhmm: string) => stopsPassed(calls, null, null, ms('14:00'), ms(hhmm))
  assert.deepEqual(at('14:01'), { passed: 0, along: 0.5 })
  assert.equal(at('14:02').passed, 1)
  assert.equal(at('14:05').passed, 2)
  assert.equal(at('14:05').along, 0.5)
  assert.equal(at('14:30').passed, 3)
  // A stop the trip skips has no time: it's behind you once the next one is.
  const skip: Call[] = [calls[0], { name: 'B' }, calls[2], calls[3]]
  assert.equal(stopsPassed(skip, null, null, ms('14:00'), ms('14:05')).passed, 1)
  assert.equal(stopsPassed(skip, null, null, ms('14:00'), ms('14:06')).passed, 3)
  // A place on the line without the stops' places (or the other way round) is no use.
  assert.equal(stopsPassed(calls, null, 0.9, ms('14:00'), ms('14:01')).passed, 0)
})

test('stops to go, in words', () => {
  assert.equal(stopsToGo(3), '3 stops to go')
  assert.equal(stopsToGo(1), '1 stop to go')
})

test('a line is cut where you are', () => {
  const [behind, ahead] = splitAt(line, 0.25)
  assert.deepEqual(behind.map((c) => +c[0].toFixed(3)), [151, 151.01])
  assert.deepEqual(ahead.map((c) => +c[0].toFixed(3)), [151.01, 151.04])
  const bent: LonLat[] = [[151.0, -33.8], [151.02, -33.8], [151.02, -33.82]]
  const [b2, a2] = splitAt(bent, 0.9)
  assert.equal(b2.length, 3)
  assert.equal(a2.length, 2)
  assert.deepEqual(splitAt(line, 0)[0], [line[0], line[0]])
  assert.deepEqual(splitAt(line, 1)[1][0], line[1])
})
