# How late the audio is

Measured on 2026-11-13 with `cmd/latencyprobe`, on one Windows PC on a home
connection (ping to `ingest.vrcdn.live` and `stream.vrcdn.live`: about
31 ms round trip each), sending to a private stream. One PC, one day: treat
the figures as a first reading, not a specification.

## The numbers

Delay from a sound going into PartyLine's sender to the same sound coming
out of the listener's decoder. Each row is 25 beeps, run twice, and the two
runs agreed within 2 ms.

| Path | FFmpeg AAC | CoreAudio AAC |
|---|---|---|
| No network (sender straight into listener) | 46 ms | not measurable this way |
| VRCDN, RTSP over UDP | 103 ms | 104 ms |
| VRCDN, RTSP over TCP | 102 ms | not measured |
| VRCDN, MPEG-TS over HTTPS | 82 ms | 83 ms |
| VRCDN, FLV over HTTPS | 81 ms | not measured |

What follows from them:

- **VRCDN and the network together add about 36 ms (HTTP outputs) to 57 ms
  (RTSP).** About 31 ms of that is the two network legs, going by ping. So
  VRCDN itself holds the audio for roughly 5 ms on the HTTP outputs and
  25 ms on RTSP. It is not where the delay is.
- **RTSP came out about 20 ms slower than MPEG-TS and FLV**, on both
  encoders. That is one AAC frame; the guess is that the RTSP output waits
  for the next frame before sending one. The player's own queue is the
  other way round (0-21 ms over RTSP, 20-40 ms over MPEG-TS, see
  [vrcdn.md](vrcdn.md)), so what a listener hears is close to a tie.
- **About 46 ms is PartyLine and AAC themselves**: filling and encoding
  frames of 21 ms, and decoding them.

## What is not in the numbers

The figure is for the freshest audio at the decoder's output. A listener
hears it later than that by:

- up to 21 ms, as the oldest sample in an AAC frame has waited for the frame
  to fill;
- the capture device's buffer on the sending side, 20 ms in the app;
- the player's queue, and the audio device's own buffer (not measured;
  typically 10-30 ms).

Adding those up, a fair expectation from a DJ's input to a listener's
speakers through VRCDN, PartyLine at both ends, is **roughly 150-200 ms**.
A DJ sending with OBS instead has OBS's own delay in place of the sender's;
that has not been measured.

## CoreAudio AAC sent in lumps (fixed)

The first measurement of CoreAudio showed the freshest audio only 12 ms
later than with FFmpeg (115 ms over RTSP, 94 ms over MPEG-TS), but the
oldest audio in each burst reaching the listener about 435 ms late, against
about 190 ms for FFmpeg in the same test. The CoreAudio path took captured
audio from libmpv as raw PCM through its encoding mode, and that mode hands
PCM over 16384 frames (341 ms) at a time. So the stream went out a third of
a second at a time, and a listener was up to a third of a second behind, or
heard gaps.

Fixed the same day: the capture now goes through libmpv's `pcm` audio
output into a pipe (`internal/pcmpipe`, `internal/sender/capture.go`),
which hands it over a frame at a time. Measured again, twice: freshest
103-104 ms over RTSP and 82-83 ms over MPEG-TS, oldest in a burst about
185 ms and 163 ms. The two encoders are now level. The table above has the
figures from after the fix.

## How the probe works

- `internal/latency` makes a test signal, a 60 ms beep every 1.337 s over
  quiet hiss, and finds the beeps again in received audio. The beeps cycle
  through eight pitches so a late one isn't mistaken for the next.
- The probe serves that signal to the real sender in real time
  (`sender.PCMInput`), so it knows when each sample was "captured".
- `player.OpenTap` listens with the player's own low-latency options, but
  into a pipe instead of an audio device, so the probe sees each piece of
  audio as it is decoded.
- Both ends are on one PC, so one clock times both.

Things learned building it, all of which gave wrong numbers first:

- **Silence is a bad test signal.** It encodes to about 80 bytes a frame,
  and anything that waits for a number of bytes then holds it ten times
  longer than it would hold music. Hence the hiss.
- **libmpv's encoding mode hands out PCM in 341 ms lumps** (see above), so
  the first tap, built that way, read about 65 ms late and smeared
  everything.
- **Audio arrives in bursts**, so when a beep's burst arrived is not when
  the beep did. The probe times the end of each burst, the one moment that
  is known, and works back along the stream. That is "freshest audio" in
  its output. "Oldest in a burst" is the other end of the burst.
- **The probe's own input is bursty**: libmpv reads raw PCM 4096 frames
  (85 ms) at a time, where a capture device gives about 20 ms. With the test
  tone and with two real input devices the sender sends a frame every 21 ms
  or so. So "oldest in a burst" is about 65 ms pessimistic in every row,
  and the freshest figure is the one to compare.
- **`-loopback` is not RTMP.** The engine's RTMP server mode stalls, so the
  no-network run sends the same AAC in FLV over a plain local TCP socket.

## Running it

```
go build -tags bundled_libmpv -o bin/latencyprobe.exe ./cmd/latencyprobe
bin\latencyprobe.exe -loopback
bin\latencyprobe.exe -key-file key.txt
bin\latencyprobe.exe -key-file key.txt -encoder coreaudio -via rtsp-udp,mpegts
```

It goes on air on that key for about half a minute per output. Use a
private stream, keep the key file outside the repo, and delete it after.

## What this means for a direct (peer-to-peer) option

Going direct would save the detour through VRCDN: at most about 36-57 ms,
less whatever the direct network path costs, so perhaps 20-40 ms. A codec
with shorter frames (Opus) could save a similar amount again. Neither is a
large part of 150-200 ms, and neither needs VRCDN to be slow to be worth
having: the case for a direct option is reaching DJs who have no VRCDN
subscription, not speed.
