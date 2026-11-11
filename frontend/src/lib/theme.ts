// Light ("Pastel Dream") or dark ("Midnight Mall"). The first run follows the
// system preference; after that the choice is remembered.
export type Theme = 'light' | 'dark'

const KEY = 'theme'

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}

export function applyTheme(theme: Theme, remember: boolean) {
  document.documentElement.dataset.theme = theme
  if (!remember) return
  try {
    localStorage.setItem(KEY, theme)
  } catch {
    // Storage can be unavailable; the theme still applies for this run.
  }
}

export function initTheme() {
  let saved: string | null = null
  try {
    saved = localStorage.getItem(KEY)
  } catch {
    // As above.
  }
  if (saved === 'light' || saved === 'dark') applyTheme(saved, false)
  else applyTheme(matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light', false)
}
