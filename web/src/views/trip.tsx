import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, AuthError } from '../api.ts'
import { fromLocalInput, toLocalInput } from '../format.ts'
import { usePolling, useVisible } from '../hooks.ts'
import { placeRequest, prefs, setTransfer, walkTraces, type Settings, type TransferTime } from '../settings.ts'
import type { DefaultsResponse, PlanRequest } from '../types.ts'
import { Board } from './board.tsx'
import type { ActiveTrip } from './intrip.tsx'

type Direction = 'to-gym' | 'home'

const REFRESH_MS = 30_000
const WINDOW_MIN = 45

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
  const [when, setWhen] = useState<When>('now')
  const [at, setAt] = useState(() => toLocalInput(new Date())) // datetime-local value (Sydney time)
  const leaveAt = when === 'now' ? null : at
  const visible = useVisible()
  const home = settings.homes.find((h) => h.id === settings.activeHome) ?? settings.homes[0]

  const request: PlanRequest | null = useMemo(() => {
    const g = settings.gyms.find((x) => x.id === gymId)
    if (!g || !home) return null
    const gym = placeRequest(g)
    const place = placeRequest(home)
    return {
      from: direction === 'to-gym' ? place : gym,
      to: direction === 'to-gym' ? gym : place,
      lines: g.lines,
      time: leaveAt ? fromLocalInput(leaveAt) : undefined,
      arrive_by: when === 'arrive' || undefined,
      window_min: WINDOW_MIN,
      prefs: prefs(settings),
    }
  }, [gymId, home, direction, leaveAt, when, settings])

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

  const saveTransfer = (t: TransferTime) => setSettings(setTransfer(settings, t))

  if (!server) return <p class="muted">Loading…</p>
  if (!home || settings.gyms.length === 0) {
    return (
      <div class="empty">
        <p>{home ? 'Add a gym to plan trips to.' : 'Add your home and a gym, so trips can start (or end) there.'}</p>
        <button class="primary" onClick={goToSettings}>
          {home ? 'Add a gym' : 'Get started'}
        </button>
      </div>
    )
  }

  const gym = settings.gyms.find((g) => g.id === gymId)
  const traces = gym ? (direction === 'to-gym' ? walkTraces(home, gym) : walkTraces(gym, home)) : undefined

  return (
    <div class="stack">
      <div class="segmented" role="group" aria-label="Direction">
        <button aria-pressed={direction === 'to-gym'} onClick={() => setDirection('to-gym')}>
          To the gym
        </button>
        <button aria-pressed={direction === 'home'} onClick={() => setDirection('home')}>
          Home
        </button>
      </div>

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

      <div class="when">
        <div class="segmented small" role="group" aria-label="When">
          <button aria-pressed={when === 'now'} onClick={() => setWhen('now')}>
            Leave now
          </button>
          <button aria-pressed={when === 'leave'} onClick={() => setWhen('leave')}>
            Leave at
          </button>
          <button aria-pressed={when === 'arrive'} onClick={() => setWhen('arrive')}>
            Arrive by
          </button>
        </div>
        {when !== 'now' && (
          <input
            type="datetime-local"
            value={at}
            onChange={(e) => {
              const v = (e.target as HTMLInputElement).value
              if (v) setAt(v)
            }}
            aria-label={when === 'arrive' ? 'Arrive by' : 'Leave at'}
          />
        )}
      </div>

      <div class="gyms">
        {(gym ? [gym] : settings.gyms).map((g) => (
          <button
            class={g.id === gymId ? 'gym selected' : 'gym'}
            aria-pressed={g.id === gymId}
            aria-label={g.id === gymId ? `${g.name}: choose a different gym` : undefined}
            onClick={() => setGymId(g.id === gymId ? null : g.id)}
          >
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
          {plan.data && plan.data.options.length === 0 && (
            <p class="muted">
              {when === 'arrive'
                ? 'No way to get there by then on these lines. Try a later time.'
                : `Nothing leaves in the next ${WINDOW_MIN} minutes. Try a later time.`}
            </p>
          )}
          {plan.data && plan.data.options.length > 0 && (
            <Board
              options={plan.data.options}
              live={leaveAt === null}
              serviceDate={plan.data.service_date}
              token={settings.token!}
              traces={traces}
              origin={direction === 'to-gym' ? [home.lon, home.lat] : [gym.lon, gym.lat]}
              destination={direction === 'to-gym' ? [gym.lon, gym.lat] : [home.lon, home.lat]}
              title={direction === 'to-gym' ? `${home.name} to ${gym.name}` : `${gym.name} to ${home.name}`}
              transfers={settings.transfers}
              onSetTransfer={saveTransfer}
              onStart={(o) =>
                      onStartTrip({
                        option: o,
                        plannedArrive: o.arrive,
                        request: request!,
                        lines: gym.lines,
                        traces,
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

function DataStatus({
  realtime, loading, updatedAt, walking,
}: { realtime?: boolean; loading: boolean; updatedAt: number; walking?: 'streets' | 'estimate' }) {
  if (!updatedAt) return <p class="muted small">{loading ? 'Planning…' : ''}</p>
  const t = new Date(updatedAt).toLocaleTimeString('en-AU', { hour: 'numeric', minute: '2-digit', timeZone: 'Australia/Sydney' })
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
