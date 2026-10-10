import { AuthError } from './api.ts'
import { ago, dayName } from './format.ts'
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

/** The version this page is: the first one the server reported since it loaded (the app is built into the server). */
let loaded = ''

/**
 * The version this page is when the server has moved on to another since the page loaded (a deploy while the app
 * stayed open), so there's a newer app to reload for; otherwise nothing.
 */
export function outdated(version: string | undefined): string {
  if (!version) return ''
  loaded ||= version
  return version === loaded ? '' : loaded
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

/** One line of the status in full: what it's about, what the server said, and whether that's a problem. */
export interface Detail {
  label: string
  value: string
  tone?: 'caution' | 'bad'
}

const FEEDS: Record<string, string> = {
  sydneytrains: 'Trains', metro: 'Metro', buses: 'Buses', 'lightrail-parramatta': 'Light rail, Parramatta', ferries: 'Ferries',
}

const count = (n: number) => n.toLocaleString('en-AU')

/** Everything the status call returned, in words: the rows the footer's status opens to. */
export function details(s: ServerStatus): Detail[] {
  const out: Detail[] = []
  const aged = (what: string, age?: number) => (age === undefined ? what : `${what} ${ago(age)}`)
  if (s.static_error) out.push({ label: 'Timetable', value: `The last download failed: ${s.static_error}`, tone: s.service_date ? 'caution' : 'bad' })
  out.push(
    s.service_date
      ? { label: s.static_error ? 'Timetable in use' : 'Timetable', value: `For ${dayName(s.service_date)}, ${aged('downloaded', s.static_age_s)}` }
      : { label: 'Timetable', value: 'Not loaded yet', tone: 'bad' },
  )
  out.push({ label: 'Live data', value: s.polling_active ? 'Updating while the app is in use' : 'Paused until the app is used' })
  for (const f of s.feeds ?? []) {
    const label = FEEDS[f.name] ?? f.name
    if (f.error) out.push({ label, value: f.error, tone: 'caution' })
    else if (f.trip_updates_age_s === undefined && f.vehicles_age_s === undefined) out.push({ label, value: 'Nothing fetched yet' })
    else out.push({ label, value: [f.trip_updates_age_s !== undefined && aged('Times', f.trip_updates_age_s), f.vehicles_age_s !== undefined && aged('vehicles', f.vehicles_age_s)].filter(Boolean).join(', ') })
  }
  if (s.realtime) {
    const r = s.realtime
    const parts = [`${count(r.matched + r.matched_by_run)} of ${count(r.updates)} matched to the timetable`]
    if (r.added) parts.push(`${count(r.added)} extra`)
    if (r.cancelled) parts.push(`${count(r.cancelled)} cancelled`)
    if (r.unmatched) parts.push(`${count(r.unmatched)} unmatched`)
    out.push({ label: 'Live services', value: parts.join(', ') })
  }
  if (s.upstream_requests_today !== undefined) out.push({ label: 'Requests to TfNSW', value: `${count(s.upstream_requests_today)} today` })
  if (s.missing_lines?.length) out.push({ label: 'Not running today', value: s.missing_lines.join(', ') })
  if (s.unknown_trackwork?.length) out.push({ label: 'Trackwork buses left out', value: s.unknown_trackwork.join(', '), tone: 'caution' })
  const d = s.data
  if (d) {
    out.push(d.walk_error
      ? { label: 'Street map', value: `${d.walk_ready ? 'In use, but the last download failed' : "Couldn't be downloaded"}: ${d.walk_error}`, tone: 'caution' }
      : { label: 'Street map', value: d.walk_ready ? aged('Ready, built', d.walk_age_s) : 'Being prepared: walks are estimates until then' })
    out.push(d.map_error
      ? { label: 'Map', value: `${d.map_ready ? 'In use, but the last download failed' : "Couldn't be downloaded"}: ${d.map_error}`, tone: 'caution' }
      : { label: 'Map', value: d.map_ready ? aged('Ready, built', d.map_age_s) : 'Being prepared: the map is blank until then' })
  }
  return out
}
