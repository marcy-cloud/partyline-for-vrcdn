<script lang="ts">
  // Add or edit a send target: a name, a stream key, and the server.
  import { errorText, type SendTarget, type SendTargetDraft } from './api'

  interface Props {
    /** The target being edited; an empty id means a new one; null closes the dialog. */
    target: SendTarget | null
    ingests: { name: string; url: string }[]
    onsave: (target: SendTargetDraft) => Promise<void>
    ondelete: (id: string) => Promise<void>
    onclose: () => void
  }
  let { target, ingests, onsave, ondelete, onclose }: Props = $props()

  let dialog: HTMLDialogElement
  let draft = $state<SendTargetDraft>({ id: '', label: '', ingest: '', key: '' })
  let error = $state('')
  let confirmingDelete = $state(false)
  let saving = $state(false)
  let isNew = $derived(draft.id === '')

  $effect(() => {
    if (target) {
      draft = { ...target, key: '' }
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
</script>

<dialog class="window" bind:this={dialog} {onclose} aria-labelledby="target-dialog-title">
  <div class="titlebar">
    <h2 id="target-dialog-title" class="pixel">{isNew ? 'Add send target' : 'Edit send target'}</h2>
    <span class="chrome">
      <button type="button" onclick={onclose} aria-label="Close" title="Close">×</button>
    </span>
  </div>
  <form
    onsubmit={(e) => {
      e.preventDefault()
      run(() => onsave($state.snapshot(draft)))
    }}
  >
    <label class="field">
      <span>Stream key</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="input mono"
        type="password"
        bind:value={draft.key}
        placeholder={isNew ? 'From the VRCDN panel' : 'Saved · type to replace'}
        spellcheck="false"
        autocomplete="off"
        autofocus
        required={isNew}
      />
      <small>Stored encrypted for this Windows account and never shown again.</small>
    </label>

    <label class="field">
      <span>Label <em>(optional)</em></span>
      <input class="input" bind:value={draft.label} placeholder="e.g. My private feed" autocomplete="off" />
    </label>

    <label class="field">
      <span>Server</span>
      <select class="input" bind:value={draft.ingest}>
        {#each ingests as i (i.url)}
          <option value={i.url}>{i.name}</option>
        {/each}
      </select>
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
      {/if}
      <span class="spacer"></span>
      <button type="button" class="btn ghost" onclick={onclose}>Cancel</button>
      <button class="btn primary" disabled={saving}>{isNew ? 'Add' : 'Save'}</button>
    </div>
  </form>
</dialog>
