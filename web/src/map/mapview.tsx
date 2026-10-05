// The live map. Loaded on demand (MapLibre is large), so it's split from the main bundle.
import { useEffect, useRef, useState } from 'preact/hooks'
import * as maplibregl from 'maplibre-gl'
import type { GeoJSONSource, StyleSpecification } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { FetchSource, PMTiles, Protocol } from 'pmtiles'
import { layers, namedFlavor } from '@protomaps/basemaps'
import { ApiError, api } from '../api.ts'
import { legTrace, type TimedWalk } from '../walks.ts'
import { isDark } from '../theme.ts'
import type { Leg, Option } from '../types.ts'
import { INK, lineColour, textOn, token, WHITE } from '../colour.ts'
import { Callout } from '../views/ui.tsx'

maplibregl.setWorkerUrl(workerUrl)

let protocol: Protocol | null = null
function registerTiles(token: string): string {
  const url = `${location.origin}/api/map.pmtiles`
  if (!protocol) {
    protocol = new Protocol()
    maplibregl.addProtocol('pmtiles', protocol.tile)
  }
  // Tiles need the access token too; PMTiles sends it as a header on its range requests.
  protocol.add(new PMTiles(new FetchSource(url, new Headers({ Authorization: `Bearer ${token}` }))))
  return `pmtiles://${url}`
}

function style(token: string, dark: boolean): StyleSpecification {
  const flavor = dark ? 'dark' : 'light'
  return {
    version: 8,
    glyphs: `${location.origin}/basemap/fonts/{fontstack}/{range}.pbf`,
    sprite: `${location.origin}/basemap/sprites/${flavor}`,
    sources: {
      protomaps: {
        type: 'vector',
        url: registerTiles(token),
        attribution: '<a href="https://openstreetmap.org/copyright">© OpenStreetMap</a>',
      },
    },
    layers: layers('protomaps', namedFlavor(flavor), { lang: 'en' }),
  }
}

/** RGB of a CSS hex colour ("#1e2226"), for drawing the walk dots. */
const rgb = (c: string) => [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16)) as [number, number, number]

/**
 * The map's own colours, taken from style.css when the map is made (MapLibre paint can't read CSS variables): the ink
 * for walks, stop and vehicle outlines, white stops, and the "you" blue.
 */
const mapColours = () => {
  const ink = token('--ink') || INK
  return { walk: rgb(ink), ink, stop: WHITE, you: token('--you') || '#2457d6' }
}

const RIDE_W = 6 // a ride's line on the map, in pixels; the stops along it are as wide



type Geometry =
  | { type: 'Point'; coordinates: [number, number] }
  | { type: 'LineString'; coordinates: [number, number][] }
type Feature = { type: 'Feature'; properties: Record<string, unknown>; geometry: Geometry }
/** A round dot of the given diameter (CSS pixels) and colour, drawn at the screen's pixel ratio for the map. */
function dot(diameter: number, [r, g, b]: [number, number, number]) {
  const size = Math.round(diameter * (window.devicePixelRatio || 1))
  const data = new Uint8ClampedArray(size * size * 4)
  const c = size / 2
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const i = (y * size + x) * 4
      data.set([r, g, b, Math.round(255 * Math.max(0, Math.min(1, c - Math.hypot(x + 0.5 - c, y + 0.5 - c) + 0.5)))], i)
    }
  }
  return { width: size, height: size, data }
}

const fc = (features: Feature[]) => ({ type: 'FeatureCollection' as const, features })

export interface MapViewProps {
  token: string
  walks: TimedWalk[] // timed walks: their traced routes are drawn instead of the street-map ones
  places: { start?: string; end?: string } // the trip's home or gym at each end (placeKey), for its timed walks
  option: Option
  serviceDate: string
  origin: [number, number] // [lon, lat] of where the trip starts (home or gym)
  destination: [number, number]
  me?: { lat: number; lon: number; accuracy: number } | null // where you are, during a trip
}

/**
 * Draws the option's legs (only the parts ridden, with the stops passed), the vehicles running them while
 * they're near your part of the trip, and you.
 */
export function MapView({ token, walks, places, option, serviceDate, origin, destination, me }: MapViewProps) {
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [error, setError] = useState('')
  const [vehicleCount, setVehicleCount] = useState<number | null>(null)
  // The note about vehicles goes by itself after a few seconds (or when dismissed); it only needs saying once.
  const [noteGone, setNoteGone] = useState(false)
  useEffect(() => {
    if (vehicleCount !== 0) return
    const id = setTimeout(() => setNoteGone(true), 5000)
    return () => clearTimeout(id)
  }, [vehicleCount === 0])
  const routeBounds = useRef<maplibregl.LngLatBounds | null>(null)
  const firstVehicle = useRef<[number, number] | null>(null) // where the vehicle for your first ride is
  const framed = useRef(false)
  // Once you've zoomed or moved the map yourself, live updates leave the view alone.
  const userMoved = useRef(false)
  const firstTrip = option.legs.find((l) => l.kind === 'ride')?.trip_id

  // Frame the route plus the vehicle you'll catch first (it may still be on its way to your stop).
  const frame = (m: maplibregl.Map, animate: boolean) => {
    if (!routeBounds.current || userMoved.current) return
    const b = new maplibregl.LngLatBounds(routeBounds.current.getSouthWest(), routeBounds.current.getNorthEast())
    if (firstVehicle.current) b.extend(firstVehicle.current)
    // The trip fills the map: just enough room for the zoom and locate buttons on the right.
    m.fitBounds(b, { padding: { top: 36, bottom: 36, left: 28, right: 64 }, maxZoom: 16, duration: animate ? 600 : 0 })
  }
  // The rides, as the vehicles endpoint wants them: <trip>|<board stop>|<alight stop>.
  const rides = option.legs
    .filter((l) => l.kind === 'ride' && l.trip_id && l.from && l.to)
    .map((l) => `${l.trip_id}|${l.from!.id}|${l.to!.id}`)
  const ridesKey = rides.join(',')

  // Create the map once.
  useEffect(() => {
    if (!el.current) return
    const dark = isDark()
    const col = mapColours()
    const m = new maplibregl.Map({
      container: el.current,
      style: style(token, dark),
      center: origin,
      zoom: 12,
      attributionControl: { compact: true },
      maxBounds: [149.9, -34.6, 152.0, -33.0],
    })
    m.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    const locate = new maplibregl.GeolocateControl({ trackUserLocation: true })
    m.addControl(locate, 'top-right')
    // Gestures and the zoom buttons carry the event that caused them; programmatic framing doesn't.
    m.on('movestart', (e) => {
      if (e.originalEvent) userMoved.current = true
    })
    locate.on('trackuserlocationstart', () => (userMoved.current = true))
    m.on('error', (e) => {
      const msg = String(e.error?.message ?? '')
      if (msg.includes('404')) setError("The map hasn't been installed on the server yet. Routes and vehicles still show.")
    })
    m.on('load', () => {
      m.addSource('route', { type: 'geojson', data: fc([]) })
      m.addSource('vehicles', { type: 'geojson', data: fc([]) })
      m.addSource('me', { type: 'geojson', data: fc([]) })
      m.addLayer({
        id: 'route-ride', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'ride'],
        paint: { 'line-color': ['get', 'color'], 'line-width': RIDE_W },
        layout: { 'line-cap': 'round', 'line-join': 'round' },
      })
      // Walking: round dots every few pixels along the street route, like the trip's steps. A dot image placed along
      // the line rather than a dashed line: MapLibre blends dash patterns between zoom levels, stretching dots into dashes.
      m.addImage('walk-dot', dot(4.5, col.walk), { pixelRatio: window.devicePixelRatio || 1 })
      m.addLayer({
        id: 'route-walk', type: 'symbol', source: 'route', filter: ['==', ['get', 'kind'], 'walk'],
        layout: {
          'symbol-placement': 'line', 'symbol-spacing': 8, 'icon-image': 'walk-dot',
          'icon-allow-overlap': true, 'icon-ignore-placement': true, 'icon-rotation-alignment': 'map',
        },
      })
      // A ride's stops, where you get on and off and those passed on the way: white dots on the line with a 1px ink
      // outline, as wide as the line.
      m.addLayer({
        id: 'route-stops', type: 'circle', source: 'route', filter: ['in', ['get', 'kind'], ['literal', ['via', 'stop']]],
        paint: {
          'circle-radius': RIDE_W / 2 - 1, 'circle-color': col.stop,
          'circle-stroke-width': 1, 'circle-stroke-color': INK, // black in both themes
        },
      })
      m.addLayer({
        id: 'vehicles-mine', type: 'circle', source: 'vehicles',
        paint: {
          'circle-radius': 14, 'circle-color': ['get', 'color'],
          'circle-stroke-color': col.ink, 'circle-stroke-width': 3,
        },
      })
      m.addLayer({
        id: 'vehicles-mine-label', type: 'symbol', source: 'vehicles',
        layout: {
          'text-field': ['get', 'name'], 'text-font': ['Noto Sans Medium'], 'text-size': 10,
          'text-allow-overlap': true,
        },
        paint: { 'text-color': ['get', 'text'] },
      })
      // You: the familiar blue dot, with a halo the size of the location's accuracy.
      m.addLayer({
        id: 'me-accuracy', type: 'circle', source: 'me',
        paint: {
          'circle-radius': ['interpolate', ['exponential', 2], ['zoom'], 10, ['get', 'px10'], 20, ['*', ['get', 'px10'], 1024]],
          'circle-color': col.you, 'circle-opacity': 0.12,
        },
      })
      m.addLayer({
        id: 'me', type: 'circle', source: 'me',
        paint: { 'circle-radius': 7, 'circle-color': col.you, 'circle-stroke-color': col.stop, 'circle-stroke-width': 2.5 },
      })
      map.current = m
    })
    // The map's box can change size after it's made (the steps under it lay out, the phone turns): follow it, and
    // re-fit the trip unless you've moved the map yourself.
    const sized = new ResizeObserver(() => {
      m.resize()
      frame(m, false)
    })
    sized.observe(el.current)
    return () => {
      sized.disconnect()
      map.current = null
      m.remove()
    }
  }, [token])

  // The option's legs, then fit the view to them.
  useEffect(() => {
    let live = true
    const draw = async (m: maplibregl.Map) => {
      const features: Feature[] = []
      const bounds = new maplibregl.LngLatBounds(origin, origin)
      bounds.extend(destination)
      const point = (l: Leg, end: 'from' | 'to'): [number, number] | null => {
        const s = l[end]
        return s ? [s.lon, s.lat] : null
      }
      for (const [i, l] of option.legs.entries()) {
        const a = point(l, 'from') ?? (i === 0 ? origin : null)
        const b = point(l, 'to') ?? (i === option.legs.length - 1 ? destination : null)
        if (!a || !b) continue
        bounds.extend(a).extend(b)
        if (l.kind === 'walk') {
          // A walk you timed and traced beats the street-map route, which beats a straight line.
          const traced = legTrace(walks, option, i, places.start, places.end)
          const coords = traced ?? (l.path && l.path.length > 1 ? l.path : [a, b])
          for (const c of coords) bounds.extend(c)
          features.push({ type: 'Feature', properties: { kind: 'walk' }, geometry: { type: 'LineString', coordinates: coords } })
          continue
        }
        const color = lineColour(l.line?.color)
        let coords: [number, number][] = [a, b]
        let via: [number, number][] = []
        try {
          const shape = await api.shape(token, serviceDate, l.trip_id!, l.from!.id, l.to!.id)
          coords = shape.coordinates
          via = shape.stops ?? []
        } catch {
          /* keep the straight line */
        }
        for (const c of coords) bounds.extend(c) // the line can bow out past its ends
        features.push({ type: 'Feature', properties: { kind: 'ride', color }, geometry: { type: 'LineString', coordinates: coords } })
        for (const p of via) features.push({ type: 'Feature', properties: { kind: 'via', color }, geometry: { type: 'Point', coordinates: p } })
        // On the line's ends (the server cuts the shape where each stop sits beside it), not at the kerb.
        features.push({ type: 'Feature', properties: { kind: 'stop', color }, geometry: { type: 'Point', coordinates: coords[0] } })
        features.push({ type: 'Feature', properties: { kind: 'stop', color }, geometry: { type: 'Point', coordinates: coords[coords.length - 1] } })
      }
      if (!live) return
      ;(m.getSource('route') as GeoJSONSource | undefined)?.setData(fc(features))
      routeBounds.current = bounds
      framed.current = firstVehicle.current !== null
      frame(m, false)
    }
    whenReady(map, el, draw)
    return () => {
      live = false
    }
  }, [option, serviceDate, token, walks])

  // Live vehicles every 10 s; this also keeps the server's realtime polling active.
  useEffect(() => {
    let live = true
    const tick = (force = false) => {
      const m = map.current
      if (!m || rides.length === 0 || (!force && document.visibilityState !== 'visible')) return
      api
        .vehicles(token, option.lines, rides)
        .then((r) => {
          if (!live) return
          setVehicleCount(r.vehicles.length)
          const first = r.vehicles.find((v) => v.trip_id === firstTrip)
          firstVehicle.current = first ? [first.lon, first.lat] : null
          if (first && !framed.current && routeBounds.current) {
            framed.current = true
            frame(m, true)
          }
          ;(m.getSource('vehicles') as GeoJSONSource | undefined)?.setData(
            fc(
              r.vehicles.map((v): Feature => ({
                type: 'Feature',
                properties: { color: lineColour(v.color), text: textOn(lineColour(v.color)), name: v.line.split(' ').slice(1).join(' ') },
                geometry: { type: 'Point', coordinates: [v.lon, v.lat] },
              })),
            ),
          )
        })
        .catch((e) => {
          if (e instanceof ApiError) setError(e.message)
        })
    }
    whenReady(map, el, () => tick(true))
    const id = setInterval(tick, 10_000)
    return () => {
      live = false
      clearInterval(id)
    }
  }, [token, ridesKey])

  // You, as the location updates.
  useEffect(() => {
    whenReady(map, el, (m) => {
      const src = m.getSource('me') as GeoJSONSource | undefined
      if (!me) return src?.setData(fc([]))
      // Accuracy (m) as pixels at zoom 10; the layer scales it with the zoom.
      const px10 = me.accuracy / (156543.03 * Math.cos((me.lat * Math.PI) / 180) / 2 ** 10)
      src?.setData(fc([{ type: 'Feature', properties: { px10 }, geometry: { type: 'Point', coordinates: [me.lon, me.lat] } }]))
    })
  }, [me?.lat, me?.lon, me?.accuracy])

  return (
    <div class="map-wrap">
      <div ref={el} class="map" role="region" aria-label="Map of the trip with live vehicles" data-my-vehicles={vehicleCount ?? ''} />
      {error && <Callout tone="caution">{error}</Callout>}
      {vehicleCount === 0 && !noteGone && (
        <Callout class="subtle" role="status" onDismiss={() => setNoteGone(true)}>
          Your services appear here once they're a few stops away.
        </Callout>
      )}
    </div>
  )
}

function whenReady(map: { current: maplibregl.Map | null }, el: { current: HTMLDivElement | null }, fn: (m: maplibregl.Map) => void) {
  if (map.current) {
    fn(map.current)
    return
  }
  // Sources are added on the map's load event; wait for that.
  const wait = setInterval(() => {
    if (map.current) {
      clearInterval(wait)
      fn(map.current)
    } else if (!el.current) {
      clearInterval(wait)
    }
  }, 100)
}

export default MapView
