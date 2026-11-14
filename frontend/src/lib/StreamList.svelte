<script lang="ts">
  import { flip } from 'svelte/animate'
  import Icon from './Icon.svelte'
  import { isCustomSource, transportLabel, type PlayerState, type Stream } from './api'

  interface Props {
    streams: Stream[]
    player: PlayerState
    disabled: boolean
    onplay: (id: string) => void
    onedit: (stream: Stream) => void
    onadd: () => void
    onmove: (id: string, to: number) => Promise<void> | void
    onfold: () => void
    onexport: () => void
    onimport: () => void
  }
  let { streams, player, disabled, onplay, onedit, onadd, onmove, onfold, onexport, onimport }: Props = $props()

  // The maintainer's own stream wears a little hearts flair in the list.
  const flaired = new Set(['marcymarcy'])
  const hasFlair = (s: Stream) => flaired.has(s.source.toLowerCase())

  // Reordering: each row's circle is a handle. While one is held, the list
  // shows the order it would have if let go now, and the saved order only
  // changes on release. Pointer events, not the browser's drag and drop,
  // so that touch and VR pointers can do it too.
  let list = $state<HTMLUListElement>()
  let order = $state<Stream[] | null>(null)
  let drag = $state<{ id: string; from: number; at: number; top: number; pitch: number } | null>(null)
  let shown = $derived(order ?? streams)

  function grab(e: PointerEvent, s: Stream, i: number) {
    if (e.button !== 0 || !list || streams.length < 2) return
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    // Rows are all the same height, so where each slot is can be worked out
    // once, and doesn't wobble while rows are sliding about.
    const rows = [...list.children] as HTMLElement[]
    const top = rows[0].getBoundingClientRect().top
    drag = { id: s.id, from: i, at: i, top, pitch: rows[1].getBoundingClientRect().top - top }
    order = [...streams]
  }

  function carry(e: PointerEvent) {
    if (!drag || !order) return
    const at = Math.max(0, Math.min(order.length - 1, Math.floor((e.clientY - drag.top) / drag.pitch)))
    if (at === drag.at) return
    const next = order.filter((s) => s.id !== drag!.id)
    next.splice(at, 0, order.find((s) => s.id === drag!.id)!)
    order = next
    drag.at = at
  }

  async function drop() {
    if (!drag) return
    const { id, from, at } = drag
    drag = null
    try {
      if (at !== from) await onmove(id, at)
    } finally {
      order = null
    }
  }

  function letGo() {
    drag = null
    order = null
  }

  // The same by keyboard: the arrow keys on a handle move its row.
  function nudge(e: KeyboardEvent, s: Stream, i: number) {
    const to = e.key === 'ArrowUp' ? i - 1 : e.key === 'ArrowDown' ? i + 1 : -1
    if (e.key === 'Escape') letGo()
    if (to < 0 || to >= streams.length) return
    e.preventDefault()
    onmove(s.id, to)
  }

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
    <ul bind:this={list}>
      {#each shown as s, i (s.id)}
        {@const active = player.streamId === s.id && player.status !== 'idle'}
        <li
          class:active
          class:dragging={drag?.id === s.id}
          data-status={active ? player.status : undefined}
          animate:flip={{ duration: 140 }}
        >
          <button
            class="grip"
            class:alone={streams.length < 2}
            onpointerdown={(e) => grab(e, s, i)}
            onpointermove={carry}
            onpointerup={drop}
            onpointercancel={letGo}
            onkeydown={(e) => nudge(e, s, i)}
            aria-label="Move {s.label}: drag, or use the up and down arrow keys"
            title="Drag to reorder"
          >
            <Icon name="grip" />
          </button>
          <button class="row" onclick={() => onplay(s.id)} {disabled} title={i < 9 ? `Play (press ${i + 1})` : 'Play'}>
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

  /* The handle a row is dragged by, in the circle at its left. */
  .grip {
    position: absolute;
    top: 50%;
    left: 10px;
    z-index: 1;
    translate: 0 -50%;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: var(--gt-border-width) solid var(--gt-border);
    border-radius: var(--gt-radius-pill);
    background: var(--gt-surface);
    color: var(--gt-text-muted);
    cursor: grab;
    /* A finger on the handle drags the row; it doesn't scroll the list. */
    touch-action: none;
    transition:
      border-color var(--gt-dur-fast),
      color var(--gt-dur-fast);
  }

  .grip :global(svg) {
    width: 14px;
    height: 14px;
  }

  .grip:hover,
  .dragging .grip {
    border-color: var(--gt-lavender);
    color: var(--gt-text);
  }

  .dragging .grip {
    cursor: grabbing;
  }

  /* One stream: nowhere to move it to. */
  .grip.alone {
    cursor: default;
    opacity: 0.5;
  }

  .streams :global(.row) {
    padding-left: 44px;
  }

  /* The row being carried stands out from the ones making way for it. */
  .dragging {
    z-index: 2;
  }

  .dragging :global(.row) {
    background: var(--gt-surface-raised);
    box-shadow: 0 0 0 var(--gt-border-width) var(--gt-lavender);
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
