// The one place the UI talks to the Go backend. Everything else imports
// types and the Api interface from here, so the UI can also run in a plain
// browser against the dev mock (npm run dev).
import * as Backend from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

export type Transport = 'rtsp-tcp' | 'rtsp-udp' | 'flv' | 'mpegts'

export const transports: { value: Transport; label: string; hint: string }[] = [
  { value: 'rtsp-tcp', label: 'RTSP · TCP', hint: 'Lowest latency that works almost everywhere' },
  { value: 'rtsp-udp', label: 'RTSP · UDP', hint: 'Can be slightly faster; often blocked by routers' },
  { value: 'flv', label: 'FLV', hint: 'Over HTTPS; use if RTSP is blocked' },
  { value: 'mpegts', label: 'MPEG-TS', hint: 'Over HTTPS; use if RTSP is blocked' },
]

export function transportLabel(t: Transport): string {
  return transports.find((x) => x.value === t)?.label ?? t
}

export interface Stream {
  id: string
  label: string
  /** VRCDN stream name, or a full URL for non-VRCDN streams. */
  source: string
  transport: Transport
}

export const isCustomSource = (source: string) => source.includes('://')

export interface Settings {
  audioDevice: string
  volume: number
  autoReconnect: boolean
  autoCatchUp: boolean
  catchUpAboveMs: number
}

export type Status = 'idle' | 'connecting' | 'playing' | 'reconnecting' | 'failed'

export interface PlayerState {
  status: Status
  streamId: string
  label: string
  url: string
  /** Queued audio in ms; -1 when unknown. */
  bufferMs: number
  catchingUp: boolean
  message: string
  attempt: number
  sinceMs: number
  info: { codec: string; sampleRate: number; channels: number }
}

export interface AudioDevice {
  name: string
  description: string
}

export interface Bootstrap {
  streams: Stream[]
  settings: Settings
  state: PlayerState
  devices: AudioDevice[]
  engine: string
  engineError: string
  engineHelp: string
  notice: string
  configPath: string
}

export const idleState: PlayerState = {
  status: 'idle',
  streamId: '',
  label: '',
  url: '',
  bufferMs: -1,
  catchingUp: false,
  message: '',
  attempt: 0,
  sinceMs: 0,
  info: { codec: '', sampleRate: 0, channels: 0 },
}

export interface Api {
  bootstrap(): Promise<Bootstrap>
  saveStream(stream: Stream): Promise<Stream>
  deleteStream(id: string): Promise<Stream[]>
  moveStream(id: string, to: number): Promise<Stream[]>
  play(id: string): Promise<void>
  stop(): Promise<void>
  resync(): Promise<void>
  /** Applies immediately without saving; for slider drags. */
  setVolume(volume: number): Promise<void>
  updateSettings(settings: Settings): Promise<Settings>
  onState(cb: (state: PlayerState) => void): void
  onDevices(cb: (devices: AudioDevice[]) => void): void
}

// The generated bindings type their arguments as classes; our interfaces are
// the same shape on the wire.
const wails: Api = {
  bootstrap: () => Backend.Bootstrap() as unknown as Promise<Bootstrap>,
  saveStream: (s) => Backend.SaveStream(s as never) as unknown as Promise<Stream>,
  deleteStream: (id) => Backend.DeleteStream(id) as unknown as Promise<Stream[]>,
  moveStream: (id, to) => Backend.MoveStream(id, to) as unknown as Promise<Stream[]>,
  play: (id) => Backend.Play(id),
  stop: () => Backend.Stop(),
  resync: () => Backend.Resync(),
  setVolume: (v) => Backend.SetVolume(v),
  updateSettings: (s) => Backend.UpdateSettings(s as never) as unknown as Promise<Settings>,
  onState: (cb) => void EventsOn('state', cb),
  onDevices: (cb) => void EventsOn('devices', cb),
}

export async function connect(): Promise<Api> {
  if ((window as { go?: { main?: { App?: unknown } } }).go?.main?.App) {
    return wails
  }
  if (import.meta.env.DEV) {
    return (await import('./mock')).createMock()
  }
  throw new Error('The player backend is not available.')
}

/** Go errors arrive as strings; JS ones as Error. */
export function errorText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}
