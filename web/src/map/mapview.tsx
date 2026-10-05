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

const RIDE_W = 6 // a ride's line on the map, in pixels; the stops along it are as wide

const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : '#5e6670')

/** Black or white, whichever reads better on the line colour (some lines are pale, e.g. yellow). */
function textOn(c?: string): string {
  if (!c || !/^[0-9a-fA-F]{6}$/.test(c)) return '#ffffff'
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(c.slice(i, i + 2), 16) / 255)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.6 ? '#1f2328' : '#ffffff'
}

type Geometry =
  | { type: 'Point'; coordinates: [number, number] }
  | { type: 'LineString'; coordinates: [number, number][] }
type Feature = { type: 'Feature'; properties: Record<string, unknown>; geometry: Geometry }
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
      m.addLayer({
        id: 'route-walk', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'walk'],
        paint: { 'line-color': dark ? '#e9ece8' : '#1f2328', 'line-width': 3, 'line-dasharray': [0.5, 2] },
        layout: { 'line-cap': 'round' },
      })
      // A ride's stops, where you get on and off and those passed on the way: white dots on the line with a 1px ink
      // outline, as wide as the line.
      m.addLayer({
        id: 'route-stops', type: 'circle', source: 'route', filter: ['in', ['get', 'kind'], ['literal', ['via', 'stop']]],
        paint: {
          'circle-radius': RIDE_W / 2 - 1, 'circle-color': '#ffffff',
          'circle-stroke-width': 1, 'circle-stroke-color': '#1e2226',
        },
      })
      m.addLayer({
        id: 'vehicles-mine', type: 'circle', source: 'vehicles',
        paint: {
          'circle-radius': 14, 'circle-color': ['get', 'color'],
          'circle-stroke-color': dark ? '#e9ece8' : '#1f2328', 'circle-stroke-width': 3,
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
          'circle-color': '#2457d6', 'circle-opacity': 0.12,
        },
      })
      m.addLayer({
        id: 'me', type: 'circle', source: 'me',
        paint: { 'circle-radius': 7, 'circle-color': '#2457d6', 'circle-stroke-color': '#ffffff', 'circle-stroke-width': 2.5 },
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
        const color = hex(l.line?.color)
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
                properties: { color: hex(v.color), text: textOn(v.color), name: v.line.split(' ').slice(1).join(' ') },
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
      {error && <p class="map-note">{error}</p>}
      {vehicleCount === 0 && !noteGone && (
        <p class="map-note subtle" role="status">
          Your services appear here once they're a few stops away.
          <button class="close" aria-label="Dismiss" onClick={() => setNoteGone(true)}>
            ×
          </button>
        </p>
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
