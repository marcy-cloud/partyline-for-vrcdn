<script lang="ts">
  // The DJ's side: send audio only to VRCDN. Rendered inside the main
  // panel's body, which styles the shared pieces (badge, controls, meter).
  import Icon from './Icon.svelte'
  import { errorText, type Levels, type SendInfo, type SendSettings, type SendState, type SendStatus } from './api'

  interface Props {
    info: SendInfo
    current: SendState
    levels: Levels
    onsettings: (patch: Partial<SendSettings>) => void
    onkey: (key: string) => Promise<void>
    onstart: () => void
    onstop: () => void
    onrefresh: () => void
  }
  let { info, current, levels, onsettings, onkey, onstart, onstop, onrefresh }: Props = $props()

  const statusText: Record<SendStatus, string> = {
    off: 'Off air',
    connecting: 'Connecting',
    live: 'On air',
    reconnecting: 'Reconnecting',
    failed: 'Stopped',
  }

  let key = $state('')
  let keyError = $state('')
  let busy = $derived(current.status === 'connecting' || current.status === 'live' || current.status === 'reconnecting')
  let ingestName = $derived(info.ingests.find((i) => i.url === info.settings.ingest)?.name ?? info.settings.ingest)

  // The saved input may be unplugged; keep it listed so the choice survives.
  let devices = $derived(
    !info.settings.device || info.devices.some((d) => d.id === info.settings.device)
      ? info.devices
      : [...info.devices, { id: info.settings.device, name: 'Saved input (not connected)' }],
  )

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

  async function saveKey(value: string) {
    keyError = ''
    try {
      await onkey(value)
      key = ''
    } catch (e) {
      keyError = errorText(e)
    }
  }
</script>

<section class="sending" data-status={current.status}>
  <div class="status-line">
    <span class="badge pixel"><span class="dot"></span>{statusText[current.status]}</span>
    {#if onAirFor}<span class="badge pixel clock num">{onAirFor}</span>{/if}
  </div>
  <h1>Send to VRCDN</h1>
  <p class="sub">Audio only · AAC {info.settings.bitrateKbps} kbps · 48 kHz stereo · {ingestName}</p>
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
    disabled={!busy && (!info.hasKey || !info.settings.device)}
    title={busy
      ? 'Stop sending'
      : !info.hasKey
        ? 'Save your stream key first'
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
      {#each devices as d (d.id)}
        <option value={d.id}>{d.name}</option>
      {/each}
    </select>
    <button class="icon-btn" onclick={onrefresh} disabled={busy} aria-label="Look for inputs again" title="Look for inputs again">
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
    <span>Server</span>
    <select
      class="input wide"
      value={info.settings.ingest}
      onchange={(e) => onsettings({ ingest: e.currentTarget.value })}
      disabled={busy}
    >
      {#each info.ingests as i (i.url)}
        <option value={i.url}>{i.name}</option>
      {/each}
    </select>
  </label>

  <form
    class="control"
    onsubmit={(e) => {
      e.preventDefault()
      if (key.trim()) saveKey(key)
    }}
  >
    <span>Key</span>
    <span class="key">
      <input
        class="input mono"
        type="password"
        aria-label="Stream key"
        bind:value={key}
        placeholder={info.hasKey ? 'Saved · type to replace' : 'Stream key from the VRCDN panel'}
        autocomplete="off"
        spellcheck="false"
        disabled={busy}
      />
      <button class="btn" disabled={busy || !key.trim()}>Save</button>
      {#if info.hasKey}
        <button type="button" class="btn ghost danger" onclick={() => saveKey('')} disabled={busy}>Forget</button>
      {/if}
    </span>
  </form>
  {#if keyError}<p class="key-error" role="alert">{keyError}</p>{/if}

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
</section>

<p class="note">
  For a DJ to VJ link, use your <b>private</b> stream key. It is stored encrypted for this Windows account and never
  shown again.
</p>

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

  /* The key field with its buttons, across the last two columns. */
  .key {
    grid-column: 2 / -1;
    display: flex;
    gap: var(--gt-space-2);
    min-width: 0;
  }

  .key .input {
    flex: 1;
    min-width: 0;
  }

  .key-error {
    margin: 0 0 0 76px;
    color: var(--gt-coral-text);
    font-weight: 600;
    font-size: var(--gt-text-small);
  }

  .note {
    margin-top: auto;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
  }
</style>
