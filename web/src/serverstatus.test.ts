import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ApiError, AuthError } from './api.ts'
import { summarise } from './serverstatus.ts'

test('summarise puts the server state into words', () => {
  assert.deepEqual(summarise({ state: 'ok', version: 'dev' }, null), { state: 'ok', title: 'Server OK', issues: [] })
  assert.deepEqual(summarise({ state: 'warning', issues: ['trackwork', 'new-thing'], version: 'dev' }, null), {
    state: 'warning',
    title: 'Server working, needs a look',
    issues: ["Some trackwork buses can't be matched to their line.", 'Something on the server needs a look.'],
  })
  assert.equal(summarise({ state: 'error', issues: ['no-timetable'], version: 'dev' }, null).title, "Server can't plan")
  assert.equal(summarise(null, null).state, 'checking')
  assert.deepEqual(summarise({ state: 'ok', version: 'dev' }, new ApiError(0, 'down')), { state: 'error', title: "Can't reach the server", issues: [] })
  assert.equal(summarise(null, new AuthError('x')).state, 'checking') // the app handles a revoked token itself
})
