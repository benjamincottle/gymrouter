import { test } from 'node:test'
import assert from 'node:assert/strict'
import { ApiError, AuthError } from './api.ts'
import { details, outdated, summarise } from './serverstatus.ts'
import { ago, dayName } from './format.ts'

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

test('the status in full: a row for everything the server reported', () => {
  const rows = details({
    state: 'warning', issues: ['trackwork', 'basemap'], version: 'e555a5b', service_date: '2026-10-10', static_age_s: 772,
    polling_active: true, upstream_requests_today: 1076,
    feeds: [{ name: 'sydneytrains', trip_updates_age_s: 3, vehicles_age_s: 15 }, { name: 'metro' }, { name: 'buses', error: 'upstream answered 503' }, { name: 'new-feed', vehicles_age_s: 200 }],
    missing_lines: ['bus 287', 'bus 612X'], unknown_trackwork: ['replacement-bus 6SH'],
    realtime: { updates: 651, matched: 640, matched_by_run: 10, added: 1, cancelled: 2, empty: 0, unmatched: 0 },
    data: { walk_ready: true, walk_age_s: 742, map_ready: false, map_error: 'pmtiles tool not found' },
  })
  assert.deepEqual(rows, [
    { label: 'Timetable', value: 'For Saturday 10th, downloaded 13 min ago' },
    { label: 'Live data', value: 'Updating while the app is in use' },
    { label: 'Trains', value: 'Times 3 s ago, vehicles 15 s ago' },
    { label: 'Metro', value: 'Nothing fetched yet' },
    { label: 'Buses', value: 'upstream answered 503', tone: 'caution' },
    { label: 'new-feed', value: 'vehicles 3 min ago' },
    { label: 'Live services', value: '650 of 651 matched to the timetable, 1 extra, 2 cancelled' },
    { label: 'Requests to TfNSW', value: '1,076 today' },
    { label: 'Not running today', value: 'bus 287, bus 612X' },
    { label: 'Trackwork buses left out', value: 'replacement-bus 6SH', tone: 'caution' },
    { label: 'Street map', value: 'Ready, built 12 min ago' },
    { label: 'Map', value: "Couldn't be downloaded: pmtiles tool not found", tone: 'caution' },
  ])
  // An older server, or one that hasn't loaded anything: only what it said.
  assert.deepEqual(details({ state: 'error', version: 'dev' }), [
    { label: 'Timetable', value: 'Not loaded yet', tone: 'bad' },
    { label: 'Live data', value: 'Paused until the app is used' },
  ])
  assert.deepEqual(details({ state: 'warning', version: 'dev', service_date: '2026-10-08', static_age_s: 200_000, static_error: 'timed out' }).slice(0, 2), [
    { label: 'Timetable', value: 'The last download failed: timed out', tone: 'caution' },
    { label: 'Timetable in use', value: 'For Thursday 8th, downloaded 2 days ago' },
  ])
})

test('ages and day names', () => {
  assert.deepEqual([0, 89, 90, 5399, 5400, 172_799, 172_800].map(ago), ['0 s ago', '89 s ago', '2 min ago', '90 min ago', '2 h ago', '48 h ago', '2 days ago'])
  assert.equal(dayName('2026-10-10'), 'Saturday 10th')
})

test('outdated notices the server moving to another version after the page loaded', () => {
  assert.equal(outdated(undefined), '') // nothing heard yet
  assert.equal(outdated('1b7180f'), '') // the version this page came with
  assert.equal(outdated('1b7180f'), '')
  assert.equal(outdated('df80f6c'), '1b7180f') // deployed since: this page is the older one
  assert.equal(outdated('1b7180f'), '') // rolled back to what's open
})
