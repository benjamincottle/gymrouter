import { clock, delay, placeName, platform, riskLabel } from '../format.ts'
import type { ComponentChildren } from 'preact'
import { findChange, type Segment, type TimedWalk } from '../walks.ts'
import type { Leg, Line, Option, StopRef, Transfer } from '../types.ts'
import { position, rows, type Row } from '../options.ts'
import { HOLD } from './icons.tsx'

const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : undefined)

export function LineChip({ line }: { line: Line }) {
  const bg = hex(line.color) ?? '#555555'
  const fg = hex(line.text_color) ?? '#ffffff'
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
  from: { name: string; home: boolean } // where the trip starts: shown at the top of the rail
  segs: Segment[]
  onTime: (s: Segment) => void
}

export function Timeline({
  option: o, destination, walks, track,
}: {
  option: Option
  destination: string
  walks: TimedWalk[]
  track?: Tracking // during a trip: the rail with you on it, and "Time my walk" (walk times only come from recordings)
}) {
  const rs = rows(o, !!track)
  const me = track && (track.at ?? position(rs, track.now))
  const seg = (leg: number) => track?.segs.find((s) => s.leg === leg)
  const rail = (r: Row, k: number) =>
    track && (
      <Rail
        kind={
          r.kind === 'start' ? (track.from.home ? 'home' : 'gym')
          : r.kind === 'arrive' ? 'end'
          : r.kind === 'change' || r.leg.kind === 'walk' ? 'walk'
          : 'ride'
        }
        color={r.kind === 'leg' ? hex(r.leg.line?.color) : undefined}
        me={me && me.row === k ? me.frac : undefined}
      />
    )
  return (
    <ol class={track ? 'timeline tracked' : 'timeline'}>
      {rs.map((r, k) => {
        if (r.kind === 'start') {
          return (
            <li class="step setoff">
              <span class="time">{clock(o.leave_at)}</span>
              {rail(r, k)}
              <span>Leave {track!.from.name}</span>
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
          <LegRow leg={r.leg} last={r.i === o.legs.length - 1} destination={destination} rail={rail(r, k)}
            onTime={s && track ? () => track.onTime(s) : undefined} />
        )
      })}
    </ol>
  )
}

/** One step's piece of the line down the left: the leg's colour for a ride, dotted for walking, you if you're here. */
function Rail({ kind, color, me }: { kind: 'ride' | 'walk' | 'end' | 'home' | 'gym'; color?: string; me?: number }) {
  return (
    <span class={`rail ${kind}`} style={color ? { '--c': color } : undefined} aria-hidden="true">
      {kind === 'ride' && <i class="stop off" />}
      {(kind === 'home' || kind === 'gym') && (
        <svg class="origin" viewBox="0 0 24 24" width="22" height="22">
          {kind === 'home' ? <path d="M3.5 11 L12 4 L20.5 11 M5.5 9.5 V20 H18.5 V9.5 M10 20 V14 H14 V20" /> : <path d={HOLD} />}
        </svg>
      )}
      {me !== undefined && (
        <i class="you" style={{ top: `${me * 100}%` }}>
          {/* heading on down the trip; at the start, the place you're leaving from */}
          <svg viewBox="0 0 24 24" width="18" height="18">
            {kind === 'home' ? (
              <path d="M5 11.5 L12 5.5 L19 11.5 M7 10 V18.5 H17 V10" />
            ) : kind === 'gym' ? (
              <path d={HOLD} transform="translate(2.4 2.4) scale(0.8)" />
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
  rail?: ComponentChildren
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
              <button class="link small" onClick={onTime}>
                Time my walk
              </button>
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
          to {placeName(leg.to)} at {clock(leg.arr)} ({mins} min{leg.stops ? `, ${leg.stops} stop${leg.stops === 1 ? '' : 's'}` : ''})
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
  rail?: ComponentChildren
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
              <button class="link" onClick={onTime}>
                Time my walk
              </button>
            </>
          )}
        </div>
      </div>
    </li>
  )
}
