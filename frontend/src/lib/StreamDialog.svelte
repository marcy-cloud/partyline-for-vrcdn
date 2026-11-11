<script lang="ts">
  import Icon from './Icon.svelte'
  import { errorText, isCustomSource, transports, type Stream } from './api'

  interface Props {
    /** The stream being edited; an empty id means a new one; null closes the dialog. */
    stream: Stream | null
    index: number
    count: number
    canPlay: boolean
    onsave: (stream: Stream, play: boolean) => Promise<void>
    ondelete: (id: string) => Promise<void>
    onmove: (id: string, to: number) => Promise<void>
    onclose: () => void
  }
  let { stream, index, count, canPlay, onsave, ondelete, onmove, onclose }: Props = $props()

  let dialog: HTMLDialogElement
  let draft = $state<Stream>({ id: '', label: '', source: '', transport: 'rtsp' })
  let error = $state('')
  let confirmingDelete = $state(false)
  let saving = $state(false)

  let isNew = $derived(draft.id === '')
  let custom = $derived(isCustomSource(draft.source) && !draft.source.includes('vrcdn.live'))
  let hint = $derived(transports.find((t) => t.value === draft.transport)?.hint ?? '')

  $effect(() => {
    if (stream) {
      draft = { ...stream }
      error = ''
      confirmingDelete = false
      if (!dialog.open) dialog.showModal()
    } else if (dialog.open) {
      dialog.close()
    }
  })

  async function run(action: () => Promise<void>) {
    saving = true
    error = ''
    try {
      await action()
    } catch (e) {
      error = errorText(e)
    } finally {
      saving = false
    }
  }

  function submit(e: SubmitEvent) {
    e.preventDefault()
    const play = (e.submitter as HTMLButtonElement | null)?.value === 'play'
    run(() => onsave($state.snapshot(draft), play))
  }
</script>

<dialog class="window" bind:this={dialog} {onclose} aria-labelledby="stream-dialog-title">
  <div class="titlebar">
    <h2 id="stream-dialog-title" class="pixel">{isNew ? 'Add stream' : 'Edit stream'}</h2>
    <span class="chrome">
      <span aria-hidden="true">♡ ✿</span>
      <button type="button" onclick={onclose} aria-label="Close" title="Close">×</button>
    </span>
  </div>
  <form onsubmit={submit}>

    <label class="field">
      <span>Stream name or URL</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="input mono"
        bind:value={draft.source}
        placeholder="performer_name, or any VRCDN URL"
        spellcheck="false"
        autocomplete="off"
        autofocus
        required
      />
    </label>

    <label class="field">
      <span>Label <em>(optional)</em></span>
      <input class="input" bind:value={draft.label} placeholder="Shown in the list, e.g. the performer's name" autocomplete="off" />
    </label>

    <label class="field">
      <span>Connection</span>
      <select class="input" bind:value={draft.transport} disabled={custom}>
        {#each transports as t (t.value)}
          <option value={t.value}>{t.label}</option>
        {/each}
      </select>
      <small>{custom ? 'Custom URLs are played exactly as written.' : hint}</small>
    </label>

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    <div class="buttons">
      {#if !isNew}
        {#if confirmingDelete}
          <button type="button" class="btn danger" onclick={() => run(() => ondelete(draft.id))} disabled={saving}>
            Really delete
          </button>
        {:else}
          <button type="button" class="btn ghost danger" onclick={() => (confirmingDelete = true)}>Delete</button>
        {/if}
        <button
          type="button"
          class="icon-btn"
          onclick={() => run(() => onmove(draft.id, index - 1))}
          disabled={saving || index <= 0}
          aria-label="Move up"
          title="Move up"
        >
          <Icon name="up" />
        </button>
        <button
          type="button"
          class="icon-btn"
          onclick={() => run(() => onmove(draft.id, index + 1))}
          disabled={saving || index >= count - 1}
          aria-label="Move down"
          title="Move down"
        >
          <Icon name="down" />
        </button>
      {/if}
      <span class="spacer"></span>
      <button type="button" class="btn ghost" onclick={onclose}>Cancel</button>
      {#if isNew}
        <!-- First in the DOM so Enter in a field picks it; shown last. -->
        {#if canPlay}
          <button class="btn primary last" value="play" disabled={saving}>Add &amp; play</button>
        {/if}
        <button class="btn" class:primary={!canPlay} value="save" disabled={saving}>Add</button>
      {:else}
        <button class="btn primary" value="save" disabled={saving}>Save</button>
      {/if}
    </div>
  </form>
</dialog>

<style>
  dialog {
    width: min(480px, calc(100vw - 48px));
    padding: 0;
    color: var(--gt-text);
  }

  dialog[open] {
    animation: pop var(--gt-dur-slow) var(--gt-ease-pop);
  }

  dialog::backdrop {
    background: color-mix(in oklab, var(--gt-bg) 75%, transparent);
  }

  @keyframes pop {
    from {
      opacity: 0;
      scale: 0.9;
    }
  }

  h2 {
    font-weight: 400;
    text-transform: lowercase;
  }

  form {
    display: grid;
    gap: var(--gt-space-4);
    padding: 20px;
  }

  em {
    font-style: normal;
    font-weight: 400;
    color: var(--gt-text-faint);
  }

  small {
    min-height: 18px;
    color: var(--gt-text-muted);
    font-size: var(--gt-text-small);
  }

  .error {
    color: var(--gt-coral-text);
    font-weight: 600;
    user-select: text;
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 6px;
    /* Room for the candy buttons' bottom lip. */
    padding-bottom: var(--gt-space-1);
  }

  .spacer {
    flex: 1;
  }

  .last {
    order: 1;
  }
</style>
