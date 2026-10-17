// Trip results: the chosen option's countdown, then every option as a strip of "tape" filling its row: each ride
// named and as wide as its share of the trip, the walking between as dots.
import { useEffect, useRef, useState } from 'preact/hooks'
import type { ComponentChildren, ComponentType } from 'preact'
import { clock, countdown, dayOf, delay, duration, placeName, riskLabel, shortDuration } from '../format.ts'
import { useNow, useWide } from '../hooks.ts'
import type { Leg, Option } from '../types.ts'
import { Timeline, type Ends, type Tracking } from './option.tsx'
import { changesAndStops, reselect, rideIsLive, riskReason, tape, tripKey } from '../options.ts'
import type { MapViewProps } from '../map/mapview.tsx'
import { ActionBar, Button, Callout, LiveTag, onRadioKeys, TextButton, useDialog } from './ui.tsx'
import { lineColour, textOn } from '../colour.ts'

const ms = (iso: string) => Date.parse(iso)

interface Props {
  options: Option[]
  live: boolean
  updating?: boolean // a newer plan is slow to arrive: the countdown's tag says so (LiveTag)
  serviceDate: string
  token: string
  origin: [number, number]
  destination: [number, number]
  title: string
  onStart?: (o: Option) => void
  onShift: Shift
  preferLatest?: boolean // arriving by a time: the latest option that makes it is chosen first
}

/** Moves the window of options: -1 earlier, +1 later. */
export type Shift = (dir: -1 | 1) => void

/** "Earlier trips" and "Later trips", above the list of options (or in place of it when nothing leaves). */
export function WindowShift({ shift }: { shift: Shift }) {
  return (
    <div class="shift">
      <TextButton quiet onClick={() => shift(-1)}>
        Earlier trips
      </TextButton>
      <TextButton quiet onClick={() => shift(1)}>
        Later trips
      </TextButton>
    </div>
  )
}

export function Board(p: Props) {
  const first = p.preferLatest ? p.options.length - 1 : 0
  const [selected, setSelected] = useState(first)
  const [mapOpen, setMapOpen] = useState(false)
  const wide = useWide()
  const now = useNow(1000)
  const opts = p.options
  // Keep the selection on the same option when the list refreshes (live times move, so match on the
  // vehicles taken; failing that, the option leaving closest to the one chosen).
  const [selKey, setSelKey] = useState<{ trips: string; leave: number } | null>(null)
  useEffect(() => {
    setSelected(selKey ? reselect(opts, selKey.trips, selKey.leave) : first)
  }, [opts])

  if (opts.length === 0) return null
  const sel = opts[Math.min(selected, opts.length - 1)]
  const choose = (i: number) => {
    setSelected(i)
    setSelKey({ trips: tripKey(opts[i]), leave: ms(opts[i].leave_at) })
  }

  return (
    <div class="board">
      <Hero option={sel} live={p.live} now={now} updating={p.updating} />
      <WindowShift shift={p.onShift} />
      <ol class="strips" role="radiogroup" aria-label="Options" onKeyDown={onRadioKeys}>
        {opts.map((o, i) => (
          <li class={i === selected ? 'selected' : undefined}>
            <button class="strip-row" role="radio" aria-checked={i === selected} tabIndex={i === selected ? 0 : -1} onClick={() => choose(i)}>
              <TripRow option={o} />
            </button>
            {/* The chosen option says how long and how much changing it takes, under its row. */}
            {i === selected && (
              <p class="strip-note">
                <span>
                  {duration(o.duration_s)} door to door, {changesAndStops(o)}
                </span>
              </p>
            )}
          </li>
        ))}
      </ol>
      <ActionBar>
        {!wide && (
          <Button class="phone-only" onClick={() => setMapOpen(true)}>
            Show on map
          </Button>
        )}
        {p.onStart && (
          <Button variant="commit" onClick={() => p.onStart!(sel)}>
            Start trip
          </Button>
        )}
      </ActionBar>
      {wide && <MapPane {...p} option={sel} />}
      {!wide && mapOpen && (
        <MapSheet {...p} option={sel} now={now} onClose={() => setMapOpen(false)} onStart={p.onStart && (() => p.onStart!(sel))} />
      )}
    </div>
  )
}

/**
 * One trip as the list of options shows it: when it leaves, its tape, when it arrives, how sure its changes are and
 * how long it takes. The columns are its container's (.strips, or .trip-row in a notice that suggests the trip).
 */
export function TripRow({ option: o }: { option: Option }) {
  return (
    <>
      <span class="strip-leave">{clock(o.leave_at)}</span>
      <Strip option={o} />
      <span class="strip-arrive">{clock(o.arrive)}</span>
      <span class={`risk-mark risk-${o.risk}`} title={`${riskLabel(o.risk)} connections`}>
        <span class="sr-only">{riskLabel(o.risk)}</span>
      </span>
      <span class="strip-dur" title="Door to door">
        {shortDuration(o.duration_s)}
      </span>
    </>
  )
}

function firstRide(o: Option): Leg | undefined {
  return o.legs.find((l) => l.kind === 'ride')
}

function Hero({ option: o, live, now, updating }: { option: Option; live: boolean; now: number; updating?: boolean }) {
  const ride = firstRide(o)
  const cd = countdown(o.leave_at, now) // "in 18 min", "now", "left 3 min ago"
  const day = dayOf(o.leave_at, now)
  const [label, value] = !live
    ? [day ? `Leave ${day} at` : 'Leave at', clock(o.leave_at)]
    : cd.startsWith('in ')
      ? ['Leave in', cd.slice(3)]
      : cd.startsWith('left ')
        ? ['Left', cd.slice(5)]
        : ['Leave', cd]
  return (
    <section class="hero" aria-live="polite">
      <p class="label">
        {label}
        {/* Whether the ride the countdown counts to has live times; a trip that's only walking has none to have. */}
        {ride && <LiveTag live={rideIsLive(ride)} updating={updating} />}
      </p>
      <p class="hero-time num">{value}</p>
      <p class="hero-arrive">
        Arrive <span class="num">{clock(o.arrive)}</span>
      </p>
      {ride && (
        <p class="hero-sub">
          for the {ride.line?.name} at {clock(ride.dep)} from {placeName(ride.from)}
          {ride.status === 'predicted' && ride.delay_s !== undefined && ` (${delay(ride.delay_s)})`}
        </p>
      )}
      {/* How sure the trip is, beside the number it qualifies: the rows' squares only show its colour. */}
      <p class="hero-sub hero-risk">
        <span class={`badge risk-${o.risk}`}>{riskLabel(o.risk)}</span>
        {riskReason(o)}
      </p>
    </section>
  )
}

/**
 * One option as tape across the whole width, on its own scale: each ride in its line's colour and as wide as its share
 * of the trip, but never narrower than its name; the walking (to the first stop, between rides, from the last) as
 * dots that give up the room. The widths are the stylesheet's doing (.strip): the shares here are only where it starts.
 */
export function Strip({ option: o }: { option: Option }) {
  const pieces = tape(o)
  const names = pieces.map((p) => (p.kind === 'ride' ? (p.leg.line?.name ?? '') : null)).filter((n) => n !== null)
  // What the stylesheet needs to know how narrow the walks may get: the names to fit (in digit widths: a letter is
  // about a quarter wider), in how many rides, between how many walks.
  const text = names.join('')
  const digits = text.replace(/\D/g, '').length
  const width = digits + (text.length - digits) * 1.25
  const fit = { '--names': `${width}`, '--rides': `${names.length}`, '--walks': `${pieces.length - names.length}` }
  return (
    <span class="strip" style={fit} aria-label={o.lines.join(', ')}>
      {pieces.map((p) => {
        const share = `${p.share * 100}%`
        if (p.kind === 'walk') return <span class="seg walk" style={{ flexBasis: share }} />
        const l = p.leg
        const bg = lineColour(l.line?.color)
        return (
          <span
            class={`seg ride${l.status === 'predicted' && (l.delay_s ?? 0) >= 120 ? ' late' : ''}`}
            style={{ flexBasis: share, background: bg, color: textOn(bg) }}
          >
            {l.line?.name}
          </span>
        )
      })}
    </span>
  )
}

/** The map, loaded only when it's first needed (MapLibre is a large download). */
function useMapView(): { View: ComponentType<MapViewProps> | null; failed: boolean } {
  const [View, setView] = useState<ComponentType<MapViewProps> | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    import('../map/mapview.tsx').then((m) => setView(() => m.MapView)).catch(() => setFailed(true))
  }, [])
  return { View, failed }
}

type MapProps = Pick<Props, 'token' | 'serviceDate' | 'origin' | 'destination'> & {
  option: Option
  me?: MapViewProps['me']
  doing?: MapViewProps['doing']
  aboard?: MapViewProps['aboard']
  focus?: MapViewProps['focus']
  progress?: MapViewProps['progress']
  catching?: MapViewProps['catching']
}

function MapBody({ View, failed, ...p }: MapProps & { View: ComponentType<MapViewProps> | null; failed: boolean }) {
  if (failed) {
    return (
      <div class="map-wrap">
        <Callout tone="bad">Couldn't load the map. Check your connection and try again.</Callout>
      </div>
    )
  }
  if (!View) {
    return (
      <div class="map-wrap">
        <Callout class="subtle">Loading map…</Callout>
      </div>
    )
  }
  return <View {...p} />
}

/** Desktop: the map beside the trip, always showing the option chosen. */
export function MapPane(p: MapProps) {
  const map = useMapView()
  return (
    <aside class="map-pane" aria-label="Map of the trip">
      <MapBody {...p} {...map} />
    </aside>
  )
}

type MapSheetProps = Omit<Props, 'onStart' | 'options' | 'onShift'> & {
  option: Option
  now: number
  onClose: () => void
  onStart?: () => void // offered on the map so a trip can start without closing it
  me?: MapViewProps['me']
  doing?: MapViewProps['doing']
  aboard?: MapViewProps['aboard']
  focus?: MapViewProps['focus']
  progress?: MapViewProps['progress']
  catching?: MapViewProps['catching']
  steps?: InTripSteps // during a trip: the trip's description under the map
}

/** What sits under the map during a trip: the steps, or with the map pulled down over them, just what to do now. */
export interface InTripSteps {
  ends: Ends
  track: Tracking
  now: { main: string; detail?: string | null; strip?: ComponentChildren } // strip: the ride you're on, counted down (RideCountdown)
  tall: boolean // the map is pulled down
  onTall: (tall: boolean) => void
}

/** How far the handle has to be dragged to move the map (less is a tap, or a change of mind). */
const PULL_PX = 24

/** Phones: the map over everything, with the way back first and, while planning, Start trip where the thumb is. */
export function MapSheet({ option, now, onClose, onStart, title, live, steps, ...p }: MapSheetProps) {
  // Open with the step you're on in view (and when the steps come back from under the map).
  const stepsRef = useRef<HTMLDivElement>(null)
  useEffect(() => stepsRef.current?.querySelector('.you')?.scrollIntoView({ block: 'center' }), [steps?.tall])
  // The handle under the map: drag it down and the map takes the steps' room, up and they come back; a tap does either.
  const [pull, setPull] = useState<number | null>(null) // the steps' height while the handle is being dragged
  const grip = useRef<{ y: number; h: number; moved: boolean } | null>(null)
  const dragged = useRef(false)
  const handle = steps && {
    onPointerDown: (e: PointerEvent) => {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
      grip.current = { y: e.clientY, h: steps.tall ? 0 : (stepsRef.current?.offsetHeight ?? 0), moved: false }
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
      if (Math.abs(e.clientY - g.y) >= PULL_PX) steps.onTall(e.clientY > g.y)
    },
    onPointerCancel: () => {
      grip.current = null
      setPull(null)
    },
    onClick: () => {
      if (!dragged.current) steps.onTall(!steps.tall)
      dragged.current = false
    },
  }
  const map = useMapView()
  const sheetRef = useRef<HTMLDivElement>(null)
  useDialog(sheetRef, onClose) // focus starts on Back; Escape closes; the page behind is inert
  return (
    <div ref={sheetRef} class="sheet" role="dialog" aria-modal="true" aria-label={`Map: ${title}`}>
      <header class="sheet-bar">
        <Button onClick={onClose}>
          <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <path d="M15 5l-7 7 7 7" />
          </svg>
          {steps ? 'Back to trip' : 'Back'}
        </Button>
        <span class="sheet-title">{title}</span>
      </header>
      <MapBody {...p} option={option} {...map} />
      <footer class={steps ? 'sheet-summary pull' : 'sheet-summary'}>
        {steps && (
          <button class="sheet-handle" aria-expanded={!steps.tall} aria-label={steps.tall ? "Show the trip's steps" : 'Show more of the map'} {...handle} />
        )}
        <div>
          {steps ? (
            // Under way: what matters is when you get there.
            <p>
              <span class="sheet-leave num">Arrive {clock(option.arrive)}</span>
              <span class="meta">{countdown(option.arrive, now).replace(/^left .*/, 'arrived')}</span>
            </p>
          ) : (
            <p>
              <span class="sheet-leave num">
                {live
                  ? Date.parse(option.leave_at) < now - 60_000
                    ? `Left ${countdown(option.leave_at, now).replace(/^left /, '')}`
                    : `Leave ${countdown(option.leave_at, now)}`
                  : `Leave ${[dayOf(option.leave_at, now), clock(option.leave_at)].filter(Boolean).join(' ')}`}
              </span>
              <span class="meta">Arrive {clock(option.arrive)}</span>
            </p>
          )}
          {!steps && <Strip option={option} />}
        </div>
        {onStart && (
          <Button variant="commit" onClick={onStart}>
            Start trip
          </Button>
        )}
        {steps && steps.tall && pull === null && (
          <div class="sheet-now">
            <p>{steps.now.main}</p>
            {steps.now.strip ?? (steps.now.detail && <p class="meta">{steps.now.detail}</p>)}
          </div>
        )}
        {steps && (!steps.tall || pull !== null) && (
          <div class="sheet-steps" ref={stepsRef} style={pull === null ? undefined : { height: `${pull}px`, overflow: 'hidden' }}>
            <Timeline option={option} ends={steps.ends} track={steps.track} />
          </div>
        )}
      </footer>
    </div>
  )
}
