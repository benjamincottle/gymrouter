import { api } from '../api.ts'
import { statusTime } from '../format.ts'
import { usePolling, useVisible } from '../hooks.ts'
import { summarise } from '../serverstatus.ts'

/** How often the footer checks the server while the app is open. */
const CHECK_MS = 5 * 60_000

/** The server's state for the footer: a status dot, a short line, and what needs a look (usually nothing). */
export function ServerStatusLine({ token }: { token: string }) {
  const visible = useVisible()
  const status = usePolling(token, (signal) => api.status(token, signal), CHECK_MS, visible)
  const s = summarise(status.data, status.error)
  return (
    <div class="server-status" role="status">
      <p>
        <span class="status-dot" data-state={s.state} aria-hidden="true" />
        {s.title}
        {status.updatedAt > 0 && s.state !== 'error' && `, checked ${statusTime(status.updatedAt)}`}
      </p>
      {s.issues.map((i) => (
        <p>{i}</p>
      ))}
      {status.data?.version && <p>Version {status.data.version}</p>}
    </div>
  )
}
