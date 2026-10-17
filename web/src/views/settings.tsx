import { useEffect, useMemo, useRef, useState, type MutableRef } from 'preact/hooks'
import { encode } from 'uqr'
import { dismissedWalks, emptySettings, MAX_GYMS, sanitize, settingsLink, showDismissedWalks, type Gym, type Home, type Settings } from '../settings.ts'
import type { ComponentChildren } from 'preact'
import { PaceTest } from './pacetest.tsx'
import { DeleteButton, EditButton, IconColour, IconDevices, IconGym, IconHome, IconPhone, IconRisk, IconWalk } from './icons.tsx'
import type { DefaultsResponse } from '../types.ts'
import { GymChooser, newGym, newHome, PlaceEditor, type EditorKind } from './places.tsx'
import { BrandLogo } from './brand.tsx'
import { Button, Callout, Confirm, ConfirmSheet, Row, RowAction, Section, Segmented, TextButton, type Tone } from './ui.tsx'

interface Props {
  backRef: MutableRef<(() => boolean) | null> // the header's Back: closes an open editor (true), else does nothing
  settings: Settings
  setSettings: (s: Settings) => void
  server: DefaultsResponse | null
  onAuthError: () => void
  onSetUp: () => void // the first gyms were added: setting up is done
}

type Editing =
  | { kind: EditorKind; place: Gym; isNew: boolean }
  | { kind: 'choose-gyms' }

const editNew = (kind: EditorKind): Editing => ({ kind, place: kind === 'home' ? { ...newHome(), lines: [] } : newGym(), isNew: true })

export function SettingsView({ backRef, settings, setSettings, server, onAuthError, onSetUp }: Props) {
  // First run: add a home, then choose gyms. Two steps, and nothing to cancel back to.
  const [setup] = useState(settings.homes.length === 0)
  const [editing, setEditing] = useState<Editing | null>(
    settings.homes.length === 0 ? editNew('home') : settings.gyms.length === 0 ? { kind: 'choose-gyms' } : null,
  )
  const [warning, setWarning] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null) // the row asking to be deleted: "home:<id>", "gym:<id>"
  const [paceTest, setPaceTest] = useState(false)
  const [resetting, setResetting] = useState(false)
  const d = server?.defaults
  backRef.current = editing
    ? () => {
        setEditing(null)
        return true
      }
    : null
  const dismissed = dismissedWalks(settings).length // "Longer walk" notices
  const home = settings.homes.find((h) => h.id === settings.activeHome) ?? settings.homes[0]
  // Connection risk: one threshold changed (undefined: back to its default). Safe can't be below tight, so the other
  // follows; when both are the defaults, the setting goes.
  const setRisk = (which: 'safe' | 'tight', v: number | undefined) => {
    const defSafe = d?.risk.safe_s ?? 180
    const defTight = d?.risk.tight_s ?? 60
    let { safe_s: safe, tight_s: tight } = settings.risk ?? { safe_s: defSafe, tight_s: defTight }
    if (which === 'safe') {
      safe = v ?? defSafe
      tight = Math.min(tight, safe)
    } else {
      tight = v ?? defTight
      safe = Math.max(safe, tight)
    }
    setSettings({ ...settings, risk: safe === defSafe && tight === defTight ? undefined : { safe_s: safe, tight_s: tight } })
  }

  if (editing?.kind === 'choose-gyms') {
    return (
      <div class="stack settings">
        <GymChooser
          known={server?.gyms ?? null}
          have={settings.gyms}
          firstRun={setup && settings.gyms.length === 0}
          home={home}
          token={settings.token!}
          maxWalkM={settings.maxWalkM}
          onAuthError={onAuthError}
          onCancel={settings.gyms.length > 0 ? () => setEditing(null) : undefined}
          onCustom={() => setEditing(editNew('gym'))}
          onAdd={(gyms, warn) => {
            setSettings({ ...settings, gyms: [...settings.gyms, ...gyms] })
            setWarning(warn ?? '')
            setEditing(null)
            if (settings.gyms.length === 0 && !warn) onSetUp()
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
        maxWalkM={settings.maxWalkM}
        walkLimitM={settings.maxWalkM ?? d?.max_walk_m}
        firstRun={setup && editing.kind === 'home' && settings.homes.length === 0}
        onAuthError={onAuthError}
        onCancel={() => setEditing(null)}
        onSave={(g) => {
          if (editing.kind === 'home') {
            const h: Home = { id: g.id, name: g.name, address: g.address, lat: g.lat, lon: g.lon, access: g.access }
            const homes = settings.homes.some((x) => x.id === h.id) ? settings.homes.map((x) => (x.id === h.id ? h : x)) : [...settings.homes, h]
            setSettings({ ...settings, homes, activeHome: settings.activeHome ?? h.id })
            setEditing(settings.gyms.length === 0 ? { kind: 'choose-gyms' } : null)
          } else {
            const gyms = settings.gyms.some((x) => x.id === g.id) ? settings.gyms.map((x) => (x.id === g.id ? g : x)) : [...settings.gyms, g]
            setSettings({ ...settings, gyms })
            setEditing(null)
            if (settings.gyms.length === 0) onSetUp()
          }
        }}
      />
    )
  }

  return (
    // Settings' sections start with their titles and the space between them, no rules.
    <div class="stack settings unruled">
      {/* Where everything on this screen is kept, said once, before any of it. */}
      <p class="meta">All settings are stored on this device only.</p>
      <Section title="Homes" icon={<IconHome />}>
        <ul class="rows">
          {settings.homes.map((h) =>
            deleting === `home:${h.id}` ? (
              <Confirm
                as="li"
                question={`Delete ${h.name}?`}
                detail="Its stops go with it."
                confirm="Delete"
                onKeep={() => setDeleting(null)}
                onConfirm={() => {
                  const homes = settings.homes.filter((x) => x.id !== h.id)
                  setSettings({ ...settings, homes, activeHome: homes[0]?.id })
                  setDeleting(null)
                }}
              />
            ) : (
              <Row main={<span class="place-name">{h.name}</span>} meta={stopCount(h)}>
                <span class="row-actions">
                  <EditButton label={`Edit ${h.name}`} onClick={() => setEditing({ kind: 'home', place: { ...h, lines: [] }, isNew: false })} />
                  <DeleteButton label={`Delete ${h.name}`} onClick={() => setDeleting(`home:${h.id}`)} />
                </span>
              </Row>
            ),
          )}
          <li class="action">
            <RowAction kind="add" onClick={() => setEditing(editNew('home'))}>
              Add home
            </RowAction>
          </li>
        </ul>
      </Section>

      <Section title="Gyms" icon={<IconGym />}>
        {warning && (
          <Callout tone="caution" role="status" onDismiss={() => setWarning('')}>
            {warning}
          </Callout>
        )}
        <ul class="rows">
          {settings.gyms.map((g) =>
            deleting === `gym:${g.id}` ? (
              <Confirm
                as="li"
                question={`Delete ${g.name}?`}
                detail={`Its ${g.lines.length} line${g.lines.length === 1 ? '' : 's'} go with it.`}
                confirm="Delete"
                onKeep={() => setDeleting(null)}
                onConfirm={() => {
                  setSettings({ ...settings, gyms: settings.gyms.filter((x) => x.id !== g.id) })
                  setDeleting(null)
                }}
              />
            ) : (
              <Row
                lead={<BrandLogo brand={server?.gyms.find((k) => k.id === g.ref)?.brand} />}
                main={<span class="place-name">{g.name}</span>}
                meta={`${g.lines.length} line${g.lines.length === 1 ? '' : 's'}${!g.homeId && home ? `, none from ${home.name} yet` : ''}`}
              >
                <span class="row-actions">
                  <EditButton label={`Edit ${g.name}`} onClick={() => setEditing({ kind: 'gym', place: g, isNew: false })} />
                  <DeleteButton label={`Delete ${g.name}`} onClick={() => setDeleting(`gym:${g.id}`)} />
                </span>
              </Row>
            ),
          )}
          <li class="action">
            <RowAction kind="add" disabled={settings.gyms.length >= MAX_GYMS} onClick={() => setEditing({ kind: 'choose-gyms' })}>
              Add gym
            </RowAction>
          </li>
        </ul>
      </Section>

      <Section title="Walking and changes" icon={<IconWalk />}>
        <div class="setting-rows">
          <NumberField
            label="Walking speed"
            unit="km/h"
            value={settings.walkSpeedMps !== undefined ? round1(settings.walkSpeedMps * 3.6) : undefined}
            fallback={d && round1(d.walk_speed_mps * 3.6)}
            step="0.1"
            extra={
              !paceTest && (
                <TextButton quiet onClick={() => setPaceTest(true)}>
                  Measure my pace
                </TextButton>
              )
            }
            onChange={(v) => setSettings({ ...settings, walkSpeedMps: v === undefined ? undefined : v / 3.6 })}
          />
          {paceTest && (
            <PaceTest
              onClose={() => setPaceTest(false)}
              onUse={(mps) => {
                setSettings({ ...settings, walkSpeedMps: Math.round(mps * 100) / 100 })
                setPaceTest(false)
              }}
            />
          )}
          <NumberField
            label="Change buffer at the same stop"
            unit="min"
            value={settings.minChangeS !== undefined ? settings.minChangeS / 60 : undefined}
            fallback={d && d.min_change_s / 60}
            step="0.5"
            onChange={(v) => setSettings({ ...settings, minChangeS: v === undefined ? undefined : Math.round(v * 60) })}
          />
          <NumberField
            label="Leave buffer"
            unit="min"
            value={settings.leaveBufferS !== undefined ? settings.leaveBufferS / 60 : undefined}
            fallback={0}
            step="1"
            about="Extra time to get out the door: shoes, keys."
            onChange={(v) => setSettings({ ...settings, leaveBufferS: v === undefined ? undefined : Math.round(v * 60) })}
          />
          <NumberField
            label="Longest walk to a stop"
            unit="m"
            value={settings.maxWalkM}
            fallback={d?.max_walk_m}
            step="50"
            onChange={(v) => setSettings({ ...settings, maxWalkM: v })}
          />
        </div>
        {dismissed > 0 && (
          <p class="meta">
            You've dismissed {dismissed} "Longer walk" notice{dismissed === 1 ? '' : 's'}.{' '}
            <TextButton quiet onClick={() => setSettings(showDismissedWalks(settings))}>
              Show {dismissed === 1 ? 'it' : 'them'} again
            </TextButton>
          </p>
        )}
      </Section>

      <Section title="Connection risk" icon={<IconRisk />} intro={'How much spare time a change needs to count as safe or tight. Below "tight" it\'s at risk.'}>
        <div class="setting-rows">
          <NumberField
            label="Safe from"
            unit="min"
            value={settings.risk && settings.risk.safe_s !== (d?.risk.safe_s ?? 180) ? settings.risk.safe_s / 60 : undefined}
            fallback={d && d.risk.safe_s / 60}
            step="0.5"
            onChange={(v) => setRisk('safe', v === undefined ? undefined : Math.round(v * 60))}
          />
          <NumberField
            label="Tight from"
            unit="min"
            value={settings.risk && settings.risk.tight_s !== (d?.risk.tight_s ?? 60) ? settings.risk.tight_s / 60 : undefined}
            fallback={d && d.risk.tight_s / 60}
            step="0.5"
            onChange={(v) => setRisk('tight', v === undefined ? undefined : Math.round(v * 60))}
          />
        </div>
      </Section>

      <Section title="Appearance" icon={<IconColour />}>
        <Segmented
          label="Light or dark"
          options={[[undefined, 'Auto'], ['light', 'Light'], ['dark', 'Dark']] as const}
          value={settings.theme}
          onChange={(t) => setSettings({ ...settings, theme: t })}
        />
      </Section>

      <Backup settings={settings} setSettings={setSettings} />

      <Section title="This device" icon={<IconPhone />} intro="Removes this device's access, homes and gyms. Backups and other devices keep theirs.">
        <Button variant="danger" onClick={() => setResetting(true)}>
          Reset this device
        </Button>
        {resetting && (
          <ConfirmSheet title="Reset this device?" confirm="Reset this device" onCancel={() => setResetting(false)} onConfirm={() => setSettings(emptySettings())}>
            <p>
              This removes the access token, {counts(settings)} from this device. Backups and other devices keep theirs.
            </p>
            <p>To use the app here again you'll need a setup link.</p>
          </ConfirmSheet>
        )}
      </Section>
      <p class="credits">Contains Transport for NSW data (CC BY 4.0). Map data © OpenStreetMap contributors. Basemap by Protomaps.</p>
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

/**
 * A number setting, on one line: its name, then a short field. Empty means "use the default", which the field shows as
 * its placeholder. A line under the name only when there's something to say: what it's for, the way back to the
 * default once it's changed, and that a change was saved.
 */
function NumberField({ label, unit, value, fallback, step, about, extra, onChange }: {
  label: string
  unit: string // in the field, after the number: "km/h", "min", "m"
  value: number | undefined // undefined: the default
  fallback: number | undefined // the default, once the server has said
  step: string
  about?: string
  extra?: ComponentChildren
  onChange: (v: number | undefined) => void
}) {
  const [saved, setSaved] = useState(false)
  const timer = useRef<number>()
  useEffect(() => () => clearTimeout(timer.current), [])
  const change = (v: number | undefined) => {
    onChange(v)
    setSaved(true)
    clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setSaved(false), 2000)
  }
  const def = fallback === undefined ? '' : String(fallback)
  return (
    <label class="setting">
      <span class="main">
        {label}
        <span class="sr-only"> ({unit})</span>
        <span class="meta" aria-live="polite">
          {about && <span>{about}</span>}
          {value !== undefined && def && (
            <span>
              <TextButton quiet onClick={() => change(undefined)}>
                Use the default ({def})
              </TextButton>
            </span>
          )}
          {extra && <span>{extra}</span>}
          {saved && <strong class="saved">Saved.</strong>}
        </span>
      </span>
      <span class="unit-field">
        <input
          type="number"
          inputMode="decimal"
          min="0"
          step={step}
          value={value ?? ''}
          placeholder={def}
          onChange={(e) => {
            const raw = (e.target as HTMLInputElement).value.trim()
            const v = raw === '' ? undefined : Number(raw)
            change(v !== undefined && Number.isFinite(v) && v >= 0 ? v : undefined)
          }}
        />
        <span class="unit" aria-hidden="true">{unit}</span>
      </span>
    </label>
  )
}

function Backup({ settings, setSettings }: { settings: Settings; setSettings: (s: Settings) => void }) {
  const [showShare, setShowShare] = useState(false)
  const [msg, setMsg] = useState<{ tone: Tone; text: string } | null>(null)
  const [incoming, setIncoming] = useState<Settings | null>(null) // a backup read from a file, waiting for a yes
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

  const readFile = async (f: File) => {
    setMsg(null)
    try {
      const s = sanitize(JSON.parse(await f.text()))
      if (!s.token && !settings.token) {
        setMsg({ tone: 'bad', text: "Couldn't import the backup: it has no access token." })
        return
      }
      setIncoming(s)
    } catch {
      setMsg({ tone: 'bad', text: "Couldn't import the backup: the file isn't a Gym Router backup." })
    }
  }

  return (
    <Section title="Backup and other devices" icon={<IconDevices />} intro="Backups include your settings and access token. Treat them like a password.">
      <ul class="rows">
        <li class="action">
          <RowAction kind="open" open={showShare} onClick={() => setShowShare(!showShare)}>
            Set up another device
          </RowAction>
        </li>
        {showShare && (
          <li class="share">
            <p>Scan with the other device, or copy the link to it privately.</p>
            {link && <QRCode text={link} />}
            <Button
              onClick={() =>
                navigator.clipboard?.writeText(link).then(
                  () => setMsg({ tone: 'neutral', text: 'Link copied.' }),
                  () => setMsg({ tone: 'caution', text: "Couldn't copy the link. Scan the QR code instead." }),
                )
              }
            >
              Copy link
            </Button>
          </li>
        )}
        <li class="action">
          <RowAction onClick={download}>Export backup</RowAction>
        </li>
        <li class="action">
          <RowAction onClick={() => fileRef.current?.click()}>Import backup</RowAction>
        </li>
      </ul>
      <input
        ref={fileRef}
        type="file"
        accept="application/json,.json"
        hidden
        onChange={(e) => {
          const f = (e.target as HTMLInputElement).files?.[0]
          if (f) readFile(f)
          ;(e.target as HTMLInputElement).value = ''
        }}
      />
      {msg && (
        <Callout tone={msg.tone} role={msg.tone === 'bad' ? 'alert' : 'status'} onDismiss={() => setMsg(null)} class="backup-msg">
          {msg.text}
        </Callout>
      )}
      {incoming && (
        <ConfirmSheet
          title="Replace this device's settings?"
          confirm="Replace"
          onCancel={() => setIncoming(null)}
          onConfirm={() => {
            setSettings({ ...incoming, token: incoming.token ?? settings.token })
            setIncoming(null)
            setMsg({ tone: 'neutral', text: 'Backup restored.' })
          }}
        >
          <p>
            The backup has {counts(incoming) || 'nothing saved'}. It replaces {counts(settings) ? `the ${counts(settings)}` : 'everything'} on this device
            {incoming.token && settings.token && incoming.token !== settings.token ? ', and its access token' : ''}.
          </p>
        </ConfirmSheet>
      )}
    </Section>
  )
}

/** "1 home and 3 gyms" */
function counts(s: Settings): string {
  const n = (k: number, one: string) => (k === 0 ? '' : `${k} ${one}${k === 1 ? '' : 's'}`)
  const parts = [n(s.homes.length, 'home'), n(s.gyms.length, 'gym')].filter(Boolean)
  return parts.length > 1 ? `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}` : parts.join('')
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
