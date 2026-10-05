// Colours that come from data (transit lines) and the fixed ones drawn next to them. The rest of the palette is
// CSS tokens in style.css (docs/DESIGN.md).

/** A feed colour ("00b5ef") as CSS, or undefined if it's missing or malformed. */
export const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : undefined)

export const WHITE = '#ffffff'
export const INK = '#1e2226' // the light theme's ink (style.css --ink)
/** A line without a colour, wherever a line is drawn. */
export const LINE_FALLBACK = '#5e6670'

/** The line's colour, or the fallback. */
export const lineColour = (c?: string) => hex(c) ?? LINE_FALLBACK

/** Text on a line colour: ink or white, whichever contrasts more (feeds' own text colours can be unreadable). */
export function textOn(bg: string): string {
  const n = parseInt(bg.slice(1), 16)
  const lin = (v: number) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
  const l = 0.2126 * lin(n >> 16) + 0.7152 * lin((n >> 8) & 255) + 0.0722 * lin(n & 255)
  const lInk = 0.0156 // INK's relative luminance
  return 1.05 / (l + 0.05) >= (l + 0.05) / (lInk + 0.05) ? WHITE : INK
}

/** A CSS token's current value (the map can't read CSS variables itself). */
export const token = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim()
