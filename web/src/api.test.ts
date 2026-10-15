import { test } from 'node:test'
import assert from 'node:assert/strict'
import { takeEvents } from './api.ts'

test('events are read off the stream as they complete, the rest kept for the next read', () => {
  assert.deepEqual(takeEvents('event: realtime\ndata: {}\n\nevent: real'), [['realtime'], 'event: real'])
  assert.deepEqual(takeEvents('event: realtime\ndata: {"realtime_at":"…"}\n\n'), [['realtime'], ''])
  assert.deepEqual(takeEvents(''), [[], ''])
})

test('keep-alive comments are no event; an event without a name is a message', () => {
  assert.deepEqual(takeEvents(': keep-alive\n\ndata: x\n\n'), [['message'], ''])
})

test('the stream may end its lines with CR LF', () => {
  assert.deepEqual(takeEvents('event: realtime\r\ndata: {}\r\n\r\n'), [['realtime'], ''])
})
