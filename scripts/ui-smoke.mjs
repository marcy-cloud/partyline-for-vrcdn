// Smoke test of the real window on Windows. Starts a development build with
// WebView2's DevTools port open, then drives the UI the way a person would:
// plays the first saved stream (muted, without saving the volume), switches
// its connection to MPEG-TS and back, stops it, starts it again with the Play
// button, looks at the Send tab (and goes live with the test tone if
// PARTYLINE_TEST_KEY holds a stream key), opens the dialog and switches theme,
// saving a screenshot at each step.
//
//   go build -tags bundled_libmpv -o bin/dev.exe .
//   node scripts/ui-smoke.mjs bin\\dev.exe bin
//
// Needs Node 22+. Release builds don't open the DevTools port.
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

if (process.argv.length < 3) {
  console.error('usage: node scripts/ui-smoke.mjs <dev build exe> [screenshot folder]')
  process.exit(2)
}
const exe = resolve(process.argv[2])
const out = resolve(process.argv[3] ?? '.')
const PORT = 9333
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const app = spawn(exe, [], {
  env: { ...process.env, VRCDN_PLAYER_DEBUG_PORT: String(PORT) },
  stdio: 'ignore',
})
let exited = null
app.on('exit', (code) => (exited = code))

let target
for (let i = 0; i < 60 && !target; i++) {
  await sleep(500)
  if (exited !== null) throw new Error(`app exited early with code ${exited}`)
  try {
    const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()
    target = list.find((t) => t.type === 'page')
  } catch {}
}
if (!target) throw new Error('no WebView page appeared')
console.log('page:', target.url, '|', target.title)

const ws = new WebSocket(target.webSocketDebuggerUrl)
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej })
let seq = 0
const pending = new Map()
ws.onmessage = (m) => {
  const msg = JSON.parse(m.data)
  if (msg.id && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id) }
}
const send = (method, params = {}) => new Promise((res) => { const id = ++seq; pending.set(id, res); ws.send(JSON.stringify({ id, method, params })) })
const js = async (expression) => {
  const r = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })
  if (r.result?.exceptionDetails) throw new Error(JSON.stringify(r.result.exceptionDetails).slice(0, 400))
  return r.result?.result?.value
}
const shot = async (name) => {
  const r = await send('Page.captureScreenshot', { format: 'png' })
  writeFileSync(join(out, `${name}.png`), Buffer.from(r.result.data, 'base64'))
  console.log('screenshot:', name)
}
const snapshot = () => js(`JSON.stringify({
  size: innerWidth + 'x' + innerHeight,
  theme: document.documentElement.dataset.theme,
  badge: document.querySelector('.badge')?.innerText ?? null,
  title: document.querySelector('.now h1')?.innerText ?? null,
  message: document.querySelector('.message')?.innerText ?? null,
  buffer: document.querySelector('.buffer .value')?.innerText?.replace(/\\s+/g, ' ') ?? null,
  level: document.querySelector('.level')?.getAttribute('aria-label') ?? null,
  format: document.querySelector('.format')?.innerText ?? null,
  rows: [...document.querySelectorAll('aside li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim()),
  flair: !!document.querySelector('.flair'),
  overflow: (() => { const b = document.querySelector('.window .body'); return b.scrollHeight - b.clientHeight })(),
  devices: document.querySelectorAll('.control select option').length,
  engine: document.querySelector('footer')?.innerText ?? null,
  callout: document.querySelector('.callout')?.innerText ?? null,
  fonts: [...document.fonts].filter((f) => f.status === 'loaded').map((f) => f.family).filter((v, i, a) => a.indexOf(v) === i),
})`).then(JSON.parse)

let originalConnection = null
let restoreConnection = null
try {
  await js(`new Promise((r) => { const t = setInterval(() => { if (document.querySelector('.shell')) { clearInterval(t); r() } }, 100) })`)
  await sleep(1500)
  console.log('idle:', JSON.stringify(await snapshot()))
  await shot('smoke-idle')

  // Mute before playing (applies without saving), then click the first stream.
  await js(`(() => { const s = document.querySelector('input[type=range]'); s.dataset.was = s.value; s.value = 0; s.dispatchEvent(new Event('input', { bubbles: true })) })()`)
  const hasRow = await js(`!!document.querySelector('aside li .row')`)
  if (hasRow) {
    await js(`document.querySelector('aside li .row').click()`)
    for (let i = 0; i < 16; i++) {
      await sleep(500)
      const s = await snapshot()
      if (i % 4 === 3 || s.badge?.trim() === 'Live') console.log(`t+${(i + 1) / 2}s:`, s.badge?.trim(), '|', s.buffer, '|', s.format, '|', s.level, '|', s.message)
      if (s.badge?.trim() === 'Live' && i >= 5) break
    }
    // Switch the connection from the main page: it must reconnect by itself.
    // (Also the way through when RTSP is not answering.)
    const setConnection = (value) => js(`(() => { const s = document.querySelector('.connection select'); s.value = ${JSON.stringify(value)}; s.dispatchEvent(new Event('change', { bubbles: true })) })()`)
    originalConnection = await js(`document.querySelector('.connection select').value`)
    await setConnection('mpegts')
    let viaTS = null
    for (let i = 0; i < 20 && viaTS !== 'Live'; i++) {
      await sleep(500)
      viaTS = (await snapshot()).badge?.trim()
    }
    console.log(`connection ${originalConnection} -> mpegts:`, viaTS, '|', (await snapshot()).format)
    restoreConnection = () => setConnection(originalConnection)

    const playing = await snapshot()
    console.log('playing:', JSON.stringify(playing))
    if (playing.overflow > 0) console.log(`WARNING: the panel overflows by ${playing.overflow}px at the default window size`)
    await shot('smoke-playing')

    // Resync, then stop.
    await js(`[...document.querySelectorAll('.actions .btn')].find((b) => /Resync/.test(b.innerText))?.click()`)
    await sleep(800)
    await js(`[...document.querySelectorAll('.actions .btn')].find((b) => /Stop/.test(b.innerText))?.click()`)
    await sleep(800)
    console.log('after stop:', (await snapshot()).badge?.trim())
    await shot('smoke-stopped')

    // The same button is now Play, and brings the stream straight back.
    const label = await js(`document.querySelector('.actions .btn')?.innerText.trim()`)
    await js(`document.querySelector('.actions .btn').click()`)
    let back = null
    for (let i = 0; i < 16 && back !== 'Live'; i++) {
      await sleep(500)
      back = (await snapshot()).badge?.trim()
    }
    console.log(`after pressing "${label}":`, back)
    await js(`[...document.querySelectorAll('.actions .btn')].find((b) => /Stop/.test(b.innerText))?.click()`)
    await sleep(500)
  }

  // The Send tab. With PARTYLINE_TEST_KEY set, also go live with the test
  // tone, then stop and forget the key again. The key is never printed.
  const sendTab = `[...document.querySelectorAll('.tabs .tab')].find((b) => /send/.test(b.innerText))`
  if (await js(`!!${sendTab}`)) {
    await js(`${sendTab}.click()`)
    await sleep(1200)
    const sendSnapshot = () => js(`JSON.stringify({
      badge: document.querySelector('.sending .badge')?.innerText?.trim() ?? null,
      message: document.querySelector('.sending .message')?.innerText ?? null,
      inputs: [...document.querySelectorAll('.controls .control select')][0]?.options.length ?? 0,
      level: document.querySelector('.level')?.getAttribute('aria-label') ?? null,
      goLive: document.querySelector('.go-live')?.innerText?.trim() + (document.querySelector('.go-live')?.disabled ? ' (disabled)' : ''),
      overflow: (() => { const b = document.querySelector('.window .body'); return b.scrollHeight - b.clientHeight })(),
    })`).then(JSON.parse)
    console.log('send tab:', JSON.stringify(await sendSnapshot()))
    await shot('smoke-send')

    const testKey = process.env.PARTYLINE_TEST_KEY
    if (testKey) {
      const setValue = (selector, value, event) => js(`(() => { const el = document.querySelector(${JSON.stringify(selector)}); const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, 'value').set.call(el, ${JSON.stringify(value)}); el.dispatchEvent(new Event(${JSON.stringify(event)}, { bubbles: true })) })()`)
      const inputWas = await js(`document.querySelector('.controls .control select').value`)
      await setValue('.controls .control select', 'test-tone', 'change')
      await setValue('.key input', testKey, 'input')
      await js(`document.querySelector('.key .btn').click()`)
      await sleep(600)
      await js(`document.querySelector('.go-live').click()`)
      let on = null
      for (let i = 0; i < 16 && on?.badge !== 'On air'; i++) {
        await sleep(500)
        on = await sendSnapshot()
      }
      await sleep(1500)
      on = await sendSnapshot()
      console.log('went live:', on.badge, '|', on.level, '|', on.message)
      await shot('smoke-on-air')
      await js(`document.querySelector('.go-live').click()`)
      await sleep(800)
      console.log('after stop:', (await sendSnapshot()).badge)
      await js(`[...document.querySelectorAll('.key .btn')].find((b) => /Forget/.test(b.innerText))?.click()`)
      await setValue('.controls .control select', inputWas, 'change')
      await sleep(400)
      console.log('key forgotten:', (await sendSnapshot()).goLive)
    }
    await js(`[...document.querySelectorAll('.tabs .tab')].find((b) => /now-playing/.test(b.innerText)).click()`)
    await sleep(300)
  }

  // The dialog and the theme switch.
  await js(`document.querySelector('button[aria-label="Add stream"]').click()`)
  await sleep(700)
  await shot('smoke-dialog')
  await js(`document.querySelector('dialog button[aria-label="Close"]').click()`)
  await js(`document.querySelector('aside footer button').click()`)
  await sleep(400)
  // The window has no Windows frame: its own buttons must be there, and the
  // title bar must be what it is dragged by.
  console.log('window buttons:', await js(`[...document.querySelectorAll('.window .titlebar .chrome button')].map((b) => b.getAttribute('aria-label')).join(', ')`),
    '| title bar drag:', await js(`getComputedStyle(document.querySelector('.window .titlebar')).getPropertyValue('--wails-draggable').trim()`))
  console.log('theme after toggle:', await js(`document.documentElement.dataset.theme`))
  await shot('smoke-other-theme')
  await js(`document.querySelector('aside footer button').click()`)

  // Put the volume back the way it was (still without saving).
  await js(`(() => { const s = document.querySelector('input[type=range]'); s.value = s.dataset.was; s.dispatchEvent(new Event('input', { bubbles: true })) })()`)
  await sleep(300)
} finally {
  // Leave the saved stream the way it was found.
  if (restoreConnection) {
    await js(`[...document.querySelectorAll('.actions .btn')].find((b) => /Stop/.test(b.innerText))?.click()`).catch(() => {})
    await restoreConnection().catch(() => {})
    await sleep(500)
  }
  ws.close()
  app.kill()
  await sleep(500)
}
console.log('done')
