import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, AuthError } from '../api.ts'
import { fromLocalInput, toLocalInput } from '../format.ts'
import { usePolling, useVisible } from '../hooks.ts'
import { homePlace, prefs, setTransfer, type Settings, type TransferTime } from '../settings.ts'
import type { GymsResponse, PlanRequest } from '../types.ts'
import { OptionCard } from './option.tsx'

type Direction = 'to-gym' | 'home'

const REFRESH_MS = 30_000
const WINDOW_MIN = 45

interface Props {
  settings: Settings
  setSettings: (s: Settings) => void
  gyms: GymsResponse | null
  onAuthError: () => void
  goToSettings: () => void
}

export function Trip({ settings, setSettings, gyms, onAuthError, goToSettings }: Props) {
  const [direction, setDirection] = useState<Direction>('to-gym')
  const [gymId, setGymId] = useState<string | null>(null)
  const [leaveAt, setLeaveAt] = useState<string | null>(null) // datetime-local value; null = now
  const visible = useVisible()
  const home = settings.homes.find((h) => h.id === settings.activeHome) ?? settings.homes[0]

  const request: PlanRequest | null = useMemo(() => {
    if (!gymId || !home) return null
    const gym = { gym: gymId }
    const place = homePlace(home)
    return {
      from: direction === 'to-gym' ? place : gym,
      to: direction === 'to-gym' ? gym : place,
      time: leaveAt ? fromLocalInput(leaveAt) : undefined,
      window_min: WINDOW_MIN,
      prefs: prefs(settings),
    }
  }, [gymId, home, direction, leaveAt, settings])

  const key = request ? JSON.stringify(request) : null
  const plan = usePolling(
    key,
    (signal) => api.plan(settings.token!, request!, signal),
    REFRESH_MS,
    visible && request !== null,
  )
  useEffect(() => {
    if (plan.error instanceof AuthError) onAuthError()
  }, [plan.error, onAuthError])

  const saveTransfer = (t: TransferTime) => setSettings(setTransfer(settings, t))

  if (!gyms) return <p class="muted">Loading gyms…</p>
  if (!home) {
    return (
      <div class="empty">
        <p>Add your home first, so trips can start (or end) there.</p>
        <button class="primary" onClick={goToSettings}>
          Add home
        </button>
      </div>
    )
  }

  const gym = gyms.gyms.find((g) => g.id === gymId)

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

      <div class="gyms">
        {gyms.gyms.map((g) => (
          <button class={g.id === gymId ? 'gym selected' : 'gym'} aria-pressed={g.id === gymId} onClick={() => setGymId(g.id)}>
            <span class="gym-name">{g.name}</span>
            {g.address && <span class="gym-address">{g.address}</span>}
          </button>
        ))}
      </div>

      <div class="when">
        <div class="segmented small" role="group" aria-label="When">
          <button aria-pressed={leaveAt === null} onClick={() => setLeaveAt(null)}>
            Leave now
          </button>
          <button aria-pressed={leaveAt !== null} onClick={() => setLeaveAt(leaveAt ?? toLocalInput(new Date()))}>
            Leave at…
          </button>
        </div>
        {leaveAt !== null && (
          <input
            type="datetime-local"
            value={leaveAt}
            onChange={(e) => setLeaveAt((e.target as HTMLInputElement).value || null)}
            aria-label="Leave at"
          />
        )}
      </div>

      {gym && (
        <section aria-live="polite">
          <h2 class="results-title">
            {direction === 'to-gym' ? `${home.name} → ${gym.name}` : `${gym.name} → ${home.name}`}
          </h2>
          <DataStatus realtime={plan.data?.realtime} loading={plan.loading} updatedAt={plan.updatedAt} />
          {plan.error !== null && !(plan.error instanceof AuthError) && (
            <p class="error">{(plan.error as Error).message}</p>
          )}
          {plan.data && plan.data.options.length === 0 && <p class="muted">No options in the next {WINDOW_MIN} minutes.</p>}
          <ol class="options">
            {plan.data?.options.map((o, i) => (
              <li>
                <OptionCard option={o} first={i === 0} live={leaveAt === null} onSetTransfer={saveTransfer} transfers={settings.transfers} />
              </li>
            ))}
          </ol>
        </section>
      )}
    </div>
  )
}

function DataStatus({ realtime, loading, updatedAt }: { realtime?: boolean; loading: boolean; updatedAt: number }) {
  if (!updatedAt) return <p class="muted small">{loading ? 'Planning…' : ''}</p>
  const t = new Date(updatedAt).toLocaleTimeString('en-AU', { hour: 'numeric', minute: '2-digit', timeZone: 'Australia/Sydney' })
  return (
    <p class="muted small">
      <span class={realtime ? 'dot live' : 'dot'} aria-hidden="true" />
      {realtime ? 'Live data' : 'Timetable only'} · updated {t}
      {loading && ' · refreshing…'}
    </p>
  )
}
