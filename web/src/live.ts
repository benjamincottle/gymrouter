// One stream of the server's events (GET /api/events) for the whole app, shared by everything listening: the trip
// screen, a trip under way and the map each want to hear about new live data or new vehicle positions, and one
// connection does for all of them. It's open while anything listens, and opened again after a drop.

import * as clock from './clock.ts'

/** What the server says has changed: live data (re-plan) or vehicle positions (fetch them again). */
export type LiveKind = 'realtime' | 'vehicles'

export type LiveNews = { kind: LiveKind } | { connected: boolean }

/** Opens the stream once: calls `onOpen` when connected and `onEvent` with each event's name, ends when it closes. */
export type Open = (token: string, signal: AbortSignal, onOpen: () => void, onEvent: (name: string) => void) => Promise<void>

/**
 * Keeps one stream open while anything listens. A dropped stream is opened again: a second after the server closed it,
 * then backing off to 30 s while it can't connect. `stopAfter` says which errors are worth no retry (a revoked token).
 */
export function createLive(open: Open, stopAfter: (e: unknown) => boolean, pauseMs = { closed: 1000, max: 30_000 }) {
  const listeners = new Set<(n: LiveNews) => void>()
  let connected = false
  let conn: { token: string; ctrl: AbortController } | null = null
  const tell = (n: LiveNews) => listeners.forEach((l) => l(n))
  const setConnected = (c: boolean) => {
    if (c === connected) return
    connected = c
    tell({ connected: c })
  }

  const start = (token: string) => {
    const ctrl = new AbortController()
    conn = { token, ctrl }
    const pause = (ms: number) =>
      new Promise<void>((r) => {
        const id = setTimeout(r, ms)
        ctrl.signal.addEventListener('abort', () => (clearTimeout(id), r()))
      })
    void (async () => {
      for (let failures = 0; !ctrl.signal.aborted; ) {
        try {
          await open(token, ctrl.signal, () => ((failures = 0), setConnected(true)), (name) => {
            if (name === 'realtime' || name === 'vehicles') tell({ kind: name })
          })
        } catch (e) {
          if (ctrl.signal.aborted) break
          if (stopAfter(e)) {
            setConnected(false)
            break
          }
          failures++
        }
        if (ctrl.signal.aborted) break
        setConnected(false)
        await pause(failures === 0 ? pauseMs.closed : Math.min(pauseMs.max, 1000 * 2 ** failures))
      }
    })()
  }

  const stop = () => {
    conn?.ctrl.abort()
    conn = null
    setConnected(false)
  }

  return {
    /** Starts listening (opening the stream if it isn't open); the listener first hears whether it's connected. */
    listen(token: string, fn: (n: LiveNews) => void): () => void {
      if (conn && conn.token !== token) stop()
      fn({ connected })
      listeners.add(fn)
      if (!conn) start(token)
      return () => {
        listeners.delete(fn)
        if (listeners.size === 0) stop()
      }
    },
  }
}

/**
 * Runs `fn` now (`run`) and again `wait()` ms after each run, for a fetch that's also run as soon as the server says
 * there's something new: each run starts the wait again. `retime` reads `wait()` afresh for the run that's pending,
 * counted from the last run, as when the stream connects or drops and the wait changes.
 */
export function every(fn: () => void, wait: () => number) {
  let cancel = () => {}
  let last = 0
  const run = () => {
    cancel()
    last = clock.now()
    fn()
    cancel = clock.after(wait(), run, 1000)
  }
  return {
    run,
    retime: () => {
      if (last === 0) return // not started: the first run sets the wait
      cancel()
      cancel = clock.after(Math.max(0, last + wait() - clock.now()), run, 1000)
    },
    stop: () => cancel(),
  }
}
