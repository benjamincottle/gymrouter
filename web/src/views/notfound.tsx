import { useEffect } from 'preact/hooks'

/**
 * What a browser without a setup link sees: a page indistinguishable from a plain-text "404 page not found" (Go's and
 * Traefik's), so there's no sign of an app here. The only way in is a setup link.
 */
export function NotFound() {
  useEffect(() => {
    const root = document.documentElement
    root.classList.add('not-found')
    // A plain-text page has no icon, manifest, theme colour or title (the browser shows its address instead).
    for (const el of document.querySelectorAll('link[rel~="icon"], link[rel="manifest"], link[rel="apple-touch-icon"], meta[name="theme-color"]')) {
      el.remove()
    }
    document.querySelector('title')?.remove()
  }, [])
  return <pre style={{ wordWrap: 'break-word', whiteSpace: 'pre-wrap' }}>404 page not found{'\n'}</pre>
}
