// A stand-in backend so the UI can be worked on in a browser with
// `npm run dev`. Only loaded in dev builds. Pick a scenario with ?demo=:
//   playing (default) | catchup | reconnecting | failed | idle | empty | noengine
import {
  idleState,
  type Api,
  type AudioDevice,
  type Bootstrap,
  type PlayerState,
  type Settings,
  type Stream,
} from './api'

export function createMock(): Api {
  const demo = new URLSearchParams(location.search).get('demo') ?? 'playing'

  let streams: Stream[] =
    demo === 'empty'
      ? []
      : [
          { id: 'a', label: 'Opening set', source: 'opening_dj', transport: 'rtsp' },
          { id: 'b', label: 'Main room', source: 'mainroom', transport: 'rtsp' },
          { id: 'c', label: 'Guest (offline)', source: 'guest_offline', transport: 'flv' },
          { id: 'd', label: 'Backup relay', source: 'rtsp://relay.example.net:8554/live/backup', transport: 'rtsp' },
          { id: 'e', label: 'Closing set', source: 'closer', transport: 'mpegts' },
        ]
  let settings: Settings = {
    audioDevice: 'auto',
    volume: 80,
    autoReconnect: true,
    autoCatchUp: true,
    catchUpAboveMs: 250,
  }
  const devices: AudioDevice[] = [
    { name: 'auto', description: 'System default' },
    { name: 'wasapi/{1}', description: 'Speakers (Realtek Audio)' },
    { name: 'wasapi/{2}', description: 'CABLE Input (VB-Audio Virtual Cable)' },
  ]

  let state: PlayerState = { ...idleState }
  let listener: (s: PlayerState) => void = () => {}
  let timers: number[] = []

  const set = (patch: Partial<PlayerState>) => {
    state = { ...state, ...patch }
    listener(state)
  }
  const clear = () => {
    timers.forEach((t) => clearInterval(t))
    timers = []
  }

  function begin(stream: Stream, scenario: string) {
    clear()
    state = {
      ...idleState,
      status: 'connecting',
      streamId: stream.id,
      label: stream.label,
      url: stream.source.includes('://') ? stream.source : `rtsp://stream.vrcdn.live/live/${stream.source}`,
    }
    listener(state)

    if (stream.id === 'c' || scenario === 'reconnecting' || scenario === 'failed') {
      timers.push(
        window.setTimeout(() => {
          if (scenario === 'failed' || !settings.autoReconnect) {
            set({ status: 'failed', message: "Stream is offline or doesn't exist" })
          } else {
            set({ status: 'reconnecting', message: "Stream is offline or doesn't exist", attempt: 3 })
          }
        }, 300),
      )
      return
    }

    timers.push(
      window.setTimeout(() => {
        set({
          status: 'playing',
          sinceMs: Date.now() - 754_000,
          bufferMs: 40,
          info: { codec: 'aac', sampleRate: 48000, channels: 2 },
          via: stream.transport.startsWith('rtsp') || stream.source.startsWith('rtsp') ? 'udp' : '',
        })
        let t = 0
        timers.push(
          window.setInterval(() => {
            t++
            const spike = scenario === 'catchup' ? 420 : t % 60 > 52 ? 330 : 0
            const bufferMs = Math.round(35 + Math.random() * 25 + spike * Math.random())
            set({ bufferMs, catchingUp: scenario === 'catchup' })
          }, 250),
        )
      }, 300),
    )
  }

  const api: Api = {
    async bootstrap(): Promise<Bootstrap> {
      const noEngine = demo === 'noengine'
      return {
        streams,
        settings,
        state,
        devices: noEngine ? [] : devices,
        engine: noEngine ? '' : 'mpv v0.40.0 (mock)',
        engineError: noEngine ? 'libmpv was not found (looked for libmpv-2.dll, mpv-2.dll)' : '',
        engineHelp: noEngine
          ? 'Put libmpv-2.dll next to this program (it is inside the mpv-dev-x86_64 download from the mpv Windows builds), then restart.'
          : '',
        notice: '',
        configPath: 'C:\\Users\\you\\AppData\\Roaming\\vrcdn-lowlatency-player\\config.json',
      }
    },
    async saveStream(s) {
      if (!s.source.trim()) throw 'enter a stream name or URL'
      const saved = { ...s, id: s.id || crypto.randomUUID(), label: s.label.trim() || s.source.trim() }
      streams = s.id ? streams.map((x) => (x.id === s.id ? saved : x)) : [...streams, saved]
      return saved
    },
    async deleteStream(id) {
      streams = streams.filter((s) => s.id !== id)
      return streams
    },
    async moveStream(id, to) {
      const from = streams.findIndex((s) => s.id === id)
      if (from < 0) return streams
      const next = streams.filter((s) => s.id !== id)
      next.splice(Math.max(0, Math.min(to, next.length)), 0, streams[from])
      streams = next
      return streams
    },
    async play(id) {
      const stream = streams.find((s) => s.id === id)
      if (stream) begin(stream, 'playing')
    },
    async stop() {
      clear()
      set({ ...idleState })
    },
    async resync() {
      if (state.status === 'playing') set({ bufferMs: 20, catchingUp: false })
    },
    async setVolume() {},
    async updateSettings(s) {
      settings = s
      return settings
    },
    onState(cb) {
      listener = cb
    },
    onDevices() {},
  }

  if (streams.length > 0 && demo !== 'idle' && demo !== 'noengine') {
    begin(streams[1], demo)
  }
  return api
}
