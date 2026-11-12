# The sender

PartyLine can send as well as listen: the Send tab pushes audio, and only
audio, from an input device to VRCDN, so a DJ does not need OBS for the
pre-feed. Context: [use-case.md](use-case.md), [vrcdn.md](vrcdn.md).

## What it does

- Captures from a Windows audio input (a mixer's USB input, a virtual
  cable), or a built-in 440 Hz test tone at -12 dBFS for checking the link
  with no gear.
- Encodes AAC at 320, 256 or 192 kbps, 48 kHz stereo, and sends it over RTMP
  with no video track, to VRCDN's default ingest or a chosen region.
- Shows the outgoing level, the time on air, and reconnects by itself if the
  connection drops. If it never gets on air in three tries (a wrong key, for
  example) it stops and says why, rather than keep knocking.
- Sends to a saved **send target**: a label, a stream key and a server,
  kept in the "Send to" wing on the right. Clicking one chooses where Go
  live sends; it can't be changed while on air.
- Keeps each target's key encrypted with Windows data protection (DPAPI),
  so only the same Windows account on the same PC can read it. The key goes
  in through the target's dialog and is never shown, logged or sent back to
  the UI. A key saved before there were targets becomes one called "My
  stream".

## How it is built

- `internal/sender`: `Sender` runs a broadcast on a libmpv core in encoding
  mode and holds the reconnect policy. `Devices` lists inputs by asking
  ffmpeg's DirectShow input, whose listing only comes out as log lines.
- **The sender runs in a process of its own.** The app starts a second copy
  of itself (`partyline.exe --send-worker`), hands it the settings and key on
  stdin, and reads state and levels from stdout; closing stdin stops it.
  `--list-inputs` does the device listing the same way. This is because
  libmpv gives every core's ffmpeg log lines to the first core created in a
  process, which in the app is the player's: a sender in the same process
  could not see its own errors or the device list. It also keeps a broadcast
  and the player from taking each other down.
- `internal/secret` protects the key; `internal/meter` is the level meter
  shared with the player.
- `cmd/sendprobe` drives the sender from a console, in one process.

## Tested, 2026-11-11

- **VRCDN accepts an audio-only stream** (AAC in FLV over RTMP, no video),
  and serves it on MPEG-TS, FLV, and RTSP over TCP and UDP. The player's
  buffer was 0–21 ms over RTSP and FLV.
- **On air about 1 second after starting**, with the test tone and with a
  real input device.
- **In the real window:** went on air with the test tone, level read
  -12 dB, stopped, and the key was forgotten again.
- **A wrong key** is refused by VRCDN with "Invalid stream key or user
  disabled"; the sender reports it and stops after three tries.

## Not done or not tested

- **Capturing what the PC is playing ("loopback").** Only input devices can
  be sent. Loopback needs our own WASAPI capture code. Until then a virtual
  audio cable does the job.
- **A level meter before going live.** The meter only moves while on air.
- **Linux and macOS.** Capture uses DirectShow, so the Send tab is hidden
  elsewhere.
- **Long runs**, a real mixer as the input, and what listeners hear when the
  upload drops and comes back.
- **VRChat world video players** and the VRCDN panel preview with an
  audio-only stream. PartyLine is the intended listener.
- End-to-end delay compared with OBS.

## Which AAC encoder

- **FFmpeg's built-in AAC** is the default and is always available. At
  320 kbps, VRCDN's maximum, it is transparent. VRCDN's own troubleshooting
  page recommends it over CoreAudio when viewers hear audio loss.
- **CoreAudio AAC** is Apple's encoder, the one DJs often pick in OBS. It
  appears as an Encoder choice on PCs where iTunes has installed it; we
  cannot ship it. How that path works is in [coreaudio.md](coreaudio.md).

## To test by hand

```
bin\sendprobe.exe -list
bin\sendprobe.exe -key-file <file with the private stream key> -for 60s
bin\probe.exe -volume 0 -for 10s <private stream name>
```

A private stream's name is its key, so treat both as secret. The smoke test
goes on air with the test tone when `PARTYLINE_TEST_KEY` is set, and forgets
the key afterwards; see [testing.md](testing.md).
