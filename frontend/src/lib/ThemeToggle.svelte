<script lang="ts">
  import { applyTheme, currentTheme, type Theme } from './theme'

  let theme = $state(currentTheme())
  let other: Theme = $derived(theme === 'dark' ? 'light' : 'dark')

  function toggle() {
    theme = other
    applyTheme(theme, true)
  }
</script>

<button class="icon-btn theme" onclick={toggle} aria-label="Switch to {other} theme" title="Switch to {other} theme">
  <span aria-hidden="true">{theme === 'dark' ? '☀' : '☾'}</span>
</button>

<style>
  .theme {
    width: 40px;
    height: 40px;
    font-size: 24px;
    line-height: 1;
  }
</style>
