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
  if (res.status === 401) throw new AuthError('This device is not set up (or its access was revoked).')
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

export const api = {
  defaults: (t: string) => call<DefaultsResponse>(t, 'GET', '/api/defaults'),
  plan: (t: string, req: PlanRequest, signal?: AbortSignal) => call<PlanResponse>(t, 'POST', '/api/plan', req, signal),
  stopsNear: (t: string, lat: number, lon: number, radius_m: number) =>
    call<{ stops: NearStop[] }>(t, 'POST', '/api/stops/near', { lat, lon, radius_m }),
  vehicles: (t: string, lines: string[]) =>
    call<{ vehicles: Vehicle[] }>(t, 'GET', `/api/vehicles?lines=${encodeURIComponent(lines.join(','))}`),
  suggestLines: (t: string, from: PlaceRequest, to: PlaceRequest[], signal?: AbortSignal) =>
    call<SuggestResponse>(t, 'POST', '/api/suggest-lines', { from, to }, signal),
  geocode: (t: string, q: string) => call<{ results: GeocodeResult[] }>(t, 'POST', '/api/geocode', { q }),
}
