// The live map. Loaded on demand (MapLibre is large), so it's split from the main bundle.
import { useEffect, useRef, useState } from 'preact/hooks'
import * as maplibregl from 'maplibre-gl'
import type { GeoJSONSource, StyleSpecification } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { FetchSource, PMTiles, Protocol } from 'pmtiles'
import { layers, namedFlavor } from '@protomaps/basemaps'
import { ApiError, api } from '../api.ts'
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

const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : '#5e6670')

type Geometry =
  | { type: 'Point'; coordinates: [number, number] }
  | { type: 'LineString'; coordinates: [number, number][] }
type Feature = { type: 'Feature'; properties: Record<string, unknown>; geometry: Geometry }
const fc = (features: Feature[]) => ({ type: 'FeatureCollection' as const, features })

export interface MapViewProps {
  token: string
  lines: string[]
  option: Option
  serviceDate: string
  origin: [number, number] // [lon, lat] of where the trip starts (home or gym)
  destination: [number, number]
}

/** Draws the option's legs, the gym's lines, and live vehicles (the option's own highlighted). */
export function MapView({ token, lines, option, serviceDate, origin, destination }: MapViewProps) {
  const linesKey = lines.join(',')
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [error, setError] = useState('')
  const [vehicleCount, setVehicleCount] = useState<number | null>(null)
  const routeBounds = useRef<maplibregl.LngLatBounds | null>(null)
  const firstVehicle = useRef<[number, number] | null>(null) // where the vehicle for your first ride is
  const framed = useRef(false)
  const firstTrip = option.legs.find((l) => l.kind === 'ride')?.trip_id

  // Frame the route plus the vehicle you'll catch first (it may still be on its way to your stop).
  const frame = (m: maplibregl.Map, animate: boolean) => {
    if (!routeBounds.current) return
    const b = new maplibregl.LngLatBounds(routeBounds.current.getSouthWest(), routeBounds.current.getNorthEast())
    if (firstVehicle.current) b.extend(firstVehicle.current)
    m.fitBounds(b, { padding: { top: 60, bottom: 220, left: 40, right: 60 }, maxZoom: 15, duration: animate ? 600 : 0 })
  }
  const myTrips = option.legs.filter((l) => l.kind === 'ride' && l.trip_id).map((l) => l.trip_id!)

  // Create the map once.
  useEffect(() => {
    if (!el.current) return
    const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
    const m = new maplibregl.Map({
      container: el.current,
      style: style(token, dark),
      center: origin,
      zoom: 12,
      attributionControl: { compact: true },
      maxBounds: [149.9, -34.6, 152.0, -33.0],
    })
    m.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    m.addControl(new maplibregl.GeolocateControl({ trackUserLocation: true }), 'top-right')
    m.on('error', (e) => {
      const msg = String(e.error?.message ?? '')
      if (msg.includes('404')) setError("The map hasn't been installed on the server yet. Routes and vehicles still show.")
    })
    m.on('load', () => {
      m.addSource('network', { type: 'geojson', data: fc([]) })
      m.addSource('route', { type: 'geojson', data: fc([]) })
      m.addSource('vehicles', { type: 'geojson', data: fc([]) })
      m.addLayer({
        id: 'network', type: 'line', source: 'network',
        paint: { 'line-color': ['get', 'color'], 'line-width': 1.5, 'line-opacity': 0.22 },
        layout: { 'line-cap': 'round', 'line-join': 'round' },
      })
      m.addLayer({
        id: 'route-casing', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'ride'],
        paint: { 'line-color': dark ? '#1b1e21' : '#ffffff', 'line-width': 11 },
        layout: { 'line-cap': 'round', 'line-join': 'round' },
      })
      m.addLayer({
        id: 'route-ride', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'ride'],
        paint: { 'line-color': ['get', 'color'], 'line-width': 6 },
        layout: { 'line-cap': 'round', 'line-join': 'round' },
      })
      m.addLayer({
        id: 'route-walk', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'walk'],
        paint: { 'line-color': dark ? '#e9ece8' : '#1f2328', 'line-width': 3, 'line-dasharray': [0.5, 2] },
        layout: { 'line-cap': 'round' },
      })
      m.addLayer({
        id: 'route-stops', type: 'circle', source: 'route', filter: ['==', ['geometry-type'], 'Point'],
        paint: {
          'circle-radius': 5, 'circle-color': dark ? '#1c1f22' : '#ffffff',
          'circle-stroke-color': ['get', 'color'], 'circle-stroke-width': 3,
        },
      })
      m.addLayer({
        id: 'vehicles', type: 'circle', source: 'vehicles', filter: ['!', ['get', 'mine']],
        paint: {
          'circle-radius': 4, 'circle-color': ['get', 'color'], 'circle-opacity': 0.45,
          'circle-stroke-color': dark ? '#1c1f22' : '#ffffff', 'circle-stroke-width': 1,
        },
      })
      m.addLayer({
        id: 'vehicles-mine', type: 'circle', source: 'vehicles', filter: ['get', 'mine'],
        paint: {
          'circle-radius': 14, 'circle-color': ['get', 'color'],
          'circle-stroke-color': dark ? '#e9ece8' : '#1f2328', 'circle-stroke-width': 3,
        },
      })
      m.addLayer({
        id: 'vehicles-mine-label', type: 'symbol', source: 'vehicles', filter: ['get', 'mine'],
        layout: {
          'text-field': ['get', 'name'], 'text-font': ['Noto Sans Medium'], 'text-size': 10,
          'text-allow-overlap': true,
        },
        paint: { 'text-color': '#ffffff' },
      })
      map.current = m
    })
    return () => {
      map.current = null
      m.remove()
    }
  }, [token])

  // Network lines for the gym.
  useEffect(() => {
    let live = true
    const draw = (m: maplibregl.Map) =>
      fetch(`/api/shapes?lines=${encodeURIComponent(lines.join(','))}`, { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' })
        .then((r) => (r.ok ? r.json() : null))
        .then((data) => {
          if (!live || !data) return
          for (const f of data.features) f.properties.color = hex(f.properties.color)
          ;(m.getSource('network') as GeoJSONSource | undefined)?.setData(data)
        })
        .catch(() => undefined)
    whenReady(map, el, draw)
    return () => {
      live = false
    }
  }, [linesKey, token])

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
          features.push({ type: 'Feature', properties: { kind: 'walk' }, geometry: { type: 'LineString', coordinates: [a, b] } })
          continue
        }
        const color = hex(l.line?.color)
        let coords: [number, number][] = [a, b]
        try {
          const q = new URLSearchParams({ date: serviceDate, trip: l.trip_id!, from: l.from!.id, to: l.to!.id })
          const r = await fetch(`/api/shape?${q}`, { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' })
          if (r.ok) coords = (await r.json()).coordinates
        } catch {
          /* keep the straight line */
        }
        features.push({ type: 'Feature', properties: { kind: 'ride', color }, geometry: { type: 'LineString', coordinates: coords } })
        features.push({ type: 'Feature', properties: { kind: 'stop', color }, geometry: { type: 'Point', coordinates: a } })
        features.push({ type: 'Feature', properties: { kind: 'stop', color }, geometry: { type: 'Point', coordinates: b } })
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
  }, [option, serviceDate, token])

  // Live vehicles every 10 s; this also keeps the server's realtime polling active.
  useEffect(() => {
    let live = true
    const mine = new Set(myTrips)
    const tick = (force = false) => {
      const m = map.current
      if (!m || (!force && document.visibilityState !== 'visible')) return
      api
        .vehicles(token, lines)
        .then((r) => {
          if (!live) return
          const myVehicles = r.vehicles.filter((v) => mine.has(v.trip_id ?? ''))
          setVehicleCount(myVehicles.length)
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
                properties: { color: hex(v.color), mine: mine.has(v.trip_id ?? ''), name: v.line.split(' ').slice(1).join(' ') },
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
  }, [linesKey, token, myTrips.join(',')])

  return (
    <div class="map-wrap">
      <div ref={el} class="map" role="region" aria-label="Map of the trip with live vehicles" data-my-vehicles={vehicleCount ?? ''} />
      {error && <p class="map-note">{error}</p>}
      {vehicleCount === 0 && (
        <p class="map-note subtle">Your services will appear here once they're running. Other dots are vehicles on the same lines.</p>
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
