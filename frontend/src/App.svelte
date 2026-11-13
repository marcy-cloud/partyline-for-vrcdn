<script lang="ts">
  import { onMount } from 'svelte'
  import { Window } from '@wailsio/runtime'
  import NowPlaying from './lib/NowPlaying.svelte'
  import Send from './lib/Send.svelte'
  import StreamDialog from './lib/StreamDialog.svelte'
  import StreamList from './lib/StreamList.svelte'
  import TargetDialog from './lib/TargetDialog.svelte'
  import TargetList from './lib/TargetList.svelte'
  import {
    connect,
    errorText,
    idleState,
    inWails,
    isCustomSource,
    sendOff,
    silentLevels,
    type Api,
    type AudioDevice,
    type Levels,
    type PlayerState,
    type SendInfo,
    type SendSettings,
    type SendState,
    type SendTarget,
    type SendTargetDraft,
    type Settings,
    type Stream,
    type Transport,
  } from './lib/api'

  // How many buffer readings the sparkline shows (4 per second).
  const HISTORY = 120

  let api: Api | undefined
  let loaded = $state(false)
  let streams = $state.raw<Stream[]>([])
  let settings = $state<Settings>({
    audioDevice: 'auto',
    volume: 100,
    autoReconnect: true,
    autoCatchUp: true,
    catchUpAboveMs: 250,
  })
  let player = $state.raw<PlayerState>(idleState)
  let devices = $state.raw<AudioDevice[]>([])
  let history = $state.raw<number[]>([])
  let levels = $state.raw<Levels>(silentLevels)

  // The sender (the Send tab). Hidden where the build can't send.
  let view = $state<'listen' | 'send'>('listen')
  let sendInfo = $state.raw<SendInfo | null>(null)
  let sendState = $state.raw<SendState>(sendOff)
  let sendLevels = $state.raw<Levels>(silentLevels)
  let onAir = $derived(sendState.status !== 'off' && sendState.status !== 'failed')
  let engine = $state('')
  let engineError = $state('')
  let engineHelp = $state('')
  let canProtect = $state(false)
  let editing = $state.raw<Stream | null>(null)
  let toast = $state('')
  let toastTimer: ReturnType<typeof setTimeout> | undefined

  // What the Play button starts: the stream that was on last, or the first
  // one in the list when nothing has been played yet.
  let lastId = $state('')
  let resume = $derived(streams.find((s) => s.id === lastId) ?? streams[0])

  let editingIndex = $derived(editing ? streams.findIndex((s) => s.id === editing!.id) : -1)

  function notify(message: string) {
    toast = message
    clearTimeout(toastTimer)
    toastTimer = setTimeout(() => (toast = ''), 6000)
  }

  function applyState(next: PlayerState) {
    if (next.status === 'playing' && next.bufferMs >= 0) {
      history = [...history, next.bufferMs].slice(-HISTORY)
    } else if (history.length > 0) {
      history = []
    }
    if (next.streamId) lastId = next.streamId
    if (next.status !== 'playing') levels = silentLevels
    player = next
  }

  // How far the meter may fall per reading (dB): peaks jump up at once and
  // sink back, like a hardware meter, so short ones stay visible.
  const METER_FALL = 2.5

  function applySend(next: SendState) {
    if (next.status !== 'live') sendLevels = silentLevels
    sendState = next
  }

  function applySendLevels(next: Levels) {
    if (sendState.status !== 'live') return
    sendLevels = {
      left: Math.max(next.left, sendLevels.left - METER_FALL),
      right: Math.max(next.right, sendLevels.right - METER_FALL),
    }
  }

  async function loadSendInfo() {
    const info = await call((a) => a.sendInfo())
    if (info) sendInfo = info
  }

  async function saveSendSettings(patch: Partial<SendSettings>) {
    if (!sendInfo) return
    const settings = { ...sendInfo.settings, ...patch }
    sendInfo = { ...sendInfo, settings }
    const saved = await call((a) => a.saveSendSettings(settings))
    if (saved && sendInfo) sendInfo = { ...sendInfo, settings: saved }
  }

  // Send targets (the right wing).
  let editingTarget = $state.raw<SendTarget | null>(null)

  async function saveSendTarget(target: SendTargetDraft) {
    if (!api || !sendInfo) return
    const saved = await api.saveSendTarget(target) // errors surface in the dialog
    const targets = target.id ? sendInfo.targets.map((t) => (t.id === saved.id ? saved : t)) : [...sendInfo.targets, saved]
    sendInfo = { ...sendInfo, targets }
    editingTarget = null
    // A first or newly added target is the one you meant to send to.
    if (!target.id) saveSendSettings({ targetId: saved.id })
  }

  async function deleteSendTarget(id: string) {
    if (!api || !sendInfo) return
    const targets = await api.deleteSendTarget(id)
    const settings = sendInfo.settings.targetId === id ? { ...sendInfo.settings, targetId: targets[0]?.id ?? '' } : sendInfo.settings
    sendInfo = { ...sendInfo, targets, settings }
    editingTarget = null
  }

  // The wings fold away, and the window narrows or widens to match so the
  // middle stays where it is on screen.
  const WING = 248 // .wing's width in base.css
  const WING_TAB = 22
  function savedWings(): { left: boolean; right: boolean } {
    try {
      return { left: true, right: true, ...JSON.parse(localStorage.getItem('wings') ?? '{}') }
    } catch {
      return { left: true, right: true } // storage can be unavailable
    }
  }
  let wings = $state(savedWings())

  async function resizeBy(delta: number, shiftLeft: boolean) {
    if (!inWails() || maximised) return
    const [size, pos] = await Promise.all([Window.Size(), Window.Position()])
    if (shiftLeft) await Window.SetPosition(pos.x - delta, pos.y)
    await Window.SetSize(size.width + delta, size.height)
  }

  function setWing(side: 'left' | 'right', open: boolean) {
    if (wings[side] === open) return
    wings[side] = open
    try {
      localStorage.setItem('wings', JSON.stringify(wings))
    } catch {
      // As above.
    }
    resizeBy((open ? 1 : -1) * (WING - WING_TAB), side === 'left')
  }

  function applyLevels(next: Levels) {
    if (player.status !== 'playing') return
    levels = {
      left: Math.max(next.left, levels.left - METER_FALL),
      right: Math.max(next.right, levels.right - METER_FALL),
    }
  }

  onMount(async () => {
    try {
      api = await connect()
      api.onState(applyState)
      api.onDevices((d) => (devices = d))
      api.onLevels(applyLevels)
      api.onSend(applySend)
      api.onSendLevels(applySendLevels)
      const boot = await api.bootstrap()
      streams = boot.streams
      settings = boot.settings
      devices = boot.devices
      engine = boot.engine
      engineError = boot.engineError
      engineHelp = boot.engineHelp
      canProtect = boot.secrets
      applyState(boot.state)
      if (boot.notice) notify(boot.notice)
      sendInfo = await api.sendInfo()
      applySend(sendInfo.state)
      // The window opens sized for both wings; fold what was left folded.
      const folded = (wings.left ? 0 : 1) + (wings.right && sendInfo.supported ? 0 : 1)
      if (folded && inWails()) {
        const size = await Window.Size()
        await Window.SetSize(size.width - folded * (WING - WING_TAB) - (sendInfo.supported ? 0 : WING_TAB), size.height)
      }
      if (new URLSearchParams(location.search).get('demo') === 'sending') view = 'send'
    } catch (e) {
      engineError = errorText(e)
    }
    loaded = true
  })

  async function call<T>(action: (api: Api) => Promise<T>): Promise<T | undefined> {
    if (!api) return
    try {
      return await action(api)
    } catch (e) {
      notify(errorText(e))
    }
  }

  function play(id: string, restart = false) {
    // Clicking the stream that's already up shouldn't cut it out.
    if (!restart && player.streamId === id && (player.status === 'playing' || player.status === 'connecting')) return
    call((a) => a.play(id))
  }

  // Changing the connection from the main page saves it on the stream and,
  // if that stream is on, reconnects it the new way straight away.
  async function setTransport(transport: Transport) {
    if (!resume) return
    const saved = await call((a) => a.saveStream({ ...resume, transport }))
    if (!saved) return
    streams = streams.map((s) => (s.id === saved.id ? saved : s))
    if (player.streamId === saved.id && player.status !== 'idle' && player.status !== 'failed') play(saved.id, true)
  }

  async function saveStream(stream: Stream, andPlay: boolean) {
    if (!api) return
    const saved = await api.saveStream(stream) // errors surface in the dialog
    streams = stream.id ? streams.map((s) => (s.id === saved.id ? saved : s)) : [...streams, saved]
    editing = null
    if (andPlay) play(saved.id)
  }

  async function deleteStream(id: string) {
    if (!api) return
    if (player.streamId === id) await api.stop()
    streams = await api.deleteStream(id)
    editing = null
  }

  const count = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

  async function exportStreams() {
    const res = await call((a) => a.exportStreams())
    if (!res?.path) return
    const left = res.private ? ` ${count(res.private, 'private stream')} left out.` : ''
    const file = res.path.split(/[\\/]/).pop()
    notify(`Saved ${count(res.count, 'stream')} to ${file}.${left}`)
  }

  async function importStreams() {
    const res = await call((a) => a.importStreams())
    if (!res || res.cancelled) return
    streams = res.streams
    const parts = [`Added ${count(res.added, 'stream')}`]
    if (res.known) parts.push(`${res.known} already here`)
    if (res.invalid) parts.push(`${res.invalid} couldn't be read`)
    notify(parts.join(' · '))
  }

  async function moveStream(id: string, to: number) {
    if (!api) return
    streams = await api.moveStream(id, to)
  }

  async function updateSettings(patch: Partial<Settings>) {
    settings = { ...settings, ...patch }
    const saved = await call((a) => a.updateSettings($state.snapshot(settings)))
    if (saved) settings = saved
  }

  function setVolume(volume: number, commit: boolean) {
    settings.volume = volume
    if (commit) updateSettings({ volume })
    else call((a) => a.setVolume(volume))
  }

  // The window has no Windows title bar, so the app tracks this itself.
  let maximised = $state(false)
  async function syncMaximised() {
    if (inWails()) maximised = await Window.IsMaximised()
  }
  async function toggleMaximise() {
    if (!inWails()) return
    await Window.ToggleMaximise()
    syncMaximised()
  }

  function onkeydown(e: KeyboardEvent) {
    if (editing || e.ctrlKey || e.metaKey || e.altKey) return
    const target = e.target as HTMLElement
    if (target.matches('input, select, textarea')) return
    if (e.key >= '1' && e.key <= '9') {
      const stream = streams[+e.key - 1]
      if (stream && !engineError) play(stream.id)
    }
  }

  const blank: Stream = { id: '', label: '', source: '', transport: 'rtsp' }
</script>

<svelte:window {onkeydown} onresize={syncMaximised} />

{#snippet sendPanel()}
  {#if sendInfo}
    <Send
      info={sendInfo}
      current={sendState}
      levels={sendLevels}
      onsettings={saveSendSettings}
      onstart={() => call((a) => a.startSend())}
      onstop={() => call((a) => a.stopSend())}
      onrefresh={loadSendInfo}
    />
  {/if}
{/snippet}

{#if loaded}
  <div class="shell">
    {#if !wings.left}
      <button class="wing-tab left" onclick={() => setWing('left', true)} aria-label="Show streams" title="Streams">›</button>
    {:else}
    <StreamList
      onfold={() => setWing('left', false)}
      {streams}
      {player}
      disabled={!!engineError}
      onplay={play}
      onedit={(s) => (editing = s)}
      onadd={() => (editing = { ...blank })}
      onexport={exportStreams}
      onimport={importStreams}
    />
    {/if}
    <NowPlaying
      {player}
      {settings}
      {devices}
      {history}
      {levels}
      {engine}
      {engineError}
      {engineHelp}
      hasStreams={streams.length > 0}
      resumeLabel={resume?.label ?? ''}
      transport={resume && !isCustomSource(resume.source) ? resume.transport : ''}
      ontransport={setTransport}
      {maximised}
      ontogglemaximise={toggleMaximise}
      {view}
      onview={(v) => {
        view = v
        if (v === 'send') loadSendInfo() // inputs may have been plugged in since
      }}
      {onAir}
      send={sendInfo?.supported ? sendPanel : undefined}
      onplay={() => resume && play(resume.id)}
      onstop={() => call((a) => a.stop())}
      onresync={() => call((a) => a.resync())}
      onretry={() => player.streamId && call((a) => a.play(player.streamId))}
      onvolume={setVolume}
      onsettings={updateSettings}
    />
    {#if sendInfo?.supported}
      {#if wings.right}
        <TargetList
          targets={sendInfo.targets}
          selectedId={sendInfo.settings.targetId}
          ingests={sendInfo.ingests}
          {onAir}
          onselect={(id) => {
            saveSendSettings({ targetId: id })
            view = 'send'
          }}
          onedit={(t) => (editingTarget = t)}
          onadd={() => (editingTarget = { id: '', label: '', ingest: sendInfo?.ingests[0]?.url ?? '' })}
          onfold={() => setWing('right', false)}
        />
      {:else}
        <button class="wing-tab right" onclick={() => setWing('right', true)} aria-label="Show send targets" title="Send to">‹</button>
      {/if}
    {/if}
  </div>

  {#if sendInfo}
    <TargetDialog
      target={editingTarget}
      ingests={sendInfo.ingests}
      onsave={saveSendTarget}
      ondelete={deleteSendTarget}
      onclose={() => (editingTarget = null)}
    />
  {/if}

  <StreamDialog
    stream={editing}
    index={editingIndex}
    count={streams.length}
    canPlay={!engineError}
    {canProtect}
    onsave={saveStream}
    ondelete={deleteStream}
    onmove={moveStream}
    onclose={() => (editing = null)}
  />

  {#if toast}
    <button class="toast" onclick={() => (toast = '')} title="Dismiss">{toast}</button>
  {/if}
{/if}

<style>
  /* The window in the middle with a wing on each side. Everything around
     them is see-through: the app window itself has no frame or fill. */
  .shell {
    display: flex;
    align-items: center;
    height: 100%;
    /* Room for the middle window's sticker shadow. */
    padding: 0 0 6px;
  }

  .toast {
    position: fixed;
    right: var(--gt-space-4);
    bottom: var(--gt-space-4);
    max-width: min(480px, calc(100vw - 2 * var(--gt-space-4)));
    padding: 10px 14px;
    border: var(--gt-border-width) solid var(--gt-coral-text);
    border-radius: var(--gt-radius-md);
    background: color-mix(in oklab, var(--gt-coral-text) 14%, var(--gt-surface));
    color: var(--gt-text);
    font-weight: 600;
    text-align: left;
    overflow-wrap: anywhere;
    /* Above the middle window, which is raised over the wings. */
    z-index: 20;
  }
</style>
