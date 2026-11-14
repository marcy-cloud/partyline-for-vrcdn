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

- **Sending everything the PC plays.** A single program can be sent (see
  "Sending a program's sound" below); the whole of an output device cannot
  yet.
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

## Sending a program's sound

In the app since 2026-11-14: the Input list has a second group, "Programs
playing sound". Choosing one sends what that program plays and nothing
else. It is for a DJ whose DJ software runs on the PC that sends.

How it is built:

- `internal/loopback` uses WASAPI process loopback, the Windows feature
  behind OBS's "Application Audio Capture" (Windows 10 version 2004 and
  later). Given a process ID, Windows hands over what that process and the
  processes it started are playing, as 48 kHz stereo 16-bit in 10 ms
  pieces. Nothing is installed and the PC's sound settings are not touched.
  It is plain COM called from Go; no C, no new dependency.
- `loopback.Programs` lists the programs that have sound open on any output
  (Windows audio sessions), named as they name themselves, playing ones
  first. PartyLine itself is left out.
- The input's ID is `app:` and the program's file name (`app:rekordbox.exe`),
  so the choice survives the program restarting. `sender/app.go` finds the
  running program when a broadcast starts.
- CoreAudio AAC takes the captured sound directly. FFmpeg AAC gets it as
  raw PCM over a local socket (`sender.PCMInput`). libavformat reads raw
  PCM a tenth of a second at a time, which held back 85 ms; it is told the
  audio is 6 kHz so that it takes 11 ms pieces, and a filter relabels it
  48 kHz (`pcmRateFix` in `devices.go`). The delay probe's no-network run
  went from bursts of 85 ms to bursts of 21 ms with that.

Tested on Windows 10 22H2:

- A second process playing a 440 Hz tone at -60 dBFS is captured at
  -61.7 dBFS, 1000 ms of audio per second, no gaps.
- **Only that program is heard.** Capturing `explorer.exe` while the tone
  played from the other process gave silence (-90 dBFS, the last bit).
- **A silent program still gives a steady stream** of near-silence, so the
  encoder is never starved. (On this PC. Microsoft doesn't promise it; if
  nothing arrives for 100 ms the sender fills in silence.)
- **A process ID that doesn't exist gives silence too, with no error**, so
  the sender watches the program itself. When it closes, the connection
  ends and the sender keeps trying, saying the program isn't running, until
  it is opened again.
- **Windows Media Player playing a FLAC**: listed as "Windows Media
  Player", playing. Sent through the FFmpeg path into a listener on the
  same PC, the level decoded at the far end follows the level going in.
  On air to a private VRCDN stream from the real window and from
  `sendprobe`, with each encoder: live within a second, levels moving with
  the music (-3 to -15 dB).

Not known yet:

- **Real DJ programs.** Some use exclusive mode or ASIO to reach the sound
  card, and Windows cannot capture those this way at all. `loopprobe -list`
  and `loopprobe -exe <name>` show quickly whether one can be captured.
- Whether the program's volume in the Windows mixer, or muting the PC's
  speakers, changes what is sent. It matters: a DJ may want the laptop
  silent while sending.
- What was heard at the far end. Levels were checked, not listened to.
- Delay with this input. Not measured.
- A program closed and reopened during a broadcast: the closing was seen to
  end the connection; the picking up again was not tried.

`cmd/loopprobe` is the console tool: `-list`, `-exe name.exe`, `-exe
name.exe -send` (through the sender into a local listener), and `-tone 20s`
to be a program playing a very quiet tone.
