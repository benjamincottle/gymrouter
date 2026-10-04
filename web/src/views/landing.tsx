/**
 * What a browser without a setup link sees: the name and the mark, nothing to press. The only way in is a setup link.
 *
 * The mark is the app icon's drawing at page scale, on the same wall: bolt holes every 12 icon units, a line arriving
 * along a row of them (with a stop every other hole) at the foot of a volume bolted into one. style.css lays it out
 * in icon units (--u) around the middle of the screen, so the holes, the line and the bolt stay on one grid.
 */
export function Landing() {
  return (
    <main class="landing">
      <div class="landing-run" aria-hidden="true">
        <span class="landing-stops" />
        <span class="landing-line" />
      </div>
      <svg class="landing-volume" viewBox="20 6 40 40" aria-hidden="true">
        <g stroke-width="2.4" stroke-linejoin="round">
          <path class="lit" d="M22 44 L48 9 L44 32 Z" />
          <path class="side" d="M48 9 L58 44 L44 32 Z" />
          <path class="base" d="M22 44 L44 32 L58 44 Z" />
        </g>
        <circle class="washer" cx="44" cy="32" r="2.6" />
        <circle class="bolt" cx="44" cy="32" r="1.3" />
      </svg>
      <h1 class="landing-name">Gym Router</h1>
    </main>
  )
}
