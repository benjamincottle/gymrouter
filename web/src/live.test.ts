import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createLive, type LiveNews, type Open } from './live.ts'

const tick = () => new Promise((r) => setTimeout(r, 0))

/** A stream that stays open until the test ends it (or it's aborted), and says what it was opened with. */
function fakeServer() {
  const opened: { token: string; send: (name: string) => void; close: () => void; fail: (e: unknown) => void; signal: AbortSignal }[] = []
  const open: Open = (token, signal, onOpen, onEvent) =>
    new Promise<void>((resolve, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
      opened.push({ token, send: onEvent, close: resolve, fail: reject, signal })
      onOpen()
    })
  return { opened, open }
}

test('everything listening shares one stream, open only while something listens', async () => {
  const srv = fakeServer()
  const live = createLive(srv.open, () => false)
  const a: LiveNews[] = []
  const b: LiveNews[] = []
  const offA = live.listen('tok', (n) => a.push(n))
  const offB = live.listen('tok', (n) => b.push(n))
  await tick()
  assert.equal(srv.opened.length, 1)
  srv.opened[0].send('vehicles')
  srv.opened[0].send('realtime')
  srv.opened[0].send('something-new') // not one the app knows: ignored
  assert.deepEqual(a, [{ connected: false }, { connected: true }, { kind: 'vehicles' }, { kind: 'realtime' }])
  assert.deepEqual(b, [{ connected: true }, { kind: 'vehicles' }, { kind: 'realtime' }]) // joined an open stream
  offA()
  assert.equal(srv.opened[0].signal.aborted, false)
  offB()
  assert.equal(srv.opened[0].signal.aborted, true)
})

test('a dropped stream is opened again, and a listener hears it go and come back', async () => {
  const srv = fakeServer()
  const live = createLive(srv.open, () => false, { closed: 0, max: 0 })
  const heard: LiveNews[] = []
  const off = live.listen('tok', (n) => heard.push(n))
  await tick()
  srv.opened[0].close() // the server ended it
  await tick()
  await tick()
  assert.equal(srv.opened.length, 2)
  srv.opened[1].fail(new Error('network')) // then a failure
  await new Promise((r) => setTimeout(r, 1100)) // a backed-off retry (at least a second)
  assert.equal(srv.opened.length, 3)
  assert.deepEqual(heard.filter((n) => 'connected' in n).map((n) => 'connected' in n && n.connected), [false, true, false, true, false, true])
  off()
})

test('an error worth no retry stops it', async () => {
  const srv = fakeServer()
  const revoked = new Error('revoked')
  const live = createLive(srv.open, (e) => e === revoked, { closed: 0, max: 0 })
  const off = live.listen('tok', () => undefined)
  await tick()
  srv.opened[0].fail(revoked)
  await new Promise((r) => setTimeout(r, 50))
  assert.equal(srv.opened.length, 1)
  off()
})

test('another token opens another stream', async () => {
  const srv = fakeServer()
  const live = createLive(srv.open, () => false)
  const off1 = live.listen('one', () => undefined)
  const off2 = live.listen('two', () => undefined)
  await tick()
  assert.deepEqual(srv.opened.map((o) => [o.token, o.signal.aborted]), [['one', true], ['two', false]])
  off1()
  off2()
})
