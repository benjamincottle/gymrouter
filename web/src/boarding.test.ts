import { test } from 'node:test'
import assert from 'node:assert/strict'
import { boarding, whereAt, type Fix, type Sighting } from './boarding.ts'

// Positions along a road running north from a stop, in metres.
const m = 1 / 111_320
const fix = (y: number, t: number, accuracy = 10): Fix => ({ lat: -33.8 + y * m, lon: 151.1, accuracy, t: t * 1000 })
const bus = (y: number, t: number): Sighting => ({ lat: -33.8 + y * m, lon: 151.1, t: t * 1000 })

test('where you were when the vehicle reported in', () => {
  const h = [fix(0, 0), fix(100, 10)]
  assert.ok(Math.abs((whereAt(h, 5000)!.lat - (-33.8)) / m - 50) < 0.5, 'half way between fixes')
  assert.equal(whereAt(h, 60_000), null, 'nothing near that time')
})

test('aboard: with the vehicle at two reports, and it moved between them', () => {
  // Waiting at the stop until t=60, then moving north with the bus at 8 m/s.
  const h: Fix[] = [fix(0, 0), fix(0, 30), fix(0, 60), fix(80, 70), fix(160, 80), fix(240, 90)]
  assert.equal(boarding(h, [bus(0, 45), bus(0, 58)]), null, 'the bus sitting at the stop beside you proves nothing')
  assert.equal(boarding(h, [bus(0, 58), bus(160, 80)]), 'aboard')
})

test('left without you: it moved off and away while you stayed put', () => {
  const h: Fix[] = [fix(0, 0), fix(2, 30), fix(1, 60), fix(3, 75), fix(2, 90)]
  assert.equal(boarding(h, [bus(0, 58), bus(220, 85)]), 'left-without-you')
  // Walking away from the stop while the bus goes the other way isn't "missed it": it's not that sure.
  const walking: Fix[] = [fix(0, 58), fix(-120, 85)]
  assert.equal(boarding(walking, [bus(0, 58), bus(220, 85)]), null)
})

test('vague fixes count their accuracy', () => {
  const h: Fix[] = [fix(0, 58, 60), fix(260, 85, 60)]
  assert.equal(boarding(h, [bus(30, 58), bus(200, 85)]), 'aboard')
})
