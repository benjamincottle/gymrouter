// Section icons for Settings: ink line drawings, each with one part filled (class "acc").
import type { ComponentChildren } from 'preact'
import { IconButton } from './ui.tsx'

function Icon({ children }: { children: ComponentChildren }) {
  return (
    <svg class="icon" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
      {children}
    </svg>
  )
}

/** The vehicle drawn for a line's mode, on the trip's rail and on the map: a train for anything on rails, a ferry, otherwise a bus. */
export type Vehicle = 'rail' | 'ferry' | 'bus'

export const vehicleOf = (mode?: string): Vehicle =>
  mode === 'ferry' ? 'ferry' : mode === 'train' || mode === 'metro' || mode === 'light-rail' || mode === 'regional-train' ? 'rail' : 'bus'

/** Each vehicle as a path of round-ended lines in a 24 × 24 box. */
export const VEHICLE_PATHS: Record<Vehicle, string> = {
  ferry: 'M3 14.5 H21 L18.5 19.5 H5.5 Z M6.5 14.5 V9.5 H17.5 V14.5 M12 9.5 V5.5',
  rail: 'M8 3.5 H16 A3 3 0 0 1 19 6.5 V14 A3 3 0 0 1 16 17 H8 A3 3 0 0 1 5 14 V6.5 A3 3 0 0 1 8 3.5 Z M5 10.5 H19 M8.5 17 L6.5 21 M15.5 17 L17.5 21',
  bus: 'M7 3.5 H17 A2 2 0 0 1 19 5.5 V18 H5 V5.5 A2 2 0 0 1 7 3.5 Z M5 11.5 H19 M8 18 V20.5 M16 18 V20.5',
}

/** The house from the trip's rail: where a trip starts, or ends, at home. */
export const HOUSE = 'M3.5 11 L12 4 L20.5 11 M5.5 9.5 V20 H18.5 V9.5 M10 20 V14 H14 V20'

// A gym's climbing shoe, toe down and to the right, in four parts. Its outline (heel, sole, toe, tongue) and the loop
// on its heel:
const SHOE_OUTLINE =
  'M5.28 6.44 C3.74 7.78 2.4 9.89 2.5 12 C2.69 13.16 3.55 13.92 4.9 13.92 L8.35 14.02 C11.23 14.12 13.54 16.04 15.26 17.67 L19.2 21.41 C19.87 22.08 21.02 21.89 21.31 20.84 C21.98 18.15 21.41 15.27 19.87 12.96 L15.84 7.01 C15.26 6.05 14.11 5.28 13.15 5.86 C12.38 6.44 12 7.4 11.04 7.88 C9.7 8.45 8.54 7.01 7.39 6.24 C6.62 5.76 5.86 5.86 5.28 6.44 Z M5.18 6.44 C4.22 3.94 5.09 2.02 6.62 2.02 C8.16 2.02 8.93 3.75 8.06 6.05'
// The rubber under the sole line, filled on a light page:
const SHOE_SOLE =
  'M2.88 10.28 C4.9 11.81 7.01 12 8.93 12.1 C11.62 12.29 13.73 13.54 15.65 15.65 C17.18 17.38 19.3 18.15 21.41 17.96 C21.63 18.91 21.55 19.89 21.31 20.84 C21.02 21.89 19.87 22.08 19.2 21.41 L15.26 17.67 C13.54 16.04 11.23 14.12 8.35 14.02 L4.9 13.92 C3.55 13.92 2.69 13.16 2.5 12 C2.47 11.41 2.56 10.81 2.73 10.23 Z'
// The upper, filled on a dark page instead: it stops short of the outline by a sole as thick as the filled one.
const SHOE_UPPER =
  'M4.01 8.79 C5.57 9.98 7.45 10.15 9.02 10.23 C12.3 10.46 14.9 12.05 17.03 14.39 C18.05 15.54 19.52 16.25 21.24 16.09 C20.98 14.98 20.51 13.92 19.87 12.96 L15.84 7.01 C15.26 6.05 14.11 5.28 13.15 5.86 C12.38 6.44 12 7.4 11.04 7.88 C9.7 8.45 8.54 7.01 7.39 6.24 C6.62 5.76 5.86 5.86 5.28 6.44 C4.63 7 4.02 7.71 3.53 8.49 Z'
// The two straps across the upper:
const SHOE_STRAPS = 'M11.33 8.07 C12.96 8.74 14.88 8.64 16.7 8.26 M14.3 10.18 C15.65 10.76 17.18 10.85 18.72 10.95'

/** The shoe, for an svg with a 24 × 24 box: in its own finer lines, its sole the dark part in either theme (the --shoe tokens). */
export const Shoe = () => (
  <g class="shoe">
    <path class="upper" d={SHOE_UPPER} />
    <path d={SHOE_OUTLINE} />
    <path class="sole" d={SHOE_SOLE} />
    <path class="straps" d={SHOE_STRAPS} />
  </g>
)

/** Which way a trip goes, in the rail's own drawings: the house, an arrow, the shoe (or the shoe, an arrow, the house). */
export function DirectionGlyph({ toGym }: { toGym: boolean }) {
  const house = (
    <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <path d={HOUSE} />
    </svg>
  )
  const shoe = (
    <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
      <Shoe />
    </svg>
  )
  return (
    <>
      {toGym ? house : shoe}
      <svg class="icon" viewBox="0 0 16 24" width="12" height="18" aria-hidden="true">
        <path d="M2 12 H13 M9 7.5 L13.5 12 L9 16.5" />
      </svg>
      {toGym ? shoe : house}
    </>
  )
}

export const IconHome = () => (
  <Icon>
    <path d="M3.5 11 L12 4 L20.5 11 M5.5 9.5 V20 H18.5 V9.5" />
    <rect class="acc" x="10" y="13.5" width="4" height="6.5" />
  </Icon>
)

export const IconGym = () => (
  <Icon>
    <Shoe />
  </Icon>
)

export const IconWalk = () => (
  <Icon>
    <ellipse cx="8.5" cy="14.5" rx="2.6" ry="4.2" transform="rotate(-12 8.5 14.5)" />
    <ellipse class="acc" cx="15.5" cy="8.5" rx="2.6" ry="4.2" transform="rotate(12 15.5 8.5)" />
    <path d="M7.6 20.4 L9.6 20" />
    <path d="M14.4 14.4 L16.4 14.8" />
  </Icon>
)

export const IconRisk = () => (
  <Icon>
    <rect class="risk-safe-fill" x="3" y="9" width="5" height="6" rx="1" />
    <rect class="risk-tight-fill" x="9.5" y="9" width="5" height="6" rx="1" />
    <rect class="risk-risk-fill" x="16" y="9" width="5" height="6" rx="1" />
  </Icon>
)

export const IconColour = () => (
  <Icon>
    <path class="acc" d="M5 17 L11 5 L13.6 6.3 L7.6 18.3 Z" />
    <path d="M19 17 L13 5 L10.4 6.3 L16.4 18.3 Z" />
  </Icon>
)

export const IconTimer = () => (
  <Icon>
    <circle cx="12" cy="13.5" r="7" />
    <path d="M10 3.5 H14 M12 3.5 V6.5 M18 7.5 L19.5 6" />
    <path class="acc-line" d="M12 13.5 L15 10" />
  </Icon>
)

export const IconDevices = () => (
  <Icon>
    <rect x="3" y="5" width="9" height="14" rx="1.6" />
    <rect x="13.5" y="8" width="7.5" height="11" rx="1.4" />
    <path class="acc-line" d="M6 15.5 H9 M16 15.5 H18.5" />
  </Icon>
)

export const IconPhone = () => (
  <Icon>
    <rect x="6.5" y="3" width="11" height="18" rx="2" />
    <circle class="acc" cx="12" cy="17.5" r="1.3" />
  </Icon>
)

/** Someone running, heading right, with speed lines behind: on a gym's row, starts the fastest trip there. */
export const IconHurry = () => (
  <svg class="icon hurry" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
    <circle class="ink" cx="18" cy="4.2" r="2" />
    <path d="M16 7.6 L12.6 13 L16.4 15.4 L15.4 20.4 M12.6 13 L9.8 16.6 L6 16.2 M15.4 8.6 L18.6 11 L21.4 9.6 M15.4 8.6 L11.8 8.2 L9.8 10.8" />
    <path class="streaks" d="M2 7 H6.5 M1.5 11.5 H5.5" />
  </svg>
)

/** Settings, in the header: a cog. */
export const IconSettings = () => (
  <svg class="icon" viewBox="0 0 24 24" width="26" height="26" aria-hidden="true">
    <path d="M9.75 4.74 L10.01 2.2 L13.99 2.2 L14.25 4.74 L15.55 5.28 L17.53 3.67 L20.33 6.47 L18.72 8.45 L19.26 9.75 L21.8 10.01 L21.8 13.99 L19.26 14.25 L18.72 15.55 L20.33 17.53 L17.53 20.33 L15.55 18.72 L14.25 19.26 L13.99 21.8 L10.01 21.8 L9.75 19.26 L8.45 18.72 L6.47 20.33 L3.67 17.53 L5.28 15.55 L4.74 14.25 L2.2 13.99 L2.2 10.01 L4.74 9.75 L5.28 8.45 L3.67 6.47 L6.47 3.67 L8.45 5.28 Z" />
    <circle cx="12" cy="12" r="3.3" />
  </svg>
)

/** Small icon buttons for list rows: edit (pencil) and delete (bin). */
export function EditButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <IconButton label={label} onClick={onClick}>
      <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
        <path d="M4 20 L4.8 16.2 L15.6 5.4 a2 2 0 0 1 2.8 0 l0.2 0.2 a2 2 0 0 1 0 2.8 L7.8 19.2 Z M13.8 7.2 L16.8 10.2" />
      </svg>
    </IconButton>
  )
}

export function DeleteButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <IconButton label={label} danger onClick={onClick}>
      <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
        <path d="M4.5 6.5 H19.5 M9.5 6.5 V4.5 H14.5 V6.5 M6.5 6.5 L7.5 20 H16.5 L17.5 6.5 M10.3 10 V16.5 M13.7 10 V16.5" />
      </svg>
    </IconButton>
  )
}
