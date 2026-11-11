<script lang="ts">
  import Icon from './Icon.svelte'
  import { isCustomSource, transportLabel, type PlayerState, type Stream } from './api'

  interface Props {
    streams: Stream[]
    player: PlayerState
    disabled: boolean
    onplay: (id: string) => void
    onedit: (stream: Stream) => void
    onadd: () => void
  }
  let { streams, player, disabled, onplay, onedit, onadd }: Props = $props()

  function detail(s: Stream): string {
    if (!isCustomSource(s.source)) return `${s.source} · ${transportLabel(s.transport)}`
    try {
      return new URL(s.source).host
    } catch {
      return s.source
    }
  }
</script>

<aside>
  <header>
    <h2>Streams</h2>
    <button class="icon-btn" onclick={onadd} aria-label="Add stream" title="Add stream">
      <Icon name="plus" />
    </button>
  </header>

  {#if streams.length === 0}
    <div class="empty">
      <p>No saved streams yet.</p>
      <p class="hint">Add a performer's VRCDN stream name, or paste any of the URLs from their VRCDN page.</p>
      <button class="btn primary" onclick={onadd}><Icon name="plus" />Add stream</button>
    </div>
  {:else}
    <ul>
      {#each streams as s, i (s.id)}
        {@const active = player.streamId === s.id && player.status !== 'idle'}
        <li class:active data-status={active ? player.status : undefined}>
          <button class="row" onclick={() => onplay(s.id)} {disabled} title={i < 9 ? `Play (press ${i + 1})` : 'Play'}>
            <span class="key num" aria-hidden="true">{i < 9 ? i + 1 : ''}</span>
            <span class="text">
              <span class="label">{s.label}</span>
              <span class="detail mono">{detail(s)}</span>
            </span>
            {#if active}<span class="dot" aria-label={player.status}></span>{/if}
          </button>
          <button class="icon-btn edit" onclick={() => onedit(s)} aria-label="Edit {s.label}" title="Edit">
            <Icon name="edit" />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</aside>

<style>
  aside {
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--panel);
    border-right: 1px solid var(--line);
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 10px 8px 16px;
  }

  h2 {
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  ul {
    flex: 1;
    min-height: 0;
    margin: 0;
    padding: 0 8px 8px;
    list-style: none;
    overflow-y: auto;
  }

  li {
    position: relative;
    border-radius: var(--radius);
  }

  li + li {
    margin-top: 2px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 8px 40px 8px 8px;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    text-align: left;
  }

  .row:hover:not(:disabled) {
    background: var(--raised);
  }

  li.active .row {
    background: var(--raised);
    box-shadow: inset 2px 0 0 var(--status, var(--accent));
  }

  .key {
    flex: none;
    width: 20px;
    height: 20px;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 5px;
    color: var(--faint);
    font-size: 11px;
  }

  .text {
    display: grid;
    min-width: 0;
    flex: 1;
  }

  .label,
  .detail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .label {
    font-weight: 550;
  }

  .detail {
    color: var(--muted);
    font-size: 11.5px;
  }

  li[data-status='playing'] {
    --status: var(--ok);
  }
  li[data-status='connecting'] {
    --status: var(--info);
  }
  li[data-status='reconnecting'] {
    --status: var(--warn);
  }
  li[data-status='failed'] {
    --status: var(--bad);
  }

  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--status);
  }

  li[data-status='connecting'] .dot,
  li[data-status='reconnecting'] .dot {
    animation: pulse 1.2s ease-in-out infinite;
  }

  .edit {
    position: absolute;
    top: 50%;
    right: 6px;
    transform: translateY(-50%);
    opacity: 0;
  }

  li:hover .edit,
  .edit:focus-visible {
    opacity: 1;
  }

  /* The status dot gives way to the edit button on hover. */
  li:hover .dot {
    visibility: hidden;
  }

  .empty {
    display: grid;
    gap: 10px;
    justify-items: start;
    padding: 12px 16px;
  }

  .hint {
    color: var(--muted);
    font-size: 13px;
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
