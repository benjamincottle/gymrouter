import { clock, delay, placeName, platform, riskLabel, spare } from '../format.ts'
import type { ComponentChildren } from 'preact'
import { findChange, type PlaceRef, type Segment, type TimedWalk } from '../walks.ts'
import type { Leg, Line, Option, StopRef, Transfer } from '../types.ts'
import { position, rows, type Row } from '../options.ts'
import { HOUSE, Shoe, VEHICLE_PATHS, vehicleOf } from './icons.tsx'
import { TextButton } from './ui.tsx'
import { lineColour, textOn } from '../colour.ts'


export function LineChip({ line }: { line: Line }) {
  const bg = lineColour(line.color)
  return (
    <span class={`chip mode-${line.mode}`} style={{ background: bg, color: textOn(bg) }}>
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

/** The vehicle drawn where you get on (see vehicleOf). */
function ModeGlyph({ mode }: { mode?: string }) {
  return (
    <svg viewBox="0 0 24 24" width="13" height="13">
      <path d={VEHICLE_PATHS[vehicleOf(mode)]} />
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
  const bg = line && lineColour(line.color)
  const style = bg && { '--c': bg, '--t': textOn(bg) }
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
        <svg class="origin" viewBox="0 0 24 24" width="22" height="22">
          {place === 'home' ? <path d={HOUSE} /> : <Shoe />}
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
                <g transform="translate(1.8 1.8) scale(0.85)">
                  <Shoe />
                </g>
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
              <TextButton onClick={onTime}>
                Time my walk
              </TextButton>
            </>
          )}
        </span>
      </li>
    )
  }
  const pf = platform(leg.from)
  const late = leg.delay_s ?? 0
  const status =
    leg.status === 'predicted' ? delay(leg.delay_s) : leg.status === 'added' ? 'extra service' : 'timetabled'
  const statusClass =
    leg.status === 'predicted' ? (late < 60 ? 'status ok' : late < 120 ? 'status slight' : 'status late') : leg.status === 'added' ? 'status' : 'status sched'
  return (
    <li class="step ride">
      <span class="time">{clock(leg.dep)}</span>
      {rail}
      <div>
        <div class="ride-head">
          {leg.line && <LineChip line={leg.line} />}
          <span>{leg.headsign ? `towards ${leg.headsign}` : ''}</span>
          <span class={statusClass}>{status}</span>
        </div>
        <span class="meta">
          from {placeName(leg.from)}
          {pf && `, ${pf[0].toLowerCase()}${pf.slice(1)}`}
        </span>
        <span class="meta">
          to {placeName(leg.to)} at {clock(leg.arr)}
        </span>
        <span class="meta">{leg.stops ? `${leg.stops} stop${leg.stops === 1 ? '' : 's'} (${mins} min)` : `${mins} min`}</span>
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
  const sameStop = placeName(from) === placeName(to)
  const walk = `${Math.max(1, Math.round(t.walk_s / 60))} min walk${sameStop ? '' : ` to ${placeName(to)}`}${mine ? ' (timed)' : ''}`

  return (
    <li class={`step change risk-${t.risk}`}>
      <span class="time">
        <ChangeIcon />
      </span>
      {rail}
      <div>
        <div>
          <span class={`badge risk-${t.risk}`}>{riskLabel(t.risk)}</span> Change at {placeName(from)}
        </div>
        <span class="meta">
          {walk}, {spare(t.slack_s)}
        </span>
        <span class="meta">{t.fallback_dep ? `If missed, the next one is at ${clock(t.fallback_dep)}` : 'No later service on this line'}</span>
        {onTime && (
          <span class="meta">
            <TextButton onClick={onTime}>Time my walk</TextButton>
          </span>
        )}
      </div>
    </li>
  )
}
