import type {
  DefaultsResponse, GeocodeResult, NearStop, PlaceRequest, PlanRequest, PlanResponse, ServerStatus, SuggestResponse, Vehicle,
} from './types.ts'

export class AuthError extends Error {}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

/**
 * What went wrong, in words for the person using the app: the server's own message when it's about the request
 * (a 4xx), otherwise a plain description of the failure. Never a raw exception.
 */
export function problem(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return "Couldn't reach the server. Check your connection."
    if (e.status === 503) return 'The server is still getting its timetable ready. Try again in a few seconds.'
    if (e.status >= 500) return 'The server ran into a problem.'
    const m = e.message.trim()
    return m ? `${m[0].toUpperCase()}${m.slice(1)}${/[.!?]$/.test(m) ? '' : '.'}` : 'The server turned the request down.'
  }
  if (e instanceof AuthError) return e.message
  return 'Something went wrong.'
}

async function call<T>(token: string, method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await send(path, {
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
  return read<T>(res)
}

/** fetch, with a failure to connect reported as an ApiError (status 0) rather than a bare TypeError. */
async function send(path: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(path, init)
  } catch (e) {
    if (init.signal?.aborted || (e instanceof DOMException && e.name === 'AbortError')) throw e
    throw new ApiError(0, "Couldn't reach the server")
  }
}

async function read<T>(res: Response): Promise<T> {
  // Without a valid token the server answers like a missing page (plain text); the API's own 404s are JSON.
  if (res.status === 404 && res.headers.get('Content-Type')?.startsWith('text/plain')) {
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
 * can outlast a request), so this starts it and polls; `onProgress` gets the fraction done. `radiusM` is the longest
 * walk (the device's setting, else the server's), so the stops it starts from are the ones a plan would use.
 */
async function suggestLines(
  t: string, from: PlaceRequest, to: PlaceRequest[], signal?: AbortSignal, onProgress?: (fraction: number) => void,
  radiusM?: number,
): Promise<SuggestResponse> {
  let job: string
  for (let attempt = 0; ; attempt++) {
    try {
      job = (await call<{ job: string }>(t, 'POST', '/api/suggest-lines', { from, to, radius_m: radiusM }, signal)).job
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
  /** One ride's route and the stops it passes ([lon, lat]). */
  shape: (t: string, date: string, trip: string, from: string, to: string) =>
    call<{ coordinates: [number, number][]; stops: [number, number][] }>(
      t, 'GET', `/api/shape?${new URLSearchParams({ date, trip, from, to })}`,
    ),
  defaults: (t: string) => call<DefaultsResponse>(t, 'GET', '/api/defaults'),
  status: (t: string, signal?: AbortSignal) => call<ServerStatus>(t, 'GET', '/api/status', undefined, signal),
  plan: (t: string, req: PlanRequest, signal?: AbortSignal) => call<PlanResponse>(t, 'POST', '/api/plan', req, signal),
  /** Asks the server to load these lines in the background, so the first search naming them doesn't wait. */
  loadLines: (t: string, lines: string[]) => call<object>(t, 'POST', '/api/lines', { lines }),
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
