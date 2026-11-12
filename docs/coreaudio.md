# CoreAudio AAC (investigation, on the `coreaudio` branch)

Can the sender use Apple's AAC encoder, the one OBS calls "CoreAudio AAC",
instead of FFmpeg's? Yes: every piece has now been shown to work on its
own. What is left is assembling them into the app. Nothing here is on
`main`.

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

## What it would take to finish

Each part is proven; this is assembly.

1. A second kind of session in `internal/sender`: libmpv captures and
   writes PCM to a local socket; our code reads it in 1024-sample frames,
   encodes with `internal/coreaudio`, and publishes with the RTMP client.
   It plugs in behind the existing `session` interface, so reconnecting and
   giving up after repeated failures come for free.
2. Levels for the meter, computed from the PCM as it passes (the libmpv
   meter filter is not in this path).
3. An "Encoder" choice in the Send tab, shown only when Apple's DLL is
   found, defaulting to FFmpeg AAC.
4. Licence check: the RTMP library is MIT and its dependencies would join
   the bundled notices.

## Is it worth it

At 320 kbps, FFmpeg's AAC is already transparent, and VRCDN's own
troubleshooting page suggests FFmpeg AAC over CoreAudio when listeners hear
dropouts. CoreAudio's advantage is at lower bitrates, which the sender does
not offer. The gain would be matching what DJs already use in OBS, not
audible quality.
