// In-trip mode: follows the chosen option, re-checks it against live data every 30 s from wherever
// you are (or the vehicle you're on), and suggests a switch when something slips.
import { useEffect, useRef, useState } from 'preact/hooks'
import { api, AuthError } from '../api.ts'
import { clock, countdown, dayOf, delay, duration, placeName, riskLabel } from '../format.ts'
import { useNow, useVisible } from '../hooks.ts'
import { assess, instruction, phaseAt, replanOrigin, replanTime, spareToBoard, tripsFrom, type Assessment, type Position } from '../intrip.ts'
import type { Option, PlanRequest } from '../types.ts'
import { mmss, type Walk } from '../walkmeasure.ts'
import { existing, segments, walkSecs, type PlaceRef, type Retime, type Segment, type TimedWalk } from '../walks.ts'
import { MapSheet, Strip } from './board.tsx'
import { WalkTimer } from './walktimer.tsx'

/** Everything needed to resume a trip after a reload; kept on this device only. */
export interface ActiveTrip {
  option: Option // the option as currently committed (updated with live times)
  plannedArrive: string
  request: PlanRequest // the original request (from/to/prefs)
  lines: string[] // the gym's lines: what the map draws
  ends?: { start: PlaceRef; end: PlaceRef } // the home or gym at each end, for timing walks to and from them
  title: string
  origin: [number, number]
  destination: [number, number]
  serviceDate: string
  walkSpeedMps: number
}

export const TRIP_KEY = 'gymrouter.trip'

const REPLAN_MS = 30_000

export function InTrip({ trip, token, walks, retime, onUpdate, onSaveWalk, onEnd, onAuthError }: {
  trip: ActiveTrip
  token: string
  walks: TimedWalk[]
  retime: Retime
  onUpdate: (t: ActiveTrip) => void
  onSaveWalk: (s: Segment, w: Walk, replace: boolean) => void
  onEnd: () => void
  onAuthError: () => void
}) {
  const now = useNow(1000)
  const visible = useVisible()
  const [pos, setPos] = useState<Position | null>(null)
  // Location is on for the whole trip (it's what makes the map and "time to spare" useful); it stays on this device.
  const [locState, setLocState] = useState<'on' | 'denied'>(() => (navigator.geolocation ? 'on' : 'denied'))
  const [check, setCheck] = useState<Assessment | null>(null)
  const [checkedAt, setCheckedAt] = useState(0)
  const [error, setError] = useState('')
  const [mapOpen, setMapOpen] = useState(false)
  const [timing, setTiming] = useState<Segment | null>(null)
  const [saved, setSaved] = useState('')
  const o = trip.option
  const phase = phaseAt(o, now)
  const posRef = useRef(pos)
  posRef.current = pos
  const checkedRef = useRef(false)

  // Keep the screen on while travelling.
  useEffect(() => {
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
  }, [])

  // Location stays on this device; it's only sent as the origin of a re-plan.
  useEffect(() => {
    if (locState !== 'on' || !navigator.geolocation) return
    const id = navigator.geolocation.watchPosition(
      (p) => setPos({ lat: p.coords.latitude, lon: p.coords.longitude, accuracy: p.coords.accuracy }),
      (e) => e.code === e.PERMISSION_DENIED && setLocState('denied'),
      { enableHighAccuracy: true, maximumAge: 10_000, timeout: 30_000 },
    )
    return () => navigator.geolocation.clearWatch(id)
  }, [locState])

  // Re-check the trip straight away, then every 30 s while the screen is visible.
  useEffect(() => {
    let live = true
    const run = async () => {
      const nowMs = Date.now()
      const ph = phaseAt(trip.option, nowMs)
      const from = replanOrigin(trip.option, ph, posRef.current, trip.request.from, nowMs)
      if (!from || (ph.kind !== 'before' && ph.kind !== 'riding')) return
      const time = from.on_trip ? undefined : replanTime(trip.option, ph, nowMs)
      try {
        const res = await api.plan(token, { from, to: trip.request.to, lines: trip.request.lines, time, window_min: 45, prefs: trip.request.prefs })
        if (!live) return
        const a = assess(tripsFrom(trip.option, ph.ride), res.options, trip.plannedArrive)
        setCheck(a)
        setCheckedAt(Date.now())
        setError('')
        if (a.status !== 'missed') {
          // Same trips: take the live times for what's still ahead.
          onUpdate({ ...trip, option: merge(trip.option, ph.ride, a.current) })
        }
      } catch (e) {
        if (e instanceof AuthError) onAuthError()
        else if (live) setError(e instanceof Error ? e.message : String(e))
      }
    }
    if (visible || !checkedRef.current) run()
    checkedRef.current = true
    const id = visible ? setInterval(run, REPLAN_MS) : undefined
    return () => {
      live = false
      clearInterval(id)
    }
  }, [visible, trip.plannedArrive, tripsFrom(trip.option, 0).join(',')])

  const switchTo = (s: Option) => {
    const ph = phaseAt(o, Date.now())
    const keep = ph.kind === 'riding' || ph.kind === 'before' ? ph.ride : o.legs.length
    onUpdate({ ...trip, option: merge(o, keep, s), plannedArrive: s.arrive })
    setCheck(null)
  }

  const ins = instruction(o, phase, trip.ends?.end.name)
  const lateBy = check && check.status !== 'missed' ? check.lateBy : 0
  const nextRide = phase.kind === 'before' ? o.legs[phase.ride] : undefined
  const spare = nextRide ? spareToBoard(nextRide, pos, now, trip.walkSpeedMps) : null
  // The next change: the one after the ride you're on or heading for.
  const upcoming = o.transfers.find((t) => (phase.kind === 'riding' || phase.kind === 'before') && t.from_leg === phase.ride)
  const start = Date.parse(o.leave_at)
  const end = Date.parse(o.arrive)

  return (
    <div class="intrip">
      <header class="sheet-bar">
        <button class="ghost" onClick={onEnd}>
          End trip
        </button>
        <span class="sheet-title">{trip.title}</span>
      </header>

      <section class="hero">
        <p class="hero-label">{phase.kind === 'arrived' ? 'Arrived' : 'Arrive'}</p>
        <p class="hero-time">{clock(o.arrive)}</p>
        <p class="hero-arrive">
          {check?.status === 'missed'
            ? 'if the plan still worked'
            : lateBy >= 60
              ? `${Math.round(lateBy / 60)} min later than planned`
              : lateBy <= -60
                ? `${Math.round(-lateBy / 60)} min earlier`
                : 'as planned'}
        </p>
      </section>

      {check?.status === 'missed' && (
        <div class="alert risk-missed" role="alert">
          {check.suggestion ? (
            <>
              <p>
                <strong>You won't make the planned connection.</strong> Next best: {check.suggestion.lines.map((l) => l.split(' ')[1]).join(', ')},
                arriving {clock(check.suggestion.arrive)}.
              </p>
              <button class="primary" onClick={() => switchTo(check.suggestion!)}>
                Switch to this
              </button>
            </>
          ) : (
            <p>
              <strong>The planned connection is gone</strong> and there's no other way on these lines right now.
            </p>
          )}
        </div>
      )}
      {check?.status === 'better' && (
        <div class="alert risk-safe" role="status">
          <p>
            <strong>A faster way just opened up:</strong> {check.suggestion.lines.map((l) => l.split(' ')[1]).join(', ')}, arriving{' '}
            {clock(check.suggestion.arrive)} ({duration((Date.parse(check.current.arrive) - Date.parse(check.suggestion.arrive)) / 1000)} sooner).
          </p>
          <button class="primary" onClick={() => switchTo(check.suggestion)}>
            Switch to this
          </button>
        </div>
      )}

      <section class="now">
        <p class="label">Now</p>
        <p class="now-main">{ins.now}</p>
        {now < start - 60_000 && (
          <p>
            Leave {dayOf(o.leave_at, now) && `${dayOf(o.leave_at, now)} `}at <strong>{clock(o.leave_at)}</strong> ({countdown(o.leave_at, now)})
          </p>
        )}
        {ins.detail && phase.kind === 'before' && (
          <p>
            {ins.detail.line?.name} leaves {clock(ins.detail.dep)} ({countdown(ins.detail.dep, now).replace(/^in /, 'in ')})
            {ins.detail.status === 'predicted' && `, ${delay(ins.detail.delay_s)}`}
            {spare !== null && (
              <span class={spare < 0 ? 'spare bad' : spare < 60 ? 'spare tight' : 'spare'}>
                {spare < 0 ? ` You need to hurry: ${Math.ceil(-spare / 60)} min short at walking pace.` : ` ${Math.floor(spare / 60)} min to spare from here.`}
              </span>
            )}
          </p>
        )}
        {ins.detail && phase.kind === 'riding' && (
          <p>
            Get off at {clock(ins.detail.arr)} ({countdown(ins.detail.arr, now)}), {placeName(ins.detail.to)}
          </p>
        )}
      </section>

      {upcoming && (
        <section class="next">
          <p class="label">Then</p>
          <p>
            <span class={`badge risk-${upcoming.risk}`}>{riskLabel(upcoming.risk)}</span> Change at {placeName(o.legs[upcoming.from_leg].to)}:{' '}
            {Math.round(upcoming.walk_s / 60)} min, {upcoming.slack_s >= 60 ? `${Math.floor(upcoming.slack_s / 60)} min ` : ''}
            {upcoming.slack_s % 60}s spare.
            {upcoming.fallback_dep && ` If missed, the next one is ${clock(upcoming.fallback_dep)}.`}
          </p>
        </section>
      )}

      {timing ? (
        <WalkTimer
          from={timing.from}
          to={timing.to}
          earlier={existing(walks, timing)?.times ?? []}
          estimateS={timing.estimateS}
          retime={retime}
          onSave={(w, replace) => {
            onSaveWalk(timing, w, replace)
            setSaved(`Saved: ${timing.label}. It's used from now on.`)
            setTiming(null)
          }}
          onCancel={() => setTiming(null)}
        />
      ) : (
        <TimeAWalk segs={segments(o, trip.ends?.start, trip.ends?.end)} current={currentLeg(o, phase)} walks={walks} onTime={setTiming} />
      )}
      {saved && !timing && (
        <p class="notice" role="status">
          {saved}
        </p>
      )}

      <div class="progress">
        <Strip option={o} start={start} end={end} now={now} />
      </div>

      <div class="actions">
        <button class="ghost" onClick={() => setMapOpen(true)}>
          Show on map
        </button>
        {locState === 'denied' && navigator.geolocation && (
          <button class="ghost" onClick={() => setLocState('on')}>
            Try my location again
          </button>
        )}
      </div>
      <p class="muted small">
        {locState === 'on' && (pos ? `Location on (±${Math.round(pos.accuracy)} m). ` : 'Finding your location… ')}
        {locState === 'denied' && 'Location unavailable; following the timetable. '}
        {checkedAt ? `Checked ${new Date(checkedAt).toLocaleTimeString('en-AU', { hour: 'numeric', minute: '2-digit', timeZone: 'Australia/Sydney' })}.` : 'Checking…'}
        {error && ` ${error}`}
      </p>

      {mapOpen && (
        <MapSheet
          live
          serviceDate={trip.serviceDate}
          token={token}
          me={pos}
          walks={walks}
          places={{ start: trip.ends?.start.key, end: trip.ends?.end.key }}
          origin={trip.origin}
          destination={trip.destination}
          title={trip.title}
          option={o}
          now={now}
          onClose={() => setMapOpen(false)}
        />
      )}
    </div>
  )
}

/** The walk leg (or for a change, the ride changed onto) that the trip is at: what "time this walk" offers first. */
function currentLeg(o: Option, phase: ReturnType<typeof phaseAt>): number {
  switch (phase.kind) {
    case 'before': {
      const first = o.legs.findIndex((l) => l.kind === 'ride')
      return phase.ride === first ? 0 : phase.ride
    }
    case 'final-walk':
    case 'arrived':
      return o.legs.length - 1
    default:
      return -1
  }
}

/** Offers to time the walk you're on (or about to start), and any other walk of the trip. */
function TimeAWalk({ segs, current, walks, onTime }: {
  segs: Segment[]
  current: number
  walks: TimedWalk[]
  onTime: (s: Segment) => void
}) {
  if (segs.length === 0) return null
  const now = segs.find((s) => s.leg === current)
  const others = segs.filter((s) => s !== now)
  const timed = (s: Segment) => {
    const w = existing(walks, s)
    return w ? ` (timed ${mmss(walkSecs(w))})` : ''
  }
  return (
    <section class="time-walk">
      {now && (
        <button class="primary" onClick={() => onTime(now)}>
          Time this walk: {now.label}
          <span class="sub">{timed(now)}</span>
        </button>
      )}
      {others.length > 0 && (
        <details>
          <summary>{now ? 'Time another walk in this trip' : 'Time a walk in this trip'}</summary>
          <ul class="list">
            {others.map((s) => (
              <li>
                <span>
                  {s.label}
                  <span class="muted small">{timed(s)}</span>
                </span>
                <button class="link" onClick={() => onTime(s)}>
                  Time it
                </button>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  )
}

/** Keeps the legs already travelled and takes the rest (with live times) from `fresh`. */
function merge(o: Option, fromLeg: number, fresh: Option): Option {
  // A re-plan from a position starts with a walk to the stop; keep ours for the part already behind.
  const firstRide = fresh.legs.findIndex((l) => l.kind === 'ride')
  const ahead = fresh.legs.slice(Math.max(0, firstRide))
  const kept = o.legs.slice(0, fromLeg)
  const offset = kept.length - Math.max(0, firstRide)
  return {
    ...fresh,
    leave_at: o.leave_at,
    legs: [...kept, ...ahead],
    rides: [...kept, ...ahead].filter((l) => l.kind === 'ride').length,
    transfers: [
      ...o.transfers.filter((t) => t.to_leg <= fromLeg),
      ...fresh.transfers
        .filter((t) => t.from_leg >= Math.max(0, firstRide))
        .map((t) => ({ ...t, from_leg: t.from_leg + offset, to_leg: t.to_leg + offset })),
    ],
  }
}
