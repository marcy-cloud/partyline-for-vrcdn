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
    onfold: () => void
    onexport: () => void
    onimport: () => void
  }
  let { streams, player, disabled, onplay, onedit, onadd, onfold, onexport, onimport }: Props = $props()

  // The maintainer's own stream wears a little hearts flair in the list.
  const flaired = new Set(['marcymarcy'])
  const hasFlair = (s: Stream) => flaired.has(s.source.toLowerCase())

  function detail(s: Stream): string {
    if (s.private) return `Private · ${transportLabel(s.transport)}`
    if (!isCustomSource(s.source)) return `${s.source} · ${transportLabel(s.transport)}`
    try {
      return new URL(s.source).host
    } catch {
      return s.source
    }
  }
</script>

<aside class="wing left streams">
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
      <p class="hint">Got a list from a friend? Load it with Import below.</p>
    </div>
  {:else}
    <ul>
      {#each streams as s, i (s.id)}
        {@const active = player.streamId === s.id && player.status !== 'idle'}
        <li class:active data-status={active ? player.status : undefined}>
          <button class="row" onclick={() => onplay(s.id)} {disabled} title={i < 9 ? `Play (press ${i + 1})` : 'Play'}>
            <span class="key pixel" aria-hidden="true">{i < 9 ? i + 1 : ''}</span>
            <span class="text">
              <span class="name">
                <span class="label">{s.label}</span>
                {#if hasFlair(s)}
                  <span class="flair" aria-hidden="true"><i>♥</i><i>♥</i><i>♥</i></span>
                {/if}
              </span>
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

  <footer>
    <button class="icon-btn fold" onclick={onfold} aria-label="Hide streams" title="Hide">‹</button>
    <span class="share">
      <button class="btn ghost" onclick={onimport} title="Add the streams from a list someone gave you">Import</button>
      <button
        class="btn ghost"
        onclick={onexport}
        disabled={streams.length === 0}
        title="Save these streams as a file to give to someone"
      >Export</button>
    </span>
  </footer>
</aside>

<style>
  footer {
    align-items: center;
    justify-content: space-between;
  }

  .share {
    display: flex;
    gap: var(--gt-space-1);
  }

  .share .btn {
    min-height: 28px;
    padding: 0 var(--gt-space-2);
    font-size: var(--gt-text-small);
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

  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  /* A profile-badge style chip: three hearts that beat one after another. */
  .flair {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 2px;
    height: 18px;
    padding: 0 7px;
    border-radius: var(--gt-radius-pill);
    background: var(--gt-sunset);
    color: var(--gt-ink);
    font-size: 10px;
    line-height: 1;
  }

  .flair i {
    font-style: normal;
    animation: beat 1.8s var(--gt-ease-pop) infinite;
  }

  .flair i:nth-child(2) {
    animation-delay: 0.2s;
  }

  .flair i:nth-child(3) {
    animation-delay: 0.4s;
  }

  @keyframes beat {
    0%,
    40%,
    100% {
      scale: 1;
    }
    20% {
      scale: 1.45;
    }
  }
</style>
