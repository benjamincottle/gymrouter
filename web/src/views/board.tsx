// Trip results: the chosen option's countdown, then every option as a strip of "tape" on one
// shared time axis, so when each leaves and arrives can be compared at a glance.
import { useEffect, useState } from 'preact/hooks'
import type { ComponentType } from 'preact'
import { clock, countdown, delay, duration, placeName, riskLabel, shortDuration } from '../format.ts'
import { useNow } from '../hooks.ts'
import type { TimedWalk } from '../walks.ts'
import type { Leg, Option, StopRef } from '../types.ts'
import { Timeline } from './option.tsx'
import { changesAndStops, reselect, tripKey } from '../options.ts'
import type { MapViewProps } from '../map/mapview.tsx'

const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : undefined)
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
  destinationName: string // the gym or home the trip ends at
  onSetChange: (a: StopRef, b: StopRef, secs: number) => void
  onStart?: (o: Option) => void
  onShift: Shift
}

/** Moves the window of options: -1 earlier, +1 later. `canEarlier` is false when the window already starts now. */
export interface Shift {
  (dir: -1 | 1): void
  canEarlier: boolean
}

/** "Earlier trips" and "Later trips", above the list of options (or in place of it when nothing leaves). */
export function WindowShift({ shift }: { shift: Shift }) {
  return (
    <div class="shift">
      <button class="link" disabled={!shift.canEarlier} onClick={() => shift(-1)}>
        Earlier trips
      </button>
      <button class="link" onClick={() => shift(1)}>
        Later trips
      </button>
    </div>
  )
}

export function Board(p: Props) {
  const [selected, setSelected] = useState(0)
  const [mapOpen, setMapOpen] = useState(false)
  const now = useNow(1000)
  const opts = p.options
  // Keep the selection on the same option when the list refreshes (live times move, so match on the
  // vehicles taken; failing that, the option leaving closest to the one chosen).
  const [selKey, setSelKey] = useState<{ trips: string; leave: number } | null>(null)
  useEffect(() => {
    setSelected(selKey ? reselect(opts, selKey.trips, selKey.leave) : 0)
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
      <ol class="strips" aria-label="Options">
        {opts.map((o, i) => (
          <li>
            <button class={i === selected ? 'strip-row selected' : 'strip-row'} aria-pressed={i === selected} onClick={() => choose(i)}>
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
      <div class="details">
        <div class="details-head">
          <p>
            <strong>{duration(sel.duration_s)}</strong> door to door, {changesAndStops(sel)}
          </p>
          <span class="actions">
            {p.onStart && (
              <button class="primary" onClick={() => p.onStart!(sel)}>
                Start trip
              </button>
            )}
            <button class="ghost" onClick={() => setMapOpen(true)}>
              Show on map
            </button>
          </span>
        </div>
        <Timeline option={sel} destination={p.destinationName} walks={p.walks} onSetChange={p.onSetChange} />
      </div>
      {mapOpen && (
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
  const cd = countdown(o.leave_at, now)
  return (
    <section class="hero" aria-live="polite">
      <p class="hero-label">{live ? 'Leave' : 'Leave at'}</p>
      <p class="hero-time">{live ? cd.replace(/^in /, '') : clock(o.leave_at)}</p>
      {ride && (
        <p class="hero-sub">
          for the {ride.line?.name} at {clock(ride.dep)} from {placeName(ride.from)}
          {ride.status === 'predicted' && ride.delay_s !== undefined && ` (${delay(ride.delay_s)})`}
        </p>
      )}
      <p class="hero-arrive">
        Arrive <strong>{clock(o.arrive)}</strong>
      </p>
    </section>
  )
}

/** One option as tape segments on the shared axis: dashed for walking, line colours for rides. */
export function Strip({ option: o, start, end, now }: { option: Option; start: number; end: number; now?: number }) {
  const span = Math.max(1, end - start)
  const pct = (t: number) => `${((t - start) / span) * 100}%`
  return (
    <span class="strip" aria-label={o.lines.join(', ')}>
      {now !== undefined && now >= start && now <= end && <span class="now-mark" style={{ left: pct(now) }} />}
      {o.legs.map((l) => {
        const left = pct(ms(l.dep))
        const width = `${Math.max(0.8, ((ms(l.arr) - ms(l.dep)) / span) * 100)}%`
        if (l.kind === 'walk') return <span class="seg walk" style={{ left, width }} />
        const bg = hex(l.line?.color) ?? '#5e6670'
        const fg = hex(l.line?.text_color) ?? '#ffffff'
        // Only label segments wide enough to show the whole name; a cut-off "2" for 292 misleads.
        const fits = ((ms(l.arr) - ms(l.dep)) / span) * 100 >= 3 + 2.2 * (l.line?.name.length ?? 0)
        return (
          <span
            class={`seg ride${l.status === 'predicted' && (l.delay_s ?? 0) >= 120 ? ' late' : ''}`}
            style={{ left, width, background: bg, color: fg }}
            title={l.line?.name}
          >
            {fits ? l.line?.name : ''}
          </span>
        )
      })}
    </span>
  )
}

type MapSheetProps = Omit<Props, 'onStart' | 'onSetChange' | 'options' | 'onShift' | 'destinationName'> & {
  option: Option
  now: number
  onClose: () => void
  onStart?: () => void // offered on the map so a trip can start without closing it
  me?: MapViewProps['me']
}

export function MapSheet({ option, now, onClose, onStart, token, walks, places, serviceDate, origin, destination, title, live, me }: MapSheetProps) {
  const [View, setView] = useState<ComponentType<MapViewProps> | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    import('../map/mapview.tsx').then((m) => setView(() => m.MapView)).catch(() => setFailed(true))
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  const start = ms(option.leave_at)
  const end = ms(option.arrive)
  return (
    <div class="sheet" role="dialog" aria-modal="true" aria-label={`Map: ${title}`}>
      <header class="sheet-bar">
        <button class="ghost" onClick={onClose}>
          Close map
        </button>
        <span class="sheet-title">{title}</span>
      </header>
      {failed ? (
        <p class="map-note">Couldn't load the map. Check your connection and try again.</p>
      ) : View ? (
        <View token={token} me={me} walks={walks} places={places} option={option} serviceDate={serviceDate} origin={origin} destination={destination} />
      ) : (
        <p class="map-note subtle">Loading map…</p>
      )}
      <footer class={onStart ? 'sheet-summary with-start' : 'sheet-summary'}>
        <div>
          <p>
            <span class="sheet-leave">{live ? `Leave ${countdown(option.leave_at, now)}` : `Leave ${clock(option.leave_at)}`}</span>
            <span>arrive {clock(option.arrive)}</span>
          </p>
          <Strip option={option} start={start} end={end} />
        </div>
        {onStart && (
          <button class="primary start" onClick={onStart}>
            Start trip
          </button>
        )}
      </footer>
    </div>
  )
}
