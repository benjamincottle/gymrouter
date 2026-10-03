// Service worker: makes the app installable and lets the shell open offline.
// API responses are never cached (they're live and carry personal data).
const CACHE = 'gymrouter-shell-v1'

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.add('/')))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))),
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== 'GET' || url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname === '/healthz') return

  // Network first so updates arrive straight away; fall back to the cached copy offline.
  event.respondWith(
    fetch(event.request)
      .then((res) => {
        if (res.ok && (url.pathname === '/' || url.pathname.startsWith('/assets/'))) {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put(url.pathname === '/' ? '/' : event.request, copy))
        }
        return res
      })
      .catch(() => caches.match(url.pathname === '/' ? '/' : event.request).then((r) => r || Response.error())),
  )
})
