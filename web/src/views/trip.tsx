import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, AuthError, problem } from '../api.ts'
import { addDays, dayLabel, distance, fromLocalInput, orList, roundUp, statusTime, toLocalInput } from '../format.ts'
import { usePolling, useSettled, useVisible, useWide } from '../hooks.ts'
import { byUse, dismissWalk, placeRef, planPlace, prefs, usedGym, walkDismissed, walkSpeed, type Gym, type Settings } from '../settings.ts'
import type { DefaultsResponse, PlanRequest } from '../types.ts'
import { Board, WindowShift, type Shift } from './board.tsx'
import { BrandLogo } from './brand.tsx'
import type { ActiveTrip } from './intrip.tsx'
import { LineChip } from './option.tsx'
import { Button, Callout, IconButton, Segmented } from './ui.tsx'

type Direction = 'to-gym' | 'home'

const REFRESH_MS = 30_000
const WINDOW_MIN = 45
const SHIFT_MS = 30 * 60_000
const SETTLE_MS = 500 // a time edit searches once it has been still this long

interface Props {
  settings: Settings
  setSettings: (s: Settings) => void
  server: DefaultsResponse | null
  onAuthError: () => void
  goToSettings: () => void
  onStartTrip: (t: ActiveTrip) => void
  serverError?: boolean // the app couldn't reach the server (it says so above, with Try again)
}

type When = 'now' | 'leave' | 'arrive'

export function Trip({ settings, setSettings, server, onAuthError, goToSettings, onStartTrip, serverError }: Props) {
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
  // Choosing a time starts from a sensible one if the one last chosen won't do: now for leaving, an hour from now for
  // arriving (arriving by "now" would only offer trips that have already left).
  const chooseTime = (w: 'leave' | 'arrive') => {
    const soonest = w === 'arrive' ? Date.now() + 30 * 60_000 : Date.now()
    if (Date.parse(fromLocalInput(at)) < soonest) setAt(roundUp(toLocalInput(new Date(w === 'arrive' ? Date.now() + 60 * 60_000 : Date.now()))))
    setWhen(w)
  }
  const [moved, setMoved] = useState<number | null>(null) // "Leave now", moved earlier or later: where the window starts
  useEffect(() => setMoved(null), [when, gymId, direction])
  // The search uses the time once it has settled, so stepping through day, hour and minute doesn't search every step.
  // Before a gym is chosen nothing searches, so the time counts at once: choosing a gym straight after it plans for it.
  const [plannedWhen, plannedAt] = useSettled(`${when} ${at}`, gymId ? SETTLE_MS : 0).split(' ') as [When, string]
  const leaveAt = plannedWhen === 'now' ? null : plannedAt
  const visible = useVisible()
  const wide = useWide()
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
      arrive_by: plannedWhen === 'arrive' || undefined,
      window_min: WINDOW_MIN,
      prefs: prefs(settings),
    }
  }, [gymId, home, direction, leaveAt, moved, plannedWhen, settings])

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


  if (!server) return serverError ? null : <GymsSkeleton />
  if (!home || settings.gyms.length === 0) {
    return (
      <div class="empty">
        <p>{home ? 'Add a gym to plan trips to.' : 'Add your home and a gym, so trips can start (or end) there.'}</p>
        <Button onClick={goToSettings}>
          {home ? 'Add a gym' : 'Get started'}
        </Button>
      </div>
    )
  }

  const gym = settings.gyms.find((g) => g.id === gymId)
  const ends = gym && (direction === 'to-gym' ? { start: placeRef('home', home), end: placeRef('gym', gym) } : { start: placeRef('gym', gym), end: placeRef('home', home) })
  const places = { start: ends?.start.key, end: ends?.end.key }
  const title = gym && (direction === 'to-gym' ? `${home.name} to ${gym.name}` : `${gym.name} to ${home.name}`)
  const brandOf = (g: Gym) => server.gyms.find((k) => k.id === g.ref)?.brand
  // Ends whose nearest stop is beyond the longest walk (on foot), which the server planned from anyway.
  const sw = plan.data?.stretched_walk
  // Each is the walk between a place and this gym's lines, so one that's dismissed stays away either way round.
  const stretched = gym && ends && sw
    ? ([[ends.start, sw.from_m], [ends.end, sw.to_m]] as const)
        .flatMap(([p, m]) => (m && !walkDismissed(settings, gym.id, p.key, m) ? [{ place: p.key, name: p.name, m }] : []))
    : []

  return (
    <div class="stack trip">
      {/* When (and which home) come first: they shape the search, which starts when a gym is chosen. */}
      <div class="when">
        {settings.homes.length > 1 && (
          <label class="inline-choice">
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
        <Segmented
          label="When"
          options={[['now', 'Leave now'], ['leave', 'Leave at'], ['arrive', 'Arrive by']] as const}
          value={when}
          onChange={(w) => (w === 'now' ? setWhen('now') : chooseTime(w))}
        />
        {when !== 'now' && <DayTime value={at} onChange={setAt} label={when === 'arrive' ? 'Arrive by' : 'Leave at'} />}
      </div>

      {gym ? (
        // The chosen gym leads: tap its name to choose another; the arrows reverse the trip.
        <div class="route">
          <BrandLogo brand={brandOf(gym)} />
          <button class="to" aria-label={`${gym.name}: choose a different gym`} onClick={() => chooseGym(null)}>
            {gym.name}
            <svg class="icon" viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
              <path d="M6 9.5l6 6 6-6" />
            </svg>
          </button>
          <span class="from meta">{direction === 'to-gym' ? `from ${home.name}` : `to ${home.name}`}</span>
          <IconButton
            label={direction === 'to-gym' ? `Reverse: from ${gym.name} to ${home.name}` : `Reverse: from ${home.name} to ${gym.name}`}
            onClick={() => setDirection(direction === 'to-gym' ? 'home' : 'to-gym')}
          >
            <svg class="icon" viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
              <path d="M8 4v15m-4-4 4 4 4-4M16 20V5m-4 4 4-4 4 4" />
            </svg>
          </IconButton>
        </div>
      ) : (
        <div class="gyms" role="group" aria-label="Choose a gym">
          {[...settings.gyms].sort((a, b) => rank(a.id) - rank(b.id)).map((g) => (
            <button class="gym" onClick={() => chooseGym(g.id)}>
              <BrandLogo brand={brandOf(g)} />
              <span class="gym-name">{g.name}</span>
              {g.address && <span class="gym-address">{g.address}</span>}
            </button>
          ))}
        </div>
      )}

      {gym && (
        <section class="results" aria-label={title}>
          <DataStatus realtime={plan.data?.realtime} loading={plan.loading} updatedAt={plan.updatedAt} walking={plan.data?.walking} />
          {plan.error !== null && !(plan.error instanceof AuthError) && (
            <Callout tone="bad" role="alert" action={<Button onClick={plan.refresh}>Try again</Button>}>
              <strong>Couldn't plan this trip.</strong> {problem(plan.error)}
              {plan.data ? ` The plan below is from ${statusTime(plan.updatedAt)}.` : ''}
            </Callout>
          )}
          {plan.data?.trackwork?.map((t) => (
            <Callout tone="caution">
              <LineChip line={t.line} /> <strong>Trackwork:</strong> buses replace some trains. Their signs say{' '}
              {orList(t.buses)}.
            </Callout>
          ))}
          {stretched.map((s) => (
            <Callout key={s.place} onDismiss={() => setSettings(dismissWalk(settings, gym.id, s.place, s.m))}>
              <strong>Longer walk:</strong> no stops on these lines within a{' '}
              {distance(settings.maxWalkM ?? server.defaults.max_walk_m)} walk of {s.name}. The nearest is a {distance(s.m)} walk.
            </Callout>
          ))}
          {!plan.data && plan.loading && <BoardSkeleton />}
          {plan.data && plan.data.options.length === 0 && <WindowShift shift={shift} />}
          {plan.data && plan.data.options.length === 0 && (
            <p class="meta">
              {plannedWhen === 'arrive'
                ? 'No way to get there by then on these lines. Try later trips.'
                : `Nothing leaves in these ${WINDOW_MIN} minutes. Try later trips.`}
            </p>
          )}
          {plan.data && plan.data.options.length > 0 && (
            <Board
              options={[...plan.data.options].sort((a, b) => Date.parse(a.leave_at) - Date.parse(b.leave_at))}
              preferLatest={plannedWhen === 'arrive'}
              live={leaveAt === null}
              serviceDate={plan.data.service_date}
              token={settings.token!}
              walks={settings.walks}
              places={places}
              origin={direction === 'to-gym' ? [home.lon, home.lat] : [gym.lon, gym.lat]}
              destination={direction === 'to-gym' ? [gym.lon, gym.lat] : [home.lon, home.lat]}
              title={title!}
              ends={ends!}
              onShift={shift}
              onStart={(o) =>
                onStartTrip({
                  option: o,
                  plannedArrive: o.arrive,
                  request: request!,
                  ends: ends!, // a gym is chosen whenever there are options
                  title: title!,
                  origin: direction === 'to-gym' ? [home.lon, home.lat] : [gym.lon, gym.lat],
                  destination: direction === 'to-gym' ? [gym.lon, gym.lat] : [home.lon, home.lat],
                  serviceDate: plan.data!.service_date,
                  walkSpeedMps: walkSpeed(settings) ?? server.defaults.walk_speed_mps,
                  tightS: settings.risk?.tight_s ?? server.defaults.risk.tight_s,
                })
              }
            />
          )}
        </section>
      )}
      {wide && !(gym && plan.data && plan.data.options.length > 0) && (
        <aside class="map-pane">
          <p class="empty-map">{gym ? 'The map shows a trip once there is one.' : 'Choose a gym and the trip shows here on the map.'}</p>
        </aside>
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
  if (!updatedAt) return <p class="meta status-line">{loading ? 'Planning…' : ''}</p>
  return (
    <p class="meta status-line">
      <span class={realtime ? 'live-dot live' : 'live-dot'} aria-hidden="true" />
      {realtime ? 'Live data' : 'Timetable only'}, updated {statusTime(updatedAt)}
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

/** While the first plan loads: the countdown and the board's rows, in outline. */
function BoardSkeleton() {
  return (
    <div class="board-skeleton" aria-label="Planning…" role="status">
      <span class="skeleton skeleton-hero" />
      <div class="strips">
        {[0, 1, 2, 3, 4].map(() => (
          <div class="skeleton-row">
            <span class="skeleton" />
            <span class="skeleton" />
            <span class="skeleton" />
          </div>
        ))}
      </div>
    </div>
  )
}

/** While the app reaches the server: the gyms' rows, in outline. */
function GymsSkeleton() {
  return (
    <div class="gyms" role="status" aria-label="Loading…">
      {[0, 1, 2].map(() => (
        <div class="gym">
          <span class="skeleton" style={{ gridRow: 'span 2', width: '32px', height: '32px' }} />
          <span class="skeleton" style={{ height: '16px', width: '60%' }} />
          <span class="skeleton" style={{ height: '12px', width: '40%', marginTop: '6px' }} />
        </div>
      ))}
    </div>
  )
}
