# CoreAudio AAC (investigation, on the `coreaudio` branch)

Can the sender use Apple's AAC encoder, the one OBS calls "CoreAudio AAC",
instead of FFmpeg's? Short answer: the encoder part works; getting its
output to VRCDN has one unsolved problem. Nothing here is on `main`.

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
  (`github.com/bluenviron/gortmplib`, MIT), the stream goes live and plays
  on MPEG-TS, FLV, and RTSP over TCP and UDP, at -12.0 dB.

`cmd/coreaudioprobe` does all of the above: `-o tone.aac` writes a file,
`-key-file` sends to an ingest.

## Why a second RTMP path is needed

The sender on `main` lets libmpv capture, encode and send. libmpv can only
send what it encodes itself: it has no way to pass through audio that is
already encoded. So audio encoded by Apple's encoder has to be sent by our
own RTMP code.

## The unsolved problem

**VRCDN closes the connection after 30 seconds, every time.** The server
sends nothing first (no ping, no status message), then closes cleanly. The
existing libmpv sender, to the same key, stays up (75 seconds tested).

Ruled out, each by a test:

| Theory | Test | Result |
| --- | --- | --- |
| The library's metadata (it announces video codec 0 and data rates 0) | Wrote the metadata by hand, worded as ffmpeg does for audio only | Still 30 s |
| A byte-count limit | Sent at 192 kbps instead of 320 | Still 30 s, so it is time, not bytes |
| VRCDN treats the client differently by its identity string | Patched the library to announce itself as ffmpeg does (`FMLE/3.0`, type `nonprivate`) | Still 30 s |
| The server is waiting on a ping reply | Logged everything the server sends | It sends nothing before closing |

Not yet tried:

- **Compare the two conversations byte for byte.** Capture what ffmpeg
  sends (a packet capture of the working sender, or a local RTMP server
  that ffmpeg will talk to; the library's own server did not get ffmpeg
  past the handshake) and diff it against ours. The difference is in there.
- **Ask VRCDN** what their ingest expects from a publisher. The author of
  the app this one is based on works on VRCDN.
- **A different RTMP client**, or a small one of our own that mirrors
  ffmpeg's sequence exactly. Publishing audio is a short conversation.

## What it would take to finish

1. Solve the 30-second cut.
2. Get PCM from the input device to the encoder. Plan: keep libmpv for
   capture and have it write raw PCM to a local socket instead of encoding;
   untested. (Or capture with WASAPI ourselves, which would also allow
   capturing what the PC is playing.)
3. Reconnect, levels and state for this path, as the libmpv sender has.
4. An "Encoder" choice in the Send tab, shown only when Apple's DLL is
   found.

## Is it worth it

At 320 kbps, FFmpeg's AAC is already transparent, and VRCDN's own
troubleshooting page suggests FFmpeg AAC over CoreAudio when listeners hear
dropouts. CoreAudio's advantage is at lower bitrates, which the sender does
not offer. The gain would be matching what DJs already use in OBS, not
audible quality.
