import { useEffect, useRef, useState } from 'preact/hooks'

/** Current time, updated every `ms` milliseconds. */
export function useNow(ms = 1000): number {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(id)
  }, [ms])
  return now
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

/**
 * Runs `fn` as soon as `key` changes, then every `ms` while `visible` (and once more when the page
 * becomes visible again). Changing `key` aborts the previous request. A null key does nothing.
 */
export function usePolling<T>(
  key: string | null,
  fn: (signal: AbortSignal) => Promise<T>,
  ms: number,
  visible: boolean,
): { data: T | null; error: unknown; loading: boolean; updatedAt: number } {
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

  // Background refresh only while visible; catch up immediately when the page comes back.
  useEffect(() => {
    if (key === null || !visible) return
    if (Date.now() - lastRun.current >= ms) run()
    const id = setInterval(run, ms)
    return () => clearInterval(id)
  }, [key, ms, visible])

  return state
}
