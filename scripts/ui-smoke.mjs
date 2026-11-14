// Smoke test of the real window on Windows. Starts a development build with
// WebView2's DevTools port open, then drives the UI the way a person would:
// plays the first saved stream (muted, without saving the volume), switches
// its connection to MPEG-TS and back, stops it, starts it again with the Play
// button, looks at the Send tab (and, if PARTYLINE_TEST_KEY holds a stream
// key, saves a send target, goes live with the test tone and deletes the
// target again), opens the dialog, folds a wing and switches theme,
// saving a screenshot at each step. With PARTYLINE_TEST_TOUCH=1 it also taps
// and drags the window by injected touch. With PARTYLINE_TEST_REORDER=1 it
// also reorders the streams by their handles. With PARTYLINE_TEST_DIALOGS=1 it also
// exports the stream list and imports it again through the system's file
// dialogs, by typing into them: leave the keyboard alone while that runs.
//
//   go build -tags bundled_libmpv -o bin/dev.exe .
//   node scripts/ui-smoke.mjs bin\\dev.exe bin
//
// Needs Node 22+. Release builds don't open the DevTools port.
import { spawn, spawnSync } from 'node:child_process'
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
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
  rows: [...document.querySelectorAll('.streams li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim()),
  flair: !!document.querySelector('.flair'),
  overflow: (() => { const b = document.querySelector('.window .body'); return b.scrollHeight - b.clientHeight })(),
  devices: document.querySelectorAll('.control select option').length,
  engine: document.querySelector('.engine')?.innerText ?? null,
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
  const hasRow = await js(`!!document.querySelector('.streams li .row')`)
  if (hasRow) {
    await js(`document.querySelector('.streams li .row').click()`)
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
      const dialogButton = (re) => js(`[...document.querySelectorAll('dialog[open] .buttons .btn')].find((b) => ${re}.test(b.innerText.trim())).click()`)
      // Save a send target with the key, through the wing and its dialog.
      const targetsBefore = await js(`document.querySelectorAll('.targets li').length`)
      await js(`document.querySelector('.targets .add').click()`); await sleep(500)
      await setValue('dialog[open] input[type=password]', testKey, 'input')
      await setValue('dialog[open] .field:nth-of-type(2) input', 'Smoke test target', 'input')
      await dialogButton('/^Add$/'); await sleep(900)
      console.log('send targets:', targetsBefore, '->', await js(`document.querySelectorAll('.targets li').length`), '| chosen:', await js(`document.querySelector('.targets li.chosen .label')?.innerText`))
      const inputWas = await js(`document.querySelector('.controls .control select').value`)
      await setValue('.controls .control select', 'test-tone', 'change')
      await sleep(300)
      // Go on air with each encoder this PC has (CoreAudio only shows where
      // Apple's encoder is installed).
      const encoderSelect = `[...document.querySelectorAll('.controls .control')].find((c) => /Encoder/.test(c.innerText))?.querySelector('select')`
      const encoders = (await js(`${encoderSelect} ? [...${encoderSelect}.options].map((o) => o.value).join(',') : ''`)) || 'ffmpeg'
      const encoderWas = await js(`${encoderSelect}?.value ?? ''`)
      for (const encoder of encoders.split(',')) {
        if (encoderWas !== '') {
          await js(`(() => { const el = ${encoderSelect}; el.value = ${JSON.stringify(encoder)}; el.dispatchEvent(new Event('change', { bubbles: true })) })()`)
          await sleep(400)
        }
        await js(`document.querySelector('.go-live').click()`)
        let on = null
        for (let i = 0; i < 16 && on?.badge !== 'On air'; i++) {
          await sleep(500)
          on = await sendSnapshot()
        }
        await sleep(1500)
        on = await sendSnapshot()
        console.log(`went live with ${encoder}:`, on.badge, '|', on.level, '|', on.message, '|', await js(`document.querySelector('.sending .sub')?.innerText.trim()`))
        await shot(`smoke-on-air-${encoder}`)
        await js(`document.querySelector('.go-live').click()`)
        await sleep(1200)
        console.log('after stop:', (await sendSnapshot()).badge)
      }
      if (encoderWas !== '') await js(`(() => { const el = ${encoderSelect}; el.value = ${JSON.stringify(encoderWas)}; el.dispatchEvent(new Event('change', { bubbles: true })) })()`)
      await setValue('.controls .control select', inputWas, 'change')
      // Remove the target again, so the key is not left saved.
      await js(`[...document.querySelectorAll('.targets li')].find((li) => /Smoke test target/.test(li.innerText)).querySelector('.edit').click()`); await sleep(500)
      await dialogButton('/^Delete$/'); await sleep(300)
      await dialogButton('/Really delete/'); await sleep(800)
      console.log('send targets after cleanup:', await js(`document.querySelectorAll('.targets li').length`))
    }
    await js(`[...document.querySelectorAll('.tabs .tab')].find((b) => /now-playing/.test(b.innerText)).click()`)
    await sleep(300)
  }

  // Sharing the stream list. Importing the list just exported adds nothing,
  // so the saved streams are left as they were.
  console.log('share buttons:', await js(`[...document.querySelectorAll('.streams .share button')].map((b) => b.innerText.trim() + (b.disabled ? ' (off)' : '')).join(', ')`),
    '| wing overflow:', await js(`(() => { const w = document.querySelector('.streams'); return w.scrollWidth - w.clientWidth })()`))
  if (process.env.PARTYLINE_TEST_DIALOGS) {
    const list = join(out, 'smoke-streams.json')
    rmSync(list, { force: true })
    // Types the path into the file dialog once it is in front and settled.
    const answer = (title) => spawnSync('powershell', ['-NoProfile', '-Command',
      `$w = New-Object -ComObject WScript.Shell; $ok = $false; for ($i = 0; $i -lt 24 -and -not $ok; $i++) { Start-Sleep -Milliseconds 250; $ok = $w.AppActivate('${title}') }; if (-not $ok) { exit 1 }; Start-Sleep -Milliseconds 2000; if (-not $w.AppActivate('${title}')) { exit 1 }; Start-Sleep -Milliseconds 300; $w.SendKeys('%n'); Start-Sleep -Milliseconds 200; $w.SendKeys('${list}{ENTER}')`]).status === 0
    const toast = async () => { for (let i = 0; i < 20; i++) { await sleep(300); const t = await js(`document.querySelector('.toast')?.innerText ?? null`); if (t) return t } return null }
    const press = (name) => js(`[...document.querySelectorAll('.streams .share button')].find((b) => b.innerText.trim() === '${name}').click()`)
    const before = (await snapshot()).rows.length
    await press('Export')
    console.log('export dialog answered:', answer('Save As'), '|', await toast(),
      '| file:', existsSync(list) ? JSON.parse(readFileSync(list, 'utf8')).streams.length + ' streams' : 'missing')
    await shot('smoke-export')
    await js(`document.querySelector('.toast')?.click()`)
    await press('Import')
    console.log('import dialog answered:', answer('Import a stream list'), '|', await toast(), '| streams:', before, '->', (await snapshot()).rows.length)
    rmSync(list, { force: true })
    await js(`document.querySelector('.toast')?.click()`)
  }

  // The dialog and the theme switch.
  await js(`document.querySelector('button[aria-label="Add stream"]').click()`)
  await sleep(700)
  await shot('smoke-dialog')
  await js(`document.querySelector('dialog[open] button[aria-label="Close"]').click()`)
  await sleep(300)
  if (await js(`!!document.querySelector('dialog[open]')`)) console.log('WARNING: the dialog did not close')
  await js(`document.querySelector('.foot button').click()`)
  await sleep(400)
  // The window has no Windows frame: its own buttons must be there, and the
  // title bar must be what it is dragged by.
  console.log('window buttons:', await js(`[...document.querySelectorAll('.window .titlebar .chrome button')].map((b) => b.getAttribute('aria-label')).join(', ')`),
    '| title bar drag:', await js(`(() => { const c = getComputedStyle(document.querySelector('.window .titlebar')); return 'wails ' + c.getPropertyValue('--wails-draggable').trim() + ', app-region ' + c.getPropertyValue('app-region') })()`))
  // Reordering the streams by their handles: by mouse, by keyboard and (with
  // PARTYLINE_TEST_TOUCH as well) by touch. It adds two streams of its own
  // to have something to move, and deletes them again.
  if (process.env.PARTYLINE_TEST_REORDER) {
    const temp = ['zz-reorder-one', 'zz-reorder-two']
    const order = () => js(`[...document.querySelectorAll('.streams li .label')].map((e) => e.innerText).join(' > ')`)
    const row = (name) => `[...document.querySelectorAll('.streams li')].find((li) => li.querySelector('.label').innerText === ${JSON.stringify(name)})`
    const button = (re) => js(`[...document.querySelectorAll('dialog[open] .buttons button')].find((b) => ${re}.test(b.innerText.trim())).click()`)
    const before = await order()
    try {
      for (const name of temp) {
        await js(`document.querySelector('button[aria-label="Add stream"]').click()`); await sleep(500)
        await js(`(() => { const el = document.querySelector('dialog[open] input.mono'); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(el, ${JSON.stringify(name)}); el.dispatchEvent(new Event('input', { bubbles: true })) })()`)
        await button('/^Add$/'); await sleep(700)
      }
      console.log('reorder, start:', await order())
      const grip = async (name) => JSON.parse(await js(`(() => { const r = ${row(name)}.querySelector('.grip').getBoundingClientRect(); return JSON.stringify({ x: r.x + r.width / 2, y: r.y + r.height / 2 }) })()`))

      // Mouse: carry the last row to the top.
      const from = await grip(temp[1])
      const top = JSON.parse(await js(`(() => { const r = document.querySelector('.streams li .grip').getBoundingClientRect(); return JSON.stringify({ x: r.x + r.width / 2, y: r.y + r.height / 2 }) })()`))
      const mouse = (type, x, y) => send('Input.dispatchMouseEvent', { type, x, y, button: 'left', buttons: type === 'mouseReleased' ? 0 : 1, clickCount: 1, pointerType: 'mouse' })
      await mouse('mousePressed', from.x, from.y)
      for (let i = 1; i <= 8; i++) { await mouse('mouseMoved', from.x, from.y + (top.y - from.y) * i / 8); await sleep(30) }
      console.log('  while held:', await order(), '| carried row marked:', await js(`document.querySelector('.streams li.dragging .label')?.innerText ?? null`))
      await mouse('mouseReleased', top.x, top.y); await sleep(700)
      console.log('  after a mouse drag of the last row to the top:', await order())

      // Keyboard: the arrow keys on a handle.
      await js(`${row(temp[1])}.querySelector('.grip').focus()`)
      for (const type of ['keyDown', 'keyUp']) await send('Input.dispatchKeyEvent', { type, key: 'ArrowDown', code: 'ArrowDown', windowsVirtualKeyCode: 40 })
      await sleep(700)
      console.log('  after Arrow Down on its handle:', await order())

      // Touch: a real touch contact on a handle, dragged down one row.
      if (process.env.PARTYLINE_TEST_TOUCH) {
        const first = await js(`document.querySelector('.streams li .label').innerText`)
        const g = await grip(first)
        const scr = JSON.parse(await js(`JSON.stringify({ x: screenX, y: screenY, s: devicePixelRatio, pitch: document.querySelectorAll('.streams li')[1].getBoundingClientRect().top - document.querySelectorAll('.streams li')[0].getBoundingClientRect().top })`))
        const r = spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', join(import.meta.dirname, 'touch-drag.ps1'), '-AppId', app.pid,
          '-X', Math.round((scr.x + g.x) * scr.s), '-Y', Math.round((scr.y + g.y) * scr.s), '-DY', Math.round(scr.pitch * scr.s)], { encoding: 'utf8' })
        await sleep(900)
        console.log(`  after a touch drag of "${first}" down one row:`, r.status === 0 ? await order() : String(r.stderr).trim().split('\n')[0])
      }
    } finally {
      // Put things back: the temporary streams go, and whatever is left
      // must be in the order it started in.
      await js(`document.querySelector('dialog[open] button[aria-label="Close"]')?.click()`)
      for (const name of temp) {
        if (!(await js(`!!${row(name)}`))) continue
        await js(`${row(name)}.querySelector('.edit').click()`); await sleep(500)
        await button('/^Delete$/'); await sleep(300)
        await button('/^Really delete$/'); await sleep(700)
      }
      const after = await order()
      console.log('reorder, cleaned up:', after, after === before ? '(as it was)' : `(WARNING: was ${before})`)
    }
  }

  // Dragging by touch, which is also how a VR controller pointer can arrive
  // (Virtual Desktop and the like). A real touch contact is injected, so the
  // window must be in front and nothing else should be touched meanwhile.
  if (process.env.PARTYLINE_TEST_TOUCH) {
    const at = () => js(`JSON.stringify({ x: screenX, y: screenY })`).then(JSON.parse)
    const touchDrag = async (selector, dx, dy) => {
      // A spot in the element that isn't on one of its buttons.
      const from = JSON.parse(await js(`(() => {
        const r = document.querySelector('${selector}').getBoundingClientRect(), s = devicePixelRatio
        return JSON.stringify({ x: Math.round((screenX + r.left + r.width * 0.6) * s), y: Math.round((screenY + r.top + r.height / 2) * s) })
      })()`))
      const before = await at()
      const r = spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', join(import.meta.dirname, 'touch-drag.ps1'), '-AppId', app.pid, '-X', from.x, '-Y', from.y, '-DX', dx, '-DY', dy], { encoding: 'utf8' })
      await sleep(1500)
      const after = await at()
      return r.status === 0 ? `moved ${after.x - before.x},${after.y - before.y}` : r.stderr.trim().split('\n')[0]
    }
    // Buttons inside a drag area must still take a tap: here, the Send tab.
    const tap = async (find) => {
      const p = JSON.parse(await js(`(() => {
        const r = (${find}).getBoundingClientRect(), s = devicePixelRatio
        return JSON.stringify({ x: Math.round((screenX + r.left + r.width / 2) * s), y: Math.round((screenY + r.top + r.height / 2) * s) })
      })()`))
      const r = spawnSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', join(import.meta.dirname, 'touch-drag.ps1'), '-AppId', app.pid, '-X', p.x, '-Y', p.y], { encoding: 'utf8' })
      if (r.status !== 0) console.log('  tap not made:', String(r.stderr).trim().split('\n')[0])
      await sleep(600)
    }
    // A tap somewhere ordinary first, to know taps arrive at all.
    const theme = () => js(`document.documentElement.dataset.theme`)
    const themeWas = await theme()
    await tap(`document.querySelector('.foot button')`)
    const themeNow = await theme()
    if (themeNow !== themeWas) await tap(`document.querySelector('.foot button')`)
    console.log('touch tap on the theme switch:', themeWas, '->', themeNow, '->', await theme())
    const tab = (name) => `[...document.querySelectorAll('.tabs .tab')].find((b) => /${name}/.test(b.innerText))`
    await tap(tab('send'))
    const selected = () => js(`document.querySelector('.tabs .tab[aria-selected="true"]')?.innerText.trim()`)
    const onSend = await selected()
    await tap(tab('now-playing'))
    console.log('touch tap on the send tab selects:', onSend, '| and back:', await selected())
    console.log('touch drag by 120,80 on the title bar:', await touchDrag('.window .titlebar', 120, 80),
      '| back by the streams heading:', await touchDrag('.streams header', -120, -80))
  }

  // The wings fold, and the window narrows and widens with them.
  const width = () => js(`innerWidth`)
  const wide = await width()
  await js(`document.querySelector('.streams .fold').click()`); await sleep(900)
  const leftFolded = await width()
  await js(`document.querySelector('.wing-tab.left').click()`); await sleep(900)
  console.log('fold left wing:', wide, '->', leftFolded, '-> back to', await width())

  console.log('theme after toggle:', await js(`document.documentElement.dataset.theme`))
  await shot('smoke-other-theme')
  await js(`document.querySelector('.foot button').click()`)

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
