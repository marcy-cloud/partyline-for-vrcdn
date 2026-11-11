import { mount } from 'svelte'
import '@fontsource-variable/fredoka/wght.css'
import '@fontsource-variable/nunito/wght.css'
import '@fontsource/vt323/latin-400.css'
import './lib/styles/tokens.css'
import './lib/styles/base.css'
import App from './App.svelte'
import { initTheme } from './lib/theme'

initTheme()

const app = mount(App, { target: document.getElementById('app')! })

export default app
