# CoreAudio AAC (on the `coreaudio` branch)

The sender can use Apple's AAC encoder, the one OBS calls "CoreAudio AAC",
as an alternative to FFmpeg's. It is built and works; it lives on the
`coreaudio` branch until it has had more use. This file records how it
works and what was learned getting there.

## In the app

- The Send tab shows an **Encoder** choice only on PCs where Apple's
  encoder is installed (it comes with iTunes). FFmpeg AAC stays the default.
- With CoreAudio chosen, `internal/sender` uses a second kind of session
  (`coreaudio_windows.go`): libmpv captures the input and writes raw PCM to
  a local socket; our code reads it a frame at a time, measures the level,
  encodes with `internal/coreaudio`, and publishes over RTMP with
  `gortmplib`. It sits behind the same `session` interface as the libmpv
  path, so reconnecting, giving up after repeated refusals, stall detection
  and the helper process are shared.
- Stopping signs off properly (`FCUnpublish`, `deleteStream`) before hanging
  up.

Tested 2026-11-11: on air in about a second from the console and from the
real window, with the test tone (-12 dB read back over RTSP) and with a
DirectShow device; ten start-stop cycles in a row; alternating with the
FFmpeg encoder back to back; a wrong key and an unreachable server both
give a clear message and stop.

## What works

- **Finding it.** Apple Application Support (installed with iTunes) records
  its folder in the registry at `HKLM\SOFTWARE\Apple Inc.\Apple Application
  Support`, value `InstallDir`. `CoreAudioToolbox.dll` is in that folder. OBS
  uses the same DLL through its `coreaudio-encoder` plugin. We cannot ship
  it; it is used only if it is already there.
- **Calling it.** `internal/coreaudio` loads the DLL and drives Apple's
  `AudioConverter` API directly from Go, with no C compiler: 16-bit PCM in,
  one AAC-LC packet out per 1024 samples, constant bitrate. A test tone
  encodes at 320 kbps about 30 times faster than real time.
- **The output is valid.** Wrapped as an ADTS file it plays back through
  libmpv at the level it went in (-12.0 dB on both channels).
- **VRCDN takes it.** Sent over RTMP with a Go client
  (`github.com/bluenviron/gortmplib`, MIT), the stream stayed on air for two
  minutes and played on MPEG-TS, FLV, and RTSP over TCP and UDP, at
  -12.0 dB.
- **libmpv can feed it.** In encoding mode libmpv will capture an input
  device and write raw 16-bit PCM to a local TCP socket (`o=tcp://127.0.0.1:
  <port>`, `of=s16le`, `oac=pcm_s16le`) in real time, starting about 0.3
  seconds after it is asked. Tested with the test tone (-12.1 dBFS read back
  from the PCM) and a DirectShow device. So capture can stay as it is.

`cmd/coreaudioprobe` covers the encoder and RTMP parts: `-o tone.aac` writes
a file, `-key-file` sends to an ingest.

## Why a second RTMP path is needed

The sender on `main` lets libmpv capture, encode and send. libmpv can only
send what it encodes itself: it has no way to pass through audio that is
already encoded. So audio encoded by Apple's encoder has to be sent by our
own RTMP code.

## The 30-second cut (solved)

At first VRCDN closed the connection after 30 seconds, every time, sending
nothing beforehand, while the libmpv sender stayed up to the same key.

**Cause:** the address was written the way ffmpeg takes it,
`rtmp://ingest.vrcdn.live/live/<key>`. This library takes the stream name
from the URL's fragment instead: `rtmp://ingest.vrcdn.live/live#<key>`.
Written as a path, the key went out as part of the app name and the stream
was published with an empty name. VRCDN accepts that, serves it, and drops
it after 30 seconds.

**How it was found:** a TCP relay recorded what each sender said to VRCDN,
and decoding the two recordings side by side showed `publish ""` against
`publish "<key>"`. Guessing had not found it: rewording the metadata,
halving the bitrate and imitating ffmpeg's client identity all left the cut
at 30 seconds.

Worth knowing for any other RTMP client: VRCDN does not refuse a publish
with a wrong or empty stream name straight away.

## What VRCDN's ingest does, learned on the way

- **A wrong key** is refused at publish with `NetStream.Publish.BadAuth`,
  "Invalid stream key or user disabled".
- **A key already in use** is refused the same way with "Stream is locked".
  The sender words the two differently.
- **A publish that never sends audio** leaves the key locked for a little
  under 30 seconds afterwards, whether or not the client signs off. After a
  real broadcast that signs off, the key is free at once.
- **A publish with an empty stream name** is accepted, served, and dropped
  after 30 seconds (see above).

## Not done

- Not tried on air for long, or with a hardware mixer as the input.
- Only 16-bit stereo at 48 kHz goes to the encoder, which is what the rest
  of the sender uses.
- If the PC is very busy, PCM could queue in the local socket; nothing
  measures or drains that yet. The FFmpeg path has the same property inside
  libmpv.

## Is it worth it

At 320 kbps, FFmpeg's AAC is already transparent, and VRCDN's own
troubleshooting page suggests FFmpeg AAC over CoreAudio when listeners hear
dropouts. CoreAudio's advantage is at lower bitrates, which the sender does
not offer. The gain would be matching what DJs already use in OBS, not
audible quality.
