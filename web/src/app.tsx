import { useCallback, useEffect, useState } from 'preact/hooks'
import { api, AuthError, problem } from './api.ts'
import { applyFragment, importQuestion, importTitle, save, type FragmentData, type Settings } from './settings.ts'
import type { DefaultsResponse } from './types.ts'
import { Landing } from './views/landing.tsx'
import { Trip } from './views/trip.tsx'
import { SettingsView } from './views/settings.tsx'
import { IconSettings } from './views/icons.tsx'
import { InTrip, TRIP_KEY, type ActiveTrip } from './views/intrip.tsx'
import { record } from './walks.ts'
import { applyTheme } from './theme.ts'
import { Button, Callout, ConfirmSheet, IconButton } from './views/ui.tsx'

function loadTrip(storage: Storage | undefined): ActiveTrip | null {
  try {
    const raw = storage?.getItem(TRIP_KEY)
    if (!raw) return null
    const t = JSON.parse(raw) as ActiveTrip
    // Drop trips that ended more than an hour ago.
    return t?.option?.arrive && t.ends && Date.parse(t.option.arrive) > Date.now() - 3600_000 ? t : null
  } catch {
    return null
  }
}

type View = 'trip' | 'settings'

export interface AppProps {
  initial: Settings
  imported: boolean
  pending: FragmentData | null // a link that would replace saved data, waiting for a yes
  storage: Storage | undefined
}

export function App({ initial, imported, pending: pendingLink, storage }: AppProps) {
  const [pending, setPending] = useState(pendingLink)
  const [serverError, setServerError] = useState('')
  const [retries, setRetries] = useState(0) // bumping this asks the server again
  const [settings, setSettingsState] = useState(initial)
  const [view, setView] = useState<View>(
    (initial.homes.length === 0 || initial.gyms.length === 0) && initial.token ? 'settings' : 'trip',
  )
  const [server, setServer] = useState<DefaultsResponse | null>(null)
  const [authFailed, setAuthFailed] = useState(false)
  const [notice, setNotice] = useState(imported ? 'Settings saved on this device.' : '')
  const [restarts, setRestarts] = useState(0) // bumping this starts the trip screen afresh
  const [storageOk, setStorageOk] = useState(true)
  const [trip, setTripState] = useState<ActiveTrip | null>(() => loadTrip(storage))
  const setTrip = useCallback(
    (t: ActiveTrip | null) => {
      setTripState(t)
      try {
        if (t) storage?.setItem(TRIP_KEY, JSON.stringify(t))
        else storage?.removeItem(TRIP_KEY)
      } catch {
        /* trip just won't survive a reload */
      }
    },
    [storage],
  )

  const setSettings = useCallback(
    (s: Settings) => {
      setSettingsState(s)
      setStorageOk(save(storage, s))
    },
    [storage],
  )

  const onAuthError = useCallback(() => setAuthFailed(true), [])

  useEffect(() => applyTheme(settings.theme), [settings.theme])

  // The highlight colour is a CSS token on the root (style.css maps each name to light and dark values).
  useEffect(() => {
    if (settings.highlight) document.documentElement.dataset.hl = settings.highlight
    else delete document.documentElement.dataset.hl
  }, [settings.highlight])

  useEffect(() => {
    if (!settings.token) return
    let live = true
    setServerError('')
    api
      .defaults(settings.token)
      .then((d) => {
        if (!live) return
        setServer(d)
        setAuthFailed(false)
      })
      .catch((e) => {
        if (e instanceof AuthError) setAuthFailed(true)
        else if (live) setServerError(problem(e))
      })
    return () => {
      live = false
    }
  }, [settings.token, retries])

  // As soon as the gyms' lines are known (opening the app, setup, a suggestion, an edit), have the server load them,
  // so the first search doesn't wait for it to read the timetable. Quick edits are sent together.
  const gymLines = [...new Set(settings.gyms.flatMap((g) => g.lines))].sort().join(',')
  useEffect(() => {
    const token = settings.token
    if (!token || !gymLines) return
    const id = setTimeout(() => api.loadLines(token, gymLines.split(',')).catch(() => {}), 300) // only a head start
    return () => clearTimeout(id)
  }, [settings.token, gymLines])

  const linkSheet = pending && (
    <ConfirmSheet
      title={importTitle(pending)}
      confirm={pending.kind === 'token' ? 'Use it' : 'Replace'}
      onCancel={() => setPending(null)}
      onConfirm={() => {
        setSettings(applyFragment(settings, pending))
        setPending(null)
        setNotice('Settings saved on this device.')
      }}
    >
      <p>{importQuestion(settings, pending)}</p>
    </ConfirmSheet>
  )

  // No access (never set up, or the token was changed on the server): just the name.
  if (!settings.token || authFailed) {
    return (
      <>
        <Landing />
        {linkSheet}
      </>
    )
  }

  if (trip) {
    return (
      <div class="app in-trip">
        <InTrip
          trip={trip}
          token={settings.token}
          walks={settings.walks}
          retime={settings.retime ?? 'average'}
          onUpdate={setTrip}
          onSaveWalk={(seg, w, replace) => setSettings({ ...settings, walks: record(settings.walks, seg, w, replace ? 'replace' : 'average') })}
          onEnd={() => setTrip(null)}
          onAuthError={onAuthError}
        />
      </div>
    )
  }

  return (
    <div class={view === 'settings' ? 'app wide-column' : 'app'}>
      <header class="topbar">
        <h1>
          <button
            class={view === 'trip' ? 'home-link active' : 'home-link'}
            aria-current={view === 'trip' ? 'page' : undefined}
            onClick={() => {
              setView('trip')
              setRestarts((n) => n + 1)
            }}
          >
            <img src="/icon.svg" alt="" width={28} height={28} />
            Gym Router
          </button>
        </h1>
        <nav>
          <IconButton
            label="Settings"
            class={view === 'settings' ? 'active' : undefined}
            aria-current={view === 'settings' ? 'page' : undefined}
            onClick={() => setView('settings')}
          >
            <IconSettings />
          </IconButton>
        </nav>
      </header>
      {notice && (
        <Callout role="status" onDismiss={() => setNotice('')}>
          {notice}
        </Callout>
      )}
      {serverError && (
        <Callout tone="bad" role="alert" action={<Button onClick={() => setRetries((n) => n + 1)}>Try again</Button>}>
          <strong>Couldn't load your trips.</strong> {serverError}
        </Callout>
      )}
      {!storageOk && (
        <Callout tone="caution" role="alert">
          This browser won't save settings (private mode?). Export a backup from Settings before closing.
        </Callout>
      )}
      <main>
        {view === 'trip' ? (
          <Trip
            key={restarts}
            settings={settings}
            setSettings={setSettings}
            server={server}
            onAuthError={onAuthError}
            goToSettings={() => setView('settings')}
            onStartTrip={setTrip}
            serverError={!!serverError}
          />
        ) : (
          <SettingsView
            settings={settings}
            setSettings={setSettings}
            server={server}
            onAuthError={onAuthError}
            onSetUp={() => {
              // The first gyms are in: on to planning.
              setView('trip')
              setNotice("You're set up. Choose a gym to plan a trip.")
            }}
          />
        )}
      </main>
      {linkSheet}
      <footer class="footer">Contains Transport for NSW data (CC BY 4.0). Map data © OpenStreetMap contributors.</footer>
    </div>
  )
}
