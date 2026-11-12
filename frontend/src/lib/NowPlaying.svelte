<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from './Icon.svelte'
  import Sparkline from './Sparkline.svelte'
  import ThemeToggle from './ThemeToggle.svelte'
  import WindowControls from './WindowControls.svelte'
  import { transports, type AudioDevice, type Levels, type PlayerState, type Settings, type Status, type Transport } from './api'

  interface Props {
    player: PlayerState
    settings: Settings
    devices: AudioDevice[]
    history: number[]
    levels: Levels
    engine: string
    engineError: string
    engineHelp: string
    hasStreams: boolean
    /** Label of the stream the Play button starts; '' when there is none. */
    resumeLabel: string
    /** Connection type of that stream; '' when there is none or it is a custom URL. */
    transport: Transport | ''
    ontransport: (transport: Transport) => void
    onplay: () => void
    /** Whether the window is maximised, for the maximise button's label. */
    maximised: boolean
    ontogglemaximise: () => void
    /** Which tab is showing. The Send tab's content comes in as a snippet. */
    view: 'listen' | 'send'
    onview: (view: 'listen' | 'send') => void
    send?: Snippet
    /** True while a broadcast is going out, shown as a dot on the Send tab. */
    onAir: boolean
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
    levels,
    engine,
    engineError,
    engineHelp,
    hasStreams,
    resumeLabel,
    transport,
    ontransport,
    onplay,
    maximised,
    ontogglemaximise,
    view,
    onview,
    send,
    onAir,
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
    if (player.via) parts.push(player.via.toUpperCase())
    return parts.join(' · ')
  })

  // The meter covers the top 60 dB: -60 dBFS and below is empty, 0 is full.
  const METER_RANGE = 60
  const meterFill = (db: number) => `${Math.min(Math.max((db + METER_RANGE) / METER_RANGE, 0), 1) * 100}%`

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
  <div class="window">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="titlebar" ondblclick={(e) => e.target === e.currentTarget && ontogglemaximise()}>
      {#if send}
        <span class="tabs" role="tablist">
          <button class="tab pixel" role="tab" aria-selected={view === 'listen'} onclick={() => onview('listen')}>
            now-playing.exe
          </button>
          <button class="tab pixel" role="tab" aria-selected={view === 'send'} onclick={() => onview('send')}>
            send.exe{#if onAir}<span class="on-air" aria-label="on air"></span>{/if}
          </button>
        </span>
      {:else}
        <span class="pixel" aria-hidden="true">now-playing.exe</span>
      {/if}
      <span class="chrome">
        <WindowControls {maximised} />
      </span>
    </div>

    <div class="body">
      {#if view === 'send' && send}
        {@render send()}
      {:else}
      {#if engineError}
        <section class="callout" role="alert">
          <h1>Audio engine not available</h1>
          <p>{engineHelp}</p>
          <p class="mono detail">{engineError}</p>
        </section>
      {:else}
        <section class="now" data-status={player.status}>
          <div class="status-line">
            <span class="badge pixel"><span class="dot"></span>{statusText[player.status]}</span>
            {#if player.catchingUp}<span class="badge pixel catching">Catching up</span>{/if}
          </div>

          {#if player.status === 'idle'}
            <h1 class="quiet">Nothing playing</h1>
            <p class="sub">
              {hasStreams ? 'Pick a stream on the left, or press 1–9.' : 'Add a stream on the left to get started.'}
            </p>
            <div class="horizon" aria-hidden="true">
              <div class="sun"></div>
              <div class="grid"></div>
            </div>
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
              <span class="meter-label pixel">Buffer</span>
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
              <span class="meter-label pixel">Live for</span>
              <span class="value num">{uptime}</span>
            </div>
            <p class="format">{format || ' '}</p>
          </div>
        </section>

        <section class="actions">
          {#if player.status === 'failed'}
            <button class="btn primary" onclick={onretry}><Icon name="play" />Try again</button>
          {:else}
            <!-- One button for both, so it keeps keyboard focus and Stop then
                 Play is two presses in the same place: pink to stop, mint to
                 play. -->
            <button
              class="btn candy toggle-play"
              class:go={!busy}
              onclick={busy ? onstop : onplay}
              disabled={!busy && !resumeLabel}
              title={busy ? 'Stop' : resumeLabel ? `Play ${resumeLabel}` : 'Add a stream first'}
            >
              <Icon name={busy ? 'stop' : 'play'} />{busy ? 'Stop' : 'Play'}
            </button>
          {/if}
          <button
            class="btn"
            onclick={onresync}
            disabled={player.status !== 'playing' && player.status !== 'reconnecting'}
            title={player.status === 'reconnecting' ? 'Retry now' : 'Drop buffered audio and jump to live'}
          >
            <Icon name="resync" />{player.status === 'reconnecting' ? 'Retry now' : 'Resync'}
          </button>

          <label
            class="connection"
            title={transport ? `How ${resumeLabel} is pulled from VRCDN` : 'Custom URLs are played exactly as written'}
          >
            <span class="pixel" aria-hidden="true">via</span>
            <select
              class="input"
              aria-label="Connection"
              value={transport}
              disabled={!transport}
              onchange={(e) => ontransport(e.currentTarget.value as Transport)}
            >
              {#if !transport}<option value="">–</option>{/if}
              {#each transports as t (t.value)}
                <option value={t.value}>{t.label}</option>
              {/each}
            </select>
          </label>
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
            style="--fill: {settings.volume}%"
            oninput={(e) => onvolume(+e.currentTarget.value, false)}
            onchange={(e) => onvolume(+e.currentTarget.value, true)}
            disabled={!!engineError}
          />
          <output class="num">{Math.round(settings.volume)}</output>
        </label>

        <div class="control" title="Level of the incoming audio, before the volume control">
          <span>Level</span>
          <div
            class="level"
            role="img"
            aria-label="Incoming level: left {Math.round(levels.left)} dB, right {Math.round(levels.right)} dB"
          >
            <div class="bar" style="--fill: {meterFill(levels.left)}"></div>
            <div class="bar" style="--fill: {meterFill(levels.right)}"></div>
          </div>
          <span class="lr pixel" aria-hidden="true">L<br />R</span>
        </div>

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

      {/if}
    </div>

    <div class="foot">
      <ThemeToggle />
      <span class="mono engine">{engine}</span>
    </div>
  </div>
</main>

<style>
  /* The window in the middle of the app; the wings attach to its sides. */
  main {
    flex: 1;
    align-self: stretch;
    min-width: 0;
    min-height: 0;
    /* Above the wings, so its sticker shadow falls across them. */
    position: relative;
    z-index: 1;
  }

  .window {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .body {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--gt-space-4);
    min-height: 0;
    padding: var(--gt-space-4) 20px 0;
    overflow-y: auto;
  }

  .body :global(section) {
    flex: none;
  }

  /* Tabs in the title bar. */
  .tabs {
    display: flex;
    gap: var(--gt-space-1);
    margin-left: calc(-1 * var(--gt-space-2));
  }

  .tabs .tab {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    width: auto;
    height: 24px;
    padding: 0 var(--gt-space-2);
    font: var(--gt-text-pixel) / 1 var(--gt-font-pixel);
    letter-spacing: 0.04em;
    opacity: 0.65;
  }

  .tabs .tab[aria-selected='true'] {
    background: color-mix(in oklab, var(--gt-white) 45%, transparent);
    opacity: 1;
  }

  .on-air {
    width: 8px;
    height: 8px;
    border: var(--gt-border-width) solid var(--gt-ink);
    border-radius: 50%;
    background: var(--gt-mint);
    box-sizing: content-box;
  }

  /* Now playing. The rules written as .body :global(...) also style the Send
     tab, whose content is rendered inside this component's body. */
  .now {
    position: relative;
    --status: var(--gt-lavender);
    --status-text: var(--gt-text-muted);
    /* Room for the message line, so the meters don't jump when it appears. */
    min-height: 116px;
  }
  .now[data-status='playing'] {
    --status: var(--gt-mint);
    --status-text: var(--gt-mint-text);
  }
  .now[data-status='connecting'] {
    --status: var(--gt-aqua);
    --status-text: var(--gt-aqua-text);
  }
  .now[data-status='reconnecting'] {
    --status: var(--gt-butter);
    --status-text: var(--gt-butter-text);
  }
  .now[data-status='failed'] {
    --status: var(--gt-coral-text);
    --status-text: var(--gt-coral-text);
  }

  .body :global(.status-line) {
    display: flex;
    align-items: center;
    gap: var(--gt-space-2);
    margin-bottom: var(--gt-space-2);
  }

  .body :global(.badge) {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 26px;
    padding: 0 var(--gt-space-3);
    border-radius: var(--gt-radius-pill);
    background: color-mix(in oklab, var(--status) 22%, var(--gt-surface));
    color: var(--status-text);
  }

  .badge.catching {
    --status: var(--gt-butter);
    --status-text: var(--gt-butter-text);
  }

  .body :global(.dot) {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: currentColor;
  }

  .now[data-status='connecting'] .dot,
  .now[data-status='reconnecting'] .dot {
    animation: pulse 1.2s ease-in-out infinite;
  }

  .body :global(h1) {
    font-size: var(--gt-text-h1);
    line-height: 1.2;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  h1.quiet {
    color: var(--gt-text-muted);
  }

  .body :global(.sub) {
    margin-top: 2px;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .body :global(.sub:not(.mono)) {
    font-size: var(--gt-text-body);
  }

  .body :global(.message) {
    margin-top: 6px;
    color: var(--status-text);
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  /* Idle decoration: a striped sun setting behind a grid horizon. It sits
     beside the text, never behind it. */
  .horizon {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 200px;
    height: 100px;
    overflow: hidden;
    pointer-events: none;
  }

  .sun {
    position: absolute;
    left: 50%;
    bottom: 12px;
    width: 84px;
    aspect-ratio: 1;
    translate: -50% 0;
    border-radius: 50%;
    background: var(--gt-sunset);
    /* Stripes: cut-outs that get thicker towards the bottom. */
    -webkit-mask: linear-gradient(
      #000 52%,
      transparent 52% 56%,
      #000 56% 65%,
      transparent 65% 71%,
      #000 71% 79%,
      transparent 79% 87%,
      #000 87%
    );
    mask: linear-gradient(#000 52%, transparent 52% 56%, #000 56% 65%, transparent 65% 71%, #000 71% 79%, transparent 79% 87%, #000 87%);
  }

  .grid {
    position: absolute;
    inset: auto -40% 0;
    height: 40px;
    --grid-line: var(--gt-lavender);
    background:
      repeating-linear-gradient(90deg, var(--grid-line) 0 1px, transparent 1px 22px),
      repeating-linear-gradient(0deg, var(--grid-line) 0 1px, transparent 1px 10px) var(--gt-surface);
    transform: perspective(70px) rotateX(50deg);
    transform-origin: bottom;
    animation: scroll 8s linear infinite;
  }

  :global([data-theme='dark']) .grid {
    --grid-line: var(--gt-pink);
  }

  @keyframes scroll {
    to {
      background-position:
        0 0,
        0 10px;
    }
  }

  /* Meters */
  .meters {
    display: grid;
    grid-template-columns: 1.6fr 1fr;
    gap: var(--gt-space-3);
  }

  .meter {
    padding: 10px 14px var(--gt-space-2);
    border-radius: var(--gt-radius-md);
    background: var(--gt-surface-raised);
    color: var(--gt-text-muted);
    min-width: 0;
  }

  .meter-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--gt-space-3);
  }

  .meter-label {
    color: var(--gt-text-muted);
    text-transform: lowercase;
  }

  .value {
    color: var(--gt-text);
    font: 600 26px / 1.1 var(--gt-font-display);
  }

  .value small {
    margin-left: 3px;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
    font-weight: 500;
  }

  /* The trace takes the meter's colour. */
  .buffer {
    color: var(--gt-lavender-text);
  }

  .buffer.over,
  .buffer.over .value {
    color: var(--gt-coral-text);
  }

  .format {
    height: 44px;
    display: flex;
    align-items: flex-end;
    font-size: var(--gt-text-small);
  }

  /* Same width for Stop and Play, so nothing shifts when it switches. */
  .toggle-play {
    min-width: 96px;
  }

  /* Connection type of the current stream, at the far end of the row. */
  .connection {
    display: flex;
    align-items: center;
    gap: var(--gt-space-2);
    margin-left: auto;
    color: var(--gt-text-muted);
  }

  .connection select {
    width: 168px;
  }

  .body :global(.actions) {
    display: flex;
    align-items: center;
    gap: var(--gt-space-2);
    /* Room for the candy button's bottom lip. */
    padding-bottom: var(--gt-space-1);
  }

  /* Controls */
  .body :global(.controls) {
    display: grid;
    gap: var(--gt-space-2);
    padding-top: var(--gt-space-4);
    border-top: var(--gt-border-width) dotted var(--gt-border);
  }

  .body :global(.control) {
    display: grid;
    grid-template-columns: 64px 1fr 32px;
    align-items: center;
    gap: var(--gt-space-3);
  }

  .body :global(.control) > :global(span) {
    color: var(--gt-text-muted);
    font-weight: 700;
    font-size: var(--gt-text-small);
  }

  .body :global(.control select) {
    grid-column: 2 / -1;
    min-width: 0;
  }

  .body :global(output) {
    text-align: right;
    color: var(--gt-text-muted);
  }

  /* Volume: a candy bar that fills with the sunset. --fill is set inline. */
  input[type='range'] {
    appearance: none;
    width: 100%;
    height: 22px;
    margin: 0;
    border-radius: var(--gt-radius-pill);
    background: transparent;
  }

  input[type='range']::-webkit-slider-runnable-track {
    height: 12px;
    border: var(--gt-border-width) solid var(--gt-border);
    border-radius: var(--gt-radius-pill);
    background:
      linear-gradient(to right, transparent var(--fill), var(--gt-surface-raised) var(--fill)),
      var(--gt-sunset);
  }

  input[type='range']::-webkit-slider-thumb {
    appearance: none;
    width: 20px;
    height: 20px;
    margin-top: -6px;
    border: var(--gt-border-width) solid var(--gt-lavender);
    border-radius: 50%;
    background: var(--gt-white);
  }

  /* Incoming level, left over right. The colours sit at fixed places on the
     scale (mint, then butter for the top 12 dB, pink for the top 3) and
     --fill uncovers them from the left. */
  .body :global(.level) {
    display: grid;
    gap: 3px;
  }

  .body :global(.bar) {
    height: 6px;
    border-radius: var(--gt-radius-pill);
    background:
      linear-gradient(to right, transparent var(--fill), var(--gt-surface-raised) var(--fill)),
      linear-gradient(to right, var(--gt-mint) 80%, var(--gt-butter) 80% 95%, var(--gt-pink) 95%);
  }

  .body :global(.control) > :global(.lr) {
    color: var(--gt-text-faint);
    font-family: var(--gt-font-pixel);
    font-size: var(--gt-text-small);
    font-weight: 400;
    line-height: 9px;
    text-align: right;
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
    gap: var(--gt-space-2);
    font-size: var(--gt-text-small);
    font-weight: 600;
  }

  /* A checkbox drawn as a switch: mint when on. */
  .toggle input {
    appearance: none;
    position: relative;
    flex: none;
    width: 38px;
    height: 22px;
    margin: 0;
    border: var(--gt-border-width) solid var(--gt-border);
    border-radius: var(--gt-radius-pill);
    background: var(--gt-surface-raised);
    cursor: pointer;
    transition: background var(--gt-dur-base);
  }

  .toggle input::before {
    content: '';
    position: absolute;
    top: 1px;
    left: 1px;
    width: 16px;
    height: 16px;
    border: var(--gt-border-width) solid var(--gt-lavender);
    border-radius: 50%;
    background: var(--gt-white);
    transition: translate var(--gt-dur-base) var(--gt-ease-pop);
  }

  .toggle input:checked {
    border-color: var(--gt-lip);
    background: var(--gt-mint);
  }

  .toggle input:checked::before {
    translate: 16px 0;
  }

  .threshold {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: -12px;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
  }

  .threshold .input {
    width: 76px;
    height: 30px;
    padding: 0 var(--gt-space-2);
    border-radius: var(--gt-radius-sm);
  }

  /* Bottom strip of the window: the day/night switch, and the engine. */
  .foot {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--gt-space-3);
    padding: 2px var(--gt-space-4) var(--gt-space-1) var(--gt-space-2);
  }

  .engine {
    color: var(--gt-text-faint);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Missing libmpv */
  .callout {
    display: grid;
    gap: 10px;
    padding: var(--gt-space-4) 20px;
    border: var(--gt-border-width) solid var(--gt-butter-text);
    border-radius: var(--gt-radius-md);
    background: color-mix(in oklab, var(--gt-butter) 22%, var(--gt-surface));
    user-select: text;
  }

  .callout h1 {
    font-size: var(--gt-text-h3);
    white-space: normal;
  }

  .detail {
    color: var(--gt-text-muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
</style>
