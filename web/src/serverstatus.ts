import { AuthError } from './api.ts'
import type { ServerStatus } from './types.ts'

/** Each issue the server reports, in words (see engine.Issue* on the server). */
const ISSUES: Record<string, string> = {
  'no-timetable': "It hasn't loaded a timetable yet.",
  'old-timetable': 'Its timetable is more than three days old.',
  'timetable-refresh': 'The last timetable download failed.',
  'live-data': "Live data isn't coming through.",
  trackwork: "Some trackwork buses can't be matched to their line.",
  'street-map': "The street map couldn't be downloaded.",
  basemap: "The map couldn't be downloaded.",
}

export interface Summary {
  state: 'ok' | 'warning' | 'error' | 'checking'
  title: string
  issues: string[]
}

/** What the footer says about the server, from its last status (or why there isn't one). */
export function summarise(status: ServerStatus | null, error: unknown): Summary {
  if (error && !(error instanceof AuthError)) return { state: 'error', title: "Can't reach the server", issues: [] }
  if (!status) return { state: 'checking', title: 'Checking the server…', issues: [] }
  const issues = (status.issues ?? []).map((i) => ISSUES[i] ?? 'Something on the server needs a look.')
  switch (status.state) {
    case 'ok':
      return { state: 'ok', title: 'Server OK', issues: [] }
    case 'warning':
      return { state: 'warning', title: 'Server working, needs a look', issues }
    default:
      return { state: 'error', title: "Server can't plan", issues }
  }
}
