import { mount } from 'svelte'
import '@fontsource-variable/fredoka/wght.css'
import '@fontsource-variable/nunito/wght.css'
import '@fontsource/vt323/latin-400.css'
import './lib/styles/tokens.css'
import './lib/styles/base.css'
import App from './App.svelte'
import { initTheme } from './lib/theme'

initTheme()

// On Windows the title bars are real ones as far as Windows is concerned
// (see "native-drag" in base.css), so touch and VR pointers can drag too.
if (navigator.userAgent.includes('Windows')) document.documentElement.classList.add('native-drag')

const app = mount(App, { target: document.getElementById('app')! })

export default app
