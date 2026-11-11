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
    <h2>Streams <span class="deco" aria-hidden="true">♡</span></h2>
    <button class="icon-btn add" onclick={onadd} aria-label="Add stream" title="Add stream">
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
            <span class="key pixel" aria-hidden="true">{i < 9 ? i + 1 : ''}</span>
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
    background: var(--gt-surface);
    border-right: var(--gt-border-width) solid var(--gt-border);
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--gt-space-4) var(--gt-space-3) var(--gt-space-2) var(--gt-space-4);
  }

  h2 {
    font-size: var(--gt-text-h3);
  }

  .deco {
    color: var(--gt-pink-text);
  }

  /* A small candy button. */
  .add {
    background: var(--gt-pink);
    color: var(--gt-ink);
    box-shadow: 0 3px 0 var(--gt-lip);
    transition:
      transform var(--gt-dur-base) var(--gt-ease-pop),
      box-shadow var(--gt-dur-base) var(--gt-ease-pop);
  }

  .add:hover:not(:disabled) {
    background: var(--gt-pink);
    color: var(--gt-ink);
  }

  .add:active {
    transform: translateY(3px);
    box-shadow: 0 0 0 var(--gt-lip);
  }

  .add:focus-visible {
    box-shadow: var(--gt-focus-ring);
  }

  ul {
    flex: 1;
    min-height: 0;
    margin: 0;
    padding: var(--gt-space-1) var(--gt-space-2) var(--gt-space-2);
    list-style: none;
    overflow-y: auto;
  }

  li {
    position: relative;
  }

  li + li {
    margin-top: 2px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: var(--gt-space-2) 40px var(--gt-space-2) var(--gt-space-3);
    border: 0;
    border-radius: var(--gt-radius-md);
    background: transparent;
    text-align: left;
    transition: background var(--gt-dur-fast);
  }

  .row:hover:not(:disabled) {
    background: var(--gt-surface-raised);
  }

  li.active .row {
    background: var(--gt-surface-raised);
    box-shadow: inset 4px 0 0 var(--gt-pink);
  }

  li.active .row:focus-visible {
    box-shadow: var(--gt-focus-ring);
  }

  .key {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: var(--gt-border-width) solid var(--gt-border);
    border-radius: var(--gt-radius-pill);
    background: var(--gt-surface);
    color: var(--gt-text-muted);
    letter-spacing: 0;
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
    font-weight: 700;
  }

  .detail {
    color: var(--gt-text-muted);
    font-size: 11.5px;
  }

  li[data-status='playing'] {
    --status: var(--gt-mint);
  }
  li[data-status='connecting'] {
    --status: var(--gt-aqua);
  }
  li[data-status='reconnecting'] {
    --status: var(--gt-butter);
  }
  li[data-status='failed'] {
    --status: var(--gt-coral-text);
  }

  /* Outlined so the pale fills still read on a pale row. */
  .dot {
    position: absolute;
    top: 50%;
    right: 14px;
    translate: 0 -50%;
    width: 12px;
    height: 12px;
    border: var(--gt-border-width) solid var(--gt-text);
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

  .edit:hover:not(:disabled) {
    background: var(--gt-surface);
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
    gap: var(--gt-space-3);
    justify-items: start;
    padding: var(--gt-space-2) var(--gt-space-4) var(--gt-space-4);
  }

  .hint {
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
  }
</style>
