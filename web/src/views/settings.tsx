import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { encode } from 'uqr'
import { dismissedWalks, emptySettings, MAX_GYMS, resetGymOrder, sanitize, settingsLink, showDismissedWalks, type Gym, type Home, type Settings } from '../settings.ts'
import type { ComponentChildren } from 'preact'
import { PaceTest } from './pacetest.tsx'
import { DeleteButton, EditButton, IconColour, IconDevices, IconGym, IconHome, IconPhone, IconRisk, IconTimer, IconWalk } from './icons.tsx'
import type { DefaultsResponse } from '../types.ts'
import { mmss } from '../walkmeasure.ts'
import { learnedPace, walkSecs } from '../walks.ts'
import { GymChooser, newGym, newHome, PlaceEditor, type EditorKind } from './places.tsx'
import { BrandLogo } from './brand.tsx'
import { Button, Callout, Confirm, ConfirmSheet, Field, Row, Section, Segmented, TextButton, type Tone } from './ui.tsx'

interface Props {
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

export function SettingsView({ settings, setSettings, server, onAuthError, onSetUp }: Props) {
  // First run: add a home, then choose gyms.
  const [editing, setEditing] = useState<Editing | null>(
    settings.homes.length === 0 ? editNew('home') : settings.gyms.length === 0 ? { kind: 'choose-gyms' } : null,
  )
  const [warning, setWarning] = useState('')
  const [deleting, setDeleting] = useState<string | null>(null) // the row asking to be deleted: "home:<id>", "gym:<id>", "walk:<index>"
  const [paceTest, setPaceTest] = useState(false)
  const [resetting, setResetting] = useState(false)
  const d = server?.defaults
  const learned = learnedPace(settings.walks) // from timed, traced walks; a speed typed in below wins
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
        walks={settings.walks}
        maxWalkM={settings.maxWalkM}
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
            if (settings.gyms.length === 0) onSetUp()
          }
        }}
      />
    )
  }

  return (
    <div class="stack settings">
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
          fallback={learned !== null ? round1(learned * 3.6) : d && round1(d.walk_speed_mps * 3.6)}
          fallbackIs={learned !== null ? 'your pace on timed walks' : undefined}
          step="0.1"
          about="Used for walks you haven't timed."
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
          label="Change buffer at the same stop (min)"
          value={settings.minChangeS !== undefined ? settings.minChangeS / 60 : undefined}
          fallback={d && d.min_change_s / 60}
          step="0.5"
          onChange={(v) => setSettings({ ...settings, minChangeS: v === undefined ? undefined : Math.round(v * 60) })}
        />
        <NumberField
          label="Leave buffer (min)"
          value={settings.leaveBufferS !== undefined ? settings.leaveBufferS / 60 : undefined}
          fallback={0}
          step="1"
          about="Extra time to get out the door: shoes, keys."
          onChange={(v) => setSettings({ ...settings, leaveBufferS: v === undefined ? undefined : Math.round(v * 60) })}
        />
        <NumberField
          label="Longest walk to a stop (m)"
          value={settings.maxWalkM}
          fallback={d?.max_walk_m}
          step="50"
          onChange={(v) => setSettings({ ...settings, maxWalkM: v })}
        />
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
        <NumberField
          label="Safe from (min)"
          value={settings.risk && settings.risk.safe_s !== (d?.risk.safe_s ?? 180) ? settings.risk.safe_s / 60 : undefined}
          fallback={d && d.risk.safe_s / 60}
          step="0.5"
          onChange={(v) => setRisk('safe', v === undefined ? undefined : Math.round(v * 60))}
        />
        <NumberField
          label="Tight from (min)"
          value={settings.risk && settings.risk.tight_s !== (d?.risk.tight_s ?? 60) ? settings.risk.tight_s / 60 : undefined}
          fallback={d && d.risk.tight_s / 60}
          step="0.5"
          onChange={(v) => setRisk('tight', v === undefined ? undefined : Math.round(v * 60))}
        />
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
      </Section>

      <Backup settings={settings} setSettings={setSettings} />

      <Section title="This device" icon={<IconPhone />} intro="Removes this device's access, homes, gyms and timed walks. Backups and other devices keep theirs.">
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
      <p class="credits">Contains Transport for NSW data (CC BY 4.0). Map data © OpenStreetMap contributors.</p>
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
 * A number setting. Empty means "use the default", and the hint says what that is; a changed value offers the default
 * back. Changes save as you go, and say so briefly.
 */
function NumberField({ label, value, fallback, fallbackIs = 'the default', step, about, extra, onChange }: {
  label: string
  value: number | undefined // undefined: the default
  fallback: number | undefined // the default, once the server has said
  fallbackIs?: string // what an empty field uses, when it isn't the server's default
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
    <Field
      label={label}
      hint={
        <span aria-live="polite">
          {about && `${about} `}
          {value === undefined ? (
            def && `Empty uses ${fallbackIs}, ${def}. `
          ) : (
            <>
              Your setting.{' '}
              {def && (
                <TextButton quiet onClick={() => change(undefined)}>
                  Use {fallbackIs} ({def})
                </TextButton>
              )}{' '}
            </>
          )}
          {extra}
          {saved && <strong class="saved"> Saved.</strong>}
        </span>
      }
    >
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
    </Field>
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
    <Section title="Backup and other devices" icon={<IconDevices />} intro="Backups and setup links include your access token and homes. Treat them like a password.">
      <div class="actions">
        <Button aria-expanded={showShare} onClick={() => setShowShare(!showShare)}>
          Set up another device
        </Button>
        <Button onClick={download}>Export backup</Button>
        <Button onClick={() => fileRef.current?.click()}>Import backup</Button>
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
      </div>
      {msg && (
        <Callout tone={msg.tone} role={msg.tone === 'bad' ? 'alert' : 'status'} onDismiss={() => setMsg(null)} class="backup-msg">
          {msg.text}
        </Callout>
      )}
      {showShare && (
        <div class="share">
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
        </div>
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
            The backup has {counts(incoming) || 'nothing saved'}. It replaces {counts(settings) ? `the ${counts(settings)}` : 'everything'} on this device.
          </p>
        </ConfirmSheet>
      )}
    </Section>
  )
}

/** "1 home, 3 gyms and 4 timed walks" */
function counts(s: Settings): string {
  const n = (k: number, one: string) => (k === 0 ? '' : `${k} ${one}${k === 1 ? '' : 's'}`)
  const parts = [n(s.homes.length, 'home'), n(s.gyms.length, 'gym'), n(s.walks.length, 'timed walk')].filter(Boolean)
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
