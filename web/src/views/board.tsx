// Trip results: the chosen option's countdown, then every option as a strip of "tape" on one
// shared time axis, so when each leaves and arrives can be compared at a glance.
import { useEffect, useRef, useState } from 'preact/hooks'
import type { ComponentType } from 'preact'
import { clock, countdown, dayOf, delay, duration, placeName, riskLabel, shortDuration } from '../format.ts'
import { useNow, useWide } from '../hooks.ts'
import type { TimedWalk } from '../walks.ts'
import type { Leg, Option } from '../types.ts'
import { Timeline, type Ends, type Tracking } from './option.tsx'
import { changesAndStops, reselect, tripKey } from '../options.ts'
import type { MapViewProps } from '../map/mapview.tsx'
import { ActionBar, Button, Callout, onRadioKeys, TextButton, useDialog } from './ui.tsx'
import { lineColour, textOn } from '../colour.ts'

const ms = (iso: string) => Date.parse(iso)

interface Props {
  options: Option[]
  live: boolean
  serviceDate: string
  token: string
  walks: TimedWalk[] // walks you've timed; their traced routes are drawn instead of the street-map ones
  places: { start?: string; end?: string } // the home or gym at each end (placeKey)
  origin: [number, number]
  destination: [number, number]
  title: string
  ends: Ends // the home or gym at each end: the ends of the description's rail
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
  const start = Math.min(...opts.map((o) => ms(o.leave_at)))
  const end = Math.max(...opts.map((o) => ms(o.arrive)))
  const choose = (i: number) => {
    setSelected(i)
    setSelKey({ trips: tripKey(opts[i]), leave: ms(opts[i].leave_at) })
  }

  return (
    <div class="board">
      <Hero option={sel} live={p.live} now={now} />
      <WindowShift shift={p.onShift} />
      <ol class="strips" role="radiogroup" aria-label="Options" onKeyDown={onRadioKeys}>
        {opts.map((o, i) => (
          <li class={i === selected ? 'selected' : undefined}>
            <button class="strip-row" role="radio" aria-checked={i === selected} tabIndex={i === selected ? 0 : -1} onClick={() => choose(i)}>
              <span class="strip-leave">{clock(o.leave_at)}</span>
              <Strip option={o} start={start} end={end} />
              <span class="strip-arrive">{clock(o.arrive)}</span>
              <span class={`risk-mark risk-${o.risk}`} title={`${riskLabel(o.risk)} connections`}>
                <span class="sr-only">{riskLabel(o.risk)}</span>
              </span>
              <span class="strip-dur" title="Door to door">
                {shortDuration(o.duration_s)}
              </span>
            </button>
            {o.alternative && i === selected && <p class="alt-note">A different route, a little slower than the best.</p>}
          </li>
        ))}
      </ol>
      <p class="summary">
        <strong>{duration(sel.duration_s)}</strong> door to door, {changesAndStops(sel)}
      </p>
      <Timeline option={sel} ends={p.ends} walks={p.walks} />
      <ActionBar>
        {!wide && (
          <Button class="phone-only" onClick={() => setMapOpen(true)}>
            Show on map
          </Button>
        )}
        {p.onStart && (
          <Button variant="primary" onClick={() => p.onStart!(sel)}>
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

function firstRide(o: Option): Leg | undefined {
  return o.legs.find((l) => l.kind === 'ride')
}

function Hero({ option: o, live, now }: { option: Option; live: boolean; now: number }) {
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
      <p class="label">{label}</p>
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
    </section>
  )
}

/** One option as tape segments on the shared axis: dashed for walking, line colours for rides. */
export function Strip({ option: o, start, end }: { option: Option; start: number; end: number }) {
  const span = Math.max(1, end - start)
  const pct = (t: number) => `${((t - start) / span) * 100}%`
  return (
    <span class="strip" aria-label={o.lines.join(', ')}>
      {o.legs.map((l) => {
        const left = pct(ms(l.dep))
        const width = `${Math.max(0.8, ((ms(l.arr) - ms(l.dep)) / span) * 100)}%`
        if (l.kind === 'walk') return <span class="seg walk" style={{ left, width }} />
        const bg = lineColour(l.line?.color)
        // Only label segments wide enough to show the whole name; a cut-off "2" for 292 misleads.
        const fits = ((ms(l.arr) - ms(l.dep)) / span) * 100 >= 3 + 2.2 * (l.line?.name.length ?? 0)
        return (
          <span
            class={`seg ride${l.status === 'predicted' && (l.delay_s ?? 0) >= 120 ? ' late' : ''}`}
            style={{ left, width, background: bg, color: textOn(bg) }}
            title={l.line?.name}
          >
            {fits ? l.line?.name : ''}
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

type MapProps = Pick<Props, 'token' | 'walks' | 'places' | 'serviceDate' | 'origin' | 'destination'> & {
  option: Option
  me?: MapViewProps['me']
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

type MapSheetProps = Omit<Props, 'onStart' | 'options' | 'onShift' | 'ends'> & {
  option: Option
  now: number
  onClose: () => void
  onStart?: () => void // offered on the map so a trip can start without closing it
  me?: MapViewProps['me']
  steps?: { ends: Ends; track: Tracking } // during a trip: the trip's description under the map
}

/** Phones: the map over everything, with the way back first and, while planning, Start trip where the thumb is. */
export function MapSheet({ option, now, onClose, onStart, title, live, steps, ...p }: MapSheetProps) {
  // Open with the step you're on in view.
  const stepsRef = useRef<HTMLDivElement>(null)
  useEffect(() => stepsRef.current?.querySelector('.you')?.scrollIntoView({ block: 'center' }), [])
  const map = useMapView()
  const sheetRef = useRef<HTMLDivElement>(null)
  useDialog(sheetRef, onClose) // focus starts on Back; Escape closes; the page behind is inert
  const start = ms(option.leave_at)
  const end = ms(option.arrive)
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
      <footer class="sheet-summary">
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
          {!steps && <Strip option={option} start={start} end={end} />}
        </div>
        {onStart && (
          <Button variant="primary" onClick={onStart}>
            Start trip
          </Button>
        )}
        {steps && (
          <div class="sheet-steps" ref={stepsRef}>
            <Timeline option={option} ends={steps.ends} walks={p.walks} track={steps.track} />
          </div>
        )}
      </footer>
    </div>
  )
}
