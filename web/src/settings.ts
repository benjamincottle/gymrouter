// Device-side settings: everything personal lives here, never on the server.

import { distance } from './format.ts'
import type { KnownGym, PlaceRequest, PlanRequest, SuggestResult } from './types.ts'
import { changeTimes, learnedPace, placeKey, placeWalks, sanitizeWalks, type PlaceRef, type Retime, type TimedWalk } from './walks.ts'

export interface AccessStop {
  stop: string
  name: string
  walk_s: number // the street map's walking time (a walk timed during a trip beats it; see walks.ts)
}

/** A place on the map: curated stops with measured walk times (empty = every stop within the walking limit). */
export interface Place {
  id: string
  name: string
  lat: number
  lon: number
  access: AccessStop[]
}

export type Home = Place

/** A destination, with the lines that could matter for getting there from home. */
export interface Gym extends Place {
  address?: string
  lines: string[] // e.g. "bus 288", "train T9"
  ref?: string // the built-in gym this was added from
  homeId?: string // the home whose nearby lines were last suggested for it
  uses?: number // how many times it has been chosen to plan a trip: the most used are listed first
}

/** A "Longer walk" notice that was dismissed: the walk between a place and a gym's lines, at the distance it said. */
export interface DismissedWalk {
  gym: string // the gym's id: the walk is to its lines
  place: string // the end the walk is from (see placeKey)
  m: number
}

export interface Settings {
  version: 1
  token?: string
  homes: Home[]
  gyms: Gym[]
  activeHome?: string
  walkSpeedMps?: number
  minChangeS?: number
  maxWalkM?: number
  leaveBufferS?: number
  risk?: { safe_s: number; tight_s: number }
  walks: TimedWalk[] // walks and changes timed during trips
  retime?: Retime // timing a walk again: average with the earlier ones (default) or replace them
  dismissedWalks?: DismissedWalk[] // "Longer walk" notices not to show again
  theme?: 'light' | 'dark' // unset: follow the system
}


export const STORAGE_KEY = 'gymrouter.settings'

export function emptySettings(): Settings {
  return { version: 1, homes: [], gyms: [], walks: [] }
}

const isNum = (v: unknown, min: number, max: number): v is number =>
  typeof v === 'number' && Number.isFinite(v) && v >= min && v <= max
const isStr = (v: unknown, max = 200): v is string => typeof v === 'string' && v.length <= max

export const MAX_GYMS = 12
export const MAX_LINES = 60
const MAX_DISMISSED = 200

// Replacement buses aren't chosen: they come with the train line they stand in for.
export const LINE_MODES = ['train', 'metro', 'light-rail', 'bus', 'ferry', 'regional-train', 'coach'] as const

/** A line key as the server writes it: "<mode> <name>", e.g. "bus 288". */
export function isLine(v: unknown): v is string {
  if (typeof v !== 'string' || v.length > 40) return false
  const i = v.indexOf(' ')
  return i > 0 && v.slice(i + 1).trim() !== '' && (LINE_MODES as readonly string[]).includes(v.slice(0, i))
}

function sanitizePlace(input: unknown): Place | null {
  if (typeof input !== 'object' || input === null) return null
  const r = input as Record<string, unknown>
  if (!isStr(r.id, 40) || !isStr(r.name, 60) || !isNum(r.lat, -90, 90) || !isNum(r.lon, -180, 180)) return null
  const access: AccessStop[] = []
  if (Array.isArray(r.access)) {
    for (const a of r.access.slice(0, 20)) {
      const x = a as Record<string, unknown>
      if (x && isStr(x.stop, 40) && isStr(x.name, 120) && isNum(x.walk_s, 0, 3600)) {
        access.push({ stop: x.stop, name: x.name, walk_s: Math.round(x.walk_s) })
      }
    }
  }
  return { id: r.id, name: r.name, lat: r.lat, lon: r.lon, access }
}

/** Validates untrusted settings (from storage, an import file or a link), dropping anything malformed. */
export function sanitize(input: unknown): Settings {
  const out = emptySettings()
  if (typeof input !== 'object' || input === null) return out
  const s = input as Record<string, unknown>
  if (isStr(s.token, 200) && /^[A-Za-z0-9_-]{32,}$/.test(s.token)) out.token = s.token
  out.walks = sanitizeWalks(s.walks)
  if (Array.isArray(s.homes)) {
    for (const h of s.homes.slice(0, 10)) {
      const p = sanitizePlace(h)
      if (p) out.homes.push(p)
    }
  }
  if (Array.isArray(s.gyms)) {
    for (const g of s.gyms.slice(0, MAX_GYMS)) {
      const p = sanitizePlace(g)
      if (!p) continue
      const r = g as Record<string, unknown>
      const ls = Array.isArray(r.lines) ? r.lines.filter(isLine).slice(0, MAX_LINES) : []
      out.gyms.push({
        ...p,
        ...(isStr(r.address, 200) && r.address ? { address: r.address } : {}),
        lines: [...new Set(ls)],
        ...(isStr(r.ref, 40) && r.ref ? { ref: r.ref } : {}),
        ...(isStr(r.homeId, 40) && r.homeId ? { homeId: r.homeId } : {}),
        ...(isNum(r.uses, 1, 1e6) ? { uses: Math.round(r.uses) } : {}),
      })
    }
  }
  if (isStr(s.activeHome, 40) && out.homes.some((h) => h.id === s.activeHome)) out.activeHome = s.activeHome
  else if (out.homes.length > 0) out.activeHome = out.homes[0].id
  if (isNum(s.walkSpeedMps, 0.3, 3)) out.walkSpeedMps = s.walkSpeedMps
  if (isNum(s.minChangeS, 0, 900)) out.minChangeS = Math.round(s.minChangeS)
  if (isNum(s.maxWalkM, 100, 2000)) out.maxWalkM = Math.round(s.maxWalkM)
  if (isNum(s.leaveBufferS, 0, 1800)) out.leaveBufferS = Math.round(s.leaveBufferS)
  if (typeof s.risk === 'object' && s.risk !== null) {
    const r = s.risk as Record<string, unknown>
    if (isNum(r.safe_s, 0, 1800) && isNum(r.tight_s, 0, 1800) && r.tight_s <= r.safe_s) {
      out.risk = { safe_s: Math.round(r.safe_s), tight_s: Math.round(r.tight_s) }
    }
  }
  if (s.retime === 'average' || s.retime === 'replace') out.retime = s.retime
  if (Array.isArray(s.dismissedWalks)) {
    const ds: DismissedWalk[] = []
    for (const d of s.dismissedWalks.slice(0, MAX_DISMISSED)) {
      const r = d as Record<string, unknown>
      if (r && isStr(r.gym, 40) && isStr(r.place, 60) && isNum(r.m, 1, 100_000)) ds.push({ gym: r.gym, place: r.place, m: Math.round(r.m) })
    }
    const live = dismissedWalks({ ...out, dismissedWalks: ds })
    if (live.length > 0) out.dismissedWalks = live
  }
  if (s.theme === 'light' || s.theme === 'dark') out.theme = s.theme
  return out
}

export function load(storage: Pick<Storage, 'getItem'> | undefined): Settings {
  try {
    const raw = storage?.getItem(STORAGE_KEY)
    return raw ? sanitize(JSON.parse(raw)) : emptySettings()
  } catch {
    return emptySettings()
  }
}

export function save(storage: Pick<Storage, 'setItem'> | undefined, s: Settings): boolean {
  try {
    storage?.setItem(STORAGE_KEY, JSON.stringify(s))
    return true
  } catch {
    return false
  }
}

// --- Links. Data lives in the URL fragment, which browsers never send to the server. ---

function fromBase64Url(b64: string): string {
  const bin = atob(b64.replace(/-/g, '+').replace(/_/g, '/'))
  return new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)))
}

function bytesToBase64Url(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function base64UrlToBytes(b64: string): Uint8Array {
  const bin = atob(b64.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

/** Runs bytes through a (de)compression stream, giving up once the output passes maxOut bytes. */
async function pipe(bytes: Uint8Array, stream: CompressionStream | DecompressionStream, maxOut = Infinity): Promise<Uint8Array> {
  const reader = new Blob([bytes as BlobPart]).stream().pipeThrough(stream).getReader()
  const chunks: Uint8Array[] = []
  let size = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    size += value.length
    if (size > maxOut) {
      await reader.cancel()
      throw new Error('too large')
    }
    chunks.push(value)
  }
  const out = new Uint8Array(size)
  let at = 0
  for (const c of chunks) {
    out.set(c, at)
    at += c.length
  }
  return out
}

/** The most a settings link may inflate to: far more than any real settings, far less than a deflate bomb. */
const MAX_SETTINGS_JSON = 100_000

/**
 * A link carrying the full settings (including the token), for backup and setting up another device.
 * The JSON is deflate-compressed so the QR code stays scannable.
 */
export async function settingsLink(origin: string, s: Settings): Promise<string> {
  const packed = await pipe(new TextEncoder().encode(JSON.stringify(s)), new CompressionStream('deflate-raw'))
  return `${origin}/#z=${bytesToBase64Url(packed)}`
}

export type FragmentData = { kind: 'token'; token: string } | { kind: 'settings'; settings: Settings }

/** Parses "#setup=<token>" (from `gymrouter setup-link`), "#z=<compressed settings>" or "#s=<settings>". */
export async function parseFragment(hash: string): Promise<FragmentData | null> {
  const h = hash.startsWith('#') ? hash.slice(1) : hash
  const params = new URLSearchParams(h)
  const tok = params.get('setup')
  if (tok && /^[A-Za-z0-9_-]{32,200}$/.test(tok)) return { kind: 'token', token: tok }
  try {
    const z = params.get('z')
    if (z && z.length < 20000) {
      const raw = await pipe(base64UrlToBytes(z), new DecompressionStream('deflate-raw'), MAX_SETTINGS_JSON)
      const json = new TextDecoder().decode(raw)
      return { kind: 'settings', settings: sanitize(JSON.parse(json)) }
    }
    const data = params.get('s')
    if (data) return { kind: 'settings', settings: sanitize(JSON.parse(fromBase64Url(data))) }
  } catch {
    return null
  }
  return null
}

/** Applies incoming link data: a token replaces the token; settings replace everything. */
export function applyFragment(cur: Settings, f: FragmentData): Settings {
  if (f.kind === 'token') return { ...cur, token: f.token }
  return { ...f.settings, token: f.settings.token ?? cur.token }
}

/**
 * Whether a link would change this device's token or replace what's saved on it. Then the app asks first: anyone can
 * send a link to this origin, and one opened by mistake mustn't log the device out or wipe its homes, gyms and walks.
 */
export function importNeedsConfirm(cur: Settings, f: FragmentData): boolean {
  const token = f.kind === 'token' ? f.token : f.settings.token
  if (cur.token && token && token !== cur.token) return true
  return f.kind === 'settings' && (cur.homes.length > 0 || cur.gyms.length > 0 || cur.walks.length > 0)
}

const count = (n: number, one: string) => (n === 0 ? '' : `${n} ${one}${n === 1 ? '' : 's'}`)

/** What to ask before applying a link that importNeedsConfirm flags. */
/** The title of the sheet that asks before a link changes saved data (importQuestion is its body). */
export function importTitle(f: FragmentData): string {
  return f.kind === 'token' ? 'Use this access token?' : "Replace this device's settings?"
}

export function importQuestion(cur: Settings, f: FragmentData): string {
  if (f.kind === 'token') {
    return "This link changes this device's access token. Only use it if it came from your own server."
  }
  const have = [count(cur.homes.length, 'home'), count(cur.gyms.length, 'gym'), count(cur.walks.length, 'timed walk')].filter(Boolean)
  const token = f.settings.token && cur.token && f.settings.token !== cur.token ? ' and its access token' : ''
  const what = have.length > 0 ? ` (${have.join(', ')})` : ''
  return `This link replaces everything saved on this device${what}${token}. Only use it if you made it.`
}

// --- Requests ---

export function placeRequest(h: Place): PlaceRequest {
  const p: PlaceRequest = { lat: h.lat, lon: h.lon }
  if (h.access.length > 0) p.access = h.access.map((a) => ({ stop: a.stop, walk_s: a.walk_s }))
  return p
}

/** A home or gym as one end of a trip: its stops, plus the walks to stops timed from it. */
export function planPlace(kind: 'home' | 'gym', p: Place & { ref?: string }, walks: TimedWalk[]): PlaceRequest {
  const r = placeRequest(p)
  const w = placeWalks(walks, placeKey(kind, p))
  if (w.length > 0) r.walks = w
  return r
}

export function placeRef(kind: 'home' | 'gym', p: Place & { ref?: string }): PlaceRef {
  return { key: placeKey(kind, p), name: p.name, lat: p.lat, lon: p.lon }
}

/** The pace walks are planned at: the one set in Settings, else the one your timed walks show, else the server's default (undefined). */
export function walkSpeed(s: Pick<Settings, 'walkSpeedMps' | 'walks'>): number | undefined {
  return s.walkSpeedMps ?? learnedPace(s.walks) ?? undefined
}

export function prefs(s: Settings): PlanRequest['prefs'] {
  const p: PlanRequest['prefs'] = {}
  const speed = walkSpeed(s)
  if (speed !== undefined) p.walk_speed_mps = speed
  if (s.minChangeS !== undefined) p.min_change_s = s.minChangeS
  if (s.maxWalkM !== undefined) p.max_walk_m = s.maxWalkM
  if (s.leaveBufferS !== undefined) p.leave_buffer_s = s.leaveBufferS
  if (s.risk) p.risk = s.risk
  const transfers = changeTimes(s.walks)
  if (transfers.length > 0) p.transfers = transfers
  return p
}

/** Gyms with the most used first; until they've been used, in the order they were added. */
export function byUse<T extends { uses?: number }>(gyms: T[]): T[] {
  return gyms.map((g, i) => ({ g, i })).sort((a, b) => (b.g.uses ?? 0) - (a.g.uses ?? 0) || a.i - b.i).map((x) => x.g)
}

/** Forgets how often each gym has been used, so they're listed as added again. */
export function resetGymOrder(s: Settings): Settings {
  return { ...s, gyms: s.gyms.map(({ uses: _, ...g }) => g) }
}

/** Counts a gym as used. */
export function usedGym(s: Settings, id: string): Settings {
  return { ...s, gyms: s.gyms.map((g) => (g.id === id ? { ...g, uses: (g.uses ?? 0) + 1 } : g)) }
}

// --- "Longer walk" notices that have been dismissed ---

/** The dismissed notices that could still come up: their gym and place are still on this device. */
export function dismissedWalks(s: Pick<Settings, 'homes' | 'gyms' | 'dismissedWalks'>): DismissedWalk[] {
  return (s.dismissedWalks ?? []).filter((d) => {
    const g = s.gyms.find((x) => x.id === d.gym)
    return g !== undefined && (d.place === placeKey('gym', g) || s.homes.some((h) => d.place === placeKey('home', h)))
  })
}

/** Whether this notice was dismissed. It shows again once it would say a different distance (the nearest stop changed). */
export function walkDismissed(s: Pick<Settings, 'dismissedWalks'>, gym: string, place: string, m: number): boolean {
  return (s.dismissedWalks ?? []).some((d) => d.gym === gym && d.place === place && distance(d.m) === distance(m))
}

export function dismissWalk(s: Settings, gym: string, place: string, m: number): Settings {
  const others = dismissedWalks(s).filter((d) => !(d.gym === gym && d.place === place))
  return { ...s, dismissedWalks: [...others, { gym, place, m }].slice(-MAX_DISMISSED) }
}

/** Shows every dismissed notice again. */
export function showDismissedWalks(s: Settings): Settings {
  const { dismissedWalks: _, ...rest } = s
  return rest
}

export function newId(): string {
  return Math.random().toString(36).slice(2, 10)
}

/** Adds a built-in gym to this device, with its curated lines and walks. */
export function gymFromKnown(k: KnownGym): Gym {
  return {
    id: newId(), name: k.name, address: k.address, lat: k.lat, lon: k.lon, lines: [...k.lines], ref: k.id,
    access: (k.access ?? []).map((a) => ({ stop: a.stop, name: a.name, walk_s: a.walk_s })),
  }
}

/** Adds the lines a suggestion search recommends to the gym's own, keeping what's already chosen. */
export function withSuggested(g: Gym, r: SuggestResult, homeId: string): Gym {
  const add = r.lines.filter((l) => l.recommended && isLine(l.line)).map((l) => l.line)
  return { ...g, lines: [...new Set([...g.lines, ...add])].slice(0, MAX_LINES), homeId }
}
