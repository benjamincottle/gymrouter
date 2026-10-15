// The app's clock: the real time, or while a trip is simulated (sim.ts) the simulator's, which can run faster than
// real time or stand still. Timers set here count in the clock's time. Without a simulation they're the browser's own.
// Temporary, with the simulator (docs/spec.md, 2026-10-16).

interface Sim {
  real: number // Date.now() when `at` was set
  at: number // the simulated time then
  rate: number // simulated ms per real ms; 0 stands still
}

let sim: Sim | null = null

/** Milliseconds since the epoch, by the clock. */
export function now(): number {
  return sim ? sim.at + (Date.now() - sim.real) * sim.rate : Date.now()
}

/** True while the clock is the simulator's. */
export const simulating = () => sim !== null

/** The simulator's speed (0 when paused); 1 without a simulation. */
export const rate = () => sim?.rate ?? 1

/** Starts simulated time at `at` (ms), at `speed`; null goes back to the real time. */
export function setSim(at: number | null, speed = 0) {
  const was = sim
  sim = at === null ? null : { real: Date.now(), at, rate: speed }
  if (was && !sim) {
    // Back to real time: what's still pending runs on the browser's timers, as long from now as it had left.
    const t = was.at + (Date.now() - was.real) * was.rate
    for (const [id, x] of timers) {
      timers.delete(id)
      x.native = setTimeout(() => fire(id, x), Math.max(0, x.due - t))
      native.set(id, x)
    }
  }
  tell()
  pump()
}

/** Runs the simulated clock at `speed` from where it is now (0 pauses it). */
export function setRate(speed: number) {
  if (!sim) return
  sim = { real: Date.now(), at: now(), rate: speed }
  tell()
  pump()
}

/** Moves the simulated clock on by `ms`. */
export function skip(ms: number) {
  if (!sim) return
  sim = { ...sim, at: sim.at + ms }
  tell()
  pump()
}

// --- listening for changes of speed or a skip (the simulator's controls) ---

const listeners = new Set<() => void>()
const tell = () => listeners.forEach((l) => l())
export function onChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

// --- timers ---

interface Timer {
  due: number // by the clock
  fn: () => void
  every?: number // repeats this often, by the clock
  minRealMs: number // and never more often than this in real time (a fast simulation would flood the server)
  lastReal: number
  native?: ReturnType<typeof setTimeout>
}

let nextId = 1
const timers = new Map<number, Timer>() // on the simulated clock
const native = new Map<number, Timer>() // on the browser's
let loop: ReturnType<typeof setInterval> | undefined

function fire(id: number, x: Timer) {
  native.delete(id)
  if (x.every !== undefined) schedule(id, { ...x, due: now() + x.every, lastReal: Date.now() })
  x.fn()
}

function schedule(id: number, x: Timer) {
  if (sim) {
    timers.set(id, x)
    pump()
  } else {
    x.native = setTimeout(() => fire(id, x), Math.max(0, x.due - now()))
    native.set(id, x)
  }
}

// While simulated timers are pending, check them a few times a second.
function pump() {
  if (sim && timers.size > 0 && loop === undefined) loop = setInterval(check, 50)
  if ((!sim || timers.size === 0) && loop !== undefined) {
    clearInterval(loop)
    loop = undefined
  }
}

function check() {
  const t = now()
  const real = Date.now()
  for (const [id, x] of [...timers].sort((a, b) => a[1].due - b[1].due)) {
    if (x.due > t || real - x.lastReal < x.minRealMs || !timers.has(id)) continue
    timers.delete(id)
    if (x.every !== undefined) timers.set(id, { ...x, due: t + x.every, lastReal: real })
    x.fn()
  }
  pump()
}

/** Runs `fn` once, `ms` from now by the clock. Returns a function that cancels it. */
export function after(ms: number, fn: () => void, minRealMs = 0): () => void {
  const id = nextId++
  schedule(id, { due: now() + ms, fn, minRealMs, lastReal: Date.now() })
  return () => cancel(id)
}

/** Runs `fn` every `ms` by the clock (in a simulation, at most every `minRealMs` of real time). Returns a function that stops it. */
export function repeat(ms: number, fn: () => void, minRealMs = 1000): () => void {
  const id = nextId++
  schedule(id, { due: now() + ms, fn, every: ms, minRealMs, lastReal: Date.now() })
  return () => cancel(id)
}

function cancel(id: number) {
  timers.delete(id)
  const n = native.get(id)
  if (n) clearTimeout(n.native)
  native.delete(id)
  pump()
}
