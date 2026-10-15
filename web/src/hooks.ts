import { useEffect, useRef, useState } from 'preact/hooks'
import { api, AuthError } from './api.ts'

/** Current time, updated every `ms` milliseconds. */
export function useNow(ms = 1000): number {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(id)
  }, [ms])
  return now
}

/** True on a screen wide enough for the trip and the map side by side (docs/DESIGN.md, Layout). */
export function useWide(): boolean {
  const q = '(min-width: 1024px)'
  const [wide, setWide] = useState(() => window.matchMedia(q).matches)
  useEffect(() => {
    const m = window.matchMedia(q)
    const on = () => setWide(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return wide
}

/** True while the page is visible; polling pauses in the background. */
export function useVisible(): boolean {
  const [visible, setVisible] = useState(document.visibilityState === 'visible')
  useEffect(() => {
    const on = () => setVisible(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', on)
    return () => document.removeEventListener('visibilitychange', on)
  }, [])
  return visible
}

/** The value once it has stopped changing for ms (the first value at once; every value at once while ms is 0).
 * Compare by value: pass a string. */
export function useSettled<T extends string | number | boolean>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    if (ms === 0) return setSettled(value)
    const id = setTimeout(() => setSettled(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return ms === 0 ? value : settled
}

/**
 * Runs `fn` as soon as `key` changes, then every `ms` while `visible` (and once more when the page
 * becomes visible again), and at once whenever `trigger` changes (the count starts again from there).
 * Changing `key` aborts the previous request. A null key does nothing.
 */
export function usePolling<T>(
  key: string | null,
  fn: (signal: AbortSignal) => Promise<T>,
  ms: number,
  visible: boolean,
  trigger = 0,
): { data: T | null; error: unknown; loading: boolean; updatedAt: number; refresh: () => void } {
  const [state, setState] = useState<{ data: T | null; error: unknown; loading: boolean; updatedAt: number }>({
    data: null, error: null, loading: false, updatedAt: 0,
  })
  const fnRef = useRef(fn)
  fnRef.current = fn
  const ctrl = useRef<AbortController | null>(null)
  const lastRun = useRef(0)

  const run = () => {
    ctrl.current?.abort()
    const c = new AbortController()
    ctrl.current = c
    lastRun.current = Date.now()
    setState((s) => ({ ...s, loading: true }))
    fnRef
      .current(c.signal)
      .then((data) => {
        if (!c.signal.aborted) setState({ data, error: null, loading: false, updatedAt: Date.now() })
      })
      .catch((error) => {
        if (!c.signal.aborted) setState((s) => ({ ...s, error, loading: false }))
      })
  }

  // A new request: always fetch straight away, whatever the visibility.
  useEffect(() => {
    setState({ data: null, error: null, loading: key !== null, updatedAt: 0 })
    if (key !== null) run()
    return () => ctrl.current?.abort()
  }, [key])

  // Asked for now (the server has newer data): before the timer below, which then counts from this run.
  useEffect(() => {
    if (trigger !== 0 && key !== null && visible) run()
  }, [trigger])

  // Background refresh only while visible; catch up immediately when the page comes back.
  useEffect(() => {
    if (key === null || !visible) return
    if (Date.now() - lastRun.current >= ms) run()
    const id = setInterval(run, ms)
    return () => clearInterval(id)
  }, [key, ms, visible, trigger])

  return { ...state, refresh: run }
}

/**
 * Listens for the server saying its live data has changed (GET /api/events) while `on` (open the stream only while
 * the page is visible and there's something to re-plan). `tick` counts the changes, for usePolling's trigger;
 * `connected` says whether the stream is open, so the timer can be slower while it is. A dropped stream is opened
 * again: at once after the server closed it, then backing off to 30 s while it can't connect.
 */
export function useLiveEvents(token: string | undefined, on: boolean): { tick: number; connected: boolean } {
  const [tick, setTick] = useState(0)
  const [connected, setConnected] = useState(false)
  useEffect(() => {
    if (!token || !on) return
    const c = new AbortController()
    const pause = (ms: number) => new Promise<void>((r) => {
      const id = setTimeout(r, ms)
      c.signal.addEventListener('abort', () => (clearTimeout(id), r()))
    })
    void (async () => {
      for (let failures = 0; !c.signal.aborted; ) {
        try {
          await api.events(token, c.signal, () => ((failures = 0), setConnected(true)), (name) => {
            if (name === 'realtime') setTick((n) => n + 1)
          })
        } catch (e) {
          if (e instanceof AuthError) break // the plans say so
          failures++
        }
        setConnected(false)
        await pause(failures === 0 ? 1000 : Math.min(30_000, 1000 * 2 ** failures))
      }
    })()
    return () => {
      c.abort()
      setConnected(false)
    }
  }, [token, on])
  return { tick, connected }
}
