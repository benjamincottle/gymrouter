import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { encode } from 'uqr'
import { emptySettings, HIGHLIGHTS, MAX_GYMS, resetGymOrder, sanitize, settingsLink, type Gym, type Home, type Settings } from '../settings.ts'
import type { ComponentChildren } from 'preact'
import { PaceTest } from './pacetest.tsx'
import { DeleteButton, EditButton, HOLD, IconColour, IconDevices, IconGym, IconHome, IconPhone, IconRisk, IconTimer, IconWalk } from './icons.tsx'
import type { DefaultsResponse } from '../types.ts'
import { mmss } from '../walkmeasure.ts'
import { walkSecs } from '../walks.ts'
import { GymChooser, newGym, newHome, PlaceEditor, type EditorKind } from './places.tsx'
import { BrandLogo } from './brand.tsx'
import { Button, Callout, Confirm, Field, Row, Section, Segmented, TextButton } from './ui.tsx'

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
  const [deleting, setDeleting] = useState<string | null>(null) // the row asking to be deleted: "home:<id>", "gym:<id>", "walk:<index>"
  const [paceTest, setPaceTest] = useState(false)
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
      <Section title="Homes" icon={<IconHome />} intro="Stored only on this device.">
        <ul class="rows">
          {settings.homes.map((h) =>
            deleting === `home:${h.id}` ? (
              <Confirm
                as="li"
                question={`Delete ${h.name}?`}
                detail="Its stops and timed walks to them go with it."
                confirm="Delete"
                onKeep={() => setDeleting(null)}
                onConfirm={() => {
                  const homes = settings.homes.filter((x) => x.id !== h.id)
                  setSettings({ ...settings, homes, activeHome: homes[0]?.id })
                  setDeleting(null)
                }}
              />
            ) : (
              <Row main={h.name} meta={stopCount(h)}>
                <span class="row-actions">
                  <EditButton label={`Edit ${h.name}`} onClick={() => setEditing({ kind: 'home', place: { ...h, lines: [] }, isNew: false })} />
                  <DeleteButton label={`Delete ${h.name}`} onClick={() => setDeleting(`home:${h.id}`)} />
                </span>
              </Row>
            ),
          )}
        </ul>
        <Button onClick={() => setEditing(editNew('home'))}>Add home</Button>
      </Section>

      <Section title="Gyms" icon={<IconGym />} intro="Stored only on this device, with the lines used to get to each one.">
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
                main={g.name}
                meta={`${g.lines.length} line${g.lines.length === 1 ? '' : 's'}${!g.homeId && home ? `, none near ${home.name} yet` : ''}`}
              >
                <span class="row-actions">
                  <EditButton label={`Edit ${g.name}`} onClick={() => setEditing({ kind: 'gym', place: g, isNew: false })} />
                  <DeleteButton label={`Delete ${g.name}`} onClick={() => setDeleting(`gym:${g.id}`)} />
                </span>
              </Row>
            ),
          )}
        </ul>
        {settings.gyms.length > 1 && (
          <p class="meta">
            Trips put your most used gyms first.{' '}
            {settings.gyms.some((g) => g.uses) && (
              <TextButton quiet onClick={() => setSettings(resetGymOrder(settings))}>
                Reset the order
              </TextButton>
            )}
          </p>
        )}
        <div class="actions section-actions">
          <Button disabled={settings.gyms.length >= MAX_GYMS} onClick={() => setEditing({ kind: 'choose-gyms' })}>
            Add gym
          </Button>
        </div>
      </Section>

      <Section title="Walking and changes" icon={<IconWalk />}>
        <NumberField
          label="Walking speed (km/h)"
          value={settings.walkSpeedMps !== undefined ? round1(settings.walkSpeedMps * 3.6) : undefined}
          placeholder={d ? String(round1(d.walk_speed_mps * 3.6)) : ''}
          step="0.1"
          onChange={(v) => setSettings({ ...settings, walkSpeedMps: v === undefined ? undefined : v / 3.6 })}
          hint={
            <>
              Used for walks you haven't timed.{' '}
              {!paceTest && (
                <TextButton quiet onClick={() => setPaceTest(true)}>
                  Measure my pace
                </TextButton>
              )}
            </>
          }
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
      </Section>

      <Section title="Connection risk" icon={<IconRisk />} intro={'How much spare time a change needs to count as safe or tight. Below "tight" it\'s at risk.'}>
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
          <TextButton onClick={() => setSettings({ ...settings, risk: undefined })}>
            Reset to defaults
          </TextButton>
        )}
      </Section>

      <Section
        title="Timed walks"
        icon={<IconTimer />}
        intro={'Walks and changes you\'ve timed during trips (tap "Time my walk" while travelling). They\'re used whenever the same walk comes up, either way round, and the route you walked is drawn on the map.'}
      >
        {settings.walks.length === 0 ? (
          <p class="meta">None yet.</p>
        ) : (
          <ul class="rows">
            {settings.walks.map((w, i) =>
              deleting === `walk:${i}` ? (
                <Confirm
                  as="li"
                  question={`Delete ${w.label}?`}
                  detail="The street map's estimate is used again."
                  confirm="Delete"
                  onKeep={() => setDeleting(null)}
                  onConfirm={() => {
                    setSettings({ ...settings, walks: settings.walks.filter((x) => x !== w) })
                    setDeleting(null)
                  }}
                />
              ) : (
                <Row
                  main={w.label}
                  meta={`${mmss(walkSecs(w))}${w.times.length > 1 ? `, average of ${w.times.length}` : ''}${w.trace ? ', traced' : ''}`}
                >
                  <span class="row-actions">
                    <DeleteButton label={`Delete the timed walk ${w.label}`} onClick={() => setDeleting(`walk:${i}`)} />
                  </span>
                </Row>
              ),
            )}
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
      </Section>

      <Section title="Appearance" icon={<IconColour />}>
        <Segmented
          label="Light or dark"
          options={[[undefined, 'Auto'], ['light', 'Light'], ['dark', 'Dark']] as const}
          value={settings.theme}
          onChange={(t) => setSettings({ ...settings, theme: t })}
        />
        <h3>Highlight colour</h3>
        <p class="meta">For underlines and what's selected. The grade colours at 9 Degrees.</p>
        <ul class="swatches" role="radiogroup" aria-label="Highlight colour">
          {HIGHLIGHTS.map((c) => {
            const on = (settings.highlight ?? 'black') === c
            return (
              <li>
                <button
                  class="swatch"
                  data-hl={c}
                  role="radio"
                  aria-checked={on}
                  onClick={() => setSettings({ ...settings, highlight: c === 'black' ? undefined : c })}
                >
                  <svg viewBox="3 2.5 18 15" aria-hidden="true">
                    <path class="hold" d={HOLD} />
                    <circle class="bolt" cx="12" cy="10.5" r="1.5" />
                  </svg>
                  {c[0].toUpperCase() + c.slice(1)}
                </button>
              </li>
            )
          })}
        </ul>
      </Section>

      <Backup settings={settings} setSettings={setSettings} />

      <Section title="This device" icon={<IconPhone />} intro="Removes this device's access, homes, gyms and timed walks. Backups and other devices keep theirs.">
        <Button variant="danger"
         
          onClick={() => {
            if (confirm('Remove all settings and access from this device?')) setSettings(emptySettings())
          }}
        >
          Reset this device
        </Button>
      </Section>
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
  hint?: ComponentChildren
  onChange: (v: number | undefined) => void
}) {
  return (
    <Field label={props.label} hint={props.hint}>
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
    </Field>
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
    <Section title="Backup and other devices" icon={<IconDevices />} intro="Backups and setup links include your access token and homes. Treat them like a password.">
      <div class="actions">
        <Button onClick={() => setShowShare(!showShare)}>{showShare ? 'Hide setup link' : 'Set up another device'}</Button>
        <Button onClick={download}>Export backup</Button>
        <Button onClick={() => fileRef.current?.click()}>Import backup</Button>
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
      {msg && <p class="meta">{msg}</p>}
      {showShare && (
        <div class="share">
          <p>Scan with the other device, or copy the link to it privately.</p>
          {link && <QRCode text={link} />}
          <Button
            onClick={() =>
              navigator.clipboard?.writeText(link).then(
                () => setMsg('Link copied.'),
                () => setMsg("Couldn't copy. Long-press the QR code area instead."),
              )
            }
          >
            Copy link
          </Button>
        </div>
      )}
    </Section>
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
