<script lang="ts">
  import { applyTheme, currentTheme, type Theme } from './theme'

  let theme = $state(currentTheme())
  let other: Theme = $derived(theme === 'dark' ? 'light' : 'dark')

  function toggle() {
    theme = other
    applyTheme(theme, true)
  }
</script>

<button onclick={toggle} aria-label="Switch to {other} theme" title="Switch to {other} theme">
  <span aria-hidden="true">{theme === 'dark' ? '☀' : '☾'}</span>
</button>
