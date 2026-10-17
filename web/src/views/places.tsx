import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, ApiError, AuthError, problem, type SuggestProgress } from '../api.ts'
import { gymFromKnown, isLine, LINE_MODES, MAX_GYMS, MAX_LINES, newId, placeRequest, withSuggested, type Gym, type Home } from '../settings.ts'
import { groupStops, relevantGroups, type StopGroup } from '../stops.ts'
import type { GeocodeResult, KnownGym, NearStop, SuggestResult } from '../types.ts'
import { BrandLogo } from './brand.tsx'
import { distance } from '../format.ts'
import { lineColour, textOn } from '../colour.ts'
import { Chevron, VEHICLE_PATHS, vehicleOf } from './icons.tsx'
import { ActionBar, Button, Callout, Field, Progress, Row, Section, Select, TextButton } from './ui.tsx'

export const newHome = (): Home => ({ id: newId(), name: 'Home', lat: NaN, lon: NaN, access: [] })
export const newGym = (): Gym => ({ id: newId(), name: '', lat: NaN, lon: NaN, access: [], lines: [] })

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** What a line search is doing, in the words beside its bar. */
const STAGE: Record<SuggestProgress['stage'], string> = {
  waiting: 'Waiting for another search',
  reading: 'Processing timetables',
  searching: 'Analysing trips',
}

export type EditorKind = 'home' | 'gym'

interface Props {
  kind: EditorKind
  place: Gym // homes ignore address and lines
  isNew: boolean
  token: string
  homes: Home[] // line suggestions start from one of these
  maxWalkM?: number // the device's longest walk, for line suggestions
  walkLimitM?: number // the longest walk in force (the device's, or the server's default), to say what "no stops chosen" means
  firstRun?: boolean // the first home: step 1 of setting up, with nothing to cancel back to
  onAuthError: () => void
  onSave: (p: Gym) => void
  onCancel: () => void
}

/** Adds or edits a home or a gym: where it is, the stops to walk to, and (for a gym) its lines. */
export function PlaceEditor({ kind, place, isNew, token, homes, maxWalkM, walkLimitM, firstRun, onAuthError, onSave, onCancel }: Props) {
  const [p, setP] = useState<Gym>(place)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<GeocodeResult[]>([])
  const [stops, setStops] = useState<NearStop[] | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const hasLocation = Number.isFinite(p.lat) && Number.isFinite(p.lon)
  const gym = kind === 'gym'
  // A home's or a built-in gym's name and place stay as they were set (a home that moves is deleted and added again):
  // only its stops, and a gym's lines, are yours to change. A gym you added yourself keeps its fields.
  const fixed = !isNew && (!gym || !!place.ref)

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
  // The list is closed until asked for: with none chosen, every stop in range is used, which suits most places.
  const [choosing, setChoosing] = useState(place.access.length > 0)
  const selectedIds = useMemo(() => new Set(p.access.map((a) => a.stop)), [p.access])
  const picked = (g: StopGroup) => g.ids.some((id) => selectedIds.has(id))
  // Ticking keeps which stops, not how long the walk is: that's worked out from the street map for each trip.
  const toggle = (g: StopGroup) => {
    const rest = p.access.filter((a) => !g.ids.includes(a.stop))
    setP({ ...p, access: picked(g) ? rest : [...rest, ...g.ids.map((stop) => ({ stop, name: g.name }))] })
  }
  const visibleGroups = showAll ? groups : relevantGroups(groups)

  const canSave = hasLocation && p.name.trim() !== '' && (!gym || p.lines.length > 0)

  return (
    <div class="stack settings">
      <Section
        title={firstRun ? 'Where do you start from?' : fixed ? p.name : isNew ? `Add ${kind}` : `Edit ${kind}`}
        intro={firstRun ? 'Step 1 of 2. Next: the gyms you climb at.' : fixed && p.address}
      >
        {!fixed && (
          <>
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
            {!gym && <p class="meta">Your address isn't stored on the server.</p>}
            <div class="actions place-actions">
              <Button onClick={useMyLocation}>Use my current location</Button>
            </div>
            {hasLocation && <p class="meta place-set">{p.address ? `Location set: ${p.address}.` : 'Location set.'}</p>}
          </>
        )}
        {busy && <p class="meta">{busy}</p>}
        {error && (
          <Callout tone="bad" role="alert">
            {error}
          </Callout>
        )}
      </Section>

      {stops && !choosing && stops.length > 0 && (
        <Section title={gym ? 'Stops near the gym' : 'Your stops'}>
          <p>Using every stop within {walkLimitM ? `a ${distance(walkLimitM)} walk` : 'your longest walk'}.</p>
          <p class="stops-more">
            <TextButton quiet aria-expanded={false} onClick={() => setChoosing(true)}>
              Choose stops
              <Chevron />
            </TextButton>
          </p>
        </Section>
      )}
      {stops && (choosing || stops.length === 0) && (
        <Section title={gym ? 'Stops near the gym' : 'Your stops'} intro={'Pick the stops you\'d actually walk to. If you pick none, every stop within your longest walk is considered (or, if there are none, the nearest ones). Walking times come from the street map, at your walking speed.'}>
          {stops.length === 0 && <p class="meta">No stops within 3 km.</p>}
          <ul class="rows stops">
            {visibleGroups.map((g) => {
              return (
                <li>
                  <label class="check">
                    <input type="checkbox" checked={picked(g)} onChange={() => toggle(g)} />
                    <span class="main">
                      {g.name}
                      <span class="meta">{g.lines.join(', ')}</span>
                    </span>
                  </label>
                  <span class="walkcell">
                    <span class="meta" title="From the street map">
                      ~{Math.max(1, Math.round(g.walk_s / 60))} min
                    </span>
                  </span>
                </li>
              )
            })}
          </ul>
          {groups.length > visibleGroups.length && (
            <p class="stops-more">
              <TextButton quiet onClick={() => setShowAll(true)}>Show {groups.length - visibleGroups.length} more</TextButton>
            </p>
          )}
        </Section>
      )}

      {gym && hasLocation && (
        <LinesSection
          gym={p}
          homes={homes}
          token={token}
          maxWalkM={maxWalkM}
          onAuthError={onAuthError}
          setLines={(lines) => setP((cur) => ({ ...cur, lines }))}
          onSuggested={(r, homeId) => setP((cur) => withSuggested(cur, r, homeId))}
        />
      )}

      {gym && hasLocation && p.lines.length === 0 && <p class="meta">Choose at least one line to save the gym.</p>}
      <ActionBar>
        {!firstRun && <Button onClick={onCancel}>Cancel</Button>}
        <Button variant="commit" disabled={!canSave} onClick={() => onSave({ ...p, name: p.name.trim() })}>
          Save {kind}
        </Button>
      </ActionBar>
    </div>
  )
}

const shareLabel = (share: number) => `${Math.max(1, Math.round(share * 100))}% of searches`

/** Each kind of transport, as the heading of its row of lines. */
const KIND: Record<(typeof LINE_MODES)[number], string> = {
  train: 'Train', metro: 'Metro', 'light-rail': 'Light rail', bus: 'Bus', ferry: 'Ferry', 'regional-train': 'Regional', coach: 'Coach',
}

const modeOf = (line: string) => line.slice(0, line.indexOf(' '))
const nameOf = (line: string) => line.slice(line.indexOf(' ') + 1)

/** Chooses the lines worth considering for a gym: the server suggests them from a home, the person decides. */
function LinesSection({
  gym, homes, token, maxWalkM, onAuthError, setLines, onSuggested,
}: {
  gym: Gym
  homes: Home[]
  token: string
  maxWalkM?: number
  onAuthError: () => void
  setLines: (l: string[]) => void
  onSuggested: (r: SuggestResult, homeId: string) => void
}) {
  const [homeId, setHomeId] = useState(homes[0]?.id ?? '')
  const [result, setResult] = useState<SuggestResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<SuggestProgress | null>(null)
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
    setProgress(null)
    setError('')
    try {
      const r = (await api.suggestLines(token, placeRequest(home), [placeRequest(gym)], c.signal, setProgress, maxWalkM)).results[0]
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
  // The gym's other lines (it was opened with them, or they were added by hand). One that's dropped stays in its
  // row as an outline, to be tapped back, for as long as the editor is open.
  const mine = useRef<string[]>([])
  for (const l of gym.lines) if (!mine.current.includes(l)) mine.current.push(l)
  const own = mine.current.filter((l) => !known.has(l))
  // A suggestion brings its lines' colours. The rest (a gym opened for editing, a line added by hand) are asked for,
  // so every chip here is in its line's colour, as it is everywhere else.
  const [colours, setColours] = useState<Record<string, string>>({})
  const ownKey = own.join(',')
  useEffect(() => {
    if (!ownKey) return
    const c = new AbortController()
    api.lineColours(token, ownKey.split(','), c.signal).then((r) => setColours((cur) => ({ ...cur, ...r.colors })), () => undefined)
    return () => c.abort()
  }, [ownKey])
  // Every line on offer: the search's, most used first, then the gym's others.
  const all: { line: string; color?: string; share?: number }[] = [
    ...candidates.map((l) => ({ line: l.line, color: l.color, share: l.share })),
    ...own.map((l) => ({ line: l, color: colours[l] })),
  ]
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
              <Select value={home?.id} onChange={(e) => setHomeId((e.target as HTMLSelectElement).value)}>
                {homes.map((h) => (
                  <option value={h.id}>{h.name}</option>
                ))}
              </Select>
            </label>
          )}
          <Button onClick={find} disabled={busy}>
            {busy ? 'Suggesting lines…' : result ? `Search again from ${home?.name}` : `Suggest lines from ${home?.name}`}
          </Button>
          {busy && <Progress fraction={progress?.fraction ?? 0}>{STAGE[progress?.stage ?? 'reading']}</Progress>}
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

      {all.length > 0 && (
        <>
          <p class="meta">Filled lines are used. Tap a line to add or drop it.</p>
          <div class="line-kinds">
            {LINE_MODES.filter((m) => all.some((l) => modeOf(l.line) === m)).map((m) => (
              <div class="line-kind" role="group" aria-label={KIND[m]}>
                <span class="line-kind-name">
                  <svg class="icon" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
                    <path d={VEHICLE_PATHS[vehicleOf(m)]} />
                  </svg>
                  {KIND[m]}
                </span>
                <div class="line-toggles">
                  {all.filter((l) => modeOf(l.line) === m).map((l) => {
                    const bg = lineColour(l.color)
                    return (
                      <button
                        type="button"
                        class="line-toggle"
                        aria-pressed={has(l.line)}
                        title={l.share === undefined ? undefined : shareLabel(l.share)}
                        style={has(l.line) ? { borderColor: bg, background: bg, color: textOn(bg) } : undefined}
                        onClick={() => toggle(l.line)}
                      >
                        {nameOf(l.line)}
                      </button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </>
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
            <Select value={mode} onChange={(e) => setMode((e.target as HTMLSelectElement).value)} aria-label="Mode">
              {LINE_MODES.map((m) => (
                <option value={m}>{m}</option>
              ))}
            </Select>
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
  firstRun?: boolean // step 2 of setting up
  home: Home | undefined
  token: string
  maxWalkM?: number // the device's longest walk, for line suggestions
  onAuthError: () => void
  /** Called with the gyms to add; `warning` says if their home-side lines couldn't be found. */
  onAdd: (gyms: Gym[], warning?: string) => void
  onCustom: () => void
  onCancel?: () => void
}

/** How long the bar is left to run to its end, saying "Adding your gyms", once the search is done. */
const FINISH_MS = 700

/** Picks gyms from the ones the app already knows, and finds the lines near home for them in one go. */
export function GymChooser({ known, have, firstRun, home, token, maxWalkM, onAuthError, onAdd, onCustom, onCancel }: ChooserProps) {
  const available = (known ?? []).filter((k) => !have.some((g) => g.ref === k.id))
  const [sel, setSel] = useState<Set<string>>(new Set()) // nothing ticked: the app knows more gyms than anyone climbs at
  const full = have.length + sel.size >= MAX_GYMS // the device has no room for another
  const many = sel.size > 1
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<SuggestProgress | null>(null)
  const [adding, setAdding] = useState(false) // the search is done: the bar runs to its end before the screen goes
  const [error, setError] = useState('')
  const ctrl = useRef<AbortController | null>(null)
  useEffect(() => () => ctrl.current?.abort(), [])

  const toggle = (id: string) => {
    const next = new Set(sel)
    if (!next.delete(id)) {
      if (full) return
      next.add(id)
    }
    setSel(next)
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
      setProgress(null)
      setError('')
      try {
        const res = await api.suggestLines(token, placeRequest(home), gyms.map((g) => placeRequest(g)), c.signal, setProgress, maxWalkM)
        if (c.signal.aborted) return
        gyms = gyms.map((g, i) => (res.results[i] ? withSuggested(g, res.results[i], home.id) : g))
        setAdding(true)
        await sleep(FINISH_MS)
        if (c.signal.aborted) return
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
            {firstRun && 'Step 2 of 2. '}The app knows these gyms and the lines that serve them. Tick the ones you climb at.{' '}
            {home ? `It will look up the lines near ${home.name} too, which can take up to a minute.` : ''}
          </p>
          <ul class="rows">
            {available.map((k) => (
              <li>
                <label class="check">
                  <input type="checkbox" checked={sel.has(k.id)} disabled={busy || (full && !sel.has(k.id))} onChange={() => toggle(k.id)} />
                  <BrandLogo brand={k.brand} />
                  <span class="main">
                    <span class="place-name">{k.name}</span>
                    {k.address && <span class="meta">{k.address}</span>}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          {full && <p class="meta">A device holds up to {MAX_GYMS} gyms. Untick one to tick another.</p>}
        </>
      )}
      {/* A gym the app doesn't know: a way forward beside the list, not a third button in the bar. */}
      <p class="meta">
        {available.length > 0 && 'Not listed? '}
        <TextButton quiet disabled={busy} onClick={onCustom}>
          Add another gym
        </TextButton>
      </p>
      {error && (
        <Callout tone="bad" role="alert">
          {error}
        </Callout>
      )}
      {(onCancel || available.length > 0) && (
        <ActionBar
          progress={
            busy && (
              <Progress fraction={adding ? 1 : (progress?.fraction ?? 0)}>
                {adding ? `Adding your gym${many ? 's' : ''}` : STAGE[progress?.stage ?? 'reading']}
              </Progress>
            )
          }
        >
          {/* Cancel stays live during the search: leaving stops waiting for it. */}
          {onCancel && <Button onClick={onCancel}>Cancel</Button>}
          {available.length > 0 && (
            <Button variant="commit" disabled={busy || sel.size === 0} onClick={add}>
              {busy ? `Adding gym${many ? 's' : ''}…` : many ? 'Add gyms' : 'Add gym'}
            </Button>
          )}
        </ActionBar>
      )}
    </Section>
  )
}
