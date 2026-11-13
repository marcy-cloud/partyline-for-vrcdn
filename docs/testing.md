# How we test

What to run, in rough order of cost, and which stream to test against.

## Test streams

- **The VRCDN demo stream, `vrcdn`**, is the default for anything that needs
  a real stream. It is public, always there, and has real audio (peaks around
  -20 dB), so it also exercises the level meter.
  - It plays over **MPEG-TS, FLV and RTSP**. Its buffer over RTSP runs
    higher than a DJ's stream (roughly 170–400 ms seen), so it is good for
    "does it connect" but not for latency numbers.
  - If RTSP gets no response, suspect the network before the code: on
    2026-11-11 it failed from coffee-shop Wi-Fi and worked later the same day
    over RTSP/TCP. Run the MPEG-TS control first.
- **A DJ's own stream** is the one to measure latency on. Ask before using
  one, play it muted, and check it is live first: an offline stream looks
  like a hang over RTSP (see below).
- **The local test tone** needs no network: the tests in
  `internal/player/mpv_live_test.go` serve an endless -12.2 dB WAV over HTTP
  and play it through the real libmpv to a null output.

## Things that look like bugs but are the stream

- **Offline over RTSP is silence, not an error.** VRCDN's RTSP server does
  not answer for a stream that is offline or does not exist; the HTTP outputs
  (`.live.ts`, `.live.flv`) answer 401 or 404 after about 4 seconds. The
  player uses that to show "Stream is offline or doesn't exist". A custom
  RTSP URL has no such check, and offline there looks like "Timed out
  connecting".
- **A stream with no audio playing reads -100 dB** on the meter. That is
  digital silence from the sender, not a broken meter.

## The steps

Run from the repository root on Windows (from WSL, use `go.exe`, `cmd.exe /c`
and so on).

1. **Unit tests**, no libmpv needed: `go test ./internal/...`
2. **With the real libmpv**, silent, no network beyond localhost:
   `go test -tags bundled_libmpv ./internal/...`
3. **UI type-check**: `cd frontend && npm run check`
4. **A real stream, no window**, muted:

   ```
   go build -o bin/probe.exe ./cmd/probe
   bin\probe.exe -volume 0 -for 10s -transport mpegts vrcdn
   bin\probe.exe -volume 0 -for 10s -transport flv vrcdn
   ```

   Expect `playing`, a buffer reading that settles under about 100 ms, and a
   `level` line that is not -100. Then RTSP with the default transport:
   `bin\probe.exe -volume 0 -for 14s vrcdn`. Expect `via udp`, or a fallback
   to `via tcp` after 4 seconds on a network that drops UDP. On a live DJ
   stream expect a buffer of 0–21 ms.
5. **The real window** (Windows):

   ```
   cd frontend && npm run build && cd ..
   go build -tags bundled_libmpv -o bin/dev.exe .
   node scripts/ui-smoke.mjs bin\dev.exe bin
   ```

   It plays the **first saved stream** muted, stops, plays again, opens the
   dialog and switches theme, and warns if the panel overflows the default
   window. Look at the `smoke-*.png` files it writes. It runs beside an open
   copy of the app.
6. **The UI against mock data**, for layout work: `cd frontend && npm run
   dev`, then `?demo=playing|catchup|reconnecting|failed|idle|empty|noengine`.
7. **Before a release**: `scripts\package.ps1`, then start the exe from the
   zip once.

## Sending

See [sender.md](sender.md) for what was tested and the commands.

- `cmd/sendprobe` sends from a console. Its default input is the -12 dB test
  tone, so nothing from the PC goes out by accident. The stream key is read
  from a file, never passed on the command line.
- With `PARTYLINE_TEST_DIALOGS=1` the real-window smoke test also exports the
  stream list and imports it again through the system's file dialogs. It
  types into them, so leave the keyboard alone while it runs. Importing the
  list just exported adds nothing, so the saved streams are not changed.
- The real-window smoke test always opens the Send tab. With the environment
  variable `PARTYLINE_TEST_KEY` set to a stream key it also saves the key,
  goes on air with the test tone, checks the level, stops, and forgets the
  key. Use a private key, and never commit or paste one.
- A stream key that has been pasted anywhere should be regenerated in the
  VRCDN panel.
