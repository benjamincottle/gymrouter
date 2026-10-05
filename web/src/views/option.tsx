import { clock, delay, placeName, platform, riskLabel } from '../format.ts'
import type { ComponentChildren } from 'preact'
import { findChange, type PlaceRef, type Segment, type TimedWalk } from '../walks.ts'
import type { Leg, Line, Option, StopRef, Transfer } from '../types.ts'
import { position, rows, type Row } from '../options.ts'
import { HOLD } from './icons.tsx'
import { TextButton } from './ui.tsx'
import { CHIP_FALLBACK, hex, LINE_FALLBACK, WHITE } from '../colour.ts'


export function LineChip({ line }: { line: Line }) {
  const bg = hex(line.color) ?? CHIP_FALLBACK
  const fg = hex(line.text_color) ?? WHITE
  return (
    <span class={`chip mode-${line.mode}`} style={{ background: bg, color: fg }}>
      {line.name}
    </span>
  )
}

/** During a trip: where you are, and the walks you can time (see walks.ts segments). */
export interface Tracking {
  now: number
  at?: { row: number; frac: number } | null // where your location puts you on the steps (else the clock decides)
  segs: Segment[]
  onTime: (s: Segment) => void
}

/** The home or gym at each end of a trip. */
export interface Ends {
  start: PlaceRef
  end: PlaceRef
}

const placeKind = (p: PlaceRef) => (p.key.startsWith('home:') ? 'home' : 'gym')

/**
 * The trip's description: a line down the middle in each leg's colours, from the home or gym it starts at to the one
 * it ends at. During a trip (`track`) you're on the line, and walks can be timed (walk times only come from recordings).
 */
export function Timeline({
  option: o, ends, walks, track,
}: {
  option: Option
  ends: Ends
  walks: TimedWalk[]
  track?: Tracking
}) {
  const rs = rows(o, true)
  const me = track && (track.at ?? position(rs, track.now))
  const seg = (leg: number) => track?.segs.find((s) => s.leg === leg)
  const rail = (r: Row, k: number) => (
    <Rail
      kind={r.kind === 'start' || r.kind === 'arrive' ? r.kind : r.kind === 'change' || r.leg.kind === 'walk' ? 'walk' : 'ride'}
      place={placeKind(r.kind === 'arrive' ? ends.end : ends.start)}
      line={r.kind === 'leg' ? r.leg.line : undefined}
      me={me && me.row === k ? me.frac : undefined}
    />
  )
  return (
    <ol class="timeline">
      {rs.map((r, k) => {
        if (r.kind === 'start') {
          return (
            <li class="step setoff">
              <span class="time">{clock(o.leave_at)}</span>
              {rail(r, k)}
              <span>Leave {ends.start.name}</span>
            </li>
          )
        }
        if (r.kind === 'arrive') {
          return (
            <li class="step arrive">
              <span class="time">{clock(o.arrive)}</span>
              {rail(r, k)}
              <span>Arrive</span>
            </li>
          )
        }
        if (r.kind === 'change') {
          const s = seg(r.t.to_leg)
          return (
            <TransferRow t={r.t} from={r.from} to={r.to} modes={r.modes} walks={walks} rail={rail(r, k)}
              onTime={s && track ? () => track.onTime(s) : undefined} />
          )
        }
        const s = r.leg.kind === 'walk' ? seg(r.i) : undefined
        return (
          <LegRow leg={r.leg} last={r.i === o.legs.length - 1} destination={ends.end.name} rail={rail(r, k)}
            onTime={s && track ? () => track.onTime(s) : undefined} />
        )
      })}
    </ol>
  )
}

const HOUSE = 'M3.5 11 L12 4 L20.5 11 M5.5 9.5 V20 H18.5 V9.5 M10 20 V14 H14 V20'

/** The vehicle drawn where you get on: a train for anything on rails, a ferry, otherwise a bus. */
function ModeGlyph({ mode }: { mode?: string }) {
  const d =
    mode === 'ferry' ? 'M3 14.5 H21 L18.5 19.5 H5.5 Z M6.5 14.5 V9.5 H17.5 V14.5 M12 9.5 V5.5'
    : mode === 'train' || mode === 'metro' || mode === 'light-rail' || mode === 'regional-train'
      ? 'M8 3.5 H16 A3 3 0 0 1 19 6.5 V14 A3 3 0 0 1 16 17 H8 A3 3 0 0 1 5 14 V6.5 A3 3 0 0 1 8 3.5 Z M5 10.5 H19 M8.5 17 L6.5 21 M15.5 17 L17.5 21'
      : 'M7 3.5 H17 A2 2 0 0 1 19 5.5 V18 H5 V5.5 A2 2 0 0 1 7 3.5 Z M5 11.5 H19 M8 18 V20.5 M16 18 V20.5'
  return (
    <svg viewBox="0 0 24 24" width="13" height="13">
      <path d={d} />
    </svg>
  )
}

/**
 * One step's piece of the line down the middle: a thick line in the ride's colour (the vehicle where you get on, a
 * white dot where you get off), dotted with a walker for walking, the home or gym at either end, and you if you're here.
 */
function Rail({ kind, place, line, me }: {
  kind: 'ride' | 'walk' | 'start' | 'arrive'
  place: 'home' | 'gym' // at the start (or the end) of the trip
  line?: Line
  me?: number
}) {
  const style = line && { '--c': hex(line.color) ?? LINE_FALLBACK, '--t': hex(line.text_color) ?? WHITE }
  return (
    <span class={`rail ${kind}`} style={style} aria-hidden="true">
      {kind === 'ride' && (
        <>
          <i class="line" />
          <i class="board">
            <ModeGlyph mode={line?.mode} />
          </i>
          <i class="alight" />
        </>
      )}
      {kind === 'walk' && (
        <svg class="walker" viewBox="0 0 24 24" width="18" height="18">
          {/* someone walking, heading right (on down the trip) */}
          <circle cx="13.6" cy="4" r="2" />
          <path d="M12.8 7.6 L11.2 13.2 L14.2 16.2 L15.2 20.6 M11.2 13.2 L9.8 17 L7.2 20.2 M12.4 8.6 L14.6 11.6 L16.8 12.4 M12.4 8.6 L9.8 10.4 L8.6 13" />
        </svg>
      )}
      {(kind === 'start' || kind === 'arrive') && (
        <svg class={`origin ${place === 'home' ? 'house' : 'hold'}`} viewBox="0 0 24 24" width="22" height="22">
          {place === 'home' ? (
            <path d={HOUSE} />
          ) : (
            <>
              {/* the hold from the Gyms icon in Settings, bolted on */}
              <path d={HOLD} />
              <circle cx="12" cy="10.5" r="1.6" />
            </>
          )}
        </svg>
      )}
      {me !== undefined && (
        <i class="you" style={{ top: `${me * 100}%` }}>
          {/* heading on down the trip; at either end, the place you're at */}
          <svg viewBox="0 0 24 24" width="18" height="18">
            {kind === 'start' || kind === 'arrive' ? (
              place === 'home' ? (
                <path d="M5 11.5 L12 5.5 L19 11.5 M7 10 V18.5 H17 V10" />
              ) : (
                <path d={HOLD} transform="translate(2.4 2.4) scale(0.8)" />
              )
            ) : (
              <path d="M6.5 9.5 L12 15 L17.5 9.5" />
            )}
          </svg>
        </i>
      )}
    </span>
  )
}

function LegRow({ leg, last, destination, rail, onTime }: {
  leg: Leg
  last: boolean
  destination: string
  rail: ComponentChildren
  onTime?: () => void
}) {
  const mins = Math.max(1, Math.round((Date.parse(leg.arr) - Date.parse(leg.dep)) / 60000))
  if (leg.kind === 'walk') {
    const target = leg.to ? `to ${placeName(leg.to)}` : last ? `to ${destination}` : ''
    return (
      <li class="step walk">
        <span class="time">{clock(leg.dep)}</span>
        {rail}
        <span>
          Walk {mins} min {target}
          {onTime && (
            <>
              {' '}
              <TextButton small onClick={onTime}>
                Time my walk
              </TextButton>
            </>
          )}
        </span>
      </li>
    )
  }
  const pf = platform(leg.from)
  const status =
    leg.status === 'predicted' ? delay(leg.delay_s) : leg.status === 'added' ? 'extra service' : 'timetable'
  return (
    <li class="step ride">
      <span class="time">{clock(leg.dep)}</span>
      {rail}
      <div>
        <div class="ride-head">
          {leg.line && <LineChip line={leg.line} />}
          <span>{leg.headsign ? `towards ${leg.headsign}` : ''}</span>
          <span class={leg.status === 'predicted' ? `rt ${(leg.delay_s ?? 0) >= 120 ? 'late' : 'ok'}` : 'rt sched'}>{status}</span>
        </div>
        <div class="muted small">
          from {placeName(leg.from)}
          {pf && ` · ${pf}`}
        </div>
        <div class="muted small">
          to {placeName(leg.to)} at {clock(leg.arr)}
        </div>
        <div class="muted small">
          {leg.stops ? `${leg.stops} stop${leg.stops === 1 ? '' : 's'} (${mins} min)` : `${mins} min`}
        </div>
      </div>
    </li>
  )
}

/** Interchange: two arrows passing each other, as on station signs. */
function ChangeIcon() {
  return (
    <svg class="change-icon" viewBox="0 0 24 24" width="22" height="22" aria-label="Change" role="img">
      <path d="M4 8h14m-4-4 4 4-4 4M20 16H6m4-4-4 4 4 4" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  )
}

function TransferRow({
  t, from, to, modes, walks, rail, onTime,
}: {
  t: Transfer
  from?: StopRef
  to?: StopRef
  modes: [string?, string?] // of the rides either side
  walks: TimedWalk[]
  rail: ComponentChildren
  onTime?: () => void
}) {
  const mine = from && to ? findChange(walks, { stop: from, mode: modes[0] }, { stop: to, mode: modes[1] }) : undefined
  const where = placeName(from) === placeName(to) ? placeName(from) : `${placeName(from)} → ${placeName(to)}`
  const spare = t.slack_s >= 60 ? `${Math.floor(t.slack_s / 60)} min ${t.slack_s % 60}s spare` : `${t.slack_s}s spare`

  return (
    <li class={`step change risk-${t.risk}`}>
      <span class="time">
        <ChangeIcon />
      </span>
      {rail}
      <div>
        <div>
          <span class={`badge risk-${t.risk}`}>{riskLabel(t.risk)}</span> Change at {where}: {Math.round(t.walk_s / 60)} min
          {mine ? ' (timed)' : ''}, {spare}
        </div>
        <div class="muted small">
          {t.fallback_dep ? `If missed, next one at ${clock(t.fallback_dep)}` : 'No later service on this line'}
          {onTime && (
            <>
              {' · '}
              <TextButton onClick={onTime}>
                Time my walk
              </TextButton>
            </>
          )}
        </div>
      </div>
    </li>
  )
}
