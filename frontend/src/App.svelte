<script lang="ts">
  import { onMount } from 'svelte'
  import NowPlaying from './lib/NowPlaying.svelte'
  import Send from './lib/Send.svelte'
  import StreamDialog from './lib/StreamDialog.svelte'
  import StreamList from './lib/StreamList.svelte'
  import {
    connect,
    errorText,
    idleState,
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

  async function setSendKey(key: string) {
    if (!api || !sendInfo) return
    await api.setSendKey(key) // errors surface next to the field
    sendInfo = { ...sendInfo, hasKey: key.trim() !== '' }
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

<svelte:window {onkeydown} />

{#snippet sendPanel()}
  {#if sendInfo}
    <Send
      info={sendInfo}
      current={sendState}
      levels={sendLevels}
      onsettings={saveSendSettings}
      onkey={setSendKey}
      onstart={() => call((a) => a.startSend())}
      onstop={() => call((a) => a.stopSend())}
      onrefresh={loadSendInfo}
    />
  {/if}
{/snippet}

{#if loaded}
  <div class="shell">
    <StreamList
      {streams}
      {player}
      disabled={!!engineError}
      onplay={play}
      onedit={(s) => (editing = s)}
      onadd={() => (editing = { ...blank })}
    />
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
  </div>

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
  .shell {
    display: grid;
    grid-template-columns: 264px minmax(0, 1fr);
    height: 100%;
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
  }
</style>
