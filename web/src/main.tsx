import { render } from 'preact'
import { App } from './app.tsx'
import { applyFragment, importNeedsConfirm, importQuestion, load, parseFragment, save } from './settings.ts'
import { applyTheme } from './theme.ts'
import '@fontsource-variable/archivo/wdth.css'
import './style.css'

function storage(): Storage | undefined {
  try {
    return window.localStorage
  } catch {
    return undefined
  }
}

// A setup or settings link puts its data in the fragment. Apply it (asking first if it would change the token or
// replace saved data), then remove it from the address bar and history so the token isn't left lying around.
let settings = load(storage())
applyTheme(settings.theme) // before anything is drawn, so the page doesn't flash the wrong way
let imported = false
const frag = await parseFragment(window.location.hash)
if (window.location.hash) {
  history.replaceState(null, '', window.location.pathname)
}
if (frag && (!importNeedsConfirm(settings, frag) || confirm(importQuestion(settings, frag)))) {
  settings = applyFragment(settings, frag)
  imported = save(storage(), settings)
}

// A link opened while the app is already loaded only changes the fragment: apply it the same way and reload.
window.addEventListener('hashchange', async () => {
  const f = await parseFragment(window.location.hash)
  if (!f) return
  history.replaceState(null, '', window.location.pathname)
  const cur = load(storage())
  if (importNeedsConfirm(cur, f) && !confirm(importQuestion(cur, f))) return
  save(storage(), applyFragment(cur, f))
  window.location.reload()
})

// Only a set-up device gets the app's extras; anyone else sees a plain 404 page (views/notfound.tsx).
if (settings.token) {
  // Ask the browser not to evict our storage (mainly matters on iOS).
  navigator.storage?.persist?.().catch(() => undefined)
  if ('serviceWorker' in navigator && import.meta.env.PROD) {
    navigator.serviceWorker.register('/sw.js').catch(() => undefined)
  }
}

render(<App initial={settings} imported={imported} storage={storage()} />, document.getElementById('app')!)
