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

/** "45 min", "1 h 5 min" */
export function duration(secs: number): string {
  const m = Math.round(secs / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const r = m % 60
  return r === 0 ? `${h} h` : `${h} h ${r} min`
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
