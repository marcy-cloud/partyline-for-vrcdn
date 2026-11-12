<script lang="ts">
  import Icon from './Icon.svelte'
  import { errorText, isCustomSource, transports, type Stream } from './api'

  interface Props {
    /** The stream being edited; an empty id means a new one; null closes the dialog. */
    stream: Stream | null
    index: number
    count: number
    canPlay: boolean
    /** Whether private streams can be saved here (needs key protection). */
    canProtect: boolean
    onsave: (stream: Stream, play: boolean) => Promise<void>
    ondelete: (id: string) => Promise<void>
    onmove: (id: string, to: number) => Promise<void>
    onclose: () => void
  }
  let { stream, index, count, canPlay, canProtect, onsave, ondelete, onmove, onclose }: Props = $props()

  let dialog: HTMLDialogElement
  let draft = $state<Stream>({ id: '', label: '', source: '', transport: 'rtsp' })
  let error = $state('')
  let confirmingDelete = $state(false)
  let saving = $state(false)

  let isNew = $derived(draft.id === '')
  let custom = $derived(!draft.private && isCustomSource(draft.source) && !draft.source.includes('vrcdn.live'))
  // Editing a stream that is already private: its key is saved and hidden,
  // and stays as it is unless a new one is typed.
  let keySaved = $derived(!!stream?.private && !isNew)
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
      <button type="button" onclick={onclose} aria-label="Close" title="Close">×</button>
    </span>
  </div>
  <form onsubmit={submit}>

    <label class="field">
      <span>{draft.private ? 'Private stream key' : 'Stream name or URL'}</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="input mono"
        type={draft.private ? 'password' : 'text'}
        bind:value={draft.source}
        placeholder={draft.private
          ? keySaved
            ? 'Saved · type to replace'
            : 'From the Private tab of the VRCDN panel'
          : 'performer_name, or any VRCDN URL'}
        spellcheck="false"
        autocomplete="off"
        autofocus
        required={!(draft.private && keySaved)}
      />
    </label>

    {#if canProtect}
      <label class="private">
        <input type="checkbox" bind:checked={draft.private} />
        <span>
          Private stream
          <small>The key is stored encrypted and never shown, here or while playing.</small>
        </span>
      </label>
    {/if}

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
  /* The Private switch, drawn like the ones on the main panel. */
  .private {
    display: flex;
    align-items: flex-start;
    gap: var(--gt-space-3);
    font-weight: 700;
    font-size: var(--gt-text-small);
  }

  .private small {
    display: block;
    min-height: 0;
    font-weight: 400;
  }

  .private input {
    appearance: none;
    position: relative;
    flex: none;
    width: 38px;
    height: 22px;
    margin: 0;
    border: var(--gt-border-width) solid var(--gt-border);
    border-radius: var(--gt-radius-pill);
    background: var(--gt-surface-raised);
    cursor: pointer;
    transition: background var(--gt-dur-base);
  }

  .private input::before {
    content: '';
    position: absolute;
    top: 1px;
    left: 1px;
    width: 16px;
    height: 16px;
    border: var(--gt-border-width) solid var(--gt-lavender);
    border-radius: 50%;
    background: var(--gt-white);
    transition: translate var(--gt-dur-base) var(--gt-ease-pop);
  }

  .private input:checked {
    border-color: var(--gt-lip);
    background: var(--gt-mint);
  }

  .private input:checked::before {
    translate: 16px 0;
  }
</style>
