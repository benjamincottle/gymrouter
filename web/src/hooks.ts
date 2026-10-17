import { useEffect, useRef, useState } from 'preact/hooks'
import { api, AuthError } from './api.ts'
import { createLive } from './live.ts'
import * as clock from './clock.ts'

/** Current time by the app's clock (clock.ts), updated every `ms` milliseconds (more often in a fast simulation). */
export function useNow(ms = 1000): number {
  const [now, setNow] = useState(clock.now())
  const every = clock.simulating() ? Math.min(ms, 250) : ms
  useEffect(() => {
    const id = setInterval(() => setNow(clock.now()), every)
    const off = clock.onChange(() => setNow(clock.now())) // a skip shows at once
    return () => {
      clearInterval(id)
      off()
    }
  }, [every])
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

// The app's one stream of the server's events (live.ts), shared by every useLiveEvents.
const live = createLive(api.events, (e) => e instanceof AuthError) // a revoked token: the plans say so

/**
 * Listens for the server saying what has changed (GET /api/events, shared: live.ts) while `on` (only while the page is
 * visible and there's something to update). `realtime` and `vehicles` count the changes heard, for usePolling's
 * trigger or an effect's; `connected` says whether the stream is open, so a timer can be slower while it is.
 */
export function useLiveEvents(token: string | undefined, on: boolean): { realtime: number; vehicles: number; connected: boolean } {
  const [s, setS] = useState({ realtime: 0, vehicles: 0, connected: false })
  useEffect(() => {
    if (!token || !on || clock.simulating()) return // a simulation has no live data (sim.ts)
    const off = live.listen(token, (n) =>
      setS((cur) => ('kind' in n ? { ...cur, [n.kind]: cur[n.kind] + 1 } : cur.connected === n.connected ? cur : { ...cur, connected: n.connected })),
    )
    return () => {
      off()
      setS((cur) => ({ ...cur, connected: false }))
    }
  }, [token, on])
  return s
}

/**
 * Whether the page is dark now (theme.ts sets <html data-theme>), following it as it changes: the Settings choice, or
 * the system's under Auto.
 */
export function useDarkPage(): boolean {
  const read = () => document.documentElement.dataset.theme === 'dark'
  const [dark, setDark] = useState(read)
  useEffect(() => {
    const watch = new MutationObserver(() => setDark(read()))
    watch.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] })
    return () => watch.disconnect()
  }, [])
  return dark
}
