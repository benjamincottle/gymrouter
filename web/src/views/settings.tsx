import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { encode } from 'uqr'
import { emptySettings, MAX_GYMS, sanitize, settingsLink, type Gym, type Home, type Settings } from '../settings.ts'
import type { DefaultsResponse } from '../types.ts'
import { mmss } from '../walkmeasure.ts'
import { walkSecs } from '../walks.ts'
import { GymChooser, newGym, newHome, PlaceEditor, type EditorKind } from './places.tsx'

interface Props {
  settings: Settings
  setSettings: (s: Settings) => void
  server: DefaultsResponse | null
  onAuthError: () => void
}

type Editing =
  | { kind: EditorKind; place: Gym; isNew: boolean }
  | { kind: 'choose-gyms' }

const editNew = (kind: EditorKind): Editing => ({ kind, place: kind === 'home' ? { ...newHome(), lines: [] } : newGym(), isNew: true })

export function SettingsView({ settings, setSettings, server, onAuthError }: Props) {
  // First run: add a home, then choose gyms.
  const [editing, setEditing] = useState<Editing | null>(
    settings.homes.length === 0 ? editNew('home') : settings.gyms.length === 0 ? { kind: 'choose-gyms' } : null,
  )
  const [warning, setWarning] = useState('')
  const d = server?.defaults
  const home = settings.homes.find((h) => h.id === settings.activeHome) ?? settings.homes[0]

  if (editing?.kind === 'choose-gyms') {
    return (
      <div class="stack">
        <GymChooser
          known={server?.gyms ?? null}
          have={settings.gyms}
          home={home}
          token={settings.token!}
          onAuthError={onAuthError}
          onCancel={settings.gyms.length > 0 ? () => setEditing(null) : undefined}
          onCustom={() => setEditing(editNew('gym'))}
          onAdd={(gyms, warn) => {
            setSettings({ ...settings, gyms: [...settings.gyms, ...gyms] })
            setWarning(warn ?? '')
            setEditing(null)
          }}
        />
      </div>
    )
  }

  if (editing) {
    return (
      <PlaceEditor
        key={editing.place.id}
        kind={editing.kind}
        place={editing.place}
        isNew={editing.isNew}
        token={settings.token!}
        homes={settings.homes}
        walks={settings.walks}
        onAuthError={onAuthError}
        onCancel={() => setEditing(null)}
        onSave={(g) => {
          if (editing.kind === 'home') {
            const h: Home = { id: g.id, name: g.name, lat: g.lat, lon: g.lon, access: g.access }
            const homes = settings.homes.some((x) => x.id === h.id) ? settings.homes.map((x) => (x.id === h.id ? h : x)) : [...settings.homes, h]
            setSettings({ ...settings, homes, activeHome: settings.activeHome ?? h.id })
            setEditing(settings.gyms.length === 0 ? { kind: 'choose-gyms' } : null)
          } else {
            const gyms = settings.gyms.some((x) => x.id === g.id) ? settings.gyms.map((x) => (x.id === g.id ? g : x)) : [...settings.gyms, g]
            setSettings({ ...settings, gyms })
            setEditing(null)
          }
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
                <button class="link" onClick={() => setEditing({ kind: 'home', place: { ...h, lines: [] }, isNew: false })}>
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
        <button onClick={() => setEditing(editNew('home'))}>Add home</button>
      </section>

      <section class="card">
        <h2>Gyms</h2>
        <p class="muted small">Stored only on this device, with the lines used to get to each one.</p>
        {warning && (
          <p class="notice warn" role="status">
            {warning}
          </p>
        )}
        <ul class="list">
          {settings.gyms.map((g) => (
            <li>
              <span>
                {g.name}
                <span class="muted small">
                  {' '}
                  · {g.lines.length} line{g.lines.length === 1 ? '' : 's'}
                  {!g.homeId && home ? ` · none near ${home.name} yet` : ''}
                </span>
              </span>
              <span>
                <button class="link" onClick={() => setEditing({ kind: 'gym', place: g, isNew: false })}>
                  {!g.homeId && home ? 'Add lines' : 'Edit'}
                </button>
                <button
                  class="link danger"
                  onClick={() => {
                    if (confirm(`Remove ${g.name}?`)) setSettings({ ...settings, gyms: settings.gyms.filter((x) => x.id !== g.id) })
                  }}
                >
                  Remove
                </button>
              </span>
            </li>
          ))}
        </ul>
        <div class="actions">
          <button disabled={settings.gyms.length >= MAX_GYMS} onClick={() => setEditing({ kind: 'choose-gyms' })}>
            Add gym
          </button>
        </div>
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
        <h2>Timed walks</h2>
        <p class="muted small">
          Walks and changes you've timed during trips (tap "Time this walk" while travelling). They're used whenever the same
          walk comes up, either way round, and the route you walked is drawn on the map.
        </p>
        {settings.walks.length === 0 ? (
          <p class="muted small">None yet.</p>
        ) : (
          <ul class="list">
            {settings.walks.map((w) => (
              <li>
                <span>
                  {w.label}{' '}
                  <span class="muted small">
                    · {mmss(walkSecs(w))}
                    {w.times.length > 1 ? `, average of ${w.times.length}` : ''}
                    {w.trace ? ', traced' : ''}
                  </span>
                </span>
                <button
                  class="link danger"
                  onClick={() => setSettings({ ...settings, walks: settings.walks.filter((x) => x !== w) })}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
        )}
        <fieldset class="choice">
          <legend>Timing a walk again</legend>
          <label class="check">
            <input
              type="radio"
              name="retime"
              checked={(settings.retime ?? 'average') === 'average'}
              onChange={() => setSettings({ ...settings, retime: 'average' })}
            />
            <span>Average it with the last few walks</span>
          </label>
          <label class="check">
            <input
              type="radio"
              name="retime"
              checked={settings.retime === 'replace'}
              onChange={() => setSettings({ ...settings, retime: 'replace' })}
            />
            <span>Replace the earlier time</span>
          </label>
        </fieldset>
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
