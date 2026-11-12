// A stand-in backend so the UI can be worked on in a browser with
// `npm run dev`. Only loaded in dev builds. Pick a scenario with ?demo=:
//   playing (default) | catchup | reconnecting | failed | idle | empty | noengine
//   | sending (a stream key is already saved, and the Send tab opens)
import {
  idleState,
  type Api,
  type AudioDevice,
  type Bootstrap,
  type Levels,
  type SendInfo,
  type SendState,
  sendOff,
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
          { id: 'f', label: 'Marcy', source: 'marcymarcy', transport: 'rtsp' },
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

  let levelListener: (levels: Levels) => void = () => {}

  let send: SendInfo = {
    supported: true,
    settings: { targetId: 't1', device: 'line-in', bitrateKbps: 320, encoder: 'ffmpeg' },
    targets: [
      { id: 't1', label: 'My private feed', ingest: 'rtmp://ingest.vrcdn.live/live' },
      { id: 't2', label: 'Club X guest key', ingest: 'rtmp://uk.ingest.vrcdn.live/live' },
    ],
    state: sendOff,
    devices: [
      { id: 'line-in', name: 'Line In (DJ Mixer USB)' },
      { id: 'cable', name: 'CABLE Output (VB-Audio Virtual Cable)' },
      { id: 'test-tone', name: 'Test tone (440 Hz)' },
    ],
    ingests: [
      { name: 'Nearest (automatic)', url: 'rtmp://ingest.vrcdn.live/live' },
      { name: 'Europe: London', url: 'rtmp://uk.ingest.vrcdn.live/live' },
    ],
    bitrates: [320, 256, 192],
    encoders: [
      { id: 'ffmpeg', name: 'FFmpeg AAC' },
      { id: 'coreaudio', name: 'CoreAudio AAC (Apple)' },
    ],
  }
  let sendTimers: number[] = []
  let sendListener: (state: SendState) => void = () => {}
  let sendLevelListener: (levels: Levels) => void = () => {}
  function setSend(state: SendState) {
    send = { ...send, state }
    sendListener(state)
  }
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
        // A wandering signal around -14 dB, slightly different per channel.
        timers.push(
          window.setInterval(() => {
            const base = -14 + 8 * Math.sin(Date.now() / 300)
            levelListener({ left: base - 6 * Math.random(), right: base - 2 - 6 * Math.random() })
          }, 50),
        )
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
        secrets: true,
        engineHelp: noEngine
          ? 'Put libmpv-2.dll next to this program (it is inside the mpv-dev-x86_64 download from the mpv Windows builds), then restart.'
          : '',
        notice: '',
        configPath: 'C:\\Users\\you\\AppData\\Roaming\\partyline\\config.json',
      }
    },
    async saveStream(s) {
      const old = streams.find((x) => x.id === s.id)
      if (!s.source.trim() && !(s.private && old?.private)) throw 'enter a stream name or URL'
      const saved = s.private
        ? { ...s, id: s.id || crypto.randomUUID(), source: '', label: s.label.trim() || 'Private stream' }
        : { ...s, id: s.id || crypto.randomUUID(), label: s.label.trim() || s.source.trim() }
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
    onLevels(cb) {
      levelListener = cb
    },

    async sendInfo() {
      return send
    },
    async saveSendSettings(s) {
      send = { ...send, settings: s }
      return s
    },
    async saveSendTarget(t) {
      if (!t.id && !t.key.trim()) throw new Error('enter the stream key')
      const saved = { id: t.id || crypto.randomUUID(), label: t.label.trim() || 'My stream', ingest: t.ingest }
      send = {
        ...send,
        targets: t.id ? send.targets.map((x) => (x.id === t.id ? saved : x)) : [...send.targets, saved],
      }
      return saved
    },
    async deleteSendTarget(id) {
      send = { ...send, targets: send.targets.filter((x) => x.id !== id) }
      return send.targets
    },
    async startSend() {
      if (!send.targets.some((t) => t.id === send.settings.targetId)) throw new Error('add somewhere to send to first')
      setSend({ ...sendOff, status: 'connecting' })
      sendTimers.push(
        window.setTimeout(() => {
          setSend({ ...sendOff, status: 'live', sinceMs: Date.now() })
          sendTimers.push(
            window.setInterval(() => {
              const base = -10 + 6 * Math.sin(Date.now() / 250)
              sendLevelListener({ left: base - 5 * Math.random(), right: base - 1 - 5 * Math.random() })
            }, 50),
          )
        }, 600),
      )
    },
    async stopSend() {
      sendTimers.forEach((t) => {
        clearTimeout(t)
        clearInterval(t)
      })
      sendTimers = []
      setSend(sendOff)
    },
    onSend(cb) {
      sendListener = cb
    },
    onSendLevels(cb) {
      sendLevelListener = cb
    },
  }

  if (streams.length > 0 && demo !== 'idle' && demo !== 'noengine') {
    begin(streams[1], demo)
  }
  return api
}
