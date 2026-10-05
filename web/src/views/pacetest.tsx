// Measuring your walking pace: a minute or two of ordinary walking, timed with GPS while you're moving.
import { useEffect, useRef, useState } from 'preact/hooks'
import { useNow } from '../hooks.ts'
import { addFix, mmss, pace, PACE_MIN_M, PACE_MIN_S, type Pace, type Recording } from '../walkmeasure.ts'
import { Button } from './ui.tsx'

type Phase = 'ready' | 'walking' | 'done'

const kmh = (mps: number) => (Math.round(mps * 36) / 10).toFixed(1)

export function PaceTest({ onUse, onClose }: { onUse: (mps: number) => void; onClose: () => void }) {
  const [phase, setPhase] = useState<Phase>('ready')
  const [result, setResult] = useState<Pace | null>(null)
  const [gps, setGps] = useState<'waiting' | 'on' | 'denied'>('waiting')
  const [, bump] = useState(0)
  const rec = useRef<Recording>({ startedAt: 0, fixes: [] })
  const watch = useRef<number | null>(null)
  const now = useNow(500)

  const stop = () => {
    if (watch.current !== null) navigator.geolocation?.clearWatch(watch.current)
    watch.current = null
  }
  useEffect(() => stop, [])

  // Keep the screen awake while walking, or the phone sleeps and stops reporting.
  useEffect(() => {
    if (phase !== 'walking') return
    let lock: WakeLockSentinel | null = null
    navigator.wakeLock?.request('screen').then((l) => (lock = l)).catch(() => undefined)
    return () => {
      lock?.release().catch(() => undefined)
    }
  }, [phase])

  const start = () => {
    rec.current = { startedAt: Date.now(), fixes: [] }
    setResult(null)
    setPhase('walking')
    if (!navigator.geolocation) return setGps('denied')
    setGps('waiting')
    watch.current = navigator.geolocation.watchPosition(
      (p) => {
        if (addFix(rec.current, { lat: p.coords.latitude, lon: p.coords.longitude, accuracy: p.coords.accuracy, t: p.timestamp || Date.now() })) {
          setGps('on')
          bump((n) => n + 1)
        }
      },
      (e) => e.code === e.PERMISSION_DENIED && setGps('denied'),
      { enableHighAccuracy: true, maximumAge: 0, timeout: 20_000 },
    )
  }

  const done = () => {
    stop()
    setResult(pace(rec.current))
    setPhase('done')
  }

  const live = pace(rec.current)
  const elapsed = Math.max(0, Math.round((now - rec.current.startedAt) / 1000))

  return (
    <div class="pace" aria-live="polite">
      {phase === 'ready' && (
        <>
          <p>
            Walk at your everyday pace for a minute or two, outdoors where the phone can see the sky. Tap <strong>Start</strong>,
            walk, then tap <strong>Done</strong>. Waiting at a crossing is fine: only the time you're moving counts.
          </p>
          <div class="actions">
            <Button variant="primary" onClick={start}>
              Start
            </Button>
            <Button onClick={onClose}>Cancel</Button>
          </div>
        </>
      )}

      {phase === 'walking' && (
        <>
          <p class="timer-clock" role="timer">
            {mmss(elapsed)}
          </p>
          <p class="muted small">
            {gps === 'waiting' && 'Waiting for a location fix… keep walking.'}
            {gps === 'denied' && "Location is off, so the pace can't be measured."}
            {gps === 'on' && (live ? `About ${kmh(live.mps)} km/h so far.` : `Keep going: at least ${PACE_MIN_S / 60} minute and ${PACE_MIN_M} m.`)}
          </p>
          <div class="actions">
            <Button variant="primary" onClick={done}>
              Done
            </Button>
            <Button
              onClick={() => {
                stop()
                onClose()
              }}
            >
              Cancel
            </Button>
          </div>
        </>
      )}

      {phase === 'done' && (
        <>
          {result ? (
            <>
              <p class="timer-clock">{kmh(result.mps)} km/h</p>
              <p class="muted small">
                {result.metres} m in {mmss(result.movingS)} of walking.
              </p>
            </>
          ) : (
            <p class="error">
              Not enough walking to tell: it needs at least a minute and {PACE_MIN_M} m with a good location (outdoors), at a
              walking pace.
            </p>
          )}
          <div class="actions">
            {result && (
              <Button variant="primary" onClick={() => onUse(result.mps)}>
                Use {kmh(result.mps)} km/h
              </Button>
            )}
            <Button onClick={start}>Try again</Button>
            <Button onClick={onClose}>Cancel</Button>
          </div>
        </>
      )}
    </div>
  )
}
