// The shared building blocks of every screen (docs/DESIGN.md, Components). Screens use these rather than writing the
// markup themselves, so a change to how a button or a callout looks happens here and in style.css, once.
import { useEffect, useRef } from 'preact/hooks'
import type { ComponentChildren, JSX } from 'preact'

type ButtonAttrs = Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, 'class'> & { class?: string }

/** A button: outlined in ink; `primary` is filled, `danger` is red. Always 44px tall. */
export function Button({ variant, class: cls, ...rest }: ButtonAttrs & { variant?: 'primary' | 'danger' }) {
  return <button class={['btn', variant, cls].filter(Boolean).join(' ')} {...rest} />
}

/** An action that reads as underlined text inside a sentence; `quiet` is in pencil. Its hit area is 44px. */
export function TextButton({ quiet, ...rest }: Omit<ButtonAttrs, 'class'> & { quiet?: boolean }) {
  return <button class={quiet ? 'text-btn quiet' : 'text-btn'} {...rest} />
}

/** A 44px square button with an icon and a label for screen readers. */
export function IconButton({ label, danger, class: cls, children, ...rest }: ButtonAttrs & { label: string; danger?: boolean }) {
  return (
    <button class={['icon-btn', danger && 'danger', cls].filter(Boolean).join(' ')} aria-label={label} title={label} {...rest}>
      {children}
    </button>
  )
}

/** A row of options, one of them pressed. */
export function Segmented<T>({ label, options, value, onChange }: {
  label: string
  options: readonly (readonly [T, string])[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div class="segmented" role="group" aria-label={label}>
      {options.map(([v, text]) => (
        <button aria-pressed={value === v} onClick={() => onChange(v)}>
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

export type Tone = 'neutral' | 'caution' | 'good' | 'bad'

/**
 * A message that stands out from the page: neutral for information, good for a better option, caution when something
 * works but is degraded, bad when something failed. At most one action; a dismiss only if it can safely be ignored.
 */
export function Callout({ tone = 'neutral', role, action, onDismiss, class: cls, children }: {
  tone?: Tone
  role?: 'status' | 'alert'
  action?: ComponentChildren
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

/** The screen's commit action, pinned to the bottom where the thumb is: the way out first, the commit last. */
export function ActionBar({ children }: { children: ComponentChildren }) {
  return <div class="action-bar">{children}</div>
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
  useEffect(() => keepRef.current?.focus(), [])
  const El = as
  return (
    <El class="confirm" role="group" aria-label={typeof question === 'string' ? question : undefined} onKeyDown={(e: KeyboardEvent) => e.key === 'Escape' && onKeep()}>
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
