// In-trip mode: follows the chosen option, re-checks it against live data whenever the server has new data (else
// every 60 s) from wherever you are (or the vehicle you're on), and suggests a switch when something slips.
import { useEffect, useMemo, useRef, useState, type MutableRef } from 'preact/hooks'
import { api, AuthError, problem } from '../api.ts'
import { clock, countdown, delay, duration, placeName, platform, spare } from '../format.ts'
import { useLiveEvents, useNow, useVisible, useWide } from '../hooks.ts'
import { assess, changesNote, focusLegs, instruction, keepFrom, phaseAt, replanOrigin, replanTime, spareToBoard, stopKey, suggestionKey, tripsFrom, USABLE_M, type Assessment, type Missed, type Phase, type Position } from '../intrip.ts'
import type { Option, PlanRequest } from '../types.ts'
import type { PlaceRef } from '../settings.ts'
import { MapPane, TripMap, TripRow } from './board.tsx'
import { RideCountdown, Timeline, type Tracking } from './option.tsx'
import { callPlaces, stopsPassed, type RideProgress } from '../countdown.ts'
import { position, rideIsLive, rows } from '../options.ts'
import { advance, phaseOf, rowLines, shapeKey, walkLeftS, walkTo, type At, type LonLat } from '../progress.ts'
import { boarding, type Fix, type Sighting } from '../boarding.ts'
import { every } from '../live.ts'
import { vehicleOf, type Doing } from './icons.tsx'
import { Button, Callout, Confirm, LiveTag, TextButton } from './ui.tsx'
import * as appClock from '../clock.ts'
import * as sim from '../sim.ts'

/** Everything needed to resume a trip after a reload; kept on this device only. */
export interface ActiveTrip {
  option: Option // the option as currently committed (updated with live times)
  plannedArrive: string
  request: PlanRequest // the original request (from/to/prefs)
  ends: { start: PlaceRef; end: PlaceRef } // the home or gym at each end
  title: string
  origin: [number, number]
  destination: [number, number]
  serviceDate: string
  walkSpeedMps: number
  tightS: number // the "tight" setting: a warning that's showing clears once there's this much to spare again
  dismissed?: string[] // "faster way" notices dismissed on this trip (suggestionKey): not offered again
  mapTall?: boolean // the phone's map is pulled down over the steps: it stays as you left it
  sim?: boolean // simulated (sim.ts): the simulator's clock, location and vehicles, and the timetable only
}

export const TRIP_KEY = 'gymrouter.trip'

const REPLAN_MS = 60_000 // without word from the server that its live data changed (useLiveEvents)
const REPLAN_UNHEARD_MS = 30_000 // while that word can't be heard
// Your vehicle's position is checked as soon as the server says new positions are in (about every 15 s), while you
// wait for it or ride it; else every 30 s, or every 10 s while the server can't be heard (useLiveEvents).
const VEHICLE_MS = 30_000
const VEHICLE_UNHEARD_MS = 10_000
const HISTORY_MS = 180_000 // your recent fixes, kept to compare with the vehicle's reports
const STALE_MS = 20_000 // a fix older than this isn't where you are now
const LOST_MS = 90_000 // no fix for this long (a tunnel, an underground platform): the clock says where you are

export function InTrip({ trip, token, onUpdate, onEnd, onAuthError }: {
  trip: ActiveTrip
  token: string
  onUpdate: (t: ActiveTrip) => void
  onEnd: () => void
  onAuthError: () => void
}) {
  const now = useNow(1000)
  const visible = useVisible()
  const geo = trip.sim ? sim.geolocation() : navigator.geolocation
  const [pos, setPos] = useState<Position | null>(null)
  // Location is on for the whole trip (it's what makes the map and "time to spare" useful); it stays on this device.
  const [locState, setLocState] = useState<'on' | 'denied'>(() => (geo ? 'on' : 'denied'))
  const [check, setCheck] = useState<Assessment | null>(null)
  const [error, setError] = useState('')
  const [checked, setChecked] = useState(false) // the last re-check worked, so the rides ahead have its times
  const [ending, setEnding] = useState(false)
  const wide = useWide()
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
  const stepLines = useMemo(() => rowLines(steps, o, trip.origin, trip.destination, shapes), [steps, shapes])
  if (trip.sim) sim.see({ option: o, rows: steps, lines: stepLines, shapes, stops: shapeStops, destination: trip.destination, walkSpeedMps: trip.walkSpeedMps })
  // On your vehicle? Your fixes moving with its live reports say so (boarding.ts); polled while you wait for it or ride it.
  const history = useRef<Fix[]>([])
  const sightings = useRef<Record<string, Sighting[]>>({})
  const [aboard, setAboard] = useState<Set<string>>(new Set()) // trip IDs you've been seen on
  const recheck = useRef<() => void>(() => undefined)
  const watching = phaseRef.current.kind === 'before' || phaseRef.current.kind === 'riding' ? o.legs[phaseRef.current.ride] : undefined
  // While there's a service ahead, the server says when its live data changes (the trip is checked again then) and
  // when new vehicle positions are in (yours is looked for then).
  const events = useLiveEvents(token, visible && watching !== undefined)
  const heard = useRef({ realtime: events.realtime, connected: events.connected })
  const connectedRef = useRef(events.connected)
  connectedRef.current = events.connected
  const vehicleChecks = useRef<ReturnType<typeof every> | null>(null)
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
    // Now, and again after a while unless new positions come in first (the wait starts again from each check).
    const checks = every(tick, () => (connectedRef.current ? VEHICLE_MS : VEHICLE_UNHEARD_MS))
    vehicleChecks.current = checks
    checks.run()
    return () => {
      live = false
      checks.stop()
      vehicleChecks.current = null
    }
  }, [watching?.trip_id, locState])
  useEffect(() => {
    if (events.vehicles > 0) vehicleChecks.current?.run()
  }, [events.vehicles])
  useEffect(() => vehicleChecks.current?.retime(), [events.connected]) // the wait changes with it
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
    if (locState !== 'on' || !geo) return
    const id = geo.watchPosition(
      (p) => {
        const f = { lat: p.coords.latitude, lon: p.coords.longitude, accuracy: p.coords.accuracy, t: p.timestamp || appClock.now() }
        setPos(f)
        history.current = [...history.current.filter((h) => h.t > f.t - HISTORY_MS), f]
      },
      (e) => e.code === e.PERMISSION_DENIED && setLocState('denied'),
      { enableHighAccuracy: true, maximumAge: 10_000, timeout: 30_000 },
    )
    return () => geo.clearWatch(id)
  }, [locState])

  // Re-check the trip straight away, then each time the server's live data changes, and on a timer, while the screen
  // is visible.
  useEffect(() => {
    let live = true
    const run = async () => {
      const nowMs = appClock.now()
      const ph = phaseRef.current
      const from = replanOrigin(trip.option, ph, posRef.current, trip.request.from, nowMs, leftRef.current)
      if (!from || (ph.kind !== 'before' && ph.kind !== 'riding')) return
      const time = from.on_trip ? undefined : replanTime(trip.option, ph, nowMs)
      // Simulated: at the simulator's time, since the server's clock isn't it (api.plan asks for the timetable alone).
      const simulated = trip.sim ? { time: time ?? new Date(nowMs).toISOString() } : {}
      try {
        // The rides still ahead are checked as they stand (keep); the search is for something better, or something else.
        const res = await api.plan(token, {
          from, to: trip.request.to, lines: trip.request.lines, time, window_min: 45, prefs: trip.request.prefs,
          keep: keepFrom(trip.option, ph.ride), ...simulated,
        })
        if (!live) return
        const a = assess(tripsFrom(trip.option, ph.ride), res.kept, res.options, trip.plannedArrive, { missed: held.current, clearS: trip.tightS })
        held.current = a.status === 'missed' ? a.missed : null
        setCheck(a)
        setChecked(true)
        setError('')
        // The same trip: take the live times for what's still ahead (also when a change in it has gone).
        if (a.current) onUpdate({ ...tripRef.current, option: merge(trip.option, ph.ride, a.current) })
      } catch (e) {
        if (e instanceof AuthError) onAuthError()
        else if (live) {
          setError(`Couldn't re-check the trip: ${problem(e)}`)
          setChecked(false) // unknown until a check works again; the line below says why
        }
      }
    }
    recheck.current = run
    // The stream connecting or dropping only changes the timer: no re-check for that alone.
    const onlyHeard = heard.current.connected !== events.connected && heard.current.realtime === events.realtime
    heard.current = { realtime: events.realtime, connected: events.connected }
    if (!onlyHeard && (visible || !checkedRef.current)) run()
    checkedRef.current = true
    // On the app's clock, so a simulation re-checks as often as a trip would (but not more than every 3 s of real time).
    const stop = visible ? appClock.repeat(events.connected ? REPLAN_MS : REPLAN_UNHEARD_MS, run, 3000) : undefined
    return () => {
      live = false
      stop?.()
    }
  }, [visible, trip.plannedArrive, tripsFrom(trip.option, 0).join(','), events.realtime, events.connected])

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
  const pf = d && platform(d.from) // "Platform 1", when the stop is one of a station's
  const from = pf ? ` from ${pf[0].toLowerCase()}${pf.slice(1)}` : ''
  const nowDetail = !d ? null
    : phase.kind === 'before'
      ? (Date.parse(d.dep) < now - 30_000
          ? `${d.line?.name} was due ${clock(d.dep)}${from} (${countdown(d.dep, now).replace(/^left /, '')})`
          : `${d.line?.name} leaves ${clock(d.dep)}${from} (${countdown(d.dep, now)})`) + (d.status === 'predicted' ? `, ${delay(d.delay_s)}` : '')
      // The strip under the instruction names the stops, so the line only says when.
      : phase.kind === 'riding' ? `Get off at ${clock(d.arr)} (${countdown(d.arr, now)})${ride ? '' : `, ${placeName(d.to)}`}` : null
  const countdownStrip = ride && onLeg ? <RideCountdown leg={onLeg} ride={ride} /> : null
  // The train you're getting to or waiting for, when the server has said where to board it.
  const catching = phase.kind === 'before' && o.legs[phase.ride]?.board ? phase.ride : null
  // The trip's description, with you on its rail.
  const track: Tracking = {
    now,
    at,
    riding: phase.kind === 'riding',
    ride,
    opened,
    onOpen: (key, open) => setOpened((cur) => ({ ...cur, [key]: open })),
  }
  const spareS = nextRide ? spareToBoard(nextRide, here, now, trip.walkSpeedMps, leftS) : null
  const lostLocation = locState === 'on' && !here && !!pos // there was a fix, and now there isn't

  // The steps open with the step you're on in view, again once your location first places you (until then it's the
  // clock's guess), and when they come back from under the map.
  const stepsRef = useRef<HTMLDivElement>(null)
  const located = here !== null
  useEffect(() => {
    // Only the steps scroll for it: scrollIntoView would scroll the panel too, and the handle with it.
    const box = stepsRef.current
    const you = box?.querySelector('.you')
    if (box && you) box.scrollTop += you.getBoundingClientRect().top - box.getBoundingClientRect().top - box.clientHeight / 2
  }, [trip.mapTall, located])
  const handle = usePullHandle(stepsRef, !!trip.mapTall, (tall) => onUpdate({ ...tripRef.current, mapTall: tall }))
  const tall = !wide && !!trip.mapTall // the phone's map pulled down over the steps

  const map = {
    serviceDate: trip.serviceDate, token, me: where, doing, aboard: riding, option: o, focus, progress: ride, catching,
    places: [trip.ends.start, trip.ends.end].map((p) => ({ kind: p.key.startsWith('home:') ? ('home' as const) : ('gym' as const), lon: p.lon, lat: p.lat })),
  }

  return (
    <div class="intrip">
      <div class="trip-top">
        <header class="trip-bar">
          <img class="app-icon" src="/icon.svg" alt="" width={28} height={28} />
          <span class="sheet-title">{trip.title}</span>
          {/* Quiet, and at the right: it never looks like a way back. */}
          {!ending && (
            <TextButton quiet onClick={() => setEnding(true)}>
              End trip
            </TextButton>
          )}
        </header>
        {ending && <EndTripConfirm onKeep={() => setEnding(false)} onConfirm={onEnd} />}
      </div>

      {wide ? <MapPane {...map} /> : <TripMap {...map} />}

      <section class="trip-panel" aria-label="The trip">
        {/* Outside what scrolls, so it's always there on the rule under the map. */}
        {!wide && (
          <button class="sheet-handle" aria-expanded={!tall} aria-label={tall ? "Show the trip's steps" : 'Show more of the map'} {...handle.props} />
        )}
        <div class="trip-panel-body">
          <p class="trip-arrive">
            <span class="num">
              {phase.kind === 'arrived' ? 'Arrived' : 'Arrive'} {clock(o.arrive)}
            </span>
            <span class="meta">
              {phase.kind !== 'arrived' && countdown(o.arrive, now).replace(/^left .*/, '')}
              {/* Whether the ride you're on, or on your way to, has live times, as the last re-check left them. */}
              {checked && (phase.kind === 'before' || phase.kind === 'riding') && <LiveTag live={rideIsLive(o.legs[phase.ride])} />}
            </span>
          </p>

          {check?.status === 'missed' && (
            <Callout
              tone="bad"
              role="alert"
              detail={check.suggestion && <div class="trip-row"><TripRow option={check.suggestion} /></div>}
              action={
                check.suggestion && (
                  <Button variant="commit" onClick={() => switchTo(check.suggestion!)}>
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
                <Button variant="commit" onClick={() => switchTo(faster.suggestion)}>
                  Switch to this
                </Button>
              }
            >
              <strong>A faster way just opened up:</strong>{' '}
              {duration((Date.parse(faster.current.arrive) - Date.parse(faster.suggestion.arrive)) / 1000)} sooner, {changesNote(faster.suggestion)}.
            </Callout>
          )}

          <div class="trip-now" aria-live="polite">
            <p class="trip-now-main">{ins.now}</p>
            {nowDetail && phase.kind === 'before' && (
              <p class="meta">
                {nowDetail}
                {spareS !== null && !phase.waiting && (
                  <span class={spareS < 0 ? 'spare bad' : spareS < 60 ? 'spare tight' : 'spare'}>
                    {spareS < 0 ? `You need to hurry: ${Math.ceil(-spareS / 60)} min short at walking pace.` : `${spare(spareS)} from here.`}
                  </span>
                )}
              </p>
            )}
            {/* On a ride whose stops are known, the strip names them, in place of the line under the instruction. */}
            {countdownStrip ?? (nowDetail && phase.kind === 'riding' && <p class="meta">{nowDetail}</p>)}
          </div>

          {/* Only when something is off: with location on and the last check good, there's nothing to say. */}
          {(lostLocation || locState === 'denied' || error) && (
            <p class="meta trip-status">
              {lostLocation && 'No location just now; following the timetable. '}
              {locState === 'denied' && 'Location unavailable; following the timetable. '}
              {error}
              {locState === 'denied' && geo && (
                <>
                  {' '}
                  <TextButton onClick={() => setLocState('on')}>Try my location again</TextButton>
                </>
              )}
            </p>
          )}

          {(!tall || handle.pull !== null) && (
            <div class="trip-steps" ref={stepsRef} style={handle.pull === null ? undefined : { height: `${handle.pull}px`, overflow: 'hidden' }}>
              <Timeline option={o} ends={trip.ends} track={track} />
            </div>
          )}
        </div>
      </section>
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

/** Asks before ending the trip, under the header with End trip in it. */
function EndTripConfirm({ onKeep, onConfirm }: { onKeep: () => void; onConfirm: () => void }) {
  return (
    <Confirm
      question="End this trip?"
      detail="Live tracking and re-checks stop."
      keep="Keep going"
      confirm="End trip"
      onKeep={onKeep}
      onConfirm={onConfirm}
    />
  )
}

/** How far the handle has to be dragged to move the map (less is a tap, or a change of mind). */
const PULL_PX = 24

/**
 * The handle on the rule under the phone's map: drag it down and the map takes the steps' room, up and they come
 * back; a tap does either. `pull` is the steps' height while it's being dragged.
 */
function usePullHandle(steps: MutableRef<HTMLDivElement | null>, tall: boolean, onTall: (tall: boolean) => void) {
  const [pull, setPull] = useState<number | null>(null)
  const grip = useRef<{ y: number; h: number; moved: boolean } | null>(null)
  const dragged = useRef(false)
  const props = {
    onPointerDown: (e: PointerEvent) => {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
      grip.current = { y: e.clientY, h: tall ? 0 : (steps.current?.offsetHeight ?? 0), moved: false }
    },
    onPointerMove: (e: PointerEvent) => {
      const g = grip.current
      if (!g) return
      if (Math.abs(e.clientY - g.y) > 4) g.moved = true
      if (g.moved) setPull(Math.max(0, g.h - (e.clientY - g.y)))
    },
    onPointerUp: (e: PointerEvent) => {
      const g = grip.current
      grip.current = null
      setPull(null)
      if (!g?.moved) return
      dragged.current = true // the click that follows a drag isn't a tap
      if (Math.abs(e.clientY - g.y) >= PULL_PX) onTall(e.clientY > g.y)
    },
    onPointerCancel: () => {
      grip.current = null
      setPull(null)
    },
    onClick: () => {
      if (!dragged.current) onTall(!tall)
      dragged.current = false
    },
  }
  return { props, pull }
}
