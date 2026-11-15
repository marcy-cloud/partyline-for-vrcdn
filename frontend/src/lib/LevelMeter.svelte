<script lang="ts">
  // A stereo peak meter, left over right, marked to VRCDN's guidance: peaks
  // should sit near 0 dBFS without clipping, with about -5 as the target.
  //
  //   below -8 dBFS   aqua   there is signal, but it is on the quiet side
  //   -8 to -2        mint   where the peaks should be
  //   -2 to 0         pink   as loud as it goes; fine, but nothing to spare
  //   0               the dot at the end lights for two seconds: it clipped
  //
  // The scale is the top 60 dB, even in decibels: empty at -60, full at 0.
  import type { Levels } from './api'

  interface Props {
    levels: Levels
    /** Whose level this is, for the label: "Incoming" or "Outgoing". */
    what: string
  }
  let { levels, what }: Props = $props()

  const RANGE = 60
  const CLIP_AT = -0.1 // dBFS; a reading this close to full scale has clipped
  const CLIP_HOLD = 2000
  const fill = (db: number) => `${Math.min(Math.max((db + RANGE) / RANGE, 0), 1) * 100}%`

  let clipped = $state([false, false])
  const timers: (ReturnType<typeof setTimeout> | undefined)[] = []
  $effect(() => {
    ;[levels.left, levels.right].forEach((db, ch) => {
      if (db < CLIP_AT) return
      clipped[ch] = true
      clearTimeout(timers[ch])
      timers[ch] = setTimeout(() => (clipped[ch] = false), CLIP_HOLD)
    })
  })
  $effect(() => () => timers.forEach(clearTimeout))
</script>

<div
  class="level"
  role="img"
  aria-label="{what} level: left {Math.round(levels.left)} dB, right {Math.round(levels.right)} dB{clipped[0] || clipped[1]
    ? ', clipping'
    : ''}"
  title="Peaks should reach the green, around -5 dB. Pink is the last 2 dB. The dot lights if it clips."
>
  {#each [levels.left, levels.right] as db, ch (ch)}
    <div class="bar" style="--fill: {fill(db)}"></div>
    <span class="clip" class:on={clipped[ch]}></span>
  {/each}
</div>

<style>
  .level {
    display: grid;
    grid-template-columns: 1fr 6px;
    align-items: center;
    gap: 3px 5px;
  }

  /* The colours sit at fixed places on the scale (-8 dBFS is 86.7% of the
     way along, -2 is 96.7%) and --fill uncovers them from the left. */
  .bar {
    height: 6px;
    border-radius: var(--gt-radius-pill);
    background:
      linear-gradient(to right, transparent var(--fill), var(--gt-surface-raised) var(--fill)),
      linear-gradient(to right, var(--gt-aqua) 86.7%, var(--gt-mint) 86.7% 96.7%, var(--gt-pink) 96.7%);
  }

  .clip {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--gt-surface-raised);
  }

  .clip.on {
    background: var(--gt-pink);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--gt-pink) 35%, transparent);
  }
</style>
