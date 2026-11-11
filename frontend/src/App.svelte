<script lang="ts">
  import { onMount } from 'svelte'
  import NowPlaying from './lib/NowPlaying.svelte'
  import StreamDialog from './lib/StreamDialog.svelte'
  import StreamList from './lib/StreamList.svelte'
  import {
    connect,
    errorText,
    idleState,
    type Api,
    type AudioDevice,
    type PlayerState,
    type Settings,
    type Stream,
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
  let engine = $state('')
  let engineError = $state('')
  let engineHelp = $state('')
  let editing = $state.raw<Stream | null>(null)
  let toast = $state('')
  let toastTimer: ReturnType<typeof setTimeout> | undefined

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
    player = next
  }

  onMount(async () => {
    try {
      api = await connect()
      api.onState(applyState)
      api.onDevices((d) => (devices = d))
      const boot = await api.bootstrap()
      streams = boot.streams
      settings = boot.settings
      devices = boot.devices
      engine = boot.engine
      engineError = boot.engineError
      engineHelp = boot.engineHelp
      applyState(boot.state)
      if (boot.notice) notify(boot.notice)
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

  function play(id: string) {
    // Clicking the stream that's already up shouldn't cut it out.
    if (player.streamId === id && (player.status === 'playing' || player.status === 'connecting')) return
    call((a) => a.play(id))
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

  const blank: Stream = { id: '', label: '', source: '', transport: 'rtsp-tcp' }
</script>

<svelte:window {onkeydown} />

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
      {engine}
      {engineError}
      {engineHelp}
      hasStreams={streams.length > 0}
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
    left: 50%;
    bottom: 18px;
    transform: translateX(-50%);
    max-width: min(560px, calc(100vw - 48px));
    padding: 10px 14px;
    border: 1px solid color-mix(in srgb, var(--bad) 50%, transparent);
    border-radius: var(--radius);
    background: #2a1719;
    color: #ffd9d9;
    text-align: left;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
  }
</style>
