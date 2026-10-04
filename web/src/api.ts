import type { DefaultsResponse, GeocodeResult, NearStop, PlaceRequest, PlanRequest, PlanResponse, SuggestResponse, Vehicle } from './types.ts'

export class AuthError extends Error {}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function call<T>(token: string, method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal,
    cache: 'no-store',
    credentials: 'omit',
    referrerPolicy: 'no-referrer',
  })
  // Without a valid token the server answers like a missing page (plain text); the API's own 404s are JSON.
  if (res.status === 401 || (res.status === 404 && res.headers.get('Content-Type')?.startsWith('text/plain'))) {
    throw new AuthError('This device is not set up (or its access was revoked).')
  }
  if (!res.ok) {
    let msg = `Request failed (${res.status})`
    try {
      const j = await res.json()
      if (typeof j.error === 'string') msg = j.error
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg)
  }
  return (await res.json()) as T
}

const wait = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) return reject(new DOMException('Aborted', 'AbortError'))
    const id = setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => {
      clearTimeout(id)
      reject(new DOMException('Aborted', 'AbortError'))
    })
  })

/**
 * Finds the lines from one place to each of several. The server runs it as a job (it reads the whole timetable, which
 * can outlast a request), so this starts it and polls; `onProgress` gets the fraction done.
 */
async function suggestLines(
  t: string, from: PlaceRequest, to: PlaceRequest[], signal?: AbortSignal, onProgress?: (fraction: number) => void,
): Promise<SuggestResponse> {
  let job: string
  for (let attempt = 0; ; attempt++) {
    try {
      job = (await call<{ job: string }>(t, 'POST', '/api/suggest-lines', { from, to }, signal)).job
      break
    } catch (e) {
      // Another search is running (another device, or a retry): wait for it to finish.
      if (!(e instanceof ApiError && e.status === 429 && attempt < 30)) throw e
      await wait(5000, signal)
    }
  }
  for (let failures = 0; ; ) {
    await wait(1500, signal)
    let s: SuggestJob
    try {
      s = await call<SuggestJob>(t, 'GET', `/api/suggest-lines/${job}`, undefined, signal)
      failures = 0
    } catch (e) {
      // A dropped connection on a phone shouldn't lose a search that's still running on the server.
      if (signal?.aborted || e instanceof AuthError || (e instanceof ApiError && e.status !== 502 && e.status !== 503) || ++failures > 5) throw e
      continue
    }
    if (s.of > 0) onProgress?.(s.done / s.of)
    if (s.state === 'done') return { results: s.results ?? [] }
    if (s.state === 'failed') throw new ApiError(400, s.error ?? "couldn't look up lines")
  }
}

interface SuggestJob {
  state: 'running' | 'done' | 'failed'
  done: number
  of: number
  results?: SuggestResponse['results']
  error?: string
}

export const api = {
  defaults: (t: string) => call<DefaultsResponse>(t, 'GET', '/api/defaults'),
  plan: (t: string, req: PlanRequest, signal?: AbortSignal) => call<PlanResponse>(t, 'POST', '/api/plan', req, signal),
  stopsNear: (t: string, lat: number, lon: number, radius_m: number) =>
    call<{ stops: NearStop[] }>(t, 'POST', '/api/stops/near', { lat, lon, radius_m }),
  /** Vehicles running the given rides (<trip>|<from stop>|<to stop>) while near the part ridden. */
  vehicles: (t: string, lines: string[], rides: string[]) =>
    call<{ vehicles: Vehicle[] }>(
      t, 'GET', `/api/vehicles?lines=${encodeURIComponent(lines.join(','))}&rides=${encodeURIComponent(rides.join(','))}`,
    ),
  suggestLines,
  geocode: (t: string, q: string) => call<{ results: GeocodeResult[] }>(t, 'POST', '/api/geocode', { q }),
}
