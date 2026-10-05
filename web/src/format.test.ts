import { test } from 'node:test'
import assert from 'node:assert/strict'
import { distance, orList } from './format.ts'

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
