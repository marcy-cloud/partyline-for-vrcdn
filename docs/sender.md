# A sender for PartyLine (investigation)

Goal: let a DJ push audio to VRCDN from PartyLine, with known-good settings,
instead of setting up OBS. Not built; this records what we know. Context:
[use-case.md](use-case.md), [vrcdn.md](vrcdn.md).

## Tested, 2026-11-11

With `cmd/sendprobe` and the libmpv that ships inside the exe:

- **Audio-only encoding works.** A test tone encodes to AAC at 320 kbps,
  48 kHz stereo, in an FLV with no video track (the FLV header's flags say
  audio only), paced in real time.
- **Capture from an input device is available**: the build includes
  FFmpeg's DirectShow input (`av://dshow:audio=<device name>`).
- **Capturing what the PC is playing ("loopback") is not available** through
  libmpv. That would need our own WASAPI capture code.

## Not tested yet

- **Whether VRCDN accepts a stream with no video**, and whether its outputs
  (RTSP especially) then play. This needs a stream key:

  ```
  bin\sendprobe.exe -key-file <file with the private stream key> -for 60s
  bin\probe.exe -volume 0 -for 20s <private stream name>
  ```

  If audio-only is refused or does not play, the fallback is to add a tiny
  static video track (a black frame, H.264, 1-second keyframes), which the
  bundled libmpv can also produce.
- End-to-end delay of a PartyLine-sent stream compared with OBS.
- Capturing from a real device, and behaviour when the upload drops.

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
