// The live map. Loaded on demand (MapLibre is large), so it's split from the main bundle.
import { useEffect, useRef, useState } from 'preact/hooks'
import * as maplibregl from 'maplibre-gl'
import type { GeoJSONSource, StyleSpecification } from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url'
import 'maplibre-gl/dist/maplibre-gl.css'
import { FetchSource, PMTiles, Protocol } from 'pmtiles'
import { layers, namedFlavor } from '@protomaps/basemaps'
import { ApiError, api } from '../api.ts'
import type { RideProgress } from '../countdown.ts'
import { splitAt } from '../progress.ts'
import { useLiveEvents, useVisible } from '../hooks.ts'
import { every } from '../live.ts'
import { isDark } from '../theme.ts'
import type { Leg, Option, Vehicle } from '../types.ts'
import { INK, lineColour, textOn, token, WHITE } from '../colour.ts'
import { Callout } from '../views/ui.tsx'
import { HOUSE, HURRY, SHOE_OUTLINE, SHOE_SOLE, SHOE_STRAPS, SHOE_UPPER, VEHICLE_PATHS, vehicleOf, WALKER, type Doing } from '../views/icons.tsx'

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

/**
 * The dark map in the colours of Google Maps' night style, over Protomaps' dark flavour (which is near black): slate
 * navy land, deep navy water, slate roads with warm highways, and warm place names.
 */
const NIGHT: Partial<ReturnType<typeof namedFlavor>> = {
  background: '#17263c', water: '#17263c', ocean_label: '#515c6d',
  earth: '#242f3e', buildings: '#2b3748', pedestrian: '#2b3646', pier: '#2f3948', runway: '#38414e',
  park_a: '#263c3f', park_b: '#263c3f', wood_a: '#24383a', wood_b: '#24383a', scrub_a: '#263a3c', scrub_b: '#263a3c',
  zoo: '#263c3f', hospital: '#2a3446', school: '#2a3446', industrial: '#283445', military: '#283445', aerodrome: '#283445',
  beach: '#2c3646', sand: '#2c3646', glacier: '#2e3a4c',
  other: '#2f3948', minor_service: '#2f3948', minor_a: '#38414e', minor_b: '#38414e', link: '#38414e', major: '#45505f',
  highway: '#746855',
  minor_service_casing: '#212a37', minor_casing: '#212a37', link_casing: '#212a37', major_casing_early: '#212a37',
  major_casing_late: '#212a37', highway_casing_early: '#1f2835', highway_casing_late: '#1f2835',
  tunnel_other: '#2c3542', tunnel_minor: '#2c3542', tunnel_link: '#2c3542', tunnel_major: '#2c3542', tunnel_highway: '#4a4538',
  tunnel_other_casing: '#1f2835', tunnel_minor_casing: '#1f2835', tunnel_link_casing: '#1f2835', tunnel_major_casing: '#1f2835',
  tunnel_highway_casing: '#1f2835',
  bridges_other: '#2f3948', bridges_minor: '#38414e', bridges_link: '#38414e', bridges_major: '#45505f', bridges_highway: '#746855',
  bridges_other_casing: '#212a37', bridges_minor_casing: '#212a37', bridges_link_casing: '#212a37', bridges_major_casing: '#212a37',
  bridges_highway_casing: '#1f2835',
  railway: '#3b4658', boundaries: '#4b6878',
  roads_label_minor: '#9ca5b3', roads_label_minor_halo: '#212a37', roads_label_major: '#9ca5b3', roads_label_major_halo: '#212a37',
  subplace_label: '#d59563', subplace_label_halo: '#242f3e', city_label: '#d59563', city_label_halo: '#242f3e',
  state_label: '#746855', state_label_halo: '#242f3e', country_label: '#9ca5b3', address_label: '#746855', address_label_halo: '#242f3e',
  landcover: {
    grassland: '#263c3f', barren: '#2a3446', urban_area: '#283445', farmland: '#263a3c', glacier: '#2e3a4c', scrub: '#263a3c', forest: '#24383a',
  },
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
    layers: layers('protomaps', dark ? { ...namedFlavor('dark'), ...NIGHT } : namedFlavor(flavor), { lang: 'en' }),
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
  return { walk: rgb(ink), ink, stop: WHITE, you: token('--you') || '#2457d6', stopInk: token('--stop-ink') || INK, wall: token('--wall') || WHITE }
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

const PLACE_PX = 30 // a home or gym on the map: a disc as big as you are, nearly

/**
 * A home or a gym as the map draws it, as on the trip's rail: the house or the shoe, in ink on the wall's colour inside
 * an ink ring. The shoe's sole is the dark part in either theme, as everywhere (on a dark page the upper is filled).
 */
function placeIcon(kind: 'home' | 'gym', ink: string, wall: string, dark: boolean): ImageData {
  const c = document.createElement('canvas')
  const ratio = window.devicePixelRatio || 1
  c.width = c.height = Math.round(PLACE_PX * ratio)
  const g = c.getContext('2d')!
  g.scale(ratio, ratio)
  g.beginPath()
  g.arc(PLACE_PX / 2, PLACE_PX / 2, PLACE_PX / 2 - 1.5, 0, 2 * Math.PI)
  g.fillStyle = wall
  g.fill()
  g.lineWidth = 2
  g.strokeStyle = ink
  g.stroke()
  const box = 18 // the drawing, in its 24 box, centred
  g.translate((PLACE_PX - box) / 2, (PLACE_PX - box) / 2)
  g.scale(box / 24, box / 24)
  g.lineCap = g.lineJoin = 'round'
  g.strokeStyle = g.fillStyle = ink
  if (kind === 'home') {
    g.lineWidth = 2.2
    g.stroke(new Path2D(HOUSE))
  } else {
    g.lineWidth = 1.5
    g.fill(new Path2D(dark ? SHOE_UPPER : SHOE_SOLE))
    g.stroke(new Path2D(SHOE_OUTLINE))
    g.strokeStyle = dark ? wall : ink
    g.stroke(new Path2D(SHOE_STRAPS))
  }
  return g.getImageData(0, 0, c.width, c.height)
}

/** A home or a gym to draw: the ends of a trip, or (before one is chosen) your home and your gyms, a gym to tap. */
export interface MapPlace {
  kind: 'home' | 'gym'
  lon: number
  lat: number
  id?: string // a gym that can be chosen from the map (onPlace)
  name?: string // shown beside it
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

/**
 * Hands the view back to the trip once you've moved the map yourself: a map control (square, like the zoom and
 * zoom buttons) that shows only while the map isn't following the trip.
 */
class RecentreControl implements maplibregl.IControl {
  private el = document.createElement('div')

  constructor(onClick: () => void) {
    this.el.className = 'maplibregl-ctrl maplibregl-ctrl-group recentre'
    const b = document.createElement('button')
    b.type = 'button'
    b.title = 'Back to the trip'
    b.setAttribute('aria-label', 'Back to the trip')
    // Four corners of a frame around a dot: the trip, fitted to the map.
    b.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5"/><circle cx="12" cy="12" r="2.5"/></svg>'
    b.addEventListener('click', onClick)
    this.el.append(b)
    this.show(false)
  }

  show(on: boolean) {
    this.el.style.display = on ? '' : 'none'
  }

  onAdd() {
    return this.el
  }

  onRemove() {
    this.el.remove()
  }
}

const fc = (features: Feature[]) => ({ type: 'FeatureCollection' as const, features })

/** A filter no stop passes: the layers for your stop draw nothing until you're on a ride. */
const NO_STOP: maplibregl.FilterSpecification = ['==', ['get', 'leg'], -1]

const VEHICLES_MS = 30_000 // without word from the server that new positions are in (useLiveEvents)
const VEHICLES_UNHEARD_MS = 10_000 // while that word can't be heard

export interface MapViewProps {
  token: string
  option?: Option // the trip to draw (without one, just the places)
  serviceDate?: string
  places?: MapPlace[] // the home and the gym at the trip's ends, or your home and gyms before one is chosen
  onPlace?: (id: string) => void // a gym tapped on the map
  me?: { lat: number; lon: number; accuracy: number } | null // where you are, during a trip
  doing?: Doing // and what you're doing: you're drawn as a walker, or as the vehicle you're on
  aboard?: string // the trip you're riding: with you on the map, its vehicle isn't drawn as well
  focus?: number[] | null // during a trip, once you've set off: the legs of the step you're on (intrip.ts focusLegs)
  progress?: RideProgress | null // the ride you're on, in stops: what's behind you is dimmed, and your stop is marked
  catching?: number | null // the train you're getting to or waiting for (its leg): its platform is picked out, and where to wait on it
}

/**
 * Draws the option's legs (only the parts ridden, with the stops passed), the vehicles running them while
 * they're near your part of the trip, you, and the home and gym at its ends. The view fits the whole trip, or during
 * one the step you're on (`focus`). Without an option it draws the places alone, and fits them.
 */
export function MapView({ token, option, serviceDate, places = [], onPlace, me, doing = 'walk', aboard, focus, progress, catching }: MapViewProps) {
  const el = useRef<HTMLDivElement>(null)
  const map = useRef<maplibregl.Map | null>(null)
  const [error, setError] = useState('')
  const onPlaceRef = useRef(onPlace)
  onPlaceRef.current = onPlace
  const placesKey = JSON.stringify(places)
  const routeBounds = useRef<maplibregl.LngLatBounds | null>(null)
  const legBounds = useRef<(maplibregl.LngLatBounds | undefined)[]>([]) // each leg's own, by its place in the option
  const legLines = useRef<([number, number][] | undefined)[]>([]) // each ride's line
  const legPlatforms = useRef<([number, number][] | undefined)[]>([]) // the outline of the platform each train is boarded from
  const vehicles = useRef<Vehicle[]>([]) // as last reported
  const vehicleFramed = useRef<string | null>(null) // the view (viewKey) last fitted with its vehicle in it
  // Once you've zoomed or moved the map yourself, live updates leave the view alone.
  const userMoved = useRef(false)
  // What the view fits, read when it's fitted (the map's own events fit it too, long after this render).
  const view = useRef({ option, focus, me, aboard, progress, catching })
  view.current = { option, focus, me, aboard, progress, catching }
  const viewKey = focus?.join(',') ?? ''

  // The vehicle that belongs in the view: the one you'll catch first, or the one you're waiting for (it may still be
  // on its way to your stop). Not the one you're on: you're on the map yourself.
  const vehicleInView = (b: maplibregl.LngLatBounds): [number, number] | null => {
    const { option: o, focus: legs, aboard: on } = view.current
    if (!o) return null
    const ride = legs ? (legs.length === 1 ? o.legs[legs[0]] : undefined) : o.legs.find((l) => l.kind === 'ride')
    if (ride?.kind !== 'ride' || !ride.trip_id || ride.trip_id === on) return null
    const v = vehicles.current.find((x) => x.trip_id === ride.trip_id)
    return v && nearRoute(b, [v.lon, v.lat]) ? [v.lon, v.lat] : null
  }
  // What to fit: the whole trip, or the step you're on with you in it (unless your location puts you well away).
  const viewBounds = (): maplibregl.LngLatBounds | null => {
    if (!routeBounds.current) return null
    const copy = (x: maplibregl.LngLatBounds) => new maplibregl.LngLatBounds(x.getSouthWest(), x.getNorthEast())
    const { focus: legs, me: you } = view.current
    const parts = (legs ?? []).map((i) => legBounds.current[i]).filter((x) => x !== undefined)
    if (parts.length === 0) return copy(routeBounds.current)
    const b = copy(parts[0])
    for (const p of parts.slice(1)) b.extend(p)
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
    // The trip fills the map: just enough room for the zoom buttons on the right.
    m.fitBounds(b, { padding: { top: 36, bottom: 36, left: 28, right: 64 }, maxZoom: 16, duration: animate ? 600 : 0 })
  }
  // The rides, as the vehicles endpoint wants them: <trip>|<board stop>|<alight stop>.
  const rides = (option?.legs ?? [])
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
      center: places[0] ? [places[0].lon, places[0].lat] : [151.2093, -33.8688],
      zoom: 12,
      attributionControl: { compact: true },
      maxBounds: [149.9, -34.6, 152.0, -33.0],
    })
    m.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    const recentre = new RecentreControl(() => {
      userMoved.current = false
      recentre.show(false)
      frame(m, true)
    })
    m.addControl(recentre, 'top-right')
    const moved = () => {
      userMoved.current = true
      recentre.show(true)
    }
    // Gestures and the zoom buttons carry the event that caused them; programmatic framing doesn't.
    m.on('movestart', (e) => {
      if (e.originalEvent) moved()
    })
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
      m.addSource('places', { type: 'geojson', data: fc([]) })
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
      // The home and the gym at the trip's ends (or yours, before a gym is chosen): the house and the shoe in a ring.
      m.addImage('place-home', placeIcon('home', col.ink, col.wall, dark), { pixelRatio: window.devicePixelRatio || 1 })
      m.addImage('place-gym', placeIcon('gym', col.ink, col.wall, dark), { pixelRatio: window.devicePixelRatio || 1 })
      m.addLayer({
        id: 'places', type: 'symbol', source: 'places',
        layout: {
          'icon-image': ['get', 'icon'], 'icon-allow-overlap': true, 'icon-ignore-placement': true,
          'text-field': ['get', 'name'], 'text-font': ['Noto Sans Medium'], 'text-size': 13, 'text-anchor': 'left',
          'text-offset': [1.4, 0], 'text-optional': true,
        },
        paint: { 'text-color': col.ink, 'text-halo-color': col.wall, 'text-halo-width': 1.5 },
      })
      m.on('click', 'places', (e) => {
        const id = e.features?.[0]?.properties?.id
        if (id && onPlaceRef.current) onPlaceRef.current(String(id))
      })
      m.on('mouseenter', 'places', (e) => {
        if (onPlaceRef.current && e.features?.[0]?.properties?.id) m.getCanvas().style.cursor = 'pointer'
      })
      m.on('mouseleave', 'places', () => (m.getCanvas().style.cursor = ''))
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
      // The places, and the trip's ends (the first and the last of them, when there's a trip).
      const ends = places.map((p): [number, number] => [p.lon, p.lat])
      const origin = ends[0]
      const destination = ends[ends.length - 1]
      const bounds = new maplibregl.LngLatBounds(origin ?? [151.2093, -33.8688], origin ?? [151.2093, -33.8688])
      for (const p of ends) bounds.extend(p)
      const perLeg: (maplibregl.LngLatBounds | undefined)[] = []
      const lines: ([number, number][] | undefined)[] = []
      const platforms: ([number, number][] | undefined)[] = []
      const legAlong = (i: number, coords: [number, number][]) => {
        perLeg[i] = coords.reduce((x, c) => x.extend(c), new maplibregl.LngLatBounds(coords[0], coords[0]))
        bounds.extend(perLeg[i]!) // the line can bow out past its ends
      }
      const point = (l: Leg, end: 'from' | 'to'): [number, number] | null => {
        const s = l[end]
        return s ? [s.lon, s.lat] : null
      }
      const legs = option?.legs ?? []
      for (const [i, l] of legs.entries()) {
        const a = point(l, 'from') ?? (i === 0 ? origin : null)
        const b = point(l, 'to') ?? (i === legs.length - 1 ? destination : null)
        if (!a || !b) continue
        bounds.extend(a).extend(b)
        if (l.kind === 'walk') {
          // Along the streets when the server knew the way, else a straight line.
          const coords = l.path && l.path.length > 1 ? l.path : [a, b]
          legAlong(i, coords)
          features.push({ type: 'Feature', properties: { kind: 'walk' }, geometry: { type: 'LineString', coordinates: coords } })
          continue
        }
        const color = lineColour(l.line?.color)
        let coords: [number, number][] = [a, b]
        let via: [number, number][] = []
        try {
          const shape = await api.shape(token, serviceDate!, l.trip_id!, l.from!.id, l.to!.id)
          coords = shape.coordinates
          via = shape.stops ?? []
          platforms[i] = shape.platform
        } catch {
          /* keep the straight line */
        }
        legAlong(i, coords)
        lines[i] = coords
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
      ;(m.getSource('places') as GeoJSONSource | undefined)?.setData(
        fc(places.map((p) => ({
          type: 'Feature', properties: { icon: `place-${p.kind}`, name: p.name ?? '', id: p.id ?? '' },
          geometry: { type: 'Point', coordinates: [p.lon, p.lat] },
        }))),
      )
      routeBounds.current = bounds
      legBounds.current = perLeg
      legLines.current = lines
      legPlatforms.current = platforms
      drawCatching(m)
      drawProgress(m)
      frame(m, false)
    }
    whenReady(map, el, draw)
    return () => {
      live = false
    }
  }, [option, serviceDate, token, placesKey])

  // The ride you're on: the line behind you dimmed, the stops behind you quiet, and your stop marked.
  // The train you're heading for: the platform it's boarded from, and where on it to wait.
  const drawCatching = (m: maplibregl.Map) => {
    const src = m.getSource('catching') as GeoJSONSource | undefined
    if (!src) return
    const i = view.current.catching
    const leg = i == null ? undefined : view.current.option?.legs[i]
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

  // Live vehicles as soon as the server says new positions are in (about every 15 s), else every 30 s, or every 10 s
  // while the server can't be heard. Fetching them also keeps the server's realtime polling going.
  const visible = useVisible()
  const events = useLiveEvents(token, visible && rides.length > 0)
  const connectedRef = useRef(events.connected)
  connectedRef.current = events.connected
  const vehicleFetches = useRef<ReturnType<typeof every> | null>(null)
  useEffect(() => {
    let live = true
    const tick = (force = false) => {
      const m = map.current
      if (!m || rides.length === 0 || (!force && document.visibilityState !== 'visible')) return
      api
        .vehicles(token, option?.lines ?? [], rides)
        .then((r) => {
          if (!live) return
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
    // Once the map is ready: now, and again after a while unless new positions come in first (the wait starts again
    // from each fetch).
    let force = true // the first fetch goes whether or not the page is in view
    const fetches = every(() => {
      tick(force)
      force = false
    }, () => (connectedRef.current ? VEHICLES_MS : VEHICLES_UNHEARD_MS))
    whenReady(map, el, () => {
      if (!live) return
      vehicleFetches.current = fetches
      fetches.run()
    })
    return () => {
      live = false
      fetches.stop()
      vehicleFetches.current = null
    }
  }, [token, ridesKey])
  useEffect(() => {
    if (events.vehicles > 0) vehicleFetches.current?.run()
  }, [events.vehicles])
  useEffect(() => vehicleFetches.current?.retime(), [events.connected]) // the wait changes with it

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
      <div ref={el} class="map" role="region" aria-label={option ? 'Map of the trip with live vehicles' : 'Map of your home and gyms'} />
      {error && <Callout tone="caution">{error}</Callout>}
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
