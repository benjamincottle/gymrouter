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
import { LAST_STOPS, type RideProgress } from '../countdown.ts'
import { splitAt } from '../progress.ts'
import { isDark } from '../theme.ts'
import type { Leg, Option, Vehicle } from '../types.ts'
import { INK, lineColour, textOn, token, WHITE } from '../colour.ts'
import { Callout } from '../views/ui.tsx'
import { HURRY, VEHICLE_PATHS, vehicleOf, WALKER, type Doing } from '../views/icons.tsx'

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
  return { walk: rgb(ink), ink, stop: WHITE, you: token('--you') || '#2457d6', stopInk: token('--stop-ink') || INK }
}

/**
 * Whether a vehicle is close enough to the trip to be worth framing with it: within half the trip's size (at least
 * about a kilometre) of its bounds. One still far off would shrink the trip to a corner of the map.
 */
function nearRoute(b: maplibregl.LngLatBounds, [lon, lat]: [number, number]): boolean {
  const sw = b.getSouthWest()
  const ne = b.getNorthEast()
  const dx = Math.max(0.01, (ne.lng - sw.lng) / 2)
  const dy = Math.max(0.01, (ne.lat - sw.lat) / 2)
  return lon > sw.lng - dx && lon < ne.lng + dx && lat > sw.lat - dy && lat < ne.lat + dy
}

const RIDE_W = 6 // a ride's line on the map, in pixels
const STOP_R = 3.5 // a stop along it, inside its outline: wider than the line
const STOP_EDGE = 1.5
const YOURS_R = 6 // where you get off the ride you're on: a larger white dot with the stop's dot in it, as on the rail
const YOURS_EDGE = 2
const YOURS_DOT = 2.5
const VEHICLE_R = 10 // a vehicle's disc, inside its outline
const VEHICLE_EDGE = 2.5
const GLYPH = 14 // the bus or train drawn in it
const ME_R = 11 // you: a blue disc inside a white border, as big as you are on the trip's rail
const ME_EDGE = 3
const ME_GLYPH = 16 // what you're doing, drawn in it

/**
 * A drawing in a 24 × 24 box (a vehicle from icons.tsx VEHICLE_PATHS, the walker or the runner) in one colour, as an
 * image for the map at the screen's pixel ratio: `px` across, its lines `width` thick in the box's units (`fine`,
 * the runner's speed lines, three quarters of that).
 */
function glyph(d: string, colour: string, px = GLYPH, width = 2.6, head?: { cx: number; cy: number; r: number }, fine?: string): ImageData {
  const c = document.createElement('canvas')
  c.width = c.height = Math.round(px * (window.devicePixelRatio || 1))
  const g = c.getContext('2d')!
  g.scale(c.width / 24, c.width / 24)
  g.strokeStyle = g.fillStyle = colour
  g.lineWidth = width
  g.lineCap = g.lineJoin = 'round'
  if (head) {
    g.beginPath()
    g.arc(head.cx, head.cy, head.r, 0, 2 * Math.PI)
    g.fill()
  }
  g.stroke(new Path2D(d))
  if (fine) {
    g.lineWidth = width * 0.75
    g.stroke(new Path2D(fine))
  }
  return g.getImageData(0, 0, c.width, c.height)
}

/** How strong the marker for where to wait is, against your own. */
const WAIT_STRENGTH = 0.45

/** The image for a vehicle on a line of this colour: its mode's glyph in ink or white, whichever shows on the colour. */
const vehicleIcon = (line: string, colour: string) => `vehicle-${vehicleOf(line.split(' ')[0])}-${textOn(colour) === WHITE ? 'white' : 'ink'}`


type Geometry =
  | { type: 'Point'; coordinates: [number, number] }
  | { type: 'LineString'; coordinates: [number, number][] }
  | { type: 'Polygon'; coordinates: [number, number][][] }
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

/** A filter no stop passes: the layers for your stop draw nothing until you're on a ride. */
const NO_STOP: maplibregl.FilterSpecification = ['==', ['get', 'leg'], -1]

export interface MapViewProps {
  token: string
  walks: TimedWalk[] // timed walks: their traced routes are drawn instead of the street-map ones
  places: { start?: string; end?: string } // the trip's home or gym at each end (placeKey), for its timed walks
  option: Option
  serviceDate: string
  origin: [number, number] // [lon, lat] of where the trip starts (home or gym)
  destination: [number, number]
  me?: { lat: number; lon: number; accuracy: number } | null // where you are, during a trip
  doing?: Doing // and what you're doing: you're drawn as a walker, or as the vehicle you're on
  aboard?: string // the trip you're riding: with you on the map, its vehicle isn't drawn as well
  focus?: number[] | null // during a trip, once you've set off: the legs of the step you're on (intrip.ts focusLegs)
  progress?: RideProgress | null // the ride you're on, in stops: what's behind you is dimmed, and your stop is marked
  catching?: number | null // the train you're getting to or waiting for (its leg): its platform is picked out, and where to wait on it
}

/**
 * Draws the option's legs (only the parts ridden, with the stops passed), the vehicles running them while
 * they're near your part of the trip, and you. The view fits the whole trip, or during one the step you're on (`focus`).
 */
export function MapView({ token, walks, places, option, serviceDate, origin, destination, me, doing = 'walk', aboard, focus, progress, catching }: MapViewProps) {
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
  const legBounds = useRef<(maplibregl.LngLatBounds | undefined)[]>([]) // each leg's own, by its place in the option
  const legLines = useRef<([number, number][] | undefined)[]>([]) // each ride's line, and the stops along it before the last
  const legStops = useRef<([number, number][] | undefined)[]>([])
  const legPlatforms = useRef<([number, number][] | undefined)[]>([]) // the outline of the platform each train is boarded from
  const vehicles = useRef<Vehicle[]>([]) // as last reported
  const vehicleFramed = useRef<string | null>(null) // the view (viewKey) last fitted with its vehicle in it
  // Once you've zoomed or moved the map yourself, live updates leave the view alone.
  const userMoved = useRef(false)
  // What the view fits, read when it's fitted (the map's own events fit it too, long after this render).
  const view = useRef({ option, focus, me, aboard, progress, catching })
  view.current = { option, focus, me, aboard, progress, catching }
  // The last few stops of a ride: the view closes in on what's left of it.
  const near = !!progress && progress.total - progress.passed <= LAST_STOPS
  const viewKey = (focus?.join(',') ?? '') + (near ? '|near' : '')

  // The vehicle that belongs in the view: the one you'll catch first, or the one you're waiting for (it may still be
  // on its way to your stop). Not the one you're on: you're on the map yourself.
  const vehicleInView = (b: maplibregl.LngLatBounds): [number, number] | null => {
    const { option: o, focus: legs, aboard: on } = view.current
    const ride = legs ? (legs.length === 1 ? o.legs[legs[0]] : undefined) : o.legs.find((l) => l.kind === 'ride')
    if (ride?.kind !== 'ride' || !ride.trip_id || ride.trip_id === on) return null
    const v = vehicles.current.find((x) => x.trip_id === ride.trip_id)
    return v && nearRoute(b, [v.lon, v.lat]) ? [v.lon, v.lat] : null
  }
  // What to fit: the whole trip, or the step you're on with you in it (unless your location puts you well away).
  const viewBounds = (): maplibregl.LngLatBounds | null => {
    if (!routeBounds.current) return null
    const copy = (x: maplibregl.LngLatBounds) => new maplibregl.LngLatBounds(x.getSouthWest(), x.getNorthEast())
    const { focus: legs, me: you, progress: ride } = view.current
    const parts = (legs ?? []).map((i) => legBounds.current[i]).filter((x) => x !== undefined)
    if (parts.length === 0) return copy(routeBounds.current)
    let b = copy(parts[0])
    for (const p of parts.slice(1)) b.extend(p)
    // With a few stops to go, only what's left of the ride: from the last stop behind you to yours.
    const line = ride && ride.total - ride.passed <= LAST_STOPS ? legLines.current[ride.leg] : undefined
    if (ride && line && line.length > 1) {
      const ahead = splitAt(line, ride.frac)[1]
      b = ahead.reduce((x, c) => x.extend(c), new maplibregl.LngLatBounds(ahead[0], ahead[0]))
      const behind = legStops.current[ride.leg]?.[ride.passed - 1]
      if (behind) b.extend(behind)
    }
    if (you && nearRoute(b, [you.lon, you.lat])) b.extend([you.lon, you.lat])
    return b
  }
  const viewKeyRef = useRef(viewKey)
  viewKeyRef.current = viewKey
  const frame = (m: maplibregl.Map, animate: boolean) => {
    const b = viewBounds()
    if (!b || userMoved.current) return
    const v = vehicleInView(b)
    if (v) {
      b.extend(v)
      vehicleFramed.current = viewKeyRef.current
    }
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
      m.addSource('behind', { type: 'geojson', data: fc([]) })
      m.addSource('vehicles', { type: 'geojson', data: fc([]) })
      m.addSource('me', { type: 'geojson', data: fc([]) })
      m.addSource('catching', { type: 'geojson', data: fc([]) })
      // The platform of the train you're heading for, in its line's colour, under everything else of the trip.
      m.addLayer({
        id: 'catching-fill', type: 'fill', source: 'catching', filter: ['==', ['geometry-type'], 'Polygon'],
        paint: { 'fill-color': ['get', 'color'], 'fill-opacity': 0.16 },
      })
      m.addLayer({
        id: 'catching-line', type: 'line', source: 'catching', filter: ['!=', ['geometry-type'], 'Point'],
        paint: { 'line-color': ['get', 'color'], 'line-opacity': 0.6, 'line-width': 1.5 },
        layout: { 'line-join': 'round' },
      })
      m.addLayer({
        id: 'route-ride', type: 'line', source: 'route', filter: ['==', ['get', 'kind'], 'ride'],
        paint: { 'line-color': ['get', 'color'], 'line-width': RIDE_W },
        layout: { 'line-cap': 'round', 'line-join': 'round' },
      })
      // The part of the ride you're on that's behind you, dimmed: a wash of the map's own ground over the line.
      m.addLayer({
        id: 'route-behind', type: 'line', source: 'behind',
        paint: { 'line-color': dark ? '#000000' : WHITE, 'line-opacity': 0.6, 'line-width': RIDE_W + 0.5 },
        layout: { 'line-cap': 'butt', 'line-join': 'round' },
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
      // A ride's stops, where you get on and off and those passed on the way: white dots on the line with an ink
      // outline, wider than the line.
      m.addLayer({
        id: 'route-stops', type: 'circle', source: 'route', filter: ['in', ['get', 'kind'], ['literal', ['via', 'stop']]],
        paint: {
          'circle-radius': STOP_R, 'circle-color': col.stop,
          'circle-stroke-width': STOP_EDGE, 'circle-stroke-color': INK, // black in both themes
        },
      })
      // Where you get off the ride you're on: a larger white dot with the stop's dot in it (none until you're riding).
      m.addLayer({
        id: 'route-yours', type: 'circle', source: 'route', filter: NO_STOP,
        paint: { 'circle-radius': YOURS_R, 'circle-color': col.stop, 'circle-stroke-width': YOURS_EDGE, 'circle-stroke-color': col.stopInk },
      })
      m.addLayer({
        id: 'route-yours-dot', type: 'circle', source: 'route', filter: NO_STOP,
        paint: { 'circle-radius': YOURS_DOT, 'circle-color': col.stopInk },
      })
      // Vehicles: a disc in the line's colour with a bus or train in it, all one size whatever the line is called
      // (which line it is shows in its colour, and the trip says).
      for (const [kind, d] of Object.entries(VEHICLE_PATHS)) {
        m.addImage(`vehicle-${kind}-white`, glyph(d, WHITE), { pixelRatio: window.devicePixelRatio || 1 })
        m.addImage(`vehicle-${kind}-ink`, glyph(d, INK), { pixelRatio: window.devicePixelRatio || 1 })
      }
      m.addLayer({
        id: 'vehicles-mine', type: 'circle', source: 'vehicles',
        paint: {
          'circle-radius': VEHICLE_R, 'circle-color': ['get', 'color'],
          'circle-stroke-color': col.ink, 'circle-stroke-width': VEHICLE_EDGE,
        },
      })
      m.addLayer({
        id: 'vehicles-mine-icon', type: 'symbol', source: 'vehicles',
        layout: { 'icon-image': ['get', 'icon'], 'icon-allow-overlap': true, 'icon-ignore-placement': true },
      })
      // You: a blue disc with what you're doing in it (walking, or the vehicle you're on), as on the trip's rail, and
      // a halo the size of the location's accuracy.
      m.addImage('you-walk', glyph(WALKER.body, WHITE, ME_GLYPH, 2.4, WALKER.head), { pixelRatio: window.devicePixelRatio || 1 })
      for (const [kind, d] of Object.entries(VEHICLE_PATHS)) {
        m.addImage(`you-${kind}`, glyph(d, WHITE, ME_GLYPH, 2.4), { pixelRatio: window.devicePixelRatio || 1 })
      }
      // Where to wait on that platform: your own marker at part strength, with the runner in it, under your own.
      m.addImage('you-hurry', glyph(HURRY.body, WHITE, ME_GLYPH, 2.4, HURRY.head, HURRY.streaks), { pixelRatio: window.devicePixelRatio || 1 })
      const spot: maplibregl.FilterSpecification = ['==', ['geometry-type'], 'Point']
      m.addLayer({
        id: 'wait-edge', type: 'circle', source: 'catching', filter: spot,
        paint: { 'circle-radius': ME_R + ME_EDGE + 2.5, 'circle-color': '#000000', 'circle-opacity': 0.35 * WAIT_STRENGTH, 'circle-blur': 0.5, 'circle-translate': [0, 1] },
      })
      m.addLayer({
        id: 'wait', type: 'circle', source: 'catching', filter: spot,
        paint: {
          'circle-radius': ME_R, 'circle-color': col.you, 'circle-opacity': WAIT_STRENGTH,
          'circle-stroke-color': col.stop, 'circle-stroke-width': ME_EDGE, 'circle-stroke-opacity': WAIT_STRENGTH,
        },
      })
      m.addLayer({
        id: 'wait-doing', type: 'symbol', source: 'catching', filter: spot,
        layout: { 'icon-image': 'you-hurry', 'icon-allow-overlap': true, 'icon-ignore-placement': true },
        paint: { 'icon-opacity': WAIT_STRENGTH },
      })
      m.addLayer({
        id: 'me-accuracy', type: 'circle', source: 'me',
        paint: {
          'circle-radius': ['interpolate', ['exponential', 2], ['zoom'], 10, ['get', 'px10'], 20, ['*', ['get', 'px10'], 1024]],
          'circle-color': col.you, 'circle-opacity': 0.12,
        },
      })
      // A soft dark edge under it, as on the trip's rail: on a pale map the white border would vanish without one.
      m.addLayer({
        id: 'me-edge', type: 'circle', source: 'me',
        paint: { 'circle-radius': ME_R + ME_EDGE + 2.5, 'circle-color': '#000000', 'circle-opacity': 0.35, 'circle-blur': 0.5, 'circle-translate': [0, 1] },
      })
      m.addLayer({
        id: 'me', type: 'circle', source: 'me',
        paint: { 'circle-radius': ME_R, 'circle-color': col.you, 'circle-stroke-color': col.stop, 'circle-stroke-width': ME_EDGE },
      })
      m.addLayer({
        id: 'me-doing', type: 'symbol', source: 'me',
        layout: { 'icon-image': ['get', 'icon'], 'icon-allow-overlap': true, 'icon-ignore-placement': true },
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
      const perLeg: (maplibregl.LngLatBounds | undefined)[] = []
      const lines: ([number, number][] | undefined)[] = []
      const stopsOf: ([number, number][] | undefined)[] = []
      const platforms: ([number, number][] | undefined)[] = []
      const legAlong = (i: number, coords: [number, number][]) => {
        perLeg[i] = coords.reduce((x, c) => x.extend(c), new maplibregl.LngLatBounds(coords[0], coords[0]))
        bounds.extend(perLeg[i]!) // the line can bow out past its ends
      }
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
          legAlong(i, coords)
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
          platforms[i] = shape.platform
        } catch {
          /* keep the straight line */
        }
        legAlong(i, coords)
        lines[i] = coords
        stopsOf[i] = via
        // Each stop knows its ride and its place on it (`seq`: 0 where you get on, then in order, the last where you
        // get off), so the ones behind you can go quiet.
        features.push({ type: 'Feature', properties: { kind: 'ride', color }, geometry: { type: 'LineString', coordinates: coords } })
        for (const [k, p] of via.entries()) features.push({ type: 'Feature', properties: { kind: 'via', color, leg: i, seq: k + 1 }, geometry: { type: 'Point', coordinates: p } })
        // On the line's ends (the server cuts the shape where each stop sits beside it), not at the kerb.
        features.push({ type: 'Feature', properties: { kind: 'stop', color, leg: i, seq: 0 }, geometry: { type: 'Point', coordinates: coords[0] } })
        features.push({ type: 'Feature', properties: { kind: 'stop', color, leg: i, seq: via.length + 1 }, geometry: { type: 'Point', coordinates: coords[coords.length - 1] } })
      }
      if (!live) return
      ;(m.getSource('route') as GeoJSONSource | undefined)?.setData(fc(features))
      routeBounds.current = bounds
      legBounds.current = perLeg
      legLines.current = lines
      legStops.current = stopsOf
      legPlatforms.current = platforms
      drawCatching(m)
      drawProgress(m)
      frame(m, false)
    }
    whenReady(map, el, draw)
    return () => {
      live = false
    }
  }, [option, serviceDate, token, walks])

  // The ride you're on: the line behind you dimmed, the stops behind you quiet, and your stop marked.
  // The train you're heading for: the platform it's boarded from, and where on it to wait.
  const drawCatching = (m: maplibregl.Map) => {
    const src = m.getSource('catching') as GeoJSONSource | undefined
    if (!src) return
    const i = view.current.catching
    const leg = i == null ? undefined : view.current.option.legs[i]
    const features: Feature[] = []
    if (i != null && leg?.board) {
      const outline = legPlatforms.current[i]
      const color = lineColour(leg.line?.color)
      if (outline && outline.length > 1) {
        const closed = outline.length > 3 && outline[0][0] === outline[outline.length - 1][0] && outline[0][1] === outline[outline.length - 1][1]
        features.push({ type: 'Feature', properties: { color }, geometry: closed ? { type: 'Polygon', coordinates: [outline] } : { type: 'LineString', coordinates: outline } })
      }
      if (leg.board.spot) features.push({ type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: leg.board.spot } })
    }
    src.setData(fc(features))
  }

  const drawProgress = (m: maplibregl.Map) => {
    if (!m.getLayer('route-stops')) return
    const ride = view.current.progress
    const line = ride ? legLines.current[ride.leg] : undefined
    ;(m.getSource('behind') as GeoJSONSource | undefined)?.setData(
      fc(ride && line && line.length > 1 ? [{ type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates: splitAt(line, ride.frac)[0] } }] : []),
    )
    const behind: maplibregl.ExpressionSpecification = ride
      ? ['all', ['==', ['get', 'leg'], ride.leg], ['<=', ['get', 'seq'], ride.passed]]
      : ['literal', false]
    m.setPaintProperty('route-stops', 'circle-radius', ['case', behind, STOP_R - 1, STOP_R])
    m.setPaintProperty('route-stops', 'circle-opacity', ['case', behind, 0.5, 1])
    m.setPaintProperty('route-stops', 'circle-stroke-opacity', ['case', behind, 0.3, 1])
    const yours: maplibregl.FilterSpecification = ride ? ['all', ['==', ['get', 'leg'], ride.leg], ['==', ['get', 'seq'], ride.total]] : NO_STOP
    m.setFilter('route-yours', yours)
    m.setFilter('route-yours-dot', yours)
  }
  useEffect(() => {
    if (map.current) drawProgress(map.current)
  }, [progress?.leg, progress?.passed, progress?.total, progress?.frac])

  // The train you're heading for, as it changes: on to the next one after a change, and gone once you're on board.
  useEffect(() => {
    whenReady(map, el, (m) => drawCatching(m))
  }, [catching])

  // On to another step of the trip: the view follows.
  useEffect(() => {
    if (map.current) frame(map.current, true)
  }, [viewKey])

  // The vehicles last reported, drawn without the one you're riding: you're on the map yourself.
  const riding = me ? aboard : undefined
  const ridingRef = useRef(riding)
  ridingRef.current = riding
  const drawVehicles = (m: maplibregl.Map) =>
    (m.getSource('vehicles') as GeoJSONSource | undefined)?.setData(
      fc(
        vehicles.current
          .filter((v) => !ridingRef.current || v.trip_id !== ridingRef.current)
          .map((v): Feature => ({
            type: 'Feature',
            properties: { color: lineColour(v.color), icon: vehicleIcon(v.line, lineColour(v.color)) },
            geometry: { type: 'Point', coordinates: [v.lon, v.lat] },
          })),
      ),
    )
  useEffect(() => {
    if (map.current) drawVehicles(map.current)
  }, [riding])

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
          vehicles.current = r.vehicles
          drawVehicles(m)
          // The first time the view's vehicle shows up, make room for it.
          const b = viewBounds()
          if (b && vehicleInView(b) && vehicleFramed.current !== viewKeyRef.current) frame(m, true)
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
      src?.setData(fc([{ type: 'Feature', properties: { px10, icon: `you-${doing}` }, geometry: { type: 'Point', coordinates: [me.lon, me.lat] } }]))
    })
  }, [me?.lat, me?.lon, me?.accuracy, doing])

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
