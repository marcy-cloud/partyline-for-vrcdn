<script lang="ts">
  // Recent buffer readings, with the catch-up threshold drawn as a guide.
  let { samples, threshold }: { samples: number[]; threshold: number } = $props()

  const W = 240
  const H = 44
  const SLOTS = 120

  let top = $derived(Math.max(threshold * 1.6, ...samples, 1))
  let y = $derived((v: number) => H - 2 - (Math.min(v, top) / top) * (H - 4))
  let points = $derived(
    samples.map((v, i) => `${((SLOTS - samples.length + i) / (SLOTS - 1)) * W},${y(v).toFixed(1)}`).join(' '),
  )
</script>

<svg viewBox="0 0 {W} {H}" preserveAspectRatio="none" aria-hidden="true">
  <line x1="0" x2={W} y1={y(threshold)} y2={y(threshold)} class="guide" />
  {#if samples.length > 1}
    <polyline {points} class="trace" />
  {/if}
</svg>

<style>
  svg {
    display: block;
    width: 100%;
    height: 44px;
  }

  .guide {
    stroke: var(--gt-text-faint);
    stroke-width: 1;
    stroke-dasharray: 3 4;
    vector-effect: non-scaling-stroke;
  }

  .trace {
    fill: none;
    stroke: currentColor;
    stroke-width: 2.5;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
  }
</style>
