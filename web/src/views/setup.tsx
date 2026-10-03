import { useState } from 'preact/hooks'
import { parseFragment } from '../settings.ts'

export function Setup({ revoked, onToken }: { revoked: boolean; onToken: (t: string) => void }) {
  const [value, setValue] = useState('')
  const [error, setError] = useState('')

  const submit = async (e: Event) => {
    e.preventDefault()
    const v = value.trim()
    // Accept the whole setup link or just the token.
    const hash = v.includes('#') ? v.slice(v.indexOf('#')) : `#setup=${v}`
    const f = await parseFragment(hash)
    if (f?.kind === 'token') onToken(f.token)
    else if (f?.kind === 'settings' && f.settings.token) onToken(f.settings.token)
    else setError("That doesn't look like a setup link.")
  }

  return (
    <div class="app setup">
      <h1>Gym Router</h1>
      {revoked ? (
        <p class="notice warn">This device's access was rejected. The token may have been changed. Open a new setup link.</p>
      ) : (
        <p>This device isn't set up yet. Open the setup link from your server, or paste it here.</p>
      )}
      <form onSubmit={submit} class="stack">
        <label>
          Setup link
          <input
            type="password"
            autocomplete="off"
            value={value}
            onInput={(e) => setValue((e.target as HTMLInputElement).value)}
            placeholder="https://…/#setup=…"
          />
        </label>
        {error && <p class="error">{error}</p>}
        <button type="submit" class="primary">
          Set up this device
        </button>
      </form>
      <p class="muted small">
        The server owner creates the link with <code>gymrouter setup-link</code>.
      </p>
    </div>
  )
}
