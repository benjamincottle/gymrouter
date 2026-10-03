import { defineConfig } from 'vite'

export default defineConfig({
  oxc: { jsx: { runtime: 'automatic', importSource: 'preact' } },
  build: {
    outDir: '../internal/webui/dist', // embedded into the Go binary
    emptyOutDir: true,
    target: 'es2022',
    sourcemap: false,
    assetsInlineLimit: 0, // keep everything as files: the CSP allows no data: scripts or styles
    modulePreload: { polyfill: false }, // avoids an inline script
    chunkSizeWarningLimit: 1200, // MapLibre is a large chunk, loaded only when the map opens
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:18080', '/healthz': 'http://127.0.0.1:18080' },
  },
})
