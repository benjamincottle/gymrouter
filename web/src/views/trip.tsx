import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, AuthError } from '../api.ts'
import { addDays, dayLabel, fromLocalInput, roundUp, statusTime, toLocalInput } from '../format.ts'
import { usePolling, useVisible } from '../hooks.ts'
import { byUse, placeRef, planPlace, prefs, usedGym, type Settings } from '../settings.ts'
import type { DefaultsResponse, PlanRequest } from '../types.ts'
import { Board, WindowShift, type Shift } from './board.tsx'
import { BrandLogo } from './brand.tsx'
import type { ActiveTrip } from './intrip.tsx'
import { Button, Segmented } from './ui.tsx'

type Direction = 'to-gym' | 'home'

const REFRESH_MS = 30_000
const WINDOW_MIN = 45
const SHIFT_MS = 30 * 60_000

interface Props {
  settings: Settings
  setSettings: (s: Settings) => void
  server: DefaultsResponse | null
  onAuthError: () => void
  goToSettings: () => void
  onStartTrip: (t: ActiveTrip) => void
}

type When = 'now' | 'leave' | 'arrive'

export function Trip({ settings, setSettings, server, onAuthError, goToSettings, onStartTrip }: Props) {
  const [direction, setDirection] = useState<Direction>('to-gym')
  const [gymId, setGymId] = useState<string | null>(null)
  // The most used gyms first, in the order they had when this screen opened (choosing one doesn't reshuffle the list).
  const [order] = useState(() => byUse(settings.gyms).map((g) => g.id))
  const rank = (id: string) => (order.includes(id) ? order.indexOf(id) : order.length)
  const chooseGym = (id: string | null) => {
    setGymId(id)
    if (id) setSettings(usedGym(settings, id))
  }
  const [when, setWhen] = useState<When>('now')
  const [at, setAt] = useState(() => roundUp(toLocalInput(new Date()))) // "YYYY-MM-DDTHH:MM", Sydney time
  // Choosing a time starts from now if the one last chosen has passed.
  const chooseTime = (w: 'leave' | 'arrive') => {
    if (Date.parse(fromLocalInput(at)) < Date.now()) setAt(roundUp(toLocalInput(new Date())))
    setWhen(w)
  }
  const [moved, setMoved] = useState<number | null>(null) // "Leave now", moved earlier or later: where the window starts
  useEffect(() => setMoved(null), [when, gymId, direction])
  const leaveAt = when === 'now' ? null : at
  const visible = useVisible()
  const home = settings.homes.find((h) => h.id === settings.activeHome) ?? settings.homes[0]

  const request: PlanRequest | null = useMemo(() => {
    const g = settings.gyms.find((x) => x.id === gymId)
    if (!g || !home) return null
    const gym = planPlace('gym', g, settings.walks)
    const place = planPlace('home', home, settings.walks)
    return {
      from: direction === 'to-gym' ? place : gym,
      to: direction === 'to-gym' ? gym : place,
      lines: g.lines,
      time: leaveAt ? fromLocalInput(leaveAt) : moved ? new Date(moved).toISOString() : undefined,
      arrive_by: when === 'arrive' || undefined,
      window_min: WINDOW_MIN,
      prefs: prefs(settings),
    }
  }, [gymId, home, direction, leaveAt, moved, when, settings])

  // Earlier / later trips: half an hour each way. With "Leave now", earlier shows what has just gone (useful when
  // you're already on your way or a service is running late); coming back round to now goes back to live.
  const shift: Shift = (dir) => {
    if (when === 'now') {
      const next = (moved ?? Date.now()) + dir * SHIFT_MS
      setMoved(Math.abs(next - Date.now()) < 60_000 ? null : next)
    } else {
      setAt(toLocalInput(new Date(Date.parse(fromLocalInput(at)) + dir * SHIFT_MS)))
    }
  }

  const key = request ? JSON.stringify(request) : null
  const plan = usePolling(
    key,
    (signal) => api.plan(settings.token!, request!, signal),
    REFRESH_MS,
    visible,
  )
  useEffect(() => {
    if (plan.error instanceof AuthError) onAuthError()
  }, [plan.error, onAuthError])


  if (!server) return <p class="muted">Loading…</p>
  if (!home || settings.gyms.length === 0) {
    return (
      <div class="empty">
        <p>{home ? 'Add a gym to plan trips to.' : 'Add your home and a gym, so trips can start (or end) there.'}</p>
        <Button variant="primary" onClick={goToSettings}>
          {home ? 'Add a gym' : 'Get started'}
        </Button>
      </div>
    )
  }

  const gym = settings.gyms.find((g) => g.id === gymId)
  const ends = gym && (direction === 'to-gym' ? { start: placeRef('home', home), end: placeRef('gym', gym) } : { start: placeRef('gym', gym), end: placeRef('home', home) })
  const places = { start: ends?.start.key, end: ends?.end.key }

  return (
    <div class="stack">
      <div class="when">
        <Segmented
          label="When"
          small
          options={[['now', 'Leave now'], ['leave', 'Leave at'], ['arrive', 'Arrive by']] as const}
          value={when}
          onChange={(w) => (w === 'now' ? setWhen('now') : chooseTime(w))}
        />
        {when !== 'now' && <DayTime value={at} onChange={setAt} label={when === 'arrive' ? 'Arrive by' : 'Leave at'} />}
      </div>

      <Segmented
        label="Direction"
        options={[['to-gym', 'To the gym'], ['home', 'Home']] as const}
        value={direction}
        onChange={setDirection}
      />

      {settings.homes.length > 1 && (
        <label class="inline">
          {direction === 'to-gym' ? 'From' : 'To'}
          <select
            value={home.id}
            onChange={(e) => setSettings({ ...settings, activeHome: (e.target as HTMLSelectElement).value })}
          >
            {settings.homes.map((h) => (
              <option value={h.id}>{h.name}</option>
            ))}
          </select>
        </label>
      )}

      <div class="gyms">
        {(gym ? [gym] : [...settings.gyms].sort((a, b) => rank(a.id) - rank(b.id))).map((g) => (
          <button
            class={g.id === gymId ? 'gym selected' : 'gym'}
            aria-pressed={g.id === gymId}
            aria-label={g.id === gymId ? `${g.name}: choose a different gym` : undefined}
            onClick={() => chooseGym(g.id === gymId ? null : g.id)}
          >
            <BrandLogo brand={server.gyms.find((k) => k.id === g.ref)?.brand} />
            <span class="gym-name">{g.name}</span>
            {g.id === gymId ? (
              <span class="gym-address">{settings.gyms.length > 1 ? 'Change gym' : g.address}</span>
            ) : (
              g.address && <span class="gym-address">{g.address}</span>
            )}
          </button>
        ))}
      </div>

      {gym && (
        <section class="results">
          <h2 class="results-title">
            {direction === 'to-gym' ? `${home.name} to ${gym.name}` : `${gym.name} to ${home.name}`}
          </h2>
          <DataStatus realtime={plan.data?.realtime} loading={plan.loading} updatedAt={plan.updatedAt} walking={plan.data?.walking} />
          {plan.error !== null && !(plan.error instanceof AuthError) && (
            <p class="error">{(plan.error as Error).message}</p>
          )}
          {plan.data && plan.data.options.length === 0 && <WindowShift shift={shift} />}
          {plan.data && plan.data.options.length === 0 && (
            <p class="muted">
              {when === 'arrive'
                ? 'No way to get there by then on these lines. Try later trips.'
                : `Nothing leaves in these ${WINDOW_MIN} minutes. Try later trips.`}
            </p>
          )}
          {plan.data && plan.data.options.length > 0 && (
            <Board
              options={plan.data.options}
              live={leaveAt === null}
              serviceDate={plan.data.service_date}
              token={settings.token!}
              walks={settings.walks}
              places={places}
              origin={direction === 'to-gym' ? [home.lon, home.lat] : [gym.lon, gym.lat]}
              destination={direction === 'to-gym' ? [gym.lon, gym.lat] : [home.lon, home.lat]}
              title={direction === 'to-gym' ? `${home.name} to ${gym.name}` : `${gym.name} to ${home.name}`}
              ends={ends!}
              onShift={shift}
              onStart={(o) =>
                onStartTrip({
                  option: o,
                  plannedArrive: o.arrive,
                  request: request!,
                  ends: ends!, // a gym is chosen whenever there are options
                  title: direction === 'to-gym' ? `${home.name} to ${gym.name}` : `${gym.name} to ${home.name}`,
                  origin: direction === 'to-gym' ? [home.lon, home.lat] : [gym.lon, gym.lat],
                  destination: direction === 'to-gym' ? [gym.lon, gym.lat] : [home.lon, home.lat],
                  serviceDate: plan.data!.service_date,
                  walkSpeedMps: settings.walkSpeedMps ?? server.defaults.walk_speed_mps,
                })
              }
            />
          )}
        </section>
      )}
    </div>
  )
}

const DAYS_AHEAD = 7 // about a week: as far ahead as the timetable reaches
const pad = (n: number) => String(n).padStart(2, '0')

/** A day ("Today (4th)", "Tomorrow (5th)", …) and a 24-hour time in five-minute steps, all in Sydney time. */
function DayTime({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  const today = toLocalInput(new Date()).slice(0, 10)
  const day = value.slice(0, 10)
  const hour = value.slice(11, 13)
  const minute = value.slice(14, 16)
  const days = Array.from({ length: DAYS_AHEAD }, (_, i) => addDays(today, i))
  if (!days.includes(day)) days.push(day) // moved outside the usual range with Earlier / Later trips
  days.sort()
  const set = (d: string, h: string, m: string) => onChange(`${d}T${h}:${m}`)
  const pick = (e: Event) => (e.target as HTMLSelectElement).value
  return (
    <div class="daytime" role="group" aria-label={label}>
      <select class="day" aria-label="Day" value={day} onChange={(e) => set(pick(e), hour, minute)}>
        {days.map((d) => (
          <option value={d}>{dayLabel(today, d)}</option>
        ))}
      </select>
      <select aria-label="Hour" value={hour} onChange={(e) => set(day, pick(e), minute)}>
        {Array.from({ length: 24 }, (_, h) => (
          <option value={pad(h)}>{pad(h)}</option>
        ))}
      </select>
      <span aria-hidden="true">:</span>
      <select aria-label="Minutes" value={minute} onChange={(e) => set(day, hour, pick(e))}>
        {Array.from({ length: 12 }, (_, i) => (
          <option value={pad(i * 5)}>{pad(i * 5)}</option>
        ))}
      </select>
    </div>
  )
}

function DataStatus({
  realtime, loading, updatedAt, walking,
}: { realtime?: boolean; loading: boolean; updatedAt: number; walking?: 'streets' | 'estimate' }) {
  if (!updatedAt) return <p class="muted small">{loading ? 'Planning…' : ''}</p>
  const t = statusTime(updatedAt)
  return (
    <p class="muted small">
      <span class={realtime ? 'dot live' : 'dot'} aria-hidden="true" />
      {realtime ? 'Live data' : 'Timetable only'}, updated {t}
      {loading && ', refreshing…'}
      {walking === 'estimate' && (
        <>
          <br />
          Walks are rough estimates until the server has prepared the street map.
        </>
      )}
    </p>
  )
}
