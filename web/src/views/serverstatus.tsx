import { useEffect, useState } from 'preact/hooks'
import { api, problem } from '../api.ts'
import { statusTime } from '../format.ts'
import { usePolling, useVisible } from '../hooks.ts'
import { details, outdated, summarise } from '../serverstatus.ts'
import type { ServerStatus } from '../types.ts'
import { TextButton } from './ui.tsx'

/** How often the footer checks the server while the app is open. */
const CHECK_MS = 5 * 60_000

/** And while the server is updating its street map, so the row says when it's done. */
const UPDATING_MS = 5_000

const Chevron = () => (
  <svg class="chevron" viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
    <path d="M6 9l6 6 6-6" />
  </svg>
)

/**
 * The server's state for the footer: a status dot, a short line, and what needs a look (usually nothing). The line
 * opens to everything the status call returned.
 */
export function ServerStatusLine({ token }: { token: string }) {
  const visible = useVisible()
  const status = usePolling(token, (signal) => api.status(token, signal), CHECK_MS, visible)
  const [open, setOpen] = useState(false)
  const s = summarise(status.data, status.error)
  const version = status.data?.version
  const updating = status.data?.data?.walk_updating ?? false
  useEffect(() => {
    if (!updating || !open || !visible) return
    const id = setInterval(status.refresh, UPDATING_MS)
    return () => clearInterval(id)
  }, [updating, open, visible])
  const toggle = () => {
    if (!open) status.refresh() // what you open is what the server says now
    setOpen(!open)
  }
  return (
    <div class="server-status">
      <button class="status-toggle" aria-expanded={open} aria-controls="server-detail" onClick={toggle}>
        <span class="status-dot" data-state={s.state} aria-hidden="true" />
        <span role="status">
          {s.title}
          {status.updatedAt > 0 && s.state !== 'error' && `, checked ${statusTime(status.updatedAt)}`}
        </span>
        <Chevron />
      </button>
      {s.issues.map((i) => (
        <p>{i}</p>
      ))}
      {open && (
        <div id="server-detail" class="server-detail">
          {status.data ? <Detail status={status.data} token={token} refresh={status.refresh} /> : <p>{s.state === 'error' ? 'Nothing to show until the server answers.' : 'Asking the server…'}</p>}
        </div>
      )}
      {version &&
        (outdated(version) ? (
          <p role="status">
            This app is out of date. <TextButton onClick={() => location.reload()}>Reload</TextButton> to get version {version}.
          </p>
        ) : (
          <p>Version {version}</p>
        ))}
    </div>
  )
}

/**
 * The status in full: a row for each thing the server reported, then its reply as it came. The street map's row can
 * ask the server to download it again (`refresh` then asks for the status, which says it's updating).
 */
function Detail({ status, token, refresh }: { status: ServerStatus; token: string; refresh: () => void }) {
  const [raw, setRaw] = useState(false)
  const [asking, setAsking] = useState(false)
  const [failed, setFailed] = useState('')
  const update = async () => {
    setAsking(true)
    setFailed('')
    try {
      await api.updateStreetMap(token)
      refresh()
    } catch (e) {
      setFailed(problem(e))
    } finally {
      setAsking(false)
    }
  }
  return (
    <>
      <dl>
        {details(status).map((d) => (
          <div class={d.tone}>
            <dt>{d.label}</dt>
            <dd>
              {d.value}
              {d.action === 'update-street-map' && (
                <p>
                  <TextButton disabled={asking} onClick={update}>Update now</TextButton>
                </p>
              )}
              {d.action === 'update-street-map' && failed && (
                <p class="problem" role="alert">
                  {failed}
                </p>
              )}
            </dd>
          </div>
        ))}
      </dl>
      <button class="status-toggle" aria-expanded={raw} aria-controls="server-raw" onClick={() => setRaw(!raw)}>
        The server's reply
        <Chevron />
      </button>
      {raw && (
        <pre id="server-raw" tabIndex={0}>
          {JSON.stringify(status, null, 2)}
        </pre>
      )}
    </>
  )
}
