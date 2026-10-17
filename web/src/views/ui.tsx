// The shared building blocks of every screen (docs/DESIGN.md, Components). Screens use these rather than writing the
// markup themselves, so a change to how a button or a callout looks happens here and in style.css, once.
import { useEffect, useRef, type MutableRef } from 'preact/hooks'
import type { ComponentChildren, JSX } from 'preact'
import { Chevron } from './icons.tsx'

type ButtonAttrs = Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, 'class'> & { class?: string }

/**
 * A button, always 44px tall: `commit` is filled in ink, for the one action that moves a screen forward (at most one
 * on a screen); otherwise it's outlined in ink; `danger` is red.
 */
export function Button({ variant, class: cls, ...rest }: ButtonAttrs & { variant?: 'commit' | 'danger' }) {
  return <button class={['btn', variant, cls].filter(Boolean).join(' ')} {...rest} />
}

/** An action that reads as underlined text inside a sentence; `quiet` is in pencil. Its hit area is 44px. */
export function TextButton({ quiet, ...rest }: Omit<ButtonAttrs, 'class'> & { quiet?: boolean }) {
  return <button class={quiet ? 'text-btn quiet' : 'text-btn'} {...rest} />
}

/** A 44px square button with an icon and a label for screen readers. */
export function IconButton({ label, class: cls, children, ...rest }: ButtonAttrs & { label: string }) {
  return (
    <button class={['icon-btn', cls].filter(Boolean).join(' ')} aria-label={label} title={label} {...rest}>
      {children}
    </button>
  )
}

/**
 * Arrow keys inside a radio group: move to the next or previous option and choose it (the group is one tab stop:
 * only the chosen option has tabIndex 0).
 */
export function onRadioKeys(e: KeyboardEvent) {
  const step = ({ ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 } as Record<string, number>)[e.key]
  if (!step) return
  const items = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role="radio"]')]
  const i = items.indexOf(document.activeElement as HTMLElement)
  if (i < 0) return
  e.preventDefault()
  const next = items[(i + step + items.length) % items.length]
  next.focus()
  next.click()
}

/** A row of options, one of them chosen: a radio group (one tab stop, arrow keys inside). */
export function Segmented<T>({ label, options, value, onChange }: {
  label: string
  options: readonly (readonly [T, string])[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div class="segmented" role="radiogroup" aria-label={label} onKeyDown={onRadioKeys}>
      {options.map(([v, text]) => (
        <button role="radio" aria-checked={value === v} tabIndex={value === v ? 0 : -1} onClick={() => onChange(v)}>
          {text}
        </button>
      ))}
    </div>
  )
}

/** A section of a screen: a heavy rule, then its title (with an icon in Settings), then an optional intro line. */
export function Section({ title, icon, intro, class: cls, children, ...rest }: Omit<JSX.HTMLAttributes<HTMLElement>, 'title' | 'icon' | 'class'> & {
  title: ComponentChildren
  icon?: ComponentChildren
  intro?: ComponentChildren
  class?: string
}) {
  return (
    <section class={cls ? `section ${cls}` : 'section'} {...rest}>
      <h2 class="title">
        {icon}
        {title}
      </h2>
      {intro && <p class="meta intro">{intro}</p>}
      {children}
    </section>
  )
}

/** A labelled field: the label above its input, then an optional hint. */
export function Field({ label, hint, children }: { label: ComponentChildren; hint?: ComponentChildren; children: ComponentChildren }) {
  return (
    <label class="field">
      {label}
      {children}
      {hint && <span class="hint">{hint}</span>}
    </label>
  )
}

/** A choice from a list: the system's own picker, drawn as a field with the app's chevron. `class` sizes it in its row. */
export function Select({ class: cls, children, ...rest }: Omit<JSX.SelectHTMLAttributes<HTMLSelectElement>, 'class'> & { class?: string }) {
  return (
    <span class={cls ? `select ${cls}` : 'select'}>
      <select {...rest}>{children}</select>
      <Chevron />
    </span>
  )
}

export type Tone = 'neutral' | 'caution' | 'good' | 'bad'

/**
 * A message that stands out from the page: neutral for information, good for a better option, caution when something
 * works but is degraded, bad when something failed. At most one action; a dismiss only if it can safely be ignored.
 */
export function Callout({ tone = 'neutral', role, action, detail, onDismiss, class: cls, children }: {
  tone?: Tone
  role?: 'status' | 'alert'
  action?: ComponentChildren
  detail?: ComponentChildren // under the text, across the callout: the trip it suggests (TripRow)
  onDismiss?: () => void
  class?: string
  children: ComponentChildren
}) {
  return (
    <div class={cls ? `callout ${cls}` : 'callout'} data-tone={tone} role={role}>
      <p>{children}</p>
      {onDismiss && (
        <IconButton class="dismiss" label="Dismiss" onClick={onDismiss}>
          <svg class="icon" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </IconButton>
      )}
      {detail && <div class="detail">{detail}</div>}
      {action && <div class="actions">{action}</div>}
    </div>
  )
}

/** A list row: an optional lead (checkbox, logo), the main text with details on a second line, then actions. */
export function Row({ lead, main, meta, children, class: cls }: {
  lead?: ComponentChildren
  main: ComponentChildren
  meta?: ComponentChildren
  children?: ComponentChildren
  class?: string
}) {
  return (
    <li class={cls}>
      {lead}
      <span class="main">
        {main}
        {meta && <span class="meta">{meta}</span>}
      </span>
      {children}
    </li>
  )
}

/**
 * An action that is a whole row of a list or of a section in Settings, not a button: `add` is the last row of its list
 * (a plus, then the words); `go` does something (it ends in ›); `open` opens something in place (a chevron, down to
 * open and up to close).
 */
export function RowAction({ kind = 'go', open, children, ...rest }: Omit<ButtonAttrs, 'class'> & { kind?: 'add' | 'go' | 'open'; open?: boolean }) {
  return (
    <button class={`row-action ${kind}`} aria-expanded={kind === 'open' ? !!open : undefined} {...rest}>
      {kind === 'add' && (
        <svg class="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <path d="M12 5v14M5 12h14" />
        </svg>
      )}
      <span>{children}</span>
      {kind === 'open' && <Chevron up={open} />}
    </button>
  )
}

/** The screen's commit action, pinned to the bottom where the thumb is: the way out first, the commit last. */
export function ActionBar({ progress, children }: { progress?: ComponentChildren; children: ComponentChildren }) {
  return (
    <div class="action-bar">
      {progress}
      {children}
    </div>
  )
}

/**
 * Whether the times on screen are live or the timetable's: a dot and one word, at the right end of the row that
 * labels the countdown or the arrive time. `updating` while newer times are on their way.
 */
export function LiveTag({ live, updating }: { live: boolean; updating?: boolean }) {
  return (
    <span class="live-tag">
      <span class={updating ? 'live-dot pending' : live ? 'live-dot live' : 'live-dot'} aria-hidden="true" />
      {updating ? 'Updating…' : live ? 'Live' : 'Timetabled'}
    </span>
  )
}

/**
 * How far a long job is: a bar that fills, a line saying what it's doing, and the percentage. In an action bar
 * (its `progress`) the bar takes the place of the hairline along the top.
 */
export function Progress({ fraction, children }: { fraction: number; children: string }) {
  const pct = Math.round(Math.min(1, Math.max(0, fraction)) * 100)
  return (
    <div class="progress" role="progressbar" aria-label={children} aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct}>
      <div class="progress-bar">
        <i style={{ width: `${pct}%` }} />
      </div>
      <p class="progress-line">
        <span>{children}</span>
        <span>{pct}%</span>
      </p>
    </div>
  )
}

/**
 * Asks before something destructive, in place: the question, what goes with it, then Keep (focused, and what Escape
 * does) and the danger action.
 */
export function Confirm({ question, detail, confirm, keep = 'Keep', onConfirm, onKeep, as = 'div' }: {
  question: ComponentChildren
  detail?: ComponentChildren
  confirm: string
  keep?: string
  onConfirm: () => void
  onKeep: () => void
  as?: 'div' | 'li'
}) {
  const keepRef = useRef<HTMLButtonElement>(null)
  useEffect(() => focusQuietly(keepRef.current), [])
  const El = as
  return (
    <El class="confirm" role="group" aria-label={typeof question === 'string' ? question : undefined} onKeyDown={(e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation() // Escape is Keep, not also closing a sheet it's in
      onKeep()
    }}>
      <span class="main">
        <strong>{question}</strong>
        {detail && <span class="meta">{detail}</span>}
      </span>
      <span class="actions">
        <button ref={keepRef} class="btn" onClick={onKeep}>
          {keep}
        </button>
        <Button variant="danger" onClick={onConfirm}>
          {confirm}
        </Button>
      </span>
    </El>
  )
}

/**
 * Whether the last thing done was with a pointer (a tap, a click) rather than the keyboard. Focus moved by the app
 * (into a dialog, onto Keep) shows its ring only after the keyboard, as a tap on a control doesn't show one either.
 */
let pointing = false
document.addEventListener('pointerdown', () => (pointing = true), true)
document.addEventListener('keydown', () => (pointing = false), true)
export const focusQuietly = (el: HTMLElement | null | undefined) => el?.focus({ focusVisible: !pointing } as FocusOptions)

const FOCUSABLE = 'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

/**
 * Makes an element behave as a modal dialog: focus moves into it (to `first`, or its first control), Tab stays inside,
 * Escape closes it, the page behind is inert, and focus goes back to whatever opened it.
 */
export function useDialog(ref: MutableRef<HTMLElement | null>, onClose: () => void, first?: MutableRef<HTMLElement | null>) {
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const opener = document.activeElement as HTMLElement | null
    // Everything else on the page, from the dialog up: inert while it's open (but for a tool that stays on top of
    // everything, marked data-over-dialogs: the simulator's panel).
    const others: HTMLElement[] = []
    for (let n: HTMLElement | null = el; n && n !== document.body; n = n.parentElement) {
      for (const sib of n.parentElement?.children ?? []) {
        if (sib !== n && sib instanceof HTMLElement && !sib.inert && !sib.hasAttribute('data-over-dialogs')) {
          sib.inert = true
          others.push(sib)
        }
      }
    }
    focusQuietly(first?.current ?? el.querySelector<HTMLElement>(FOCUSABLE) ?? el)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        close.current()
      } else if (e.key === 'Tab') {
        const items = [...el.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null)
        if (items.length === 0) return
        const i = items.indexOf(document.activeElement as HTMLElement)
        if (e.shiftKey && i <= 0) {
          e.preventDefault()
          items[items.length - 1].focus()
        } else if (!e.shiftKey && i === items.length - 1) {
          e.preventDefault()
          items[0].focus()
        }
      }
    }
    el.addEventListener('keydown', onKey)
    return () => {
      el.removeEventListener('keydown', onKey)
      for (const o of others) o.inert = false
      if (opener) focusQuietly(opener)
    }
  }, [])
}

/**
 * Asks before a whole-device action, in a sheet over the page: a title that says what will happen, what is lost, then
 * Cancel (focused) and the action.
 */
export function ConfirmSheet({ title, children, confirm, danger = true, onConfirm, onCancel }: {
  title: string
  children: ComponentChildren
  confirm: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  useDialog(ref, onCancel, cancelRef)
  return (
    <div class="scrim" onClick={(e) => e.target === e.currentTarget && onCancel()}>
      <div ref={ref} class="sheet-dialog" role="dialog" aria-modal="true" aria-labelledby="sheet-title">
        <h2 id="sheet-title" class="title">
          {title}
        </h2>
        <div class="sheet-body">{children}</div>
        <div class="actions">
          <button ref={cancelRef} class="btn" onClick={onCancel}>
            Cancel
          </button>
          <Button variant={danger ? 'danger' : 'commit'} onClick={onConfirm}>
            {confirm}
          </Button>
        </div>
      </div>
    </div>
  )
}
