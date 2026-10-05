import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, ApiError, AuthError, problem } from '../api.ts'
import { gymFromKnown, isLine, LINE_MODES, MAX_LINES, newId, placeRequest, withSuggested, type Gym, type Home } from '../settings.ts'
import { groupStops, relevantGroups, type StopGroup } from '../stops.ts'
import type { GeocodeResult, KnownGym, NearStop, SuggestResult } from '../types.ts'
import { LineChip } from './option.tsx'
import { BrandLogo } from './brand.tsx'
import { mmss } from '../walkmeasure.ts'
import { placeKey, walkSecs, type AccessWalk, type TimedWalk } from '../walks.ts'
import { ActionBar, Button, Callout, Field, Row, Section, TextButton } from './ui.tsx'

export const newHome = (): Home => ({ id: newId(), name: 'Home', lat: NaN, lon: NaN, access: [] })
export const newGym = (): Gym => ({ id: newId(), name: '', lat: NaN, lon: NaN, access: [], lines: [] })

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

export type EditorKind = 'home' | 'gym'

interface Props {
  kind: EditorKind
  place: Gym // homes ignore address and lines
  isNew: boolean
  token: string
  homes: Home[] // line suggestions start from one of these
  walks: TimedWalk[] // walks timed during trips, shown against their stops
  onAuthError: () => void
  onSave: (p: Gym) => void
  onCancel: () => void
}

/** Adds or edits a home or a gym: where it is, the stops to walk to, and (for a gym) its lines. */
export function PlaceEditor({ kind, place, isNew, token, homes, walks, onAuthError, onSave, onCancel }: Props) {
  const [p, setP] = useState<Gym>(place)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<GeocodeResult[]>([])
  const [stops, setStops] = useState<NearStop[] | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const hasLocation = Number.isFinite(p.lat) && Number.isFinite(p.lon)
  const gym = kind === 'gym'

  const fail = (e: unknown) => {
    setBusy('')
    if (e instanceof AuthError) onAuthError()
    // the API's failures in words; location failures already are
    else setError(e instanceof ApiError ? problem(e) : e instanceof Error ? e.message : String(e))
  }

  // The server reads the whole timetable shortly after it starts; wait for it rather than failing.
  const loadStops = async (lat: number, lon: number) => {
    setBusy('Finding nearby stops…')
    try {
      for (let attempt = 0; ; attempt++) {
        try {
          setStops((await api.stopsNear(token, lat, lon, gym ? 1200 : 1500)).stops)
          setBusy('')
          return
        } catch (e) {
          if (!(e instanceof ApiError && e.status === 503 && attempt < 8)) throw e
          setBusy('The server is still reading the timetable…')
          await sleep(3000)
        }
      }
    } catch (e) {
      fail(e)
    }
  }

  useEffect(() => {
    if (hasLocation) loadStops(p.lat, p.lon)
  }, [])

  const setLocation = (lat: number, lon: number, address?: string) => {
    setError('')
    setResults([])
    setP((cur) => ({ ...cur, lat, lon, access: [], address: address ?? cur.address }))
    loadStops(lat, lon)
  }

  const search = async (e: Event) => {
    e.preventDefault()
    setError('')
    setBusy('Searching…')
    try {
      const r = await api.geocode(token, query)
      setResults(r.results)
      setBusy('')
      if (r.results.length === 0) setError('No matches.')
    } catch (err) {
      fail(err)
    }
  }

  const useMyLocation = () => {
    if (!navigator.geolocation) return setError("This browser can't share your location.")
    setBusy('Getting your location…')
    navigator.geolocation.getCurrentPosition(
      (pos) => setLocation(pos.coords.latitude, pos.coords.longitude),
      (err) => fail(new Error(err.code === err.PERMISSION_DENIED ? 'Location is turned off for this site.' : "Couldn't get your location.")),
      { enableHighAccuracy: true, timeout: 15000 },
    )
  }

  // One row per station (all its platforms) or standalone stop.
  const groups = useMemo(() => groupStops(stops ?? []), [stops])
  const [showAll, setShowAll] = useState(false)
  const selectedIds = useMemo(() => new Set(p.access.map((a) => a.stop)), [p.access])
  const groupWalk = (g: StopGroup) => p.access.find((a) => g.ids.includes(a.stop))?.walk_s
  const toggle = (g: StopGroup) => {
    const on = g.ids.some((id) => selectedIds.has(id))
    const rest = p.access.filter((a) => !g.ids.includes(a.stop))
    setP({ ...p, access: on ? rest : [...rest, ...g.ids.map((stop) => ({ stop, name: g.name, walk_s: g.walk_s }))] })
  }
  const visibleGroups = showAll ? groups : relevantGroups(groups)

  // Walks timed during trips win over the minutes typed here.
  const key = placeKey(kind, p)
  const timedOf = (g: StopGroup) =>
    walks.find((w): w is AccessWalk => w.kind === 'access' && w.place === key && w.stop.some((id) => id === g.key || g.ids.includes(id)))

  const canSave = hasLocation && p.name.trim() !== '' && (!gym || p.lines.length > 0)

  return (
    <div class="stack">
      <Section title={isNew ? `Add ${kind}` : `Edit ${kind}`}>
        <Field label="Name">
          <input
            value={p.name}
            maxLength={60}
            placeholder={gym ? 'e.g. 9 Degrees Lane Cove' : ''}
            onInput={(e) => setP({ ...p, name: (e.target as HTMLInputElement).value })}
          />
        </Field>
        <form onSubmit={search} class="row-form">
          <Field label="Address">
            <input type="search" value={query} placeholder="Street address or place" onInput={(e) => setQuery((e.target as HTMLInputElement).value)} />
          </Field>
          <Button type="submit" disabled={query.trim().length < 3}>
            Search
          </Button>
        </form>
        {results.length > 0 && (
          <ul class="rows pick" aria-label="Places found">
            {results.map((r) => (
              <li>
                <button class="pick-row" onClick={() => setLocation(r.lat, r.lon, r.name)}>
                  {r.name}
                </button>
              </li>
            ))}
          </ul>
        )}
        <p class="meta">Searches go through your server to the Transport for NSW trip planner; nothing is stored.</p>
        <div class="actions place-actions">
          <Button onClick={useMyLocation}>Use my current location</Button>
        </div>
        {hasLocation && <p class="meta place-set">{p.address ? `Location set: ${p.address}.` : 'Location set.'}</p>}
        {busy && <p class="meta">{busy}</p>}
        {error && (
          <Callout tone="bad" role="alert">
            {error}
          </Callout>
        )}
      </Section>

      {stops && (
        <Section title={gym ? 'Stops near the gym' : 'Your stops'} intro={'Pick the stops you\'d actually walk to. If you pick none, every stop within your walking limit is considered. Walking times come from the street map until you time a walk on a trip ("Time my walk"); a timed walk is used for its stop, picked here or not.'}>
          {stops.length === 0 && <p class="meta">No stops within {gym ? '1.2' : '1.5'} km.</p>}
          <ul class="rows stops">
            {visibleGroups.map((g) => {
              const walk = groupWalk(g)
              const timed = timedOf(g)
              return (
                <li>
                  <label class="check">
                    <input type="checkbox" checked={walk !== undefined} onChange={() => toggle(g)} />
                    <span class="main">
                      {g.name}
                      <span class="meta">{g.lines.join(', ')}</span>
                    </span>
                  </label>
                  <span class="walkcell">
                    {timed ? (
                      <span title="Timed during a trip; this is the time used">timed {mmss(walkSecs(timed))}</span>
                    ) : (
                      <span class="meta" title="From the street map">
                        ~{Math.max(1, Math.round(g.walk_s / 60))} min
                      </span>
                    )}
                  </span>
                </li>
              )
            })}
          </ul>
          {groups.length > visibleGroups.length && (
            <p class="stops-more">
              <TextButton onClick={() => setShowAll(true)}>Show {groups.length - visibleGroups.length} more</TextButton>
            </p>
          )}
        </Section>
      )}

      {gym && hasLocation && (
        <LinesSection
          gym={p}
          homes={homes}
          token={token}
          onAuthError={onAuthError}
          setLines={(lines) => setP((cur) => ({ ...cur, lines }))}
          onSuggested={(r, homeId) => setP((cur) => withSuggested(cur, r, homeId))}
        />
      )}

      {gym && hasLocation && p.lines.length === 0 && <p class="meta">Choose at least one line to save the gym.</p>}
      <ActionBar>
        <Button onClick={onCancel}>Cancel</Button>
        <Button disabled={!canSave} onClick={() => onSave({ ...p, name: p.name.trim() })}>
          Save {kind}
        </Button>
      </ActionBar>
    </div>
  )
}

const shareLabel = (share: number) => `${Math.max(1, Math.round(share * 100))}% of searches`

function chipOf(key: string, color?: string) {
  const i = key.indexOf(' ')
  return (
    <>
      <LineChip line={{ mode: key.slice(0, i), name: key.slice(i + 1), color }} /> {key.slice(0, i)}
    </>
  )
}

/** Chooses the lines worth considering for a gym: the server suggests them from a home, the person decides. */
function LinesSection({
  gym, homes, token, onAuthError, setLines, onSuggested,
}: {
  gym: Gym
  homes: Home[]
  token: string
  onAuthError: () => void
  setLines: (l: string[]) => void
  onSuggested: (r: SuggestResult, homeId: string) => void
}) {
  const [homeId, setHomeId] = useState(homes[0]?.id ?? '')
  const [result, setResult] = useState<SuggestResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState('')
  const [mode, setMode] = useState<string>('bus')
  const [name, setName] = useState('')
  const ctrl = useRef<AbortController | null>(null)
  useEffect(() => () => ctrl.current?.abort(), [])
  const home = homes.find((h) => h.id === homeId) ?? homes[0]

  const find = async () => {
    if (!home) return
    ctrl.current?.abort()
    const c = new AbortController()
    ctrl.current = c
    setBusy(true)
    setProgress(0)
    setError('')
    try {
      const r = (await api.suggestLines(token, placeRequest(home), [placeRequest(gym)], c.signal, setProgress)).results[0]
      if (c.signal.aborted) return
      setResult(r)
      onSuggested(r, home.id) // keeps what's already chosen and adds what the search recommends
    } catch (e) {
      if (c.signal.aborted) return
      if (e instanceof AuthError) onAuthError()
      else setError(problem(e))
    } finally {
      if (!c.signal.aborted) setBusy(false)
    }
  }

  const has = (l: string) => gym.lines.includes(l)
  const toggle = (l: string) => setLines(has(l) ? gym.lines.filter((x) => x !== l) : [...gym.lines, l].slice(0, MAX_LINES))
  const candidates = (result?.lines ?? []).filter((l) => isLine(l.line))
  const known = new Set(candidates.map((l) => l.line))
  const own = gym.lines.filter((l) => !known.has(l))
  const manual = `${mode} ${name.trim()}`
  const canAdd = name.trim() !== '' && isLine(manual) && !has(manual)

  return (
    <Section title="Lines" intro="The router only considers these lines, from your home to the gym and back. Include the lines near your home too. Fewer lines means a faster, sharper search.">
      {homes.length === 0 ? (
        <p class="meta">Add a home first to get suggestions. You can also add lines by hand below.</p>
      ) : (
        <div class="suggest">
          {homes.length > 1 && (
            <label class="inline-choice">
              From
              <select value={home?.id} onChange={(e) => setHomeId((e.target as HTMLSelectElement).value)}>
                {homes.map((h) => (
                  <option value={h.id}>{h.name}</option>
                ))}
              </select>
            </label>
          )}
          <Button onClick={find} disabled={busy}>
            {busy ? `Reading the timetable… ${Math.round(progress * 100)}%` : result ? `Search again from ${home?.name}` : `Suggest lines from ${home?.name}`}
          </Button>
          {busy && <p class="meta">Checking trips at many departure times. This can take up to a minute.</p>}
        </div>
      )}
      {error && (
        <Callout tone="bad" role="alert">
          {error}
        </Callout>
      )}

      {result && result.lines.length === 0 && <p class="meta">No way to get there on public transport. Check both locations.</p>}
      {result && result.windows.length > 0 && (
        <p class="meta">
          Checked{' '}
          {result.windows
            .map((w) => `${w.label.toLowerCase()}${w.typical_s ? ` (about ${Math.round(w.typical_s / 60)} min)` : ''}`)
            .join(' and ')}
          .
        </p>
      )}

      {(candidates.length > 0 || own.length > 0) && (
        <ul class="rows lines">
          {candidates.map((l) => (
            <li>
              <label class="check">
                <input type="checkbox" checked={has(l.line)} onChange={() => toggle(l.line)} />
                <span class="main">
                  {chipOf(l.line, l.color)}
                  <span class="meta">{shareLabel(l.share)}</span>
                </span>
              </label>
            </li>
          ))}
          {own.map((l) => (
            <li>
              <label class="check">
                <input type="checkbox" checked onChange={() => toggle(l)} />
                <span class="main">
                  {chipOf(l)}
                  {result && <span class="meta">Added by you</span>}
                </span>
              </label>
            </li>
          ))}
        </ul>
      )}

      {result && result.itineraries.length > 0 && (
        <details>
          <summary>How the best trips go</summary>
          <ul class="rows itins">
            {result.itineraries.map((it) => (
              <Row main={it.desc} meta={`${Math.round(it.median_s / 60)} min, ${it.seen} of ${it.of} ${it.window.toLowerCase()} departures`} />
            ))}
          </ul>
        </details>
      )}

      <form
        class="row-form"
        onSubmit={(e) => {
          e.preventDefault()
          if (canAdd) {
            setLines([...gym.lines, manual].slice(0, MAX_LINES))
            setName('')
          }
        }}
      >
        <Field label="Add a line">
          <span class="inline-pair">
            <select value={mode} onChange={(e) => setMode((e.target as HTMLSelectElement).value)} aria-label="Mode">
              {LINE_MODES.map((m) => (
                <option value={m}>{m}</option>
              ))}
            </select>
            <input value={name} maxLength={12} placeholder="288, T9, M1" onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          </span>
        </Field>
        <Button type="submit" disabled={!canAdd}>
          Add
        </Button>
      </form>
    </Section>
  )
}


interface ChooserProps {
  known: KnownGym[] | null
  have: Gym[] // already on this device
  home: Home | undefined
  token: string
  onAuthError: () => void
  /** Called with the gyms to add; `warning` says if their home-side lines couldn't be found. */
  onAdd: (gyms: Gym[], warning?: string) => void
  onCustom: () => void
  onCancel?: () => void
}

/** Picks gyms from the ones the app already knows, and finds the lines near home for them in one go. */
export function GymChooser({ known, have, home, token, onAuthError, onAdd, onCustom, onCancel }: ChooserProps) {
  const available = (known ?? []).filter((k) => !have.some((g) => g.ref === k.id))
  const [picked, setPicked] = useState<Set<string> | null>(null)
  const sel = picked ?? new Set(available.map((k) => k.id)) // everything ticked until the person says otherwise
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState('')
  const ctrl = useRef<AbortController | null>(null)
  useEffect(() => () => ctrl.current?.abort(), [])

  const toggle = (id: string) => {
    const next = new Set(sel)
    if (!next.delete(id)) next.add(id)
    setPicked(next)
  }

  const add = async () => {
    let gyms = available.filter((k) => sel.has(k.id)).map(gymFromKnown)
    if (gyms.length === 0) return
    let warning: string | undefined
    if (home) {
      ctrl.current?.abort()
      const c = new AbortController()
      ctrl.current = c
      setBusy(true)
      setProgress(0)
      setError('')
      try {
        const res = await api.suggestLines(token, placeRequest(home), gyms.map((g) => placeRequest(g)), c.signal, setProgress)
        if (c.signal.aborted) return
        gyms = gyms.map((g, i) => (res.results[i] ? withSuggested(g, res.results[i], home.id) : g))
      } catch (e) {
        if (c.signal.aborted) return
        if (e instanceof AuthError) return onAuthError()
        warning = `Added, but couldn't find the lines near ${home.name}. ${problem(e)} Edit a gym to try again.`
      }
      setBusy(false)
    } else {
      warning = 'Add a home to find the lines near it.'
    }
    onAdd(gyms, warning)
  }

  return (
    <Section title={have.length === 0 ? 'Which gyms do you climb at?' : 'Add a gym'}>
      {!known ? (
        <p class="loading">Loading…</p>
      ) : available.length === 0 ? (
        <p class="meta">You have all the gyms the app knows about.</p>
      ) : (
        <>
          <p class="meta intro">
            The app knows these gyms and the lines that serve them.{' '}
            {home ? `It will look up the lines near ${home.name} too, which can take up to a minute.` : ''}
          </p>
          <ul class="rows">
            {available.map((k) => (
              <li>
                <label class="check">
                  <input type="checkbox" checked={sel.has(k.id)} disabled={busy} onChange={() => toggle(k.id)} />
                  <BrandLogo brand={k.brand} />
                  <span class="main">
                    {k.name}
                    {k.address && <span class="meta">{k.address}</span>}
                  </span>
                </label>
              </li>
            ))}
          </ul>
        </>
      )}
      {busy && home && <p class="meta">Finding the lines near {home.name}: {Math.round(progress * 100)}%. This can take up to a minute.</p>}
      {error && (
        <Callout tone="bad" role="alert">
          {error}
        </Callout>
      )}
      <ActionBar>
        {onCancel && (
          <Button disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button disabled={busy} onClick={onCustom}>
          Add another gym
        </Button>
        {available.length > 0 && (
          <Button disabled={busy || sel.size === 0} onClick={add}>
            {busy ? 'Finding lines…' : sel.size === 1 ? 'Add gym' : `Add ${sel.size} gyms`}
          </Button>
        )}
      </ActionBar>
    </Section>
  )
}
