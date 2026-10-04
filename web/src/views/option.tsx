import { useState } from 'preact/hooks'
import { clock, delay, placeName, platform, riskLabel } from '../format.ts'
import type { TransferTime } from '../settings.ts'
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
  option: o, transfers, onSetTransfer,
}: { option: Option; transfers: TransferTime[]; onSetTransfer: (t: TransferTime) => void }) {
  const into = new Map<number, Transfer>(o.transfers.map((t) => [t.to_leg, t]))
  return (
    <ol class="timeline">
      {o.legs.map((leg, i) => {
        const t = into.get(i)
        const prevRide = t ? o.legs[t.from_leg] : undefined
        return (
          <>
            {t && prevRide && <TransferRow t={t} from={prevRide.to} to={leg.from} transfers={transfers} onSet={onSetTransfer} />}
            <LegRow leg={leg} last={i === o.legs.length - 1} />
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

function LegRow({ leg, last }: { leg: Leg; last: boolean }) {
  const mins = Math.max(1, Math.round((Date.parse(leg.arr) - Date.parse(leg.dep)) / 60000))
  if (leg.kind === 'walk') {
    // Walking between two rides is shown as part of the change.
    if (leg.from && leg.to) return null
    const target = leg.to ? `to ${placeName(leg.to)}` : last ? 'to your destination' : ''
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

function stationKey(s: StopRef | undefined): string | undefined {
  return s?.station_id || s?.id
}

function TransferRow({
  t, from, to, transfers, onSet,
}: { t: Transfer; from?: StopRef; to?: StopRef; transfers: TransferTime[]; onSet: (t: TransferTime) => void }) {
  const [editing, setEditing] = useState(false)
  const fromKey = stationKey(from)
  const toKey = stationKey(to)
  const mine = transfers.find((x) => x.from === fromKey && x.to === toKey)
  const [mins, setMins] = useState(String(Math.round((mine?.secs ?? t.walk_s) / 60)))
  const where = placeName(from) === placeName(to) ? placeName(from) : `${placeName(from)} → ${placeName(to)}`
  const spare = t.slack_s >= 60 ? `${Math.floor(t.slack_s / 60)} min ${t.slack_s % 60}s spare` : `${t.slack_s}s spare`

  return (
    <li class={`step change risk-${t.risk}`}>
      <span class="time" />
      <div>
        <div>
          <span class={`badge risk-${t.risk}`}>{riskLabel(t.risk)}</span> Change at {where}: {Math.round(t.walk_s / 60)} min
          {mine ? ' (your time)' : ''}, {spare}
        </div>
        <div class="muted small">
          {t.fallback_dep ? `If missed, next one at ${clock(t.fallback_dep)}` : 'No later service on this line'}
          {fromKey && toKey && !editing && (
            <>
              {' · '}
              <button class="link" onClick={() => setEditing(true)}>
                Set my time
              </button>
            </>
          )}
        </div>
        {editing && fromKey && toKey && (
          <form
            class="inline-form"
            onSubmit={(e) => {
              e.preventDefault()
              const m = Number(mins)
              if (Number.isFinite(m) && m >= 0 && m <= 60) {
                onSet({ from: fromKey, to: toKey, secs: Math.round(m * 60), label: where })
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
