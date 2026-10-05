// The shared building blocks of every screen (docs/DESIGN.md, Components). Screens use these rather than writing the
// markup themselves, so a change to how a button or a callout looks happens here and in style.css, once.
import type { ComponentChildren, JSX } from 'preact'

type ButtonAttrs = Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, 'class'> & { class?: string }

/** A button: outlined in ink; `primary` is filled, `ghost` has a quiet outline, `danger` is red. */
export function Button({ variant, class: cls, ...rest }: ButtonAttrs & { variant?: 'primary' | 'ghost' | 'danger' }) {
  return <button class={[variant, cls].filter(Boolean).join(' ') || undefined} {...rest} />
}

/** An action that reads as underlined text, usually inside a sentence. */
export function TextButton({ small, ...rest }: Omit<ButtonAttrs, 'class'> & { small?: boolean }) {
  return <button class={small ? 'link small' : 'link'} {...rest} />
}

/** A row of options, one of them pressed. */
export function Segmented<T>({ label, small, options, value, onChange }: {
  label: string
  small?: boolean
  options: readonly (readonly [T, string])[]
  value: T
  onChange: (v: T) => void
}) {
  return (
    <div class={small ? 'segmented small' : 'segmented'} role="group" aria-label={label}>
      {options.map(([v, text]) => (
        <button aria-pressed={value === v} onClick={() => onChange(v)}>
          {text}
        </button>
      ))}
    </div>
  )
}

/** A section of a screen: a heavy rule, then its title (with an icon in Settings). */
export function Section({ title, icon, class: cls, children, ...rest }: Omit<JSX.HTMLAttributes<HTMLElement>, 'title' | 'icon' | 'class'> & {
  title: ComponentChildren
  icon?: ComponentChildren
  class?: string
}) {
  return (
    <section class={cls ? `card ${cls}` : 'card'} {...rest}>
      <h2>
        {icon}
        {icon ? ' ' : null}
        {title}
      </h2>
      {children}
    </section>
  )
}

/** A labelled field: the label above its input, then an optional hint. */
export function Field({ label, hint, children }: { label: ComponentChildren; hint?: ComponentChildren; children: ComponentChildren }) {
  return (
    <label>
      {label}
      {children}
      {hint && <span class="muted small">{hint}</span>}
    </label>
  )
}

export type Tone = 'neutral' | 'caution' | 'good' | 'bad'

/** A message that stands out from the page; the tone says what kind (docs/DESIGN.md, Callout). */
export function Callout({ tone = 'neutral', role, children }: { tone?: Tone; role?: 'status' | 'alert'; children: ComponentChildren }) {
  if (tone === 'good' || tone === 'bad') {
    return (
      <div class={tone === 'good' ? 'alert risk-safe' : 'alert risk-missed'} role={role}>
        {children}
      </div>
    )
  }
  return (
    <p class={tone === 'caution' ? 'notice warn' : 'notice'} role={role}>
      {children}
    </p>
  )
}
