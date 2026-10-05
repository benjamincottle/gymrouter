// Colours that come from data (transit lines) and the fixed ones drawn next to them. The rest of the palette is
// CSS tokens in style.css (docs/DESIGN.md).

/** A feed colour ("00b5ef") as CSS, or undefined if it's missing or malformed. */
export const hex = (c?: string) => (c && /^[0-9a-fA-F]{6}$/.test(c) ? `#${c}` : undefined)

export const WHITE = '#ffffff'
/** A line without a colour: its tape, rail and map line. */
export const LINE_FALLBACK = '#5e6670'
/** A line without a colour: its chip. (DESIGN.md wants one fallback; the restyle merges this into LINE_FALLBACK.) */
export const CHIP_FALLBACK = '#555555'
