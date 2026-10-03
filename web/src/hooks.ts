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
 * Runs `fn` now and then every `ms` while `enabled`, aborting the previous run when a new one
 * starts or the inputs change. `key` identifies the request; changing it restarts immediately.
 */
export function usePolling<T>(
  key: string | null,
  fn: (signal: AbortSignal) => Promise<T>,
  ms: number,
  enabled: boolean,
): { data: T | null; error: unknown; loading: boolean; updatedAt: number } {
  const [state, setState] = useState<{ data: T | null; error: unknown; loading: boolean; updatedAt: number }>({
    data: null, error: null, loading: false, updatedAt: 0,
  })
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    setState({ data: null, error: null, loading: key !== null, updatedAt: 0 })
  }, [key])

  useEffect(() => {
    if (key === null || !enabled) return
    let ctrl: AbortController | null = null
    const run = () => {
      ctrl?.abort()
      ctrl = new AbortController()
      const c = ctrl
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
    run()
    const id = setInterval(run, ms)
    return () => {
      clearInterval(id)
      ctrl?.abort()
    }
  }, [key, ms, enabled])

  return state
}
