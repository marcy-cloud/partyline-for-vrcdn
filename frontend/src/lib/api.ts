// The one place the UI talks to the Go backend. Everything else imports
// types and the Api interface from here, so the UI can also run in a plain
// browser against the dev mock (npm run dev).
import { Events } from '@wailsio/runtime'
import { App as Backend } from '../../bindings/partyline'

export type Transport = 'rtsp' | 'rtsp-udp' | 'rtsp-tcp' | 'flv' | 'mpegts'

// label is for the dialog's menu, short for the narrow list rows.
export const transports: { value: Transport; label: string; short: string; hint: string }[] = [
  { value: 'rtsp', label: 'RTSP', short: 'RTSP', hint: 'Tries UDP for the lowest latency, then TCP, then MPEG-TS' },
  { value: 'rtsp-udp', label: 'RTSP · UDP only', short: 'UDP only', hint: 'Lowest latency; often blocked by routers' },
  { value: 'rtsp-tcp', label: 'RTSP · TCP only', short: 'TCP only', hint: 'Works almost everywhere' },
  { value: 'flv', label: 'FLV', short: 'FLV', hint: 'Over HTTPS; use if RTSP is blocked' },
  { value: 'mpegts', label: 'MPEG-TS', short: 'MPEG-TS', hint: 'Over HTTPS; use if RTSP is blocked' },
]

export function transportLabel(t: Transport): string {
  return transports.find((x) => x.value === t)?.short ?? t
}

export interface Stream {
  id: string
  label: string
  /**
   * VRCDN stream name, or a full URL for non-VRCDN streams. For a private
   * stream it is always '' coming from the backend; going to the backend it
   * carries a newly typed key, or '' to keep the saved one.
   */
  source: string
  transport: Transport
  /** A VRCDN private stream: its name is a secret, stored encrypted. */
  private?: boolean
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
  /** RTSP transport in use: 'udp', 'tcp', or '' when the stream isn't RTSP. */
  via: string
}

/** Peak level of the incoming audio per channel, in dBFS (0 = full scale). */
export interface Levels {
  left: number
  right: number
}

/** What the meter shows when there is no signal, and the bottom of its scale. */
export const silentLevels: Levels = { left: -100, right: -100 }

export type SendStatus = 'off' | 'connecting' | 'live' | 'reconnecting' | 'failed'

export interface SendState {
  status: SendStatus
  message: string
  attempt: number
  /** When this broadcast first went live (Unix ms), or 0. */
  sinceMs: number
}

export const sendOff: SendState = { status: 'off', message: '', attempt: 0, sinceMs: 0 }

export interface SendSettings {
  /** The send target chosen; '' when there is none. */
  targetId: string
  device: string
  bitrateKbps: number
  /** Which AAC encoder to send with: an id from SendInfo.encoders. */
  encoder: string
}

/** A saved place to send to. The key is never sent back from the backend. */
export interface SendTarget {
  id: string
  label: string
  ingest: string
}

/** A target on its way to be saved: with a newly typed key, or '' to keep the saved one. */
export type SendTargetDraft = SendTarget & { key: string }

export interface SendInfo {
  /** False where this build can't send; the Send tab is then hidden. */
  supported: boolean
  settings: SendSettings
  targets: SendTarget[]
  state: SendState
  devices: { id: string; name: string }[]
  ingests: { name: string; url: string }[]
  bitrates: number[]
  /** AAC encoders usable on this PC, default first; more than one only where Apple's is installed. */
  encoders: { id: string; name: string }[]
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
  /** Whether private streams can be saved on this system. */
  secrets: boolean
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
  via: '',
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
  /** About 20 readings a second while a stream is playing. */
  onLevels(cb: (levels: Levels) => void): void

  sendInfo(): Promise<SendInfo>
  saveSendSettings(settings: SendSettings): Promise<SendSettings>
  saveSendTarget(target: SendTargetDraft): Promise<SendTarget>
  deleteSendTarget(id: string): Promise<SendTarget[]>
  startSend(): Promise<void>
  stopSend(): Promise<void>
  onSend(cb: (state: SendState) => void): void
  onSendLevels(cb: (levels: Levels) => void): void
}

// The generated bindings use their own (enum-based) types; our interfaces
// are the same shape on the wire. Go sends an empty list as null.
const wails: Api = {
  bootstrap: async () => {
    const boot = (await Backend.Bootstrap()) as unknown as Bootstrap
    return { ...boot, streams: boot.streams ?? [], devices: boot.devices ?? [] }
  },
  saveStream: (s) => Backend.SaveStream(s as never) as unknown as Promise<Stream>,
  deleteStream: async (id) => ((await Backend.DeleteStream(id)) ?? []) as unknown as Stream[],
  moveStream: async (id, to) => ((await Backend.MoveStream(id, to)) ?? []) as unknown as Stream[],
  play: (id) => Backend.Play(id),
  stop: () => Backend.Stop(),
  resync: () => Backend.Resync(),
  setVolume: (v) => Backend.SetVolume(v),
  updateSettings: (s) => Backend.UpdateSettings(s as never) as unknown as Promise<Settings>,
  onState: (cb) => void Events.On('state', (e) => cb(e.data as unknown as PlayerState)),
  onDevices: (cb) => void Events.On('devices', (e) => cb((e.data ?? []) as unknown as AudioDevice[])),
  onLevels: (cb) => void Events.On('levels', (e) => cb(e.data as unknown as Levels)),

  sendInfo: async () => {
    const info = (await Backend.SendInfo()) as unknown as SendInfo
    return {
      ...info,
      targets: info.targets ?? [],
      devices: info.devices ?? [],
      ingests: info.ingests ?? [],
      bitrates: info.bitrates ?? [],
      encoders: info.encoders ?? [],
    }
  },
  saveSendSettings: (s) => Backend.SaveSendSettings(s) as unknown as Promise<SendSettings>,
  saveSendTarget: (t) => Backend.SaveSendTarget(t) as unknown as Promise<SendTarget>,
  deleteSendTarget: async (id) => ((await Backend.DeleteSendTarget(id)) ?? []) as unknown as SendTarget[],
  startSend: () => Backend.StartSend(),
  stopSend: () => Backend.StopSend(),
  onSend: (cb) => void Events.On('send', (e) => cb(e.data as unknown as SendState)),
  onSendLevels: (cb) => void Events.On('sendLevels', (e) => cb(e.data as unknown as Levels)),
}

/** True inside the Wails window, false in a plain browser (npm run dev). */
export function inWails(): boolean {
  const w = window as {
    chrome?: { webview?: { postMessage?: unknown } }
    webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } }
  }
  return !!(w.chrome?.webview?.postMessage || w.webkit?.messageHandlers?.external?.postMessage)
}

export async function connect(): Promise<Api> {
  if (inWails()) {
    return wails
  }
  if (import.meta.env.DEV) {
    return (await import('./mock')).createMock()
  }
  throw new Error('The player backend is not available.')
}

/** Turns whatever a failed call rejected with into text for the user. */
export function errorText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}
