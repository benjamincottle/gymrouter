import { useState } from 'preact/hooks'
import { clock, delay, placeName, platform, riskLabel } from '../format.ts'
import { findChange, walkSecs, type TimedWalk } from '../walks.ts'
import type { Leg, Line, Option, StopRef, Transfer } from '../types.ts'

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

export function Timeline({
  option: o, destination, walks, onSetChange,
}: { option: Option; destination: string; walks: TimedWalk[]; onSetChange: (a: StopRef, b: StopRef, secs: number) => void }) {
  const into = new Map<number, Transfer>(o.transfers.map((t) => [t.to_leg, t]))
  return (
    <ol class="timeline">
      {o.legs.map((leg, i) => {
        const t = into.get(i)
        const prevRide = t ? o.legs[t.from_leg] : undefined
        return (
          <>
            {t && prevRide && <TransferRow t={t} from={prevRide.to} to={leg.from} walks={walks} onSet={onSetChange} />}
            <LegRow leg={leg} last={i === o.legs.length - 1} destination={destination} />
          </>
        )
      })}
      <li class="step arrive">
        <span class="time">{clock(o.arrive)}</span>
        <span>Arrive</span>
      </li>
    </ol>
  )
}

function LegRow({ leg, last, destination }: { leg: Leg; last: boolean; destination: string }) {
  const mins = Math.max(1, Math.round((Date.parse(leg.arr) - Date.parse(leg.dep)) / 60000))
  if (leg.kind === 'walk') {
    // Walking between two rides is shown as part of the change.
    if (leg.from && leg.to) return null
    const target = leg.to ? `to ${placeName(leg.to)}` : last ? `to ${destination}` : ''
    return (
      <li class="step walk">
        <span class="time">{clock(leg.dep)}</span>
        <span>
          Walk {mins} min {target}
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
  t, from, to, walks, onSet,
}: { t: Transfer; from?: StopRef; to?: StopRef; walks: TimedWalk[]; onSet: (a: StopRef, b: StopRef, secs: number) => void }) {
  const [editing, setEditing] = useState(false)
  const mine = from && to ? findChange(walks, from, to)?.walk : undefined
  const [mins, setMins] = useState(String(Math.round((mine ? walkSecs(mine) : t.walk_s) / 60)))
  const where = placeName(from) === placeName(to) ? placeName(from) : `${placeName(from)} → ${placeName(to)}`
  const spare = t.slack_s >= 60 ? `${Math.floor(t.slack_s / 60)} min ${t.slack_s % 60}s spare` : `${t.slack_s}s spare`

  return (
    <li class={`step change risk-${t.risk}`}>
      <span class="time">
        <ChangeIcon />
      </span>
      <div>
        <div>
          <span class={`badge risk-${t.risk}`}>{riskLabel(t.risk)}</span> Change at {where}: {Math.round(t.walk_s / 60)} min
          {mine ? ' (your time)' : ''}, {spare}
        </div>
        <div class="muted small">
          {t.fallback_dep ? `If missed, next one at ${clock(t.fallback_dep)}` : 'No later service on this line'}
          {from && to && !editing && (
            <>
              {' · '}
              <button class="link" onClick={() => setEditing(true)}>
                Set my time
              </button>
            </>
          )}
        </div>
        {editing && from && to && (
          <form
            class="inline-form"
            onSubmit={(e) => {
              e.preventDefault()
              const m = Number(mins)
              if (Number.isFinite(m) && m >= 0 && m <= 60) {
                onSet(from, to, Math.round(m * 60))
                setEditing(false)
              }
            }}
          >
            <label>
              My time for this change (min)
              <input type="number" min="0" max="60" step="0.5" value={mins} onInput={(e) => setMins((e.target as HTMLInputElement).value)} />
            </label>
            <button type="submit">Save</button>
            <button type="button" class="link" onClick={() => setEditing(false)}>
              Cancel
            </button>
          </form>
        )}
      </div>
    </li>
  )
}
