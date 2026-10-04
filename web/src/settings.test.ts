import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  applyFragment, emptySettings, gymFromKnown, isLine, withSuggested, load, parseFragment, placeRequest, planPlace, prefs, sanitize, save, settingsLink,
  type Settings,
} from './settings.ts'
import { addDays, countdown, dayLabel, dayOf, delay, duration, fromLocalInput, ordinal, platform, roundUp, shortDuration, toLocalInput } from './format.ts'

const TOKEN = 'abcdefghijklmnopqrstuvwxyz0123456789_-ABCDEF'

const sample: Settings = {
  version: 1,
  token: TOKEN,
  homes: [{ id: 'h1', name: 'Home', lat: -33.8, lon: 151.1, access: [{ stop: '2077', name: 'Some Station', walk_s: 600 }] }],
  gyms: [
    {
      id: 'g1', name: '9 Degrees Lane Cove', address: '1a/21 Mars Rd, Lane Cove West', lat: -33.80795, lon: 151.15063,
      access: [{ stop: '206638', name: 'Mars Rd Stop', walk_s: 300 }], lines: ['train T9', 'metro M1', 'bus 288'],
    },
  ],
  activeHome: 'h1',
  walkSpeedMps: 1.4,
  risk: { safe_s: 120, tight_s: 30 },
  walks: [
    { kind: 'change', from: ['2121'], to: ['2121'], label: 'Change at Epping', times: [150] },
    { kind: 'access', place: 'home:h1', stop: ['2077'], label: 'Home – Some Station', times: [500, 520], trace: [[151.1, -33.8], [151.11, -33.81]] },
  ],
}

test('sanitize keeps valid data and drops malformed entries', () => {
  const dirty = {
    ...sample,
    token: 'short',
    homes: [...sample.homes, { id: 'x', name: 'Bad', lat: 999, lon: 0 }, 'nope'],
    walkSpeedMps: 99,
    risk: { safe_s: 10, tight_s: 60 },
    walks: [{ kind: 'change', from: ['a'], to: ['b'], label: 'x', times: [-5] }, { kind: 'nope' }, 'x'],
    extra: '<script>',
  }
  const s = sanitize(dirty)
  assert.equal(s.token, undefined)
  assert.equal(s.homes.length, 1)
  assert.equal(s.walkSpeedMps, undefined)
  assert.equal(s.risk, undefined)
  assert.deepEqual(s.walks, [])
  assert.equal('extra' in s, false)
  assert.deepEqual(sanitize(null), emptySettings())
  assert.deepEqual(sanitize(sample), sample)
})

test('gyms keep their lines and drop malformed ones', () => {
  const s = sanitize({
    ...sample,
    gyms: [
      { ...sample.gyms[0], lines: ['bus 288', 'bus 288', 'tram 1', 'bus', 'bus ', 42, 'light-rail L4', 'x'.repeat(50)] },
      { id: 'bad', name: 'No place' },
      'nope',
    ],
  })
  assert.equal(s.gyms.length, 1)
  assert.deepEqual(s.gyms[0].lines, ['bus 288', 'light-rail L4'])
  assert.equal(sanitize({ ...sample, gyms: undefined }).gyms.length, 0, 'old backups have no gyms')
  assert.equal(sanitize({ ...sample, gyms: Array.from({ length: 30 }, (_, i) => ({ ...sample.gyms[0], id: `g${i}` })) }).gyms.length, 12)
})

test('isLine accepts what the server writes', () => {
  for (const ok of ['bus 288', 'train T9', 'metro M1', 'light-rail L4', 'bus 160X']) assert.equal(isLine(ok), true, ok)
  for (const bad of ['', 'bus', 'tram 1', 'bus  ', 'Bus 288', 5]) assert.equal(isLine(bad), false, String(bad))
})

test('a built-in gym keeps its curated lines and walks, with a fresh id', () => {
  const k = { id: 'lanecove', name: 'Gym', lat: -33.8, lon: 151.1, lines: ['bus 288'], access: [{ stop: '1', name: 'Mars Rd', walk_s: 90 }] }
  const g = gymFromKnown(k)
  assert.deepEqual(g.lines, ['bus 288'])
  assert.deepEqual(g.access, [{ stop: '1', name: 'Mars Rd', walk_s: 90 }])
  assert.equal(g.ref, 'lanecove')
  assert.ok(g.id.length > 0)
  assert.notEqual(g.lines, k.lines, 'must be a copy')
})

test('suggested lines are added to the gym end, never replacing it', () => {
  const g = { ...sample.gyms[0], lines: ['bus 288'] }
  const r = {
    windows: [], itineraries: [],
    lines: [
      { line: 'train T4', share: 1, recommended: true },
      { line: 'bus 288', share: 0.9, recommended: true },
      { line: 'bus 530', share: 0.16, recommended: false },
      { line: 'train ', share: 1, recommended: true },
    ],
  }
  const out = withSuggested(g, r, 'h1')
  assert.deepEqual(out.lines, ['bus 288', 'train T4'])
  assert.equal(out.homeId, 'h1')
})

test('gym ref and homeId survive sanitize', () => {
  const s = sanitize({ ...sample, gyms: [{ ...sample.gyms[0], ref: 'lanecove', homeId: 'h1' }] })
  assert.equal(s.gyms[0].ref, 'lanecove')
  assert.equal(s.gyms[0].homeId, 'h1')
})

test('activeHome falls back to the first home', () => {
  assert.equal(sanitize({ ...sample, activeHome: 'missing' }).activeHome, 'h1')
})

test('settings link round-trips through the fragment, compressed', async () => {
  const big = { ...sample, homes: [{ ...sample.homes[0], access: Array.from({ length: 8 }, (_, i) => ({ stop: `21212${i}`, name: 'Central Station', walk_s: 1140 })) }] }
  const link = await settingsLink('https://gym.example.com', big)
  assert.ok(link.startsWith('https://gym.example.com/#z='))
  assert.ok(link.length < JSON.stringify(big).length, 'compressed link should be shorter than the JSON')
  const f = await parseFragment(new URL(link).hash)
  assert.ok(f && f.kind === 'settings')
  assert.deepEqual(f.settings, big)
  assert.equal(await parseFragment('#z=notdeflate'), null)
})

test('setup link carries just the token', async () => {
  const f = await parseFragment(`#setup=${TOKEN}`)
  assert.deepEqual(f, { kind: 'token', token: TOKEN })
  const s = applyFragment(sample, f!)
  assert.equal(s.homes.length, 1, 'token link keeps existing settings')
  assert.equal(await parseFragment('#setup=short'), null)
  assert.equal(await parseFragment('#s=!!!notbase64'), null)
  assert.equal(await parseFragment(''), null)
})

test('settings link without a token keeps the current token', async () => {
  const { token: _, ...noToken } = sample
  const f = (await parseFragment(new URL(await settingsLink('https://x', { ...noToken })).hash))!
  assert.equal(applyFragment({ ...emptySettings(), token: TOKEN }, f).token, TOKEN)
})

test('load/save survive broken storage', () => {
  const store = new Map<string, string>()
  const storage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) }
  assert.ok(save(storage, sample))
  assert.deepEqual(load(storage), sample)
  assert.deepEqual(load({ getItem: () => '{broken' }), emptySettings())
  assert.deepEqual(load({ getItem: () => { throw new Error('denied') } }), emptySettings())
  assert.equal(save({ setItem: () => { throw new Error('quota') } }, sample), false)
})

test('requests carry curated access and preferences', () => {
  assert.deepEqual(placeRequest(sample.homes[0]), { lat: -33.8, lon: 151.1, access: [{ stop: '2077', walk_s: 600 }] })
  assert.deepEqual(placeRequest({ ...sample.homes[0], access: [] }), { lat: -33.8, lon: 151.1 })
  assert.deepEqual(prefs(sample), {
    walk_speed_mps: 1.4, risk: { safe_s: 120, tight_s: 30 },
    transfers: [{ from: '2121', to: '2121', secs: 150 }],
  })
  // A home's timed walks go with it; suggestions (placeRequest) never carry them.
  assert.deepEqual(planPlace('home', sample.homes[0], sample.walks).walks, [{ stop: '2077', walk_s: 510 }])
  assert.equal(planPlace('gym', { ...sample.gyms[0], ref: 'lane-cove' }, sample.walks).walks, undefined)
})

test('walks timed with older versions move to the walk list', () => {
  const trace = [[151.15, -33.8], [151.151, -33.801]]
  const old = {
    version: 1, token: TOKEN,
    homes: [{ id: 'h1', name: 'Home', lat: -33.8, lon: 151.1, access: [
      { stop: 'p1', name: 'Epping Station', walk_s: 600, times: [590, 610], trace },
      { stop: 'p2', name: 'Epping Station', walk_s: 600, times: [590, 610], trace },
      { stop: 'b1', name: 'Bus stop', walk_s: 120 },
    ] }],
    gyms: [{ ...sample.gyms[0], ref: 'lane-cove', access: [{ stop: 's', name: 'Mars Rd', walk_s: 300, times: [300, 'x', -1] }] }],
    transfers: [{ from: '2121', to: '2122', secs: 150, label: 'Epping' }],
  }
  const s = sanitize(old)
  assert.deepEqual(s.homes[0].access[0], { stop: 'p1', name: 'Epping Station', walk_s: 600 }, 'the stop keeps only its typed time')
  assert.deepEqual(s.walks, [
    { kind: 'access', place: 'home:h1', stop: ['p1', 'p2'], label: 'Home – Epping Station', times: [590, 610], trace },
    { kind: 'access', place: 'gym:lane-cove', stop: ['s'], label: '9 Degrees Lane Cove – Mars Rd', times: [300] },
    { kind: 'change', from: ['2121'], to: ['2122'], label: 'Epping', times: [150] },
  ])
  assert.deepEqual(sanitize(s), s, 'migrating twice changes nothing')
  assert.deepEqual(sanitize({ ...old, walks: s.walks }).walks, s.walks, 'already-migrated walks are not duplicated')
})

test('formatting', () => {
  assert.equal(duration(45 * 60), '45 min')
  assert.equal(duration(65 * 60), '1 h 5 min')
  assert.equal(duration(120 * 60), '2 h')
  const now = Date.parse('2026-10-08T05:00:00Z')
  assert.equal(countdown('2026-10-08T16:06:00+11:00', now), 'in 6 min')
  assert.equal(countdown('2026-10-08T16:00:30+11:00', now), 'now')
  assert.equal(countdown('2026-10-08T15:57:00+11:00', now), 'left 3 min ago')
  assert.equal(delay(0), 'on time')
  assert.equal(delay(125), '2 min late')
  assert.equal(delay(-60), '1 min early')
  assert.equal(delay(undefined), '')
  assert.equal(platform({ name: 'Epping Station, Platform 1', station: 'Epping Station' }), 'Platform 1')
  assert.equal(platform({ name: 'Epping Rd At Rivett Rd' }), '')
})

test('datetime-local conversion uses Sydney time across daylight saving', () => {
  assert.equal(toLocalInput(new Date('2026-10-08T05:30:00Z')), '2026-10-08T16:30') // AEDT +11
  assert.equal(toLocalInput(new Date('2026-07-01T06:30:00Z')), '2026-07-01T16:30') // AEST +10
  assert.equal(fromLocalInput('2026-10-08T16:30'), '2026-10-08T16:30:00+11:00')
  assert.equal(fromLocalInput('2026-07-01T16:30'), '2026-07-01T16:30:00+10:00')
  assert.equal(roundUp('2026-10-08T16:31'), '2026-10-08T16:35')
  assert.equal(roundUp('2026-10-08T23:58'), '2026-10-09T00:00')
  assert.equal(shortDuration(80 * 60), '80m')
})

test('days, the Australian way', () => {
  assert.deepEqual([1, 2, 3, 4, 11, 12, 13, 21, 22, 23, 31].map(ordinal), ['1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '23rd', '31st'])
  assert.equal(addDays('2026-10-31', 1), '2026-11-01')
  assert.equal(dayLabel('2026-10-04', '2026-10-04'), 'Today (4th)')
  assert.equal(dayLabel('2026-10-04', '2026-10-05'), 'Tomorrow (5th)')
  assert.equal(dayLabel('2026-10-04', '2026-10-06'), 'Tuesday (6th)')
  const now = Date.parse('2026-10-04T15:00:00+11:00')
  assert.equal(dayOf('2026-10-04T23:30:00+11:00', now), '')
  assert.equal(dayOf('2026-10-05T00:10:00+11:00', now), 'tomorrow')
  assert.equal(dayOf('2026-10-06T08:00:00+11:00', now), 'Tuesday 6th')
})
