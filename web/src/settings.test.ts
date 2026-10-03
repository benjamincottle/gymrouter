import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  applyFragment, emptySettings, homePlace, load, parseFragment, prefs, sanitize, save, setTransfer, settingsLink,
  type Settings,
} from './settings.ts'
import { countdown, delay, duration, fromLocalInput, platform, toLocalInput } from './format.ts'

const TOKEN = 'abcdefghijklmnopqrstuvwxyz0123456789_-ABCDEF'

const sample: Settings = {
  version: 1,
  token: TOKEN,
  homes: [{ id: 'h1', name: 'Home', lat: -33.8, lon: 151.1, access: [{ stop: '2077', name: 'Some Station', walk_s: 600 }] }],
  activeHome: 'h1',
  walkSpeedMps: 1.4,
  risk: { safe_s: 120, tight_s: 30 },
  transfers: [{ from: '2121', to: '2121', secs: 150, label: 'Epping' }],
}

test('sanitize keeps valid data and drops malformed entries', () => {
  const dirty = {
    ...sample,
    token: 'short',
    homes: [...sample.homes, { id: 'x', name: 'Bad', lat: 999, lon: 0 }, 'nope'],
    walkSpeedMps: 99,
    risk: { safe_s: 10, tight_s: 60 },
    transfers: [{ from: 'a', to: 'b', secs: -5, label: 'x' }],
    extra: '<script>',
  }
  const s = sanitize(dirty)
  assert.equal(s.token, undefined)
  assert.equal(s.homes.length, 1)
  assert.equal(s.walkSpeedMps, undefined)
  assert.equal(s.risk, undefined)
  assert.deepEqual(s.transfers, [])
  assert.equal('extra' in s, false)
  assert.deepEqual(sanitize(null), emptySettings())
  assert.deepEqual(sanitize(sample), sample)
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
  assert.deepEqual(homePlace(sample.homes[0]), { lat: -33.8, lon: 151.1, access: [{ stop: '2077', walk_s: 600 }] })
  assert.deepEqual(homePlace({ ...sample.homes[0], access: [] }), { lat: -33.8, lon: 151.1 })
  assert.deepEqual(prefs(sample), {
    walk_speed_mps: 1.4, risk: { safe_s: 120, tight_s: 30 }, transfers: [{ from: '2121', to: '2121', secs: 150 }],
  })
  const s = setTransfer(sample, { from: '2121', to: '2121', secs: 90, label: 'Epping' })
  assert.equal(s.transfers.length, 1)
  assert.equal(s.transfers[0].secs, 90)
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
})
