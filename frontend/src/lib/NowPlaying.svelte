<script lang="ts">
  import Icon from './Icon.svelte'
  import Sparkline from './Sparkline.svelte'
  import type { AudioDevice, PlayerState, Settings, Status } from './api'

  interface Props {
    player: PlayerState
    settings: Settings
    devices: AudioDevice[]
    history: number[]
    engine: string
    engineError: string
    engineHelp: string
    hasStreams: boolean
    onstop: () => void
    onresync: () => void
    onretry: () => void
    onvolume: (volume: number, commit: boolean) => void
    onsettings: (patch: Partial<Settings>) => void
  }
  let {
    player,
    settings,
    devices,
    history,
    engine,
    engineError,
    engineHelp,
    hasStreams,
    onstop,
    onresync,
    onretry,
    onvolume,
    onsettings,
  }: Props = $props()

  const statusText: Record<Status, string> = {
    idle: 'Idle',
    connecting: 'Connecting',
    playing: 'Live',
    reconnecting: 'Waiting for stream',
    failed: 'Stopped',
  }

  let now = $state(Date.now())
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(timer)
  })

  let uptime = $derived.by(() => {
    if (player.status !== 'playing' || !player.sinceMs) return '–'
    const total = Math.max(0, Math.floor((now - player.sinceMs) / 1000))
    const h = Math.floor(total / 3600)
    const m = Math.floor((total % 3600) / 60)
    const s = total % 60
    const pad = (n: number) => String(n).padStart(2, '0')
    return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
  })

  let format = $derived.by(() => {
    const { codec, sampleRate, channels } = player.info
    if (!codec) return ''
    const parts = [codec.toUpperCase()]
    if (sampleRate) parts.push(`${sampleRate / 1000} kHz`)
    if (channels) parts.push(channels === 2 ? 'stereo' : channels === 1 ? 'mono' : `${channels} ch`)
    return parts.join(' · ')
  })

  let overThreshold = $derived(player.bufferMs > settings.catchUpAboveMs)
  let busy = $derived(player.status !== 'idle' && player.status !== 'failed')

  // Keep a saved device selectable even while it's unplugged.
  let deviceOptions = $derived(
    devices.some((d) => d.name === settings.audioDevice)
      ? devices
      : [
          ...devices,
          {
            name: settings.audioDevice,
            description: settings.audioDevice === 'auto' ? 'System default' : `${settings.audioDevice} (not connected)`,
          },
        ],
  )
</script>

<main>
  {#if engineError}
    <section class="callout" role="alert">
      <h1>Audio engine not available</h1>
      <p>{engineHelp}</p>
      <p class="mono detail">{engineError}</p>
    </section>
  {:else}
    <section class="now" data-status={player.status}>
      <div class="status-line">
        <span class="pill"><span class="dot"></span>{statusText[player.status]}</span>
        {#if player.catchingUp}<span class="tag">Catching up</span>{/if}
      </div>

      {#if player.status === 'idle'}
        <h1 class="quiet">Nothing playing</h1>
        <p class="sub">
          {hasStreams ? 'Pick a stream on the left, or press 1–9.' : 'Add a stream on the left to get started.'}
        </p>
      {:else}
        <h1>{player.label}</h1>
        <p class="sub mono" title={player.url}>{player.url}</p>
        {#if player.message}
          <p class="message num">
            {player.status === 'reconnecting'
              ? `${player.message} · retrying (attempt ${player.attempt})`
              : player.message}
          </p>
        {/if}
      {/if}
    </section>

    <section class="meters">
      <div class="meter buffer" class:over={overThreshold}>
        <div class="meter-head">
          <span class="meter-label">Buffer</span>
          <span class="value num">
            {#if player.status === 'playing' && player.bufferMs >= 0}
              {player.bufferMs}<small>ms</small>
            {:else}
              –
            {/if}
          </span>
        </div>
        <Sparkline samples={history} threshold={settings.catchUpAboveMs} />
      </div>
      <div class="meter">
        <div class="meter-head">
          <span class="meter-label">Live for</span>
          <span class="value num">{uptime}</span>
        </div>
        <p class="format">{format || ' '}</p>
      </div>
    </section>

    <section class="actions">
      {#if player.status === 'failed'}
        <button class="btn primary" onclick={onretry}><Icon name="play" />Try again</button>
      {:else}
        <button class="btn" onclick={onstop} disabled={!busy}><Icon name="stop" />Stop</button>
      {/if}
      <button
        class="btn"
        onclick={onresync}
        disabled={player.status !== 'playing' && player.status !== 'reconnecting'}
        title={player.status === 'reconnecting' ? 'Retry now' : 'Drop buffered audio and jump to live'}
      >
        <Icon name="resync" />{player.status === 'reconnecting' ? 'Retry now' : 'Resync'}
      </button>
    </section>
  {/if}

  <section class="controls">
    <label class="control">
      <span>Volume</span>
      <input
        type="range"
        min="0"
        max="100"
        step="1"
        value={settings.volume}
        oninput={(e) => onvolume(+e.currentTarget.value, false)}
        onchange={(e) => onvolume(+e.currentTarget.value, true)}
        disabled={!!engineError}
      />
      <output class="num">{Math.round(settings.volume)}</output>
    </label>

    <label class="control">
      <span>Output</span>
      <select
        class="input"
        value={settings.audioDevice}
        onchange={(e) => onsettings({ audioDevice: e.currentTarget.value })}
        disabled={!!engineError}
      >
        {#each deviceOptions as d (d.name)}
          <option value={d.name}>{d.description}</option>
        {/each}
      </select>
    </label>

    <div class="toggles">
      <label class="toggle" title="Keep retrying while a stream is offline, and reconnect if it drops">
        <input
          type="checkbox"
          checked={settings.autoReconnect}
          onchange={(e) => onsettings({ autoReconnect: e.currentTarget.checked })}
        />
        Auto-reconnect
      </label>
      <label class="toggle" title="Play slightly fast to drain delay that builds up after network hiccups">
        <input
          type="checkbox"
          checked={settings.autoCatchUp}
          onchange={(e) => onsettings({ autoCatchUp: e.currentTarget.checked })}
        />
        Auto catch-up above
      </label>
      <label class="threshold">
        <input
          class="input num"
          type="number"
          min="100"
          max="5000"
          step="50"
          value={settings.catchUpAboveMs}
          onchange={(e) => onsettings({ catchUpAboveMs: +e.currentTarget.value || settings.catchUpAboveMs })}
          disabled={!settings.autoCatchUp}
          aria-label="Catch-up threshold in milliseconds"
        />
        ms
      </label>
    </div>
  </section>

  <footer class="mono">{engine}</footer>
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-width: 0;
    padding: 24px 28px 12px;
    overflow-y: auto;
  }

  section {
    flex: none;
  }

  /* Now playing */
  .now {
    --status: var(--faint);
    /* Room for the message line, so the meters don't jump when it appears. */
    min-height: 128px;
  }
  .now[data-status='playing'] {
    --status: var(--ok);
  }
  .now[data-status='connecting'] {
    --status: var(--info);
  }
  .now[data-status='reconnecting'] {
    --status: var(--warn);
  }
  .now[data-status='failed'] {
    --status: var(--bad);
  }

  .status-line {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }

  .pill,
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 24px;
    padding: 0 10px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 600;
  }

  .pill {
    background: color-mix(in srgb, var(--status) 14%, transparent);
    color: var(--status);
  }

  .now[data-status='idle'] .pill {
    color: var(--muted);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
  }

  .now[data-status='connecting'] .dot,
  .now[data-status='reconnecting'] .dot {
    animation: pulse 1.2s ease-in-out infinite;
  }

  .tag {
    border: 1px solid var(--line);
    color: var(--warn);
  }

  h1 {
    font-size: 28px;
    font-weight: 650;
    line-height: 1.2;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  h1.quiet {
    color: var(--muted);
  }

  .sub {
    margin-top: 4px;
    color: var(--muted);
    font-size: 12.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub:not(.mono) {
    font-size: 14px;
  }

  .message {
    margin-top: 8px;
    color: var(--status);
    overflow-wrap: anywhere;
  }

  /* Meters */
  .meters {
    display: grid;
    grid-template-columns: 1.6fr 1fr;
    gap: 12px;
  }

  .meter {
    padding: 12px 14px 10px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--panel);
    color: var(--muted);
    min-width: 0;
  }

  .meter-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }

  .meter-label {
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .value {
    color: var(--text);
    font-size: 26px;
    font-weight: 600;
    line-height: 1.1;
  }

  .value small {
    margin-left: 3px;
    color: var(--muted);
    font-size: 13px;
    font-weight: 500;
  }

  .buffer {
    color: var(--ok);
  }

  .buffer .meter-label {
    color: var(--muted);
  }

  .buffer.over {
    color: var(--warn);
  }

  .buffer.over .value {
    color: var(--warn);
  }

  .format {
    height: 44px;
    display: flex;
    align-items: flex-end;
    font-size: 12.5px;
  }

  .actions {
    display: flex;
    gap: 8px;
  }

  /* Controls */
  .controls {
    display: grid;
    gap: 12px;
    padding-top: 18px;
    border-top: 1px solid var(--line);
  }

  .control {
    display: grid;
    grid-template-columns: 64px 1fr 32px;
    align-items: center;
    gap: 12px;
  }

  .control > span {
    color: var(--muted);
    font-size: 13px;
  }

  .control select {
    grid-column: 2 / -1;
    min-width: 0;
  }

  output {
    text-align: right;
    color: var(--muted);
  }

  input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  .toggles {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 20px;
    padding-left: 76px;
  }

  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  .toggle input {
    width: 15px;
    height: 15px;
    margin: 0;
    accent-color: var(--accent);
  }

  .threshold {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: -12px;
    color: var(--muted);
    font-size: 13px;
  }

  .threshold .input {
    width: 72px;
    height: 28px;
    padding: 0 6px;
  }

  footer {
    margin-top: auto;
    padding-top: 4px;
    color: var(--faint);
    font-size: 11px;
    min-height: 16px;
  }

  /* Missing libmpv */
  .callout {
    display: grid;
    gap: 10px;
    padding: 18px 20px;
    border: 1px solid color-mix(in srgb, var(--warn) 45%, transparent);
    border-radius: 10px;
    background: color-mix(in srgb, var(--warn) 8%, transparent);
    user-select: text;
  }

  .callout h1 {
    font-size: 18px;
    white-space: normal;
  }

  .detail {
    color: var(--muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }

  @keyframes pulse {
    50% {
      opacity: 0.3;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none !important;
    }
  }
</style>
