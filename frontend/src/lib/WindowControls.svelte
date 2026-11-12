<script lang="ts">
  // Minimise, maximise and close, since the window has no Windows title bar.
  // Rendered nowhere outside the app (a plain browser has its own).
  import { Window } from '@wailsio/runtime'
  import { inWails } from './api'

  let { maximised }: { maximised: boolean } = $props()
</script>

{#if inWails()}
  <button onclick={() => Window.Minimise()} aria-label="Minimise" title="Minimise">
    <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2 9.5h8" /></svg>
  </button>
  <button
    onclick={() => Window.ToggleMaximise()}
    aria-label={maximised ? 'Restore' : 'Maximise'}
    title={maximised ? 'Restore' : 'Maximise'}
  >
    <svg viewBox="0 0 12 12" aria-hidden="true">
      {#if maximised}
        <path d="M2 4.5h5.5V10H2zM4.5 4.5V2H10v5.5H7.5" />
      {:else}
        <rect x="2" y="2" width="8" height="8" rx="1.5" />
      {/if}
    </svg>
  </button>
  <button class="close" onclick={() => Window.Close()} aria-label="Close" title="Close">
    <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2.5 2.5l7 7M9.5 2.5l-7 7" /></svg>
  </button>
{/if}

<style>
  svg {
    width: 12px;
    height: 12px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  /* Text on a candy fill is ink, so the close button warns with its fill. */
  .close:hover {
    background: var(--gt-ink);
    color: var(--gt-white);
  }
</style>
