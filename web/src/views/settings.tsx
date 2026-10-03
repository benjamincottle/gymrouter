import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { encode } from 'uqr'
import { api, AuthError } from '../api.ts'
import { emptySettings, newId, sanitize, settingsLink, type Home, type Settings } from '../settings.ts'
import type { GeocodeResult, GymsResponse, NearStop } from '../types.ts'
import { groupStops, relevantGroups, type StopGroup } from '../stops.ts'

interface Props {
  settings: Settings
  setSettings: (s: Settings) => void
  gyms: GymsResponse | null
  onAuthError: () => void
}

export function SettingsView({ settings, setSettings, gyms, onAuthError }: Props) {
  const [editing, setEditing] = useState<Home | null>(settings.homes.length === 0 ? newHome() : null)
  const d = gyms?.defaults

  if (editing) {
    return (
      <HomeEditor
        home={editing}
        token={settings.token!}
        onAuthError={onAuthError}
        onCancel={() => setEditing(null)}
        onSave={(h) => {
          const exists = settings.homes.some((x) => x.id === h.id)
          const homes = exists ? settings.homes.map((x) => (x.id === h.id ? h : x)) : [...settings.homes, h]
          setSettings({ ...settings, homes, activeHome: settings.activeHome ?? h.id })
          setEditing(null)
        }}
      />
    )
  }

  return (
    <div class="stack">
      <section class="card">
        <h2>Homes</h2>
        <p class="muted small">Stored only on this device.</p>
        <ul class="list">
          {settings.homes.map((h) => (
            <li>
              <span>
                {h.name}
                <span class="muted small">
                  {' '}
                  · {stopCount(h)}
                </span>
              </span>
              <span>
                <button class="link" onClick={() => setEditing(h)}>
                  Edit
                </button>
                <button
                  class="link danger"
                  onClick={() => {
                    if (!confirm(`Remove ${h.name}?`)) return
                    const homes = settings.homes.filter((x) => x.id !== h.id)
                    setSettings({ ...settings, homes, activeHome: homes[0]?.id })
                  }}
                >
                  Remove
                </button>
              </span>
            </li>
          ))}
        </ul>
        <button onClick={() => setEditing(newHome())}>Add home</button>
      </section>

      <section class="card">
        <h2>Walking and changes</h2>
        <NumberField
          label="Walking speed (km/h)"
          value={settings.walkSpeedMps !== undefined ? round1(settings.walkSpeedMps * 3.6) : undefined}
          placeholder={d ? String(round1(d.walk_speed_mps * 3.6)) : ''}
          step="0.1"
          onChange={(v) => setSettings({ ...settings, walkSpeedMps: v === undefined ? undefined : v / 3.6 })}
          hint="Used to estimate walks to and from stops you haven't timed."
        />
        <NumberField
          label="Change buffer at the same stop (min)"
          value={settings.minChangeS !== undefined ? settings.minChangeS / 60 : undefined}
          placeholder={d ? String(d.min_change_s / 60) : ''}
          step="0.5"
          onChange={(v) => setSettings({ ...settings, minChangeS: v === undefined ? undefined : Math.round(v * 60) })}
        />
        <NumberField
          label="Leave buffer (min)"
          value={settings.leaveBufferS !== undefined ? settings.leaveBufferS / 60 : undefined}
          placeholder="0"
          step="1"
          onChange={(v) => setSettings({ ...settings, leaveBufferS: v === undefined ? undefined : Math.round(v * 60) })}
          hint="Extra time to get out the door: shoes, keys."
        />
        <NumberField
          label="Longest walk to a stop (m)"
          value={settings.maxWalkM}
          placeholder={d ? String(d.max_walk_m) : ''}
          step="50"
          onChange={(v) => setSettings({ ...settings, maxWalkM: v })}
        />
      </section>

      <section class="card">
        <h2>Connection risk</h2>
        <p class="muted small">How much spare time a change needs to count as safe or tight. Below "tight" it's at risk.</p>
        <NumberField
          label="Safe from (min)"
          value={(settings.risk?.safe_s ?? d?.risk.safe_s ?? 180) / 60}
          step="0.5"
          onChange={(v) => {
            const safe = Math.round((v ?? 3) * 60)
            const tight = Math.min(settings.risk?.tight_s ?? d?.risk.tight_s ?? 60, safe)
            setSettings({ ...settings, risk: { safe_s: safe, tight_s: tight } })
          }}
        />
        <NumberField
          label="Tight from (min)"
          value={(settings.risk?.tight_s ?? d?.risk.tight_s ?? 60) / 60}
          step="0.5"
          onChange={(v) => {
            const tight = Math.round((v ?? 1) * 60)
            const safe = Math.max(settings.risk?.safe_s ?? d?.risk.safe_s ?? 180, tight)
            setSettings({ ...settings, risk: { safe_s: safe, tight_s: tight } })
          }}
        />
        {settings.risk && (
          <button class="link" onClick={() => setSettings({ ...settings, risk: undefined })}>
            Reset to defaults
          </button>
        )}
      </section>

      <section class="card">
        <h2>My change times</h2>
        {settings.transfers.length === 0 ? (
          <p class="muted small">None yet. Tap "Set my time" on a change in a trip to add one.</p>
        ) : (
          <ul class="list">
            {settings.transfers.map((t) => (
              <li>
                <span>
                  {t.label} <span class="muted small">· {round1(t.secs / 60)} min</span>
                </span>
                <button
                  class="link danger"
                  onClick={() => setSettings({ ...settings, transfers: settings.transfers.filter((x) => x !== t) })}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <Backup settings={settings} setSettings={setSettings} />

      <section class="card">
        <h2>This device</h2>
        <button
          class="danger"
          onClick={() => {
            if (confirm('Remove all settings and access from this device?')) setSettings(emptySettings())
          }}
        >
          Reset this device
        </button>
      </section>
    </div>
  )
}

function stopCount(h: Home): string {
  const n = new Set(h.access.map((a) => a.name)).size // a station's platforms share one name
  return n === 0 ? 'nearby stops' : `${n} stop${n > 1 ? 's' : ''}`
}

function round1(v: number) {
  return Math.round(v * 10) / 10
}

function newHome(): Home {
  return { id: newId(), name: 'Home', lat: NaN, lon: NaN, access: [] }
}

function NumberField(props: {
  label: string
  value: number | undefined
  placeholder?: string
  step: string
  hint?: string
  onChange: (v: number | undefined) => void
}) {
  return (
    <label>
      {props.label}
      <input
        type="number"
        inputMode="decimal"
        min="0"
        step={props.step}
        value={props.value ?? ''}
        placeholder={props.placeholder}
        onChange={(e) => {
          const raw = (e.target as HTMLInputElement).value.trim()
          const v = raw === '' ? undefined : Number(raw)
          props.onChange(v !== undefined && Number.isFinite(v) && v >= 0 ? v : undefined)
        }}
      />
      {props.hint && <span class="muted small">{props.hint}</span>}
    </label>
  )
}

function HomeEditor({
  home, token, onAuthError, onSave, onCancel,
}: { home: Home; token: string; onAuthError: () => void; onSave: (h: Home) => void; onCancel: () => void }) {
  const [h, setH] = useState<Home>(home)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<GeocodeResult[]>([])
  const [stops, setStops] = useState<NearStop[] | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const hasLocation = Number.isFinite(h.lat) && Number.isFinite(h.lon)

  const fail = (e: unknown) => {
    setBusy('')
    if (e instanceof AuthError) onAuthError()
    else setError(e instanceof Error ? e.message : String(e))
  }

  const setLocation = async (lat: number, lon: number) => {
    setError('')
    setResults([])
    setH((cur) => ({ ...cur, lat, lon }))
    setBusy('Finding nearby stops…')
    try {
      const r = await api.stopsNear(token, lat, lon, 1500)
      setStops(r.stops)
      setBusy('')
    } catch (e) {
      fail(e)
    }
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
    if (!navigator.geolocation) return setError('Location is not available in this browser.')
    setBusy('Getting your location…')
    navigator.geolocation.getCurrentPosition(
      (p) => setLocation(p.coords.latitude, p.coords.longitude),
      (err) => fail(new Error(err.message || 'Location unavailable')),
      { enableHighAccuracy: true, timeout: 15000 },
    )
  }

  // One row per station (all its platforms) or standalone stop.
  const groups = useMemo(() => groupStops(stops ?? []), [stops])
  const [showAll, setShowAll] = useState(false)
  const selectedIds = useMemo(() => new Set(h.access.map((a) => a.stop)), [h.access])
  const groupWalk = (g: StopGroup) => h.access.find((a) => g.ids.includes(a.stop))?.walk_s
  const toggle = (g: StopGroup) => {
    const on = g.ids.some((id) => selectedIds.has(id))
    const rest = h.access.filter((a) => !g.ids.includes(a.stop))
    setH({ ...h, access: on ? rest : [...rest, ...g.ids.map((stop) => ({ stop, name: g.name, walk_s: g.walk_s }))] })
  }
  const setWalk = (g: StopGroup, mins: number) =>
    setH({ ...h, access: h.access.map((a) => (g.ids.includes(a.stop) ? { ...a, walk_s: Math.round(mins * 60) } : a)) })
  const visibleGroups = showAll ? groups : relevantGroups(groups)

  return (
    <div class="stack">
      <section class="card">
        <h2>{Number.isFinite(home.lat) ? 'Edit home' : 'Add home'}</h2>
        <label>
          Name
          <input value={h.name} maxLength={60} onInput={(e) => setH({ ...h, name: (e.target as HTMLInputElement).value })} />
        </label>
        <form onSubmit={search} class="row-form">
          <label>
            Address
            <input type="search" value={query} placeholder="Street address or place" onInput={(e) => setQuery((e.target as HTMLInputElement).value)} />
          </label>
          <button type="submit" disabled={query.trim().length < 3}>
            Search
          </button>
        </form>
        <p class="muted small">Searches go through your server to the Transport for NSW trip planner; nothing is stored.</p>
        {results.length > 0 && (
          <ul class="list pick">
            {results.map((r) => (
              <li>
                <button class="link" onClick={() => setLocation(r.lat, r.lon)}>
                  {r.name}
                </button>
              </li>
            ))}
          </ul>
        )}
        <button onClick={useMyLocation}>Use my current location</button>
        {hasLocation && <p class="muted small">Location set ({h.lat.toFixed(5)}, {h.lon.toFixed(5)}).</p>}
        {busy && <p class="muted small">{busy}</p>}
        {error && <p class="error">{error}</p>}
      </section>

      {stops && (
        <section class="card">
          <h2>Your stops</h2>
          <p class="muted small">
            Pick the stops you'd actually walk to and set your real walking time. If you pick none, every stop within your
            walking limit is considered.
          </p>
          {stops.length === 0 && <p class="muted">No stops on the gyms' lines within 1.5 km.</p>}
          <ul class="list stops">
            {visibleGroups.map((g) => {
              const walk = groupWalk(g)
              return (
                <li>
                  <label class="check">
                    <input type="checkbox" checked={walk !== undefined} onChange={() => toggle(g)} />
                    <span>
                      {g.name}
                      <span class="muted small"> · {g.lines.join(', ')}</span>
                    </span>
                  </label>
                  {walk !== undefined ? (
                    <label class="walk">
                      <input
                        type="number"
                        min="0"
                        max="60"
                        step="0.5"
                        value={walk / 60}
                        onChange={(e) => setWalk(g, Number((e.target as HTMLInputElement).value) || 0)}
                        aria-label={`Walking time to ${g.name} in minutes`}
                      />
                      min
                    </label>
                  ) : (
                    <span class="muted small">~{Math.round(g.walk_s / 60)} min</span>
                  )}
                </li>
              )
            })}
          </ul>
          {groups.length > visibleGroups.length && (
            <button class="link" onClick={() => setShowAll(true)}>
              Show {groups.length - visibleGroups.length} more
            </button>
          )}
        </section>
      )}

      <div class="actions">
        <button class="primary" disabled={!hasLocation || h.name.trim() === ''} onClick={() => onSave({ ...h, name: h.name.trim() })}>
          Save home
        </button>
        <button onClick={onCancel}>Cancel</button>
      </div>
    </div>
  )
}

function Backup({ settings, setSettings }: { settings: Settings; setSettings: (s: Settings) => void }) {
  const [showShare, setShowShare] = useState(false)
  const [msg, setMsg] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const [link, setLink] = useState('')
  useEffect(() => {
    let live = true
    settingsLink(window.location.origin, settings).then((l) => live && setLink(l))
    return () => {
      live = false
    }
  }, [settings])

  const download = () => {
    const blob = new Blob([JSON.stringify(settings, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'gymrouter-settings.json'
    a.click()
    setTimeout(() => URL.revokeObjectURL(a.href), 1000)
  }

  const importFile = async (f: File) => {
    try {
      const s = sanitize(JSON.parse(await f.text()))
      if (!s.token && !settings.token) throw new Error('The file has no access token.')
      if (!confirm('Replace the settings on this device with the backup?')) return
      setSettings({ ...s, token: s.token ?? settings.token })
      setMsg('Backup restored.')
    } catch (e) {
      setMsg(`Couldn't import: ${e instanceof Error ? e.message : e}`)
    }
  }

  return (
    <section class="card">
      <h2>Backup and other devices</h2>
      <p class="muted small">
        Backups and setup links include your access token and homes. Treat them like a password.
      </p>
      <div class="actions">
        <button onClick={() => setShowShare(!showShare)}>{showShare ? 'Hide setup link' : 'Set up another device'}</button>
        <button onClick={download}>Export backup</button>
        <button onClick={() => fileRef.current?.click()}>Import backup</button>
        <input
          ref={fileRef}
          type="file"
          accept="application/json,.json"
          hidden
          onChange={(e) => {
            const f = (e.target as HTMLInputElement).files?.[0]
            if (f) importFile(f)
            ;(e.target as HTMLInputElement).value = ''
          }}
        />
      </div>
      {msg && <p class="muted small">{msg}</p>}
      {showShare && (
        <div class="share">
          <p class="small">Scan with the other device, or copy the link to it privately.</p>
          {link && <QRCode text={link} />}
          <button
            onClick={() =>
              navigator.clipboard?.writeText(link).then(
                () => setMsg('Link copied.'),
                () => setMsg("Couldn't copy. Long-press the QR code area instead."),
              )
            }
          >
            Copy link
          </button>
        </div>
      )}
    </section>
  )
}

function QRCode({ text }: { text: string }) {
  const { size, data } = useMemo(() => encode(text, { ecc: 'L', border: 2 }), [text])
  const rects: preact.JSX.Element[] = []
  data.forEach((row, y) =>
    row.forEach((on, x) => {
      if (on) rects.push(<rect x={x} y={y} width="1.02" height="1.02" />)
    }),
  )
  return (
    <svg class="qr" viewBox={`0 0 ${size} ${size}`} role="img" aria-label="QR code for the setup link">
      <rect width={size} height={size} fill="#fff" />
      <g fill="#000">{rects}</g>
    </svg>
  )
}
