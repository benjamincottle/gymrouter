// The trip simulator's controls (sim.ts; docs/DESIGN.md, Simulator). Temporary, for testing at a desk.
import { useEffect, useState } from 'preact/hooks'
import * as appClock from '../clock.ts'
import * as sim from '../sim.ts'
import { clock, dayOf } from '../format.ts'
import type { Option } from '../types.ts'
import { LineChip } from './option.tsx'
import { Segmented, Select, TextButton } from './ui.tsx'

const SPEEDS = [[0, 'Pause'], [1, '1×'], [5, '5×'], [20, '20×'], [60, '60×']] as const
const PACES = [0.5, 0.75, 1, 1.25, 1.5]
const GPS = [['good', 'Good'], ['poor', 'Poor'], ['off', 'Off']] as const
const LATE = Array.from({ length: 16 }, (_, m) => m)

const secs = new Intl.DateTimeFormat('en-AU', {
  hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23', timeZone: 'Australia/Sydney',
})

export function SimPanel({ option }: { option: Option }) {
  const [, redraw] = useState(0)
  const [hidden, setHidden] = useState(false)
  useEffect(() => {
    const a = sim.onChange(() => redraw((n) => n + 1))
    const b = appClock.onChange(() => redraw((n) => n + 1))
    return () => {
      a()
      b()
    }
  }, [])
  const s = sim.current()
  if (!s) return null
  const t = appClock.now()
  const day = dayOf(new Date(t).toISOString(), Date.now())
  const sc = s.scenario
  const rides = option.legs.filter((l) => l.kind === 'ride' && l.trip_id && l.line)

  return (
    <aside class={hidden ? 'sim-panel hidden' : 'sim-panel'} aria-label="Simulation" data-over-dialogs>
      <p class="sim-head">
        <strong>Simulation</strong>
        <span class="num">{day && `${day} `}{secs.format(t)}</span>
        <TextButton quiet onClick={() => setHidden(!hidden)}>{hidden ? 'Show' : 'Hide'}</TextButton>
      </p>
      {!hidden && (
        <>
          <Segmented label="Speed" options={SPEEDS} value={appClock.rate()} onChange={appClock.setRate} />
          <p class="sim-skips">
            <TextButton quiet onClick={() => appClock.skip(60_000)}>+1 min</TextButton>
            <TextButton quiet onClick={() => appClock.skip(5 * 60_000)}>+5 min</TextButton>
            <TextButton quiet onClick={sim.skipToNext}>Next step</TextButton>
          </p>
          <p class="meta">{sim.doing(s)}</p>
          <div class="sim-rows">
            <label class="sim-row">
              Walking pace
              <Select value={String(sc.pace)} onChange={(e) => sim.setScenario({ pace: Number(e.currentTarget.value) })}>
                {PACES.map((p) => <option value={String(p)}>{p * 100}%</option>)}
              </Select>
            </label>
            <label class="sim-row">
              Stay where you are
              <input type="checkbox" checked={sc.still} onChange={(e) => sim.setScenario({ still: e.currentTarget.checked })} />
            </label>
            <div class="sim-row">
              Location
              <Segmented label="Location" options={GPS} value={sc.gps} onChange={(gps) => sim.setScenario({ gps })} />
            </div>
            {rides.map((l) => (
              <label class="sim-row">
                <span>
                  <LineChip line={l.line!} /> {clock(l.dep)}
                </span>
                <Select
                  value={String(sc.late[l.trip_id!] ?? 0)}
                  onChange={(e) => sim.setScenario({ late: { ...sc.late, [l.trip_id!]: Number(e.currentTarget.value) } })}
                >
                  {LATE.map((m) => <option value={String(m)}>{m === 0 ? 'On time' : `${m} min late`}</option>)}
                </Select>
              </label>
            ))}
          </div>
        </>
      )}
    </aside>
  )
}
