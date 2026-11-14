<script lang="ts">
  // The right wing: saved places to send to. Clicking one chooses where
  // "Go live" sends; it does not start sending.
  import Icon from './Icon.svelte'
  import type { SendTarget } from './api'

  interface Props {
    targets: SendTarget[]
    selectedId: string
    ingests: { name: string; url: string }[]
    /** True while a broadcast is going out: the target can't change under it. */
    onAir: boolean
    onselect: (id: string) => void
    onedit: (target: SendTarget) => void
    onadd: () => void
    onfold: () => void
  }
  let { targets, selectedId, ingests, onAir, onselect, onedit, onadd, onfold }: Props = $props()

  const server = (t: SendTarget) => ingests.find((i) => i.url === t.ingest)?.name ?? t.ingest
</script>

<aside class="wing right targets">
  <header>
    <h2>Send to <span class="deco" aria-hidden="true">✦</span></h2>
    <button class="icon-btn add" onclick={onadd} aria-label="Add send target" title="Add send target">
      <Icon name="plus" />
    </button>
  </header>

  {#if targets.length === 0}
    <div class="empty">
      <p>Nowhere to send yet.</p>
      <p class="hint">Add a stream key from the VRCDN panel. For a DJ to VJ link, use your private key.</p>
      <button class="btn" onclick={onadd}><Icon name="plus" />Add send target</button>
    </div>
  {:else}
    <ul>
      {#each targets as t (t.id)}
        {@const chosen = t.id === selectedId}
        <!-- Chosen is where Go live would send. The pink bar that marks a
             playing stream in the other wing is kept for a target that is
             being sent to right now. -->
        <li class:chosen class:active={chosen && onAir}>
          <button
            class="row"
            onclick={() => onselect(t.id)}
            disabled={onAir && !chosen}
            title={chosen ? (onAir ? 'On air' : 'Chosen: Go live sends here') : onAir ? 'Stop sending to change this' : 'Send here'}
          >
            <span class="text">
              <span class="label">{t.label}</span>
              <span class="detail mono">{server(t)}</span>
            </span>
            {#if chosen && onAir}<span class="dot" aria-label="on air"></span>{/if}
          </button>
          <button class="icon-btn edit" onclick={() => onedit(t)} disabled={onAir} aria-label="Edit {t.label}" title="Edit">
            <Icon name="edit" />
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  <footer>
    <button class="icon-btn fold" onclick={onfold} aria-label="Hide send targets" title="Hide">›</button>
  </footer>
</aside>

<style>
  .chosen :global(.row) {
    background: var(--gt-surface-raised);
  }

  .deco {
    color: var(--gt-pink-text);
  }

  footer {
    justify-content: flex-end;
  }
</style>
