// In-trip mode: follows the chosen option, re-checks it against live data every 30 s from wherever
// you are (or the vehicle you're on), and suggests a switch when something slips.
import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, AuthError, problem } from '../api.ts'
import { clock, countdown, dayOf, delay, duration, placeName, riskLabel, spare, statusTime } from '../format.ts'
import { useNow, useVisible, useWide } from '../hooks.ts'
import { assess, changesNote, focusLegs, instruction, keepFrom, phaseAt, replanOrigin, replanTime, spareToBoard, suggestionKey, tripsFrom, USABLE_M, type Assessment, type Missed, type Phase, type Position } from '../intrip.ts'
import type { Option, PlanRequest } from '../types.ts'
import type { Walk } from '../walkmeasure.ts'
import { existing, legTrace, segments, stopKey, type PlaceRef, type Retime, type Segment, type TimedWalk } from '../walks.ts'
import { MapPane, MapSheet, TripRow } from './board.tsx'
import { RideCountdown, Timeline, type Tracking } from './option.tsx'
import { callPlaces, stopsPassed, type RideProgress } from '../countdown.ts'
import { position, rows } from '../options.ts'
import { advance, phaseOf, rowLines, shapeKey, walkLeftS, walkTo, type At, type LonLat } from '../progress.ts'
import { boarding, type Fix, type Sighting } from '../boarding.ts'
import { vehicleOf, type Doing } from './icons.tsx'
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
  tightS: number // the "tight" setting: a warning that's showing clears once there's this much to spare again
  dismissed?: string[] // "faster way" notices dismissed on this trip (suggestionKey): not offered again
  mapTall?: boolean // the phone's map is pulled down over the steps: it stays as you left it
}

export const TRIP_KEY = 'gymrouter.trip'

const REPLAN_MS = 30_000
const VEHICLE_MS = 10_000 // how often your vehicle's position is checked while you wait for it or ride it
const HISTORY_MS = 180_000 // your recent fixes, kept to compare with the vehicle's reports
const STALE_MS = 20_000 // a fix older than this isn't where you are now
const LOST_MS = 90_000 // no fix for this long (a tunnel, an underground platform): the clock says where you are

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
  const phaseRef = useRef<Phase>(phaseAt(o, now))
  const tripRef = useRef(trip) // the trip as it is now, for a re-check that started before it last changed
  tripRef.current = trip

  // Where you are on the steps: from your location along each step's line, the clock only as a fallback.
  const [shapes, setShapes] = useState<Record<string, LonLat[]>>({})
  const [shapeStops, setShapeStops] = useState<Record<string, LonLat[]>>({}) // the stops along each, before the last
  const [platforms, setPlatforms] = useState<Record<string, LonLat[]>>({}) // the platform each is boarded from, for trains
  const rides = o.legs.filter((l) => l.kind === 'ride' && l.trip_id && l.from && l.to)
  useEffect(() => {
    let live = true
    for (const l of rides) {
      const k = shapeKey(l)
      if (shapes[k]) continue
      api
        .shape(token, trip.serviceDate, l.trip_id!, l.from!.id, l.to!.id)
        .then((s) => {
          if (!live) return
          setShapes((cur) => ({ ...cur, [k]: s.coordinates }))
          setShapeStops((cur) => ({ ...cur, [k]: s.stops ?? [] }))
          if (s.platform) setPlatforms((cur) => ({ ...cur, [k]: s.platform! }))
        })
        .catch(() => undefined) // straight between the stops will do
    }
    return () => {
      live = false
    }
  }, [rides.map(shapeKey).join(',')])
  const steps = useMemo(() => rows(o, true), [o])
  // A walk you've timed and traced is followed along your own route, as on the map.
  const stepLines = useMemo(
    () => rowLines(steps, o, trip.origin, trip.destination, shapes, (i) => legTrace(walks, o, i, trip.ends.start.key, trip.ends.end.key)),
    [steps, shapes, walks],
  )
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
  // A fix that's gone quiet isn't where you are: underground the last one is the station entrance, and going by it
  // you'd never board. Without a recent one the clock decides, as it does with no location at all.
  const fixAge = now - (history.current.at(-1)?.t ?? 0)
  const here = pos && fixAge <= LOST_MS ? pos : null
  const posRef = useRef(here)
  posRef.current = here
  let where: Position | null = pos // you, on the map too
  const vehicleAt = (ride: number) => {
    const s = sightings.current[o.legs[ride]?.trip_id ?? '']?.at(-1)
    return s && { lat: s.lat, lon: s.lon, accuracy: 30 }
  }
  if (here) {
    // Aboard with no fresh fix of your own (a tunnel): where your vehicle is, is where you are.
    const ride = phaseRef.current.kind === 'riding' && onVehicle(phaseRef.current.ride) ? phaseRef.current.ride : -1
    const stale = fixAge > STALE_MS || here.accuracy > 100
    where = (stale && vehicleAt(ride)) || here
    at = advance(lastAt.current?.key === stepsKey ? lastAt.current.at : null, stepLines, where, clockAt)
    lastAt.current = { key: stepsKey, at }
    phase = phaseOf(o, steps, at, stepLines, where, onVehicle, now, (ride) => platforms[shapeKey(o.legs[ride])])
  } else if (phase.kind === 'riding') {
    where = vehicleAt(phase.ride) || pos // riding by the clock: your vehicle's last report, if it has made one
  }
  phaseRef.current = phase
  const riding = phase.kind === 'riding' ? o.legs[phase.ride].trip_id : undefined

  // Walking to a ride: how much of the trip's own walk is left, your timed one if there is one (not the street map's
  // route from wherever your location puts you). The time to spare and the re-checks both go by it.
  const nextRide = phase.kind === 'before' ? o.legs[phase.ride] : undefined
  const toRide = phase.kind === 'before' ? walkTo(steps, o, phase.ride, trip.request.prefs.leave_buffer_s) : null
  const leftS = !toRide || !here || here.accuracy > USABLE_M ? null
    : phase.kind === 'before' && phase.waiting ? 0 // at the stop already
      : walkLeftS(stepLines[toRide.row], toRide.secs, here, trip.walkSpeedMps)
  const leftRef = useRef<{ stop: string; secs: number } | null>(null)
  leftRef.current = nextRide?.from && leftS !== null ? { stop: stopKey({ stop: nextRide.from, mode: nextRide.line?.mode }), secs: leftS } : null

  const checkedRef = useRef(false)
  const held = useRef<Missed | null>(null) // the warning showing, which stays until it's comfortably clear (assess)

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
      const from = replanOrigin(trip.option, ph, posRef.current, trip.request.from, nowMs, leftRef.current)
      if (!from || (ph.kind !== 'before' && ph.kind !== 'riding')) return
      const time = from.on_trip ? undefined : replanTime(trip.option, ph, nowMs)
      try {
        // The rides still ahead are checked as they stand (keep); the search is for something better, or something else.
        const res = await api.plan(token, {
          from, to: trip.request.to, lines: trip.request.lines, time, window_min: 45, prefs: trip.request.prefs,
          keep: keepFrom(trip.option, ph.ride),
        })
        if (!live) return
        const a = assess(tripsFrom(trip.option, ph.ride), res.kept, res.options, trip.plannedArrive, { missed: held.current, clearS: trip.tightS })
        held.current = a.status === 'missed' ? a.missed : null
        setCheck(a)
        setCheckedAt(Date.now())
        setError('')
        // The same trip: take the live times for what's still ahead (also when a change in it has gone).
        if (a.current) onUpdate({ ...tripRef.current, option: merge(trip.option, ph.ride, a.current) })
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
    onUpdate({ ...trip, option: merge(o, keep, s), plannedArrive: s.arrive, dismissed: undefined })
    held.current = null
    setCheck(null)
  }

  // The ride you're on, in stops: counted from where you are on its line (your location, or your vehicle's), or with
  // nothing to place you by, from the times it calls at them. Never back a stop: a location wobbles.
  const counted = useRef<{ ride: string; passed: number } | null>(null)
  let ride: RideProgress | null = null
  const onLeg = phase.kind === 'riding' ? o.legs[phase.ride] : undefined
  if (onLeg?.calls?.length && phase.kind === 'riding') {
    const row = steps.findIndex((r) => r.kind === 'leg' && r.i === phase.ride)
    const placed = here && at.row === row
    const k = shapeKey(onLeg)
    const places = shapes[k] && shapeStops[k] ? callPlaces(shapes[k], shapeStops[k], onLeg.calls) : null
    const p = stopsPassed(onLeg.calls, places, placed ? at.frac : null, Date.parse(onLeg.dep), now)
    const before = counted.current?.ride === k ? counted.current.passed : 0
    counted.current = { ride: k, passed: Math.max(before, p.passed) }
    ride = {
      leg: phase.ride, total: onLeg.calls.length, passed: counted.current.passed,
      along: p.passed < counted.current.passed ? 0 : p.along, frac: at.row === row ? at.frac : 0,
    }
  }
  // Rides whose stops you've opened or closed yourself, in the steps here and under the map alike.
  const [opened, setOpened] = useState<Record<string, boolean>>({})

  // A faster way that's been dismissed stays away: the same vehicles aren't offered again on this trip.
  const faster = check?.status === 'better' && !trip.dismissed?.includes(suggestionKey(o, check.suggestion)) ? check : null
  // The map frames the step you're on once you've set off; until then, the whole trip.
  const focus = focusLegs(o, phase, steps[at.row]?.kind !== 'start')

  // You, on the map: on foot, or the vehicle you're on.
  const doing: Doing = phase.kind === 'riding' ? vehicleOf(o.legs[phase.ride].line?.mode) : 'walk'

  const ins = instruction(o, phase, trip.ends.end.name, ride ? ride.total - ride.passed : undefined)
  // The line under it: when the ride you're heading for leaves, or when you get off the one you're on.
  const d = ins.detail
  const nowDetail = !d ? null
    : phase.kind === 'before'
      ? (Date.parse(d.dep) < now - 30_000
          ? `${d.line?.name} was due ${clock(d.dep)} (${countdown(d.dep, now).replace(/^left /, '')})`
          : `${d.line?.name} leaves ${clock(d.dep)} (${countdown(d.dep, now)})`) + (d.status === 'predicted' ? `, ${delay(d.delay_s)}` : '')
      // The strip under the instruction names the stops, so the line only says when.
      : phase.kind === 'riding' ? `Get off at ${clock(d.arr)} (${countdown(d.arr, now)})${ride ? '' : `, ${placeName(d.to)}`}` : null
  const countdownStrip = ride && onLeg ? <RideCountdown leg={onLeg} ride={ride} /> : null
  // The trip's description: you on its rail, and its walks to time (from the map, timing goes back to this screen).

  const track: Tracking = {
    now,
    at,
    riding: phase.kind === 'riding',
    ride,
    opened,
    onOpen: (key, open) => setOpened((cur) => ({ ...cur, [key]: open })),
    segs: segments(o, trip.ends.start, trip.ends.end),
    onTime: (s) => {
      setMapOpen(false)
      setSaved('')
      setTiming(s)
    },
  }
  const lateBy = check && check.status !== 'missed' ? check.lateBy : 0
  const spareS = nextRide ? spareToBoard(nextRide, here, now, trip.walkSpeedMps, leftS) : null
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
          detail={check.suggestion && <div class="trip-row"><TripRow option={check.suggestion} /></div>}
          action={
            check.suggestion && (
              <Button onClick={() => switchTo(check.suggestion!)}>
                Switch to this
              </Button>
            )
          }
        >
          <strong>{missedText(check.missed)}</strong>{' '}
          {check.suggestion ? `Next best, ${changesNote(check.suggestion)}:` : "There's no other way on these lines right now."}
        </Callout>
      )}
      {faster && (
        <Callout
          tone="good"
          role="status"
          detail={<div class="trip-row"><TripRow option={faster.suggestion} /></div>}
          onDismiss={() => onUpdate({ ...trip, dismissed: [...(trip.dismissed ?? []), suggestionKey(o, faster.suggestion)] })}
          action={
            <Button onClick={() => switchTo(faster.suggestion)}>
              Switch to this
            </Button>
          }
        >
          <strong>A faster way just opened up:</strong>{' '}
          {duration((Date.parse(faster.current.arrive) - Date.parse(faster.suggestion.arrive)) / 1000)} sooner, {changesNote(faster.suggestion)}.
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
        {nowDetail && phase.kind === 'before' && (
          <p>
            {nowDetail}
            {spareS !== null && !(phase.kind === 'before' && phase.waiting) && (
              <span class={spareS < 0 ? 'spare bad' : spareS < 60 ? 'spare tight' : 'spare'}>
                {spareS < 0 ? `You need to hurry: ${Math.ceil(-spareS / 60)} min short at walking pace.` : `${spare(spareS)} from here.`}
              </span>
            )}
          </p>
        )}
        {countdownStrip}
        {nowDetail && phase.kind === 'riding' && <p>{nowDetail}</p>}
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
        {locState === 'on' && (here ? `Location on (±${Math.round(here.accuracy)} m). ` : pos ? 'No location just now; following the timetable. ' : 'Finding your location… ')}
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
          me={where}
          doing={doing}
          aboard={riding}
          walks={walks}
          places={{ start: trip.ends.start.key, end: trip.ends.end.key }}
          origin={trip.origin}
          destination={trip.destination}
          option={o}
          focus={focus}
          progress={ride}
        />
      )}

      {!wide && mapOpen && (
        <MapSheet
          live
          serviceDate={trip.serviceDate}
          token={token}
          me={where}
          doing={doing}
          aboard={riding}
          walks={walks}
          places={{ start: trip.ends.start.key, end: trip.ends.end.key }}
          origin={trip.origin}
          destination={trip.destination}
          title={trip.title}
          option={o}
          focus={focus}
          progress={ride}
          now={now}
          onClose={() => setMapOpen(false)}
          steps={{
            ends: trip.ends, track, now: { main: ins.now, detail: nowDetail, strip: countdownStrip },
            tall: !!trip.mapTall, onTall: (tall) => onUpdate({ ...trip, mapTall: tall }),
          }}
        />
      )}
    </div>
  )
}

/** What can't be made, in a sentence. */
function missedText(m: Missed): string {
  switch (m.why) {
    case 'gone':
      return 'One of your services is no longer running.'
    case 'board':
      return `You ${m.unsure ? 'may not' : "won't"} make the ${m.ride.line?.name ?? 'next service'} at ${clock(m.ride.dep)}.`
    case 'change':
      return `You ${m.unsure ? 'may not' : "won't"} make the change at ${placeName(m.at.to)}: the ${m.ride.line?.name ?? 'next service'} leaves at ${clock(m.ride.dep)}.`
  }
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
