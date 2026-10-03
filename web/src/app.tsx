import { useCallback, useEffect, useState } from 'preact/hooks'
import { api, AuthError } from './api.ts'
import { save, type Settings } from './settings.ts'
import type { GymsResponse } from './types.ts'
import { Setup } from './views/setup.tsx'
import { Trip } from './views/trip.tsx'
import { SettingsView } from './views/settings.tsx'
import { InTrip, TRIP_KEY, type ActiveTrip } from './views/intrip.tsx'

function loadTrip(storage: Storage | undefined): ActiveTrip | null {
  try {
    const raw = storage?.getItem(TRIP_KEY)
    if (!raw) return null
    const t = JSON.parse(raw) as ActiveTrip
    // Drop trips that ended more than an hour ago.
    return t?.option?.arrive && Date.parse(t.option.arrive) > Date.now() - 3600_000 ? t : null
  } catch {
    return null
  }
}

type View = 'trip' | 'settings'

export interface AppProps {
  initial: Settings
  imported: boolean
  storage: Storage | undefined
}

export function App({ initial, imported, storage }: AppProps) {
  const [settings, setSettingsState] = useState(initial)
  const [view, setView] = useState<View>(initial.homes.length === 0 && initial.token ? 'settings' : 'trip')
  const [gyms, setGyms] = useState<GymsResponse | null>(null)
  const [authFailed, setAuthFailed] = useState(false)
  const [notice, setNotice] = useState(imported ? 'Settings saved on this device.' : '')
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

  useEffect(() => {
    if (!settings.token) return
    let live = true
    api
      .gyms(settings.token)
      .then((g) => {
        if (!live) return
        setGyms(g)
        setAuthFailed(false)
      })
      .catch((e) => {
        if (e instanceof AuthError) setAuthFailed(true)
        else if (live) setNotice(`Couldn't reach the server: ${e.message}`)
      })
    return () => {
      live = false
    }
  }, [settings.token])

  if (!settings.token || authFailed) {
    return (
      <Setup
        revoked={authFailed}
        onToken={(token) => {
          setAuthFailed(false)
          setSettings({ ...settings, token })
        }}
      />
    )
  }

  if (trip) {
    return (
      <div class="app">
        <InTrip trip={trip} token={settings.token} onUpdate={setTrip} onEnd={() => setTrip(null)} onAuthError={onAuthError} />
      </div>
    )
  }

  return (
    <div class="app">
      <header class="topbar">
        <h1>Gym Router</h1>
        <nav>
          <button class={view === 'trip' ? 'tab active' : 'tab'} onClick={() => setView('trip')}>
            Trips
          </button>
          <button class={view === 'settings' ? 'tab active' : 'tab'} onClick={() => setView('settings')}>
            Settings
          </button>
        </nav>
      </header>
      {notice && (
        <p class="notice" role="status">
          {notice} <button class="link" onClick={() => setNotice('')}>Dismiss</button>
        </p>
      )}
      {!storageOk && (
        <p class="notice warn" role="alert">
          This browser won't save settings (private mode?). Export a backup from Settings before closing.
        </p>
      )}
      <main>
        {view === 'trip' ? (
          <Trip
            settings={settings}
            setSettings={setSettings}
            gyms={gyms}
            onAuthError={onAuthError}
            goToSettings={() => setView('settings')}
            onStartTrip={setTrip}
          />
        ) : (
          <SettingsView settings={settings} setSettings={setSettings} gyms={gyms} onAuthError={onAuthError} />
        )}
      </main>
      <footer class="footer">
        <p>Contains Transport for NSW data (CC BY 4.0).</p>
        <p>Map data © OpenStreetMap contributors.</p>
      </footer>
    </div>
  )
}
