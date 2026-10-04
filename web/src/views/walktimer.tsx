import { useEffect, useRef, useState } from 'preact/hooks'
import { useNow } from '../hooks.ts'
import { addFix, finish, meanSecs, mmss, MIN_WALK_S, walkedM, type Recording, type Walk } from '../walkmeasure.ts'
import type { Retime } from '../walks.ts'

interface Point {
  name: string
  lat: number
  lon: number
}

interface Props {
  from: Point // where the walk starts (a home, a gym or a stop)
  to: Point
  earlier: number[] // walks already timed for this walk, seconds
  estimateS: number // what the trip assumed, for comparison
  retime: Retime // what saving does when there are earlier walks; the other choice is offered too
  onSave: (w: Walk, replace: boolean) => void
  onCancel: () => void
}

type Phase = 'ready' | 'recording' | 'review'
type Gps = 'idle' | 'waiting' | 'on' | 'denied'

/**
 * Times a walk and traces it with GPS. Everything stays on this device; the trace is only drawn on the map and the
 * time is used for the walk from then on.
 */
export function WalkTimer({ from, to, earlier, estimateS, retime, onSave, onCancel }: Props) {
  const [phase, setPhase] = useState<Phase>('ready')
  const [gps, setGps] = useState<Gps>('idle')
  const [accuracy, setAccuracy] = useState<number | null>(null)
  const [, bump] = useState(0)
  const [walk, setWalk] = useState<Walk | null>(null)
  const rec = useRef<Recording>({ startedAt: 0, fixes: [] })
  const watch = useRef<number | null>(null)
  const now = useNow(500)

  const stopWatching = () => {
    if (watch.current !== null) navigator.geolocation?.clearWatch(watch.current)
    watch.current = null
  }
  useEffect(() => stopWatching, [])

  // Keep the screen awake while timing, or the phone sleeps mid-walk and stops reporting.
  useEffect(() => {
    if (phase !== 'recording') return
    let lock: WakeLockSentinel | null = null
    const take = () => {
      if (document.visibilityState === 'visible') navigator.wakeLock?.request('screen').then((l) => (lock = l)).catch(() => undefined)
    }
    take()
    document.addEventListener('visibilitychange', take)
    return () => {
      document.removeEventListener('visibilitychange', take)
      lock?.release().catch(() => undefined)
    }
  }, [phase])

  const start = () => {
    rec.current = { startedAt: Date.now(), fixes: [] }
    setPhase('recording')
    if (!navigator.geolocation) return setGps('denied')
    setGps('waiting')
    watch.current = navigator.geolocation.watchPosition(
      (p) => {
        setAccuracy(Math.round(p.coords.accuracy))
        if (addFix(rec.current, { lat: p.coords.latitude, lon: p.coords.longitude, accuracy: p.coords.accuracy, t: p.timestamp || Date.now() })) {
          setGps('on')
          bump((n) => n + 1)
        }
      },
      (e) => {
        if (e.code === e.PERMISSION_DENIED) setGps('denied')
      },
      { enableHighAccuracy: true, maximumAge: 0, timeout: 20_000 },
    )
  }

  const arrive = () => {
    const end = Date.now()
    stopWatching()
    setWalk(finish(rec.current, end, from, to))
    setPhase('review')
  }

  const reset = () => {
    stopWatching()
    setGps('idle')
    setAccuracy(null)
    setWalk(null)
    setPhase('ready')
  }

  const elapsed = Math.max(0, Math.round((now - rec.current.startedAt) / 1000))
  const walked = Math.round(walkedM(rec.current))

  return (
    <section class="card timer" aria-live="polite">
      <h2>Time the walk</h2>
      <p class="muted small">
        {from.name} to {to.name}
      </p>

      {phase === 'ready' && (
        <>
          <p>
            Tap <strong>Start</strong> as you set off from {from.name}, walk to {to.name} the way you normally would, and tap{' '}
            <strong>I'm here</strong> when you get there.
          </p>
          <p class="muted small">
            It uses your location to trace the route, so the map can show your shortcut. Nothing leaves this device. Keep the
            screen on; the app asks the phone not to sleep while timing.
          </p>
          {earlier.length > 0 && (
            <p class="muted small">
              Timed before: {earlier.length === 1 ? mmss(earlier[0]) : `${earlier.length} walks, averaging ${mmss(meanSecs(earlier))}`}.
            </p>
          )}
          <div class="actions">
            <button class="primary" onClick={start}>
              Start
            </button>
            <button onClick={onCancel}>Cancel</button>
          </div>
        </>
      )}

      {phase === 'recording' && (
        <>
          <p class="timer-clock" role="timer">
            {mmss(elapsed)}
          </p>
          <p class="muted small">
            {gps === 'on' && `Tracing: ${walked} m so far${accuracy !== null ? ` (location good to ${accuracy} m)` : ''}.`}
            {gps === 'waiting' && 'Waiting for a location fix… keep walking, the time is still counting.'}
            {gps === 'denied' && "Location is off, so only the time is recorded (the route won't be drawn)."}
          </p>
          <div class="actions">
            <button class="primary" onClick={arrive}>
              I'm here
            </button>
            <button onClick={reset}>Cancel</button>
          </div>
        </>
      )}

      {phase === 'review' && walk && (
        <>
          <p class="timer-clock">{mmss(walk.secs)}</p>
          <p class="small">
            {walk.distanceM > 0 ? `${walk.distanceM} m walked. ` : ''}
            {estimateS > 0 && `The trip assumed ${mmss(estimateS)}.`}
          </p>
          {walk.secs < MIN_WALK_S && <p class="error">That was very short. Cancel and try again if it was a mistaken tap.</p>}
          {(walk.startedNearM ?? 0) > 120 && (
            <p class="notice warn">
              Your location was {walk.startedNearM} m from {from.name} when you started. If you weren't there, the route will be
              off.
            </p>
          )}
          {(walk.endedNearM ?? 0) > 80 && (
            <p class="notice warn">
              You finished {walk.endedNearM} m from {to.name}. Was that where you meant?
            </p>
          )}
          {walk.trace.length < 2 && gps !== 'idle' && <p class="muted small">No usable route was recorded; only the time will be saved.</p>}
          {earlier.length > 0 && (
            <p class="muted small">
              Averaged with {earlier.length === 1 ? 'the earlier walk' : `the ${earlier.length} earlier walks`}:{' '}
              {mmss(meanSecs([...earlier, walk.secs].slice(-5)))}. Replacing them: {mmss(walk.secs)}.
            </p>
          )}
          <div class="actions">
            {earlier.length === 0 ? (
              <button class="primary" disabled={walk.secs < MIN_WALK_S} onClick={() => onSave(walk, true)}>
                Save
              </button>
            ) : (
              (retime === 'replace' ? [true, false] : [false, true]).map((replace, i) => (
                <button class={i === 0 ? 'primary' : ''} disabled={walk.secs < MIN_WALK_S} onClick={() => onSave(walk, replace)}>
                  {replace ? 'Replace earlier' : 'Save and average'}
                </button>
              ))
            )}
            <button onClick={reset}>Try again</button>
            <button onClick={onCancel}>Discard</button>
          </div>
        </>
      )}
    </section>
  )
}
