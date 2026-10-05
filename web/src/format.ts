// Time and text formatting. Times are shown in Sydney time regardless of the device's zone.

const clockFmt = new Intl.DateTimeFormat('en-AU', {
  hour: 'numeric',
  minute: '2-digit',
  hour12: false,
  timeZone: 'Australia/Sydney',
})

export function clock(iso: string): string {
  return clockFmt.format(new Date(iso))
}

/** When data was last updated or checked, as a clock time like every other: "09:31". */
export function statusTime(ms: number): string {
  return clockFmt.format(new Date(ms))
}

/** "45 min", "1 h 5 min" */
export function duration(secs: number): string {
  const m = Math.round(secs / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const r = m % 60
  return r === 0 ? `${h} h` : `${h} h ${r} min`
}

/** Compact, for tabular columns only (the options board): "45m", "1h 20m". */
export function shortDuration(secs: number): string {
  const m = Math.round(secs / 60)
  return m < 60 ? `${m}m` : m % 60 === 0 ? `${m / 60}h` : `${Math.floor(m / 60)}h ${m % 60}m`
}

/** Time to spare at a change, in whole minutes (rounded down); seconds only under a minute: "2 min spare", "40 s spare". */
export function spare(secs: number): string {
  return secs >= 60 ? `${Math.floor(secs / 60)} min spare` : `${Math.max(0, Math.round(secs))} s spare`
}

/** Countdown to leaving: "now", "in 6 min", "in 1 h 5 min", "left 2 min ago". */
export function countdown(leaveIso: string, nowMs: number): string {
  const secs = Math.round((new Date(leaveIso).getTime() - nowMs) / 1000)
  if (secs <= -60) return `left ${duration(-secs)} ago`
  if (secs < 60) return 'now'
  return `in ${duration(secs)}`
}

/** Short delay label: "on time", "3 min late", "1 min early". */
export function delay(secs: number | undefined): string {
  if (secs === undefined) return ''
  const m = Math.round(secs / 60)
  if (m === 0) return 'on time'
  return m > 0 ? `${m} min late` : `${-m} min early`
}

export function riskLabel(r: string): string {
  return { safe: 'Safe', tight: 'Tight', 'at-risk': 'At risk', missed: 'Missed' }[r] ?? r
}

/** Station name if the stop belongs to one, else the stop name. */
export function placeName(s: { name: string; station?: string } | undefined): string {
  if (!s) return ''
  return s.station || s.name
}

/** "Epping Station, Platform 1" → "Platform 1" when the station is already shown. */
export function platform(s: { name: string; station?: string } | undefined): string {
  if (!s?.station || !s.name.startsWith(s.station)) return ''
  return s.name.slice(s.station.length).replace(/^,\s*/, '')
}

/** Value for <input type="datetime-local"> in Sydney time. */
export function toLocalInput(d: Date): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Australia/Sydney', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hour12: false,
  }).formatToParts(d)
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? '00'
  const hour = get('hour') === '24' ? '00' : get('hour')
  return `${get('year')}-${get('month')}-${get('day')}T${hour}:${get('minute')}`
}

/** Converts a datetime-local value (Sydney time) to an RFC 3339 string with the right offset. */
export function fromLocalInput(v: string): string {
  // Find the Sydney offset at that wall time by probing (handles daylight saving).
  const naive = new Date(`${v}:00Z`)
  for (const off of [11, 10]) {
    const t = new Date(naive.getTime() - off * 3600_000)
    if (toLocalInput(t) === v) return `${v}:00+${String(off).padStart(2, '0')}:00`
  }
  return `${v}:00+10:00`
}

// --- Days, the Australian way: "Today (4th)", "Tomorrow (5th)", "Tuesday (6th)". ---

const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

/** 1st, 2nd, 3rd, 4th … 11th, 12th, 13th … 21st, 22nd, 23rd … */
export function ordinal(n: number): string {
  const tens = n % 100
  const suffix = tens >= 11 && tens <= 13 ? 'th' : (['th', 'st', 'nd', 'rd'][n % 10] ?? 'th')
  return `${n}${suffix}`
}

/** A calendar day (YYYY-MM-DD) `offset` days after `day`; plain date arithmetic, no time zones involved. */
export function addDays(day: string, offset: number): string {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d + offset)).toISOString().slice(0, 10)
}

/** How many days `day` is after `today` (both YYYY-MM-DD). */
export function daysBetween(today: string, day: string): number {
  return Math.round((Date.parse(`${day}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / 86_400_000)
}

/** "Today (4th)", "Tomorrow (5th)", "Tuesday (6th)", "Yesterday (3rd)". */
export function dayLabel(today: string, day: string): string {
  const n = daysBetween(today, day)
  const date = ordinal(Number(day.slice(8, 10)))
  const name = n === 0 ? 'Today' : n === 1 ? 'Tomorrow' : n === -1 ? 'Yesterday' : WEEKDAYS[new Date(`${day}T00:00:00Z`).getUTCDay()]
  return `${name} (${date})`
}

/** The day something happens, for a time that might not be today: "" (today), "tomorrow", "Tuesday 6th". */
export function dayOf(iso: string, nowMs: number): string {
  const today = toLocalInput(new Date(nowMs)).slice(0, 10)
  const day = toLocalInput(new Date(iso)).slice(0, 10)
  const n = daysBetween(today, day)
  if (n === 0) return ''
  if (n === 1) return 'tomorrow'
  if (n === -1) return 'yesterday'
  return `${WEEKDAYS[new Date(`${day}T00:00:00Z`).getUTCDay()]} ${ordinal(Number(day.slice(8, 10)))}`
}

/** A datetime-local value rounded up to the next `step` minutes. */
export function roundUp(v: string, step = 5): string {
  const t = Date.parse(fromLocalInput(v))
  return toLocalInput(new Date(Math.ceil(t / (step * 60_000)) * step * 60_000))
}

/** Names joined for a sentence: "23T4", "20T4 or 23T4", "20T4, 23T4 or 28T4". */
export function orList(names: string[]): string {
  return names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} or ${names[names.length - 1]}`
}
