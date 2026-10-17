import { boardText, clock, delay, placeName, platform, riskLabel, spare } from '../format.ts'
import type { ComponentChildren } from 'preact'
import { LAST_STOPS, stopsToGo, type RideProgress } from '../countdown.ts'
import type { PlaceRef } from '../settings.ts'
import type { Leg, Line, Option, StopRef, Transfer } from '../types.ts'
import { position, rows, type Row } from '../options.ts'
import { Chevron, HOUSE, IconHurry, Shoe, VEHICLE_PATHS, vehicleOf, Walker } from './icons.tsx'
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

/** During a trip: where you are. */
export interface Tracking {
  now: number
  at?: { row: number; frac: number } | null // where your location puts you on the steps (else the clock decides)
  riding?: boolean // on a vehicle: you're drawn as it on its step (else on foot: walking, or waiting at the stop)
  ride?: RideProgress | null // the ride you're on, counted in stops (when its stops are known)
  opened: Record<string, boolean> // rides whose stops you've opened or closed yourself (rideKey): your tap wins
  onOpen: (ride: string, open: boolean) => void
}

/** A ride, for remembering whether its stops are open: the vehicle and where you get on it. */
export const rideKey = (l: Leg) => `${l.trip_id ?? ''}|${l.from?.id ?? ''}`

/** The home or gym at each end of a trip. */
export interface Ends {
  start: PlaceRef
  end: PlaceRef
}

const placeKind = (p: PlaceRef) => (p.key.startsWith('home:') ? 'home' : 'gym')

/**
 * The trip's steps, during a trip: a line down the middle in each leg's colours, from the home or gym it starts at to
 * the one it ends at, with you on it.
 */
export function Timeline({
  option: o, ends, track,
}: {
  option: Option
  ends: Ends
  track: Tracking
}) {
  const rs = rows(o, true)
  const me = track.at ?? position(rs, track.now)
  // A ride's stops: opened or closed by a tap on its stops line, or by themselves for the last few stops of the ride
  // you're on.
  const { opened, onOpen: setOpen } = track
  const rail = (r: Row, k: number) => (
    <Rail
      kind={r.kind === 'start' || r.kind === 'arrive' ? r.kind : 'walk'}
      place={placeKind(r.kind === 'arrive' ? ends.end : ends.start)}
      me={me.row === k ? me.frac : undefined}
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
          return <TransferRow t={r.t} from={r.from} to={r.to} rail={rail(r, k)} />
        }
        if (r.leg.kind === 'ride') {
          const ride = track.ride?.leg === r.i ? track.ride : undefined
          const key = rideKey(r.leg)
          const known = !!r.leg.calls?.length
          // Where to board, until you're on board: rides still ahead, and the one you're waiting for.
          const boarding = me.row < k || (me.row === k && !track.riding && me.frac < 0.5)
          return (
            <RideRow leg={r.leg} me={me.row === k ? me.frac : undefined} riding={!!track.riding && me.row === k} ride={ride} boarding={boarding}
              open={known && (opened[key] ?? (!!ride && ride.total - ride.passed <= LAST_STOPS))}
              onOpen={known ? (open) => setOpen(key, open) : undefined} />
          )
        }
        return <LegRow leg={r.leg} last={r.i === o.legs.length - 1} destination={ends.end.name} rail={rail(r, k)} />
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
 * One step's piece of the line down the middle, for the steps that aren't rides (a ride draws its own, see RideRow):
 * dotted with a walker for walking, the home or gym at either end, and you if you're here.
 */
function Rail({ kind, place, me }: {
  kind: 'walk' | 'start' | 'arrive'
  place: 'home' | 'gym' // at the start (or the end) of the trip
  me?: number
}) {
  return (
    <span class={`rail ${kind}`} aria-hidden="true">
      {kind === 'walk' && (
        <svg class="walker" viewBox="0 0 24 24" width="18" height="18">
          <Walker />
        </svg>
      )}
      {(kind === 'start' || kind === 'arrive') && (
        <svg class="origin" viewBox="0 0 24 24" width="22" height="22">
          {place === 'home' ? <path d={HOUSE} /> : <Shoe />}
        </svg>
      )}
      {me !== undefined && (
        <i class="you" style={{ top: `${me * 100}%` }}>
          {/* on foot; at either end, the place you're at */}
          <svg viewBox="0 0 24 24" width="18" height="18">
            {kind === 'walk' ? (
              <Walker />
            ) : place === 'home' ? (
              <path d="M5 11.5 L12 5.5 L19 11.5 M7 10 V18.5 H17 V10" />
            ) : (
              <g transform="translate(1.8 1.8) scale(0.85)">
                <Shoe />
              </g>
            )}
          </svg>
        </i>
      )}
    </span>
  )
}

/** A walk to the first stop or from the last (a walk between two rides is part of the change). */
function LegRow({ leg, last, destination, rail }: {
  leg: Leg
  last: boolean
  destination: string
  rail: ComponentChildren
}) {
  const mins = Math.max(1, Math.round((Date.parse(leg.arr) - Date.parse(leg.dep)) / 60000))
  const target = leg.to ? `to ${placeName(leg.to)}` : last ? `to ${destination}` : ''
  return (
    <li class="step walk">
      <span class="time">{clock(leg.dep)}</span>
      {rail}
      <span>
        Walk {mins} min {target}
      </span>
    </li>
  )
}

/**
 * A ride. Closed, it's one step: where it's from and to, and how many stops. Open (a tap on the stops line, or by
 * itself for the last few stops of the ride you're on), it runs on into a row for each stop still ahead, the last
 * yours. On the ride you're on (`ride`), the stops behind you fold into a count and the line behind you is dimmed.
 */
function RideRow({ leg, me, riding, ride, boarding, open, onOpen }: {
  leg: Leg
  me?: number // you're on this step, this far down it
  riding?: boolean // and on the vehicle (else waiting where you get on, or just off it)
  boarding: boolean // not on board yet: where to board shows, when the server has said
  ride?: RideProgress // how far through it you are, in stops, when they're known
  open: boolean
  onOpen?: (open: boolean) => void // its stops are known, so they can be shown
}) {
  const mins = Math.max(1, Math.round((Date.parse(leg.arr) - Date.parse(leg.dep)) / 60000))
  const bg = lineColour(leg.line?.color)
  const style = { '--c': bg, '--t': textOn(bg) }
  const calls = leg.calls ?? []
  const pf = platform(leg.from)
  const late = leg.delay_s ?? 0
  const status =
    leg.status === 'predicted' ? delay(leg.delay_s) : leg.status === 'added' ? 'extra service' : 'timetabled'
  const statusClass =
    leg.status === 'predicted' ? (late < 60 ? 'status ok' : late < 120 ? 'status slight' : 'status late') : leg.status === 'added' ? 'status' : 'status sched'
  const head = (
    <>
      <div class="ride-head">
        {leg.line && <LineChip line={leg.line} />}
        <span>{leg.headsign ? `towards ${leg.headsign}` : ''}</span>
        <span class={statusClass}>{status}</span>
      </div>
      <span class="meta">
        from {placeName(leg.from)}
        {pf && `, ${pf[0].toLowerCase()}${pf.slice(1)}`}
      </span>
    </>
  )
  // How many stops: all of them, or on the ride you're on, how many are left. With the stops known, it opens them.
  const count = ride
    ? `${ride.total - ride.passed} of ${ride.total} stops to go`
    : leg.stops ? `${leg.stops} stop${leg.stops === 1 ? '' : 's'} (${mins} min)` : `${mins} min`
  const stopsLine = (
    <span class="meta">
      {onOpen ? (
        <TextButton quiet aria-expanded={open} onClick={() => onOpen(!open)}>
          {count}
          <Chevron up={open} />
        </TextButton>
      ) : count}
    </span>
  )
  const you = (top: string) => (
    <i class="you" style={{ top }}>
      <svg viewBox="0 0 24 24" width="18" height="18">
        {riding ? <path d={VEHICLE_PATHS[vehicleOf(leg.line?.mode)]} /> : <Walker />}
      </svg>
    </i>
  )
  const board = (
    <i class="board">
      <ModeGlyph mode={leg.line?.mode} />
    </i>
  )
  if (!open) {
    return (
      <li class="step ride">
        <span class="time">{clock(leg.dep)}</span>
        <span class="rail ride" style={style} aria-hidden="true">
          <i class="line" />
          {riding && me !== undefined && <i class="travelled" style={{ height: `max(0px, calc(${me * 100}% - 12px))` }} />}
          {board}
          <i class="alight" />
          {me !== undefined && you(`${me * 100}%`)}
        </span>
        <div>
          {head}
          <span class="meta">
            to {placeName(leg.to)} at {clock(leg.arr)}
          </span>
          {boarding && <BoardStrip leg={leg} />}
          {stopsLine}
        </div>
      </li>
    )
  }
  const passed = ride?.passed ?? 0
  // On this step but not riding: waiting where you get on, or just off where it ends.
  const atEnd = !ride && me !== undefined && me >= 0.5
  const rail = (children: ComponentChildren) => (
    <span class="rail ride" style={style} aria-hidden="true">
      {children}
    </span>
  )
  return (
    <>
      <li class="step ride opened">
        <span class="time">{clock(leg.dep)}</span>
        {rail(
          <>
            <i class={ride ? 'line past' : 'line'} />
            {board}
            {!ride && me !== undefined && !atEnd && you('0%')}
          </>,
        )}
        <div>
          {head}
          {boarding && <BoardStrip leg={leg} />}
          {stopsLine}
        </div>
      </li>
      {ride && passed > 0 && (
        <li class="step ride call">
          <span class="time" />
          {rail(
            <>
              <i class="line past" />
              <i class="call-dot past" />
            </>,
          )}
          <span class="passed">
            {passed} stop{passed === 1 ? '' : 's'} passed
          </span>
        </li>
      )}
      {ride && (
        <li class="step ride call here">
          <span class="time" />
          {rail(
            <>
              <i class="line past" />
              <i class="line ahead" />
              {you('50%')}
            </>,
          )}
          <span />
        </li>
      )}
      {calls.map((c, i) => {
        if (ride && i < passed) return null
        const last = i === calls.length - 1
        return (
          <li class={last ? 'step ride call last' : 'step ride call'}>
            <span class="time">{c.arr ? clock(c.arr) : ''}</span>
            {rail(
              <>
                <i class="line" />
                <i class={last ? 'your-stop' : 'call-dot'} />
                {last && atEnd && you('50%')}
              </>,
            )}
            <span class="call-name">
              {placeName(c)}
              {ride && i === passed && !last && <span class="stop-tag">Next</span>}
              {last && <span class="stop-tag">Get off</span>}
            </span>
          </li>
        )
      })}
    </>
  )
}

/**
 * The ride you're on as the line diagram above a train's doors: where you got on, each stop, yours at the end, and you
 * along it, with what's behind you dimmed. Under it, the next stop and how many are left.
 */
export function RideCountdown({ leg, ride }: { leg: Leg; ride: RideProgress }) {
  const bg = lineColour(leg.line?.color)
  const calls = leg.calls ?? []
  const left = ride.total - ride.passed
  const at = ((ride.passed + ride.along) / ride.total) * 100
  return (
    <div class="countdown">
      <div class="stop-strip" style={{ '--c': bg, '--t': textOn(bg) }} role="img" aria-label={`${left} of ${ride.total} stops to go`}>
        <i class="line" />
        <i class="travelled" style={{ width: `calc(${at}% + 7px)` }} />
        <i class="board">
          <ModeGlyph mode={leg.line?.mode} />
        </i>
        {calls.slice(0, -1).map((_, i) => (
          <i class={i < ride.passed ? 'call-dot past' : 'call-dot'} style={{ left: `${((i + 1) / ride.total) * 100}%` }} />
        ))}
        <i class="your-stop" />
        <i class="you" style={{ left: `${at}%` }}>
          <svg viewBox="0 0 24 24" width="18" height="18">
            <path d={VEHICLE_PATHS[vehicleOf(leg.line?.mode)]} />
          </svg>
        </i>
      </div>
      <div class="strip-ends">
        <span>{left > 1 && calls[ride.passed] ? `Next: ${placeName(calls[ride.passed])}` : ''}</span>
        <span>{left > 1 ? stopsToGo(left) : 'Next stop is yours'}</span>
      </div>
    </div>
  )
}

/**
 * Where to board a train so you step off beside your way on: the train heading right, a piece for each carriage with
 * yours outlined in the line's colour, and under it the runner and the instruction.
 */
export function BoardStrip({ leg }: { leg: Leg }) {
  const b = leg.board
  if (!b) return null
  const say = boardText(b.carriage, b.of)
  return (
    <div class="board-strip" style={{ '--c': lineColour(leg.line?.color) }}>
      <div class="carriages" role="img" aria-label={say}>
        {/* The front carriage is the last drawn: the train heads right. */}
        {Array.from({ length: b.of }, (_, k) => {
          const n = b.of - k
          return n === 1 ? (
            <i class={n === b.carriage ? 'cab yours' : 'cab'}>
              <b />
              <svg viewBox="0 0 20 22" width="20" height="22" aria-hidden="true">
                <path d="M0 .75 H.25 C7.25 .75 15.25 7.75 18.75 15.75 Q19.25 17.25 19.25 18.75 Q19.25 21.25 16.75 21.25 H0" />
              </svg>
            </i>
          ) : (
            <i class={n === b.carriage ? 'yours' : undefined} />
          )
        })}
      </div>
      <p class="board-say">
        <IconHurry />
        <span>{say}</span>
      </p>
    </div>
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
  t, from, to, rail,
}: {
  t: Transfer
  from?: StopRef
  to?: StopRef
  rail: ComponentChildren
}) {
  const sameStop = placeName(from) === placeName(to)
  const walk = `${Math.max(1, Math.round(t.walk_s / 60))} min walk${sameStop ? '' : ` to ${placeName(to)}`}`

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
      </div>
    </li>
  )
}
