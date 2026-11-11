<script lang="ts">
  type Name = 'plus' | 'edit' | 'stop' | 'play' | 'resync' | 'up' | 'down'
  let { name }: { name: Name } = $props()
</script>

<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === 'plus'}
    <path d="M12 5v14M5 12h14" />
  {:else if name === 'edit'}
    <path d="M4 20h4L19 9l-4-4L4 16v4z" />
  {:else if name === 'stop'}
    <rect x="6" y="6" width="12" height="12" rx="1.5" fill="currentColor" stroke="none" />
  {:else if name === 'play'}
    <path d="M8 5.5v13l11-6.5z" fill="currentColor" stroke="none" />
  {:else if name === 'resync'}
    <path d="M20 12a8 8 0 1 1-2.4-5.7M20 4v5h-5" />
  {:else if name === 'up'}
    <path d="M12 19V5M6 11l6-6 6 6" />
  {:else if name === 'down'}
    <path d="M12 5v14M6 13l6 6 6-6" />
  {/if}
</svg>
