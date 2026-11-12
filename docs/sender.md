# A sender for PartyLine (investigation)

Goal: let a DJ push audio to VRCDN from PartyLine, with known-good settings,
instead of setting up OBS. Not built; this records what we know. Context:
[use-case.md](use-case.md), [vrcdn.md](vrcdn.md).

## Tested, 2026-11-11

With `cmd/sendprobe` and the libmpv that ships inside the exe:

- **VRCDN accepts an audio-only stream.** A test tone sent to a private key
  as AAC, 320 kbps, 48 kHz stereo, in FLV over RTMP with no video track,
  went live about 3 seconds after sending started.
- **Every output plays it.** PartyLine's probe played it over MPEG-TS, FLV,
  RTSP on TCP and RTSP on UDP, with the same level on all four. The
  player's buffer was 0–21 ms over RTSP and FLV, and 130–170 ms over
  MPEG-TS.
- **Sending ran in real time for 75 seconds** with no errors or warnings
  from the encoder or the RTMP connection.
- **Capture from an input device is available**: the build includes
  FFmpeg's DirectShow input (`av://dshow:audio=<device name>`).
- **Capturing what the PC is playing ("loopback") is not available** through
  libmpv. That would need our own WASAPI capture code.

So a sender does not need a video track, and can be built on the libmpv we
already ship.

## Not tested yet

- **VRChat world video players** with an audio-only stream. PartyLine is
  the intended listener, so this only matters if someone points a world
  player at the DJ's pre-feed.
- **VRCDN's control panel preview** with no video.
- End-to-end delay of a PartyLine-sent stream compared with OBS.
- Capturing from a real device, long runs, and behaviour when the upload
  drops.

To repeat the test:

```
bin\sendprobe.exe -key-file <file with the private stream key> -for 60s
bin\probe.exe -volume 0 -for 10s <private stream name>
```

A private stream's name is its key, so treat both as secret.

## Which AAC encoder

- **FFmpeg's built-in AAC** is what the bundled libmpv has, so it is what a
  sender can use today. At 320 kbps, VRCDN's maximum, it is transparent. It
  is weaker than CoreAudio at low bitrates, which do not apply here. VRCDN's
  own troubleshooting page recommends it over CoreAudio when viewers hear
  audio loss.
- **CoreAudio AAC** is Apple's encoder. OBS on Windows does not include it:
  it loads Apple's DLLs if iTunes or Apple Application Support is installed.
  We cannot ship those DLLs. Supporting it would mean detecting them, calling
  Apple's AudioConverter API ourselves, and writing our own FLV/RTMP output,
  because libmpv cannot pass through audio that is already encoded. That is a
  separate project, not a setting.
- **Plan:** an "Encoder" choice in the sender that lists what is actually
  available on the machine. It starts with FFmpeg AAC only; CoreAudio would
  appear when Apple's DLLs are present, once that work is done.

## Settings to use

From VRCDN's wiki: RTMP to `rtmp://ingest.vrcdn.live/live` (or a regional
ingest), AAC, 48 kHz, 320 kbps or less. For the DJ to VJ link, VRCDN's own
advice is to send on the account's **private** key.

## Handling the stream key

It is a secret. Read it from a file or the OS credential store; never put it
on a command line, in the settings file in plain text, in logs, or in
screenshots. `sendprobe` replaces it with `<key>` in everything it prints.
