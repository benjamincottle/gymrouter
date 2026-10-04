// Section icons for Settings: ink line drawings, each with one part in the highlight colour (class "acc").
import type { ComponentChildren } from 'preact'

function Icon({ children }: { children: ComponentChildren }) {
  return (
    <svg class="icon" viewBox="0 0 24 24" width="24" height="24" aria-hidden="true">
      {children}
    </svg>
  )
}

/** The faceted hold from the app icon, scaled to fit around (12, 11). */
export const HOLD = 'M4.5 12.5 L6.5 6.5 L12 3.8 L17.8 5.8 L19.5 11.8 L15.8 16 L8.2 16.4 Z'

export const IconHome = () => (
  <Icon>
    <path d="M3.5 11 L12 4 L20.5 11 M5.5 9.5 V20 H18.5 V9.5" />
    <rect class="acc" x="10" y="13.5" width="4" height="6.5" />
  </Icon>
)

export const IconGym = () => (
  <Icon>
    <path class="acc" d={HOLD} />
    <circle cx="12" cy="10.5" r="1.6" class="ink" />
    <path d="M7 20.5 H17" />
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
