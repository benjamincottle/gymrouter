import { render } from 'preact'
import { App } from './app.tsx'
import { applyFragment, load, parseFragment, save } from './settings.ts'
import '@fontsource-variable/archivo/wdth.css'
import './style.css'

function storage(): Storage | undefined {
  try {
    return window.localStorage
  } catch {
    return undefined
  }
}

// A setup or settings link puts its data in the fragment. Apply it, then remove it from the
// address bar and history so the token isn't left lying around.
let settings = load(storage())
let imported = false
const frag = await parseFragment(window.location.hash)
if (frag) {
  settings = applyFragment(settings, frag)
  imported = save(storage(), settings)
}
if (window.location.hash) {
  history.replaceState(null, '', window.location.pathname)
}

// A link opened while the app is already loaded only changes the fragment: apply it and reload.
window.addEventListener('hashchange', async () => {
  const f = await parseFragment(window.location.hash)
  if (!f) return
  save(storage(), applyFragment(load(storage()), f))
  history.replaceState(null, '', window.location.pathname)
  window.location.reload()
})

// Ask the browser not to evict our storage (mainly matters on iOS).
navigator.storage?.persist?.().catch(() => undefined)

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  navigator.serviceWorker.register('/sw.js').catch(() => undefined)
}

render(<App initial={settings} imported={imported} storage={storage()} />, document.getElementById('app')!)
