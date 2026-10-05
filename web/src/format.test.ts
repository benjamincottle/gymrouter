import { test } from 'node:test'
import assert from 'node:assert/strict'
import { orList } from './format.ts'

test('orList joins names for a sentence', () => {
  assert.equal(orList(['23T4']), '23T4')
  assert.equal(orList(['20T4', '23T4']), '20T4 or 23T4')
  assert.equal(orList(['20T4', '23T4', '28T4']), '20T4, 23T4 or 28T4')
})
