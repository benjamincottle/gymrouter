import { test } from 'node:test'
import assert from 'node:assert/strict'
import { boardText, distance, orList } from './format.ts'

test('orList joins names for a sentence', () => {
  assert.equal(orList(['23T4']), '23T4')
  assert.equal(orList(['20T4', '23T4']), '20T4 or 23T4')
  assert.equal(orList(['20T4', '23T4', '28T4']), '20T4, 23T4 or 28T4')
})

test('distance rounds walks for reading', () => {
  assert.equal(distance(812), '800 m')
  assert.equal(distance(20), '50 m')
  assert.equal(distance(980), '1 km')
  assert.equal(distance(1000), '1 km')
  assert.equal(distance(1432), '1.4 km')
  assert.equal(distance(2960), '3 km')
})

test('where to board a train reads as an instruction', () => {
  assert.equal(boardText(1, 8), 'Board the front carriage.')
  assert.equal(boardText(8, 8), 'Board the back carriage.')
  assert.equal(boardText(4, 8), 'Board the 4th carriage from the front.')
  assert.equal(boardText(2, 6), 'Board the 2nd carriage from the front.')
  assert.equal(boardText(3, 10), 'Board the 3rd carriage from the front.')
  assert.equal(boardText(6, 6), 'Board the back carriage.')
})

