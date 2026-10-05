// In-trip mode: follows the chosen option, re-checks it against live data every 30 s from wherever
// you are (or the vehicle you're on), and suggests a switch when something slips.
import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, AuthError, problem } from '../api.ts'
import { clock, countdown, dayOf, delay, duration, placeName, riskLabel, spare, statusTime } from '../format.ts'
import { useNow, useVisible, useWide } from '../hooks.ts'
import { assess, instruction, phaseAt, replanOrigin, replanTime, spareToBoard, tripsFrom, type Assessment, type Phase, type Position } from '../intrip.ts'
import type { Option, PlanRequest } from '../types.ts'
import type { Walk } from '../walkmeasure.ts'
import { existing, segments, type PlaceRef, type Retime, type Segment, type TimedWalk } from '../walks.ts'
import { MapPane, MapSheet } from './board.tsx'
import { Timeline, type Tracking } from './option.tsx'
import { position, rows } from '../options.ts'
import { advance, phaseOf, rowLines, shapeKey, type At, type LonLat } from '../progress.ts'
import { boarding, type Fix, type Sighting } from '../boarding.ts'
import { WalkTimer } from './walktimer.tsx'
import { ActionBar, Button, Callout, Confirm, TextButton } from './ui.tsx'

/** Everything needed to resume a trip after a reload; kept on this device only. */
export interface ActiveTrip {
  option: Option // the option as currently committed (updated with live times)
  plannedArrive: string
  request: PlanRequest // the original request (from/to/prefs)
  ends: { start: PlaceRef; end: PlaceRef } // the home or gym at each end, for timing walks to and from them
  title: string
  origin: [number, number]
  destination: [number, number]
  serviceDate: string
  walkSpeedMps: number
}

export const TRIP_KEY = 'gymrouter.trip'

const REPLAN_MS = 30_000
const VEHICLE_MS = 10_000 // how often your vehicle's position is checked while you wait for it or ride it
const HISTORY_MS = 180_000 // your recent fixes, kept to compare with the vehicle's reports
const STALE_MS = 20_000 // a fix older than this isn't where you are now

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
  const [ending, setEnding] = useState(false)
  const wide = useWide()
  const [timing, setTiming] = useState<Segment | null>(null)
  const [saved, setSaved] = useState('')
  const timerRef = useRef<HTMLDivElement>(null)
  useEffect(() => timerRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), [timing])
  const o = trip.option
  const posRef = useRef(pos)
  posRef.current = pos
  const phaseRef = useRef<Phase>(phaseAt(o, now))

  // Where you are on the steps: from your location along each step's line, the clock only as a fallback.
  const [shapes, setShapes] = useState<Record<string, LonLat[]>>({})
  const rides = o.legs.filter((l) => l.kind === 'ride' && l.trip_id && l.from && l.to)
  useEffect(() => {
    let live = true
    for (const l of rides) {
      const k = shapeKey(l)
      if (shapes[k]) continue
      api
        .shape(token, trip.serviceDate, l.trip_id!, l.from!.id, l.to!.id)
        .then((s) => live && setShapes((cur) => ({ ...cur, [k]: s.coordinates })))
        .catch(() => undefined) // straight between the stops will do
    }
    return () => {
      live = false
    }
  }, [rides.map(shapeKey).join(',')])
  const steps = useMemo(() => rows(o, true), [o])
  const stepLines = useMemo(() => rowLines(steps, o, trip.origin, trip.destination, shapes), [steps, shapes])
  // On your vehicle? Your fixes moving with its live reports say so (boarding.ts); polled while you wait for it or ride it.
  const history = useRef<Fix[]>([])
  const sightings = useRef<Record<string, Sighting[]>>({})
  const [aboard, setAboard] = useState<Set<string>>(new Set()) // trip IDs you've been seen on
  const recheck = useRef<() => void>(() => undefined)
  const watching = phaseRef.current.kind === 'before' || phaseRef.current.kind === 'riding' ? o.legs[phaseRef.current.ride] : undefined
  useEffect(() => {
    const l = watching
    if (!l?.trip_id || !l.from || !l.to || !l.line || locState !== 'on') return
    let live = true
    let gone = 0 // the report time a "left without you" was last acted on
    const tick = () =>
      api
        .vehicles(token, [`${l.line!.mode} ${l.line!.name}`], [`${l.trip_id}|${l.from!.id}|${l.to!.id}`])
        .then((r) => {
          if (!live) return
          const seen = (sightings.current[l.trip_id!] ??= [])
          for (const v of r.vehicles) {
            const t = Date.parse(v.ts)
            if (v.trip_id === l.trip_id && !seen.some((s) => s.t === t)) seen.push({ lat: v.lat, lon: v.lon, t })
          }
          const b = boarding(history.current, seen)
          if (b === 'aboard') setAboard((cur) => (cur.has(l.trip_id!) ? cur : new Set(cur).add(l.trip_id!)))
          const last = seen[seen.length - 1]?.t ?? 0
          if (b === 'left-without-you' && last > gone) {
            gone = last
            recheck.current() // don't wait for the next check: offer the next way now
          }
        })
        .catch(() => undefined)
    tick()
    const id = setInterval(tick, VEHICLE_MS)
    return () => {
      live = false
      clearInterval(id)
    }
  }, [watching?.trip_id, locState])
  const onVehicle = (ride: number) => aboard.has(o.legs[ride]?.trip_id ?? '')

  // Where you are and what you're doing, from your location: the marker, Now / Then and the re-checks all use this.
  // Without any location (refused or unavailable) the clock decides, as the timetable has it.
  const lastAt = useRef<{ key: string; at: At } | null>(null)
  const stepsKey = steps.map((r) => r.kind).join(',') + tripsFrom(o, 0).join(',')
  const clockAt = position(steps, now)
  let at: At = clockAt
  let phase: Phase = phaseAt(o, now)
  if (pos) {
    // Aboard with no fresh fix of your own (a tunnel): where your vehicle is, is where you are.
    const ride = phaseRef.current.kind === 'riding' ? o.legs[phaseRef.current.ride] : undefined
    const latest = ride?.trip_id && aboard.has(ride.trip_id) ? sightings.current[ride.trip_id]?.at(-1) : undefined
    const stale = Date.now() - (history.current.at(-1)?.t ?? 0) > STALE_MS || pos.accuracy > 100
    const where = stale && latest ? { lat: latest.lat, lon: latest.lon, accuracy: 30 } : pos
    at = advance(lastAt.current?.key === stepsKey ? lastAt.current.at : null, stepLines, where, clockAt)
    lastAt.current = { key: stepsKey, at }
    phase = phaseOf(o, steps, at, stepLines, where, onVehicle)
  }
  phaseRef.current = phase

  const checkedRef = useRef(false)

  // Open at the top: the planning screen may have been scrolled down to its Start button.
  useEffect(() => window.scrollTo(0, 0), [])

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
      (p) => {
        const f = { lat: p.coords.latitude, lon: p.coords.longitude, accuracy: p.coords.accuracy, t: p.timestamp || Date.now() }
        setPos(f)
        history.current = [...history.current.filter((h) => h.t > f.t - HISTORY_MS), f]
      },
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
      const ph = phaseRef.current
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
        else if (live) setError(`Couldn't re-check the trip: ${problem(e)}`)
      }
    }
    recheck.current = run
    if (visible || !checkedRef.current) run()
    checkedRef.current = true
    const id = visible ? setInterval(run, REPLAN_MS) : undefined
    return () => {
      live = false
      clearInterval(id)
    }
  }, [visible, trip.plannedArrive, tripsFrom(trip.option, 0).join(',')])

  const switchTo = (s: Option) => {
    const ph = phaseRef.current
    const keep = ph.kind === 'riding' || ph.kind === 'before' ? ph.ride : o.legs.length
    onUpdate({ ...trip, option: merge(o, keep, s), plannedArrive: s.arrive })
    setCheck(null)
  }

  const ins = instruction(o, phase, trip.ends.end.name)
  // The trip's description: you on its rail, and its walks to time (from the map, timing goes back to this screen).

  const track: Tracking = {
    now,
    at,
    segs: segments(o, trip.ends.start, trip.ends.end),
    onTime: (s) => {
      setMapOpen(false)
      setSaved('')
      setTiming(s)
    },
  }
  const lateBy = check && check.status !== 'missed' ? check.lateBy : 0
  const nextRide = phase.kind === 'before' ? o.legs[phase.ride] : undefined
  const spareS = nextRide ? spareToBoard(nextRide, pos, now, trip.walkSpeedMps) : null
  // The next change: the one after the ride you're on or heading for.
  const upcoming = o.transfers.find((t) => (phase.kind === 'riding' || phase.kind === 'before') && t.from_leg === phase.ride)
  const start = Date.parse(o.leave_at)

  return (
    <div class="intrip">
      <header class="trip-bar">
        <span class="sheet-title">{trip.title}</span>
        {!ending && (
          <TextButton quiet onClick={() => setEnding(true)}>
            End trip
          </TextButton>
        )}
      </header>
      {ending && (
        <Confirm
          question="End this trip?"
          detail="Live tracking and re-checks stop."
          keep="Keep going"
          confirm="End trip"
          onKeep={() => setEnding(false)}
          onConfirm={onEnd}
        />
      )}

      <section class="hero">
        <p class="label">{phase.kind === 'arrived' ? 'Arrived' : 'Arrive'}</p>
        <p class="hero-time num">{clock(o.arrive)}</p>
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
        <Callout
          tone="bad"
          role="alert"
          action={
            check.suggestion && (
              <Button onClick={() => switchTo(check.suggestion!)}>
                Switch to this
              </Button>
            )
          }
        >
          {check.suggestion ? (
            <>
              <strong>You won't make the planned connection.</strong> Next best: {check.suggestion.lines.map((l) => l.split(' ')[1]).join(', ')},
              arriving {clock(check.suggestion.arrive)}.
            </>
          ) : (
            <>
              <strong>The planned connection is gone</strong> and there's no other way on these lines right now.
            </>
          )}
        </Callout>
      )}
      {check?.status === 'better' && (
        <Callout
          tone="good"
          role="status"
          action={
            <Button onClick={() => switchTo(check.suggestion)}>
              Switch to this
            </Button>
          }
        >
          <strong>A faster way just opened up:</strong> {check.suggestion.lines.map((l) => l.split(' ')[1]).join(', ')}, arriving{' '}
          {clock(check.suggestion.arrive)}, {duration((Date.parse(check.current.arrive) - Date.parse(check.suggestion.arrive)) / 1000)} sooner.
        </Callout>
      )}

      <section class="now">
        <p class="label">Now</p>
        <p class="now-main">{ins.now}</p>
        {steps[at.row]?.kind === 'start' && now < start - 60_000 && (
          <p>
            Leave {dayOf(o.leave_at, now) && `${dayOf(o.leave_at, now)} `}at <strong>{clock(o.leave_at)}</strong> ({countdown(o.leave_at, now)})
          </p>
        )}
        {ins.detail && phase.kind === 'before' && (
          <p>
            {Date.parse(ins.detail.dep) < now - 30_000
              ? `${ins.detail.line?.name} was due ${clock(ins.detail.dep)} (${countdown(ins.detail.dep, now).replace(/^left /, '')})`
              : `${ins.detail.line?.name} leaves ${clock(ins.detail.dep)} (${countdown(ins.detail.dep, now)})`}
            {ins.detail.status === 'predicted' && `, ${delay(ins.detail.delay_s)}`}
            {spareS !== null && !(phase.kind === 'before' && phase.waiting) && (
              <span class={spareS < 0 ? 'spare bad' : spareS < 60 ? 'spare tight' : 'spare'}>
                {spareS < 0 ? `You need to hurry: ${Math.ceil(-spareS / 60)} min short at walking pace.` : `${spare(spareS)} from here.`}
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
            {Math.max(1, Math.round(upcoming.walk_s / 60))} min walk, {spare(upcoming.slack_s)}.
            {upcoming.fallback_dep && ` If missed, the next one is at ${clock(upcoming.fallback_dep)}.`}
          </p>
        </section>
      )}

      {timing ? (
        <div ref={timerRef}>
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
        </div>
      ) : null}
      {saved && !timing && (
        <Callout role="status" onDismiss={() => setSaved('')}>
          {saved}
        </Callout>
      )}

      <p class="meta status-line">
        {locState === 'on' && (pos ? `Location on (±${Math.round(pos.accuracy)} m). ` : 'Finding your location… ')}
        {locState === 'denied' && 'Location unavailable; following the timetable. '}
        {checkedAt ? `Checked ${statusTime(checkedAt)}.` : 'Checking…'}
        {error && ` ${error}`}
        {locState === 'denied' && navigator.geolocation && (
          <>
            {' '}
            <TextButton onClick={() => setLocState('on')}>Try my location again</TextButton>
          </>
        )}
      </p>

      <section class="steps" aria-label="The trip">
        <Timeline option={o} ends={trip.ends} walks={walks} track={track} />
      </section>

      {!wide && (
        <ActionBar>
          <Button onClick={() => setMapOpen(true)}>Show on map</Button>
        </ActionBar>
      )}
      {wide && (
        <MapPane
          serviceDate={trip.serviceDate}
          token={token}
          me={pos}
          walks={walks}
          places={{ start: trip.ends.start.key, end: trip.ends.end.key }}
          origin={trip.origin}
          destination={trip.destination}
          option={o}
        />
      )}

      {!wide && mapOpen && (
        <MapSheet
          live
          serviceDate={trip.serviceDate}
          token={token}
          me={pos}
          walks={walks}
          places={{ start: trip.ends.start.key, end: trip.ends.end.key }}
          origin={trip.origin}
          destination={trip.destination}
          title={trip.title}
          option={o}
          now={now}
          onClose={() => setMapOpen(false)}
          steps={{ ends: trip.ends, track }}
        />
      )}
    </div>
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
