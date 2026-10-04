// Light or dark: chosen in Settings, or following the system. Sets <html data-theme> (style.css keys off it) and the
// browser's theme colour, and keeps following the system while that's the choice.

export type ThemeChoice = 'light' | 'dark' | undefined // undefined: follow the system

const system = () => window.matchMedia('(prefers-color-scheme: dark)')
let unfollow: (() => void) | null = null

function set(dark: boolean) {
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
  for (const m of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    m.content = dark ? '#1b1e21' : '#ebeeea'
  }
}

export function applyTheme(choice: ThemeChoice) {
  unfollow?.()
  unfollow = null
  if (choice) return set(choice === 'dark')
  const mq = system()
  const on = () => set(mq.matches)
  on()
  mq.addEventListener('change', on)
  unfollow = () => mq.removeEventListener('change', on)
}

/** Whether the page is dark right now (for things drawn outside CSS, like the map). */
export function isDark(): boolean {
  return document.documentElement.dataset.theme === 'dark'
}
