<script lang="ts">
  // The DJ's side: send audio only to VRCDN. Rendered inside the main
  // panel's body, which styles the shared pieces (badge, controls, meter).
  import Icon from './Icon.svelte'
  import type { Levels, SendInfo, SendSettings, SendState, SendStatus } from './api'

  interface Props {
    info: SendInfo
    current: SendState
    levels: Levels
    onsettings: (patch: Partial<SendSettings>) => void
    onstart: () => void
    onstop: () => void
    onrefresh: () => void
  }
  let { info, current, levels, onsettings, onstart, onstop, onrefresh }: Props = $props()

  const statusText: Record<SendStatus, string> = {
    off: 'Off air',
    connecting: 'Connecting',
    live: 'On air',
    reconnecting: 'Reconnecting',
    failed: 'Stopped',
  }

  // Where it goes: the send target chosen in the wing on the right.
  let target = $derived(info.targets.find((t) => t.id === info.settings.targetId))
  let busy = $derived(current.status === 'connecting' || current.status === 'live' || current.status === 'reconnecting')
  let encoderName = $derived(
    (info.encoders.find((e) => e.id === info.settings.encoder)?.name ?? 'AAC').replace(' (Apple)', ''),
  )
  let ingestName = $derived(info.ingests.find((i) => i.url === target?.ingest)?.name ?? '')

  // The saved input may be unplugged, or a program that isn't open; keep it
  // listed so the choice survives.
  const isProgram = (id: string) => id.startsWith('app:')
  let saved = $derived(info.settings.device)
  let missing = $derived(!!saved && !info.devices.some((d) => d.id === saved))
  let inputs = $derived([
    ...info.devices.filter((d) => !d.program),
    ...(missing && !isProgram(saved) ? [{ id: saved, name: 'Saved input (not connected)' }] : []),
  ])
  let programs = $derived([
    ...info.devices.filter((d) => d.program),
    ...(missing && isProgram(saved) ? [{ id: saved, name: `${saved.slice(4).replace(/\.exe$/i, '')} (not running)` }] : []),
  ])

  let now = $state(Date.now())
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(timer)
  })
  let onAirFor = $derived.by(() => {
    if (!busy || !current.sinceMs) return ''
    const total = Math.max(0, Math.floor((now - current.sinceMs) / 1000))
    const h = Math.floor(total / 3600)
    const m = Math.floor((total % 3600) / 60)
    const pad = (n: number) => String(n).padStart(2, '0')
    return h > 0 ? `${h}:${pad(m)}:${pad(total % 60)}` : `${m}:${pad(total % 60)}`
  })

  // The meter covers the top 60 dB, as on the player.
  const meterFill = (db: number) => `${Math.min(Math.max((db + 60) / 60, 0), 1) * 100}%`

</script>

<section class="sending" data-status={current.status}>
  <div class="status-line">
    <span class="badge pixel"><span class="dot"></span>{statusText[current.status]}</span>
    {#if onAirFor}<span class="badge pixel clock num">{onAirFor}</span>{/if}
  </div>
  <h1 class:quiet={!target}>{target ? target.label : 'Nowhere to send yet'}</h1>
  <p class="sub">
    {#if target}
      Audio only · {encoderName} {info.settings.bitrateKbps} kbps · 48 kHz stereo · {ingestName}
    {:else}
      Add a stream key under "Send to" on the right.
    {/if}
  </p>
  {#if current.message}
    <p class="message num">
      {current.status === 'reconnecting' ? `${current.message} · retrying (attempt ${current.attempt})` : current.message}
    </p>
  {/if}
</section>

<section class="actions">
  <button
    class="btn candy go-live"
    class:go={!busy}
    onclick={busy ? onstop : onstart}
    disabled={!busy && (!target || !info.settings.device)}
    title={busy
      ? 'Stop sending'
      : !target
        ? 'Add somewhere to send to first'
        : !info.settings.device
          ? 'Choose an input first'
          : 'Start sending'}
  >
    <Icon name={busy ? 'stop' : 'play'} />{busy ? 'Stop' : 'Go live'}
  </button>
</section>

<section class="controls">
  <label class="control">
    <span>Input</span>
    <!-- One column only, leaving the third for the refresh button. -->
    <select
      class="input"
      style="grid-column: 2"
      value={info.settings.device}
      onchange={(e) => onsettings({ device: e.currentTarget.value })}
      disabled={busy}
    >
      {#if !info.settings.device}<option value="">Choose an input…</option>{/if}
      <optgroup label="Inputs">
        {#each inputs as d (d.id)}
          <option value={d.id}>{d.name}</option>
        {/each}
      </optgroup>
      <!-- A DJ program on this PC: only its sound is sent, nothing else's. -->
      <optgroup label={programs.length ? 'Programs playing sound' : 'Programs playing sound (none right now)'}>
        {#each programs as d (d.id)}
          <option value={d.id}>{d.name}</option>
        {/each}
      </optgroup>
    </select>
    <button
      class="icon-btn"
      onclick={onrefresh}
      disabled={busy}
      aria-label="Look for inputs and programs again"
      title="Look for inputs and programs again"
    >
      <Icon name="resync" />
    </button>
  </label>

  <div class="control" title="Level of the audio going out">
    <span>Level</span>
    <div
      class="level"
      role="img"
      aria-label="Outgoing level: left {Math.round(levels.left)} dB, right {Math.round(levels.right)} dB"
    >
      <div class="bar" style="--fill: {meterFill(levels.left)}"></div>
      <div class="bar" style="--fill: {meterFill(levels.right)}"></div>
    </div>
    <span class="lr pixel" aria-hidden="true">L<br />R</span>
  </div>

  <label class="control">
    <span>Quality</span>
    <select
      class="input wide"
      value={info.settings.bitrateKbps}
      onchange={(e) => onsettings({ bitrateKbps: +e.currentTarget.value })}
      disabled={busy}
    >
      {#each info.bitrates as b (b)}
        <option value={b}>{b} kbps</option>
      {/each}
    </select>
  </label>


  {#if info.encoders.length > 1}
    <label class="control" title="CoreAudio is Apple's encoder, the one OBS offers when iTunes is installed">
      <span>Encoder</span>
      <select
        class="input wide"
        value={info.settings.encoder}
        onchange={(e) => onsettings({ encoder: e.currentTarget.value })}
        disabled={busy}
      >
        {#each info.encoders as e (e.id)}
          <option value={e.id}>{e.name}</option>
        {/each}
      </select>
    </label>
  {/if}
</section>

<p class="note">Sends audio only, with no video. For a DJ to VJ link, send to your <b>private</b> stream key.</p>

<style>
  .sending {
    --status: var(--gt-lavender);
    --status-text: var(--gt-text-muted);
    /* Room for the message line, so the controls don't jump. */
    min-height: 116px;
  }
  .sending[data-status='live'] {
    --status: var(--gt-mint);
    --status-text: var(--gt-mint-text);
  }
  .sending[data-status='connecting'] {
    --status: var(--gt-aqua);
    --status-text: var(--gt-aqua-text);
  }
  .sending[data-status='reconnecting'] {
    --status: var(--gt-butter);
    --status-text: var(--gt-butter-text);
  }
  .sending[data-status='failed'] {
    --status: var(--gt-coral-text);
    --status-text: var(--gt-coral-text);
  }

  .sending[data-status='connecting'] .dot,
  .sending[data-status='reconnecting'] .dot {
    animation: pulse 1.2s ease-in-out infinite;
  }

  .clock {
    --status: var(--gt-lavender);
    --status-text: var(--gt-text-muted);
  }

  .go-live {
    min-width: 112px;
  }

  .wide {
    grid-column: 2 / -1;
  }

  h1.quiet {
    color: var(--gt-text-muted);
  }

  .note {
    margin-top: auto;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
  }
</style>
