# VRCDN Low Latency Player

A small desktop app that plays the **audio** of a [VRCDN](https://vrcdn.live/)
stream with as little delay as possible. It is meant for VJs and lighting
operators who need a performer's audio in their own software: save each
performer's stream once, then switch between them with one click.

It is a from-scratch rebuild of the Electron
[VRCDN-LowLatency-Player](https://github.com/jazzy348/VRCDN-LowLatency-Player),
written in Go with [Wails](https://wails.io) and an embedded
[libmpv](https://mpv.io) instead of a spawned `mpv.exe`. On Windows it is a
single exe with nothing else to install.

## What it does

- **Saved streams.** Paste a stream name or any URL from the VRCDN page
  (RTSP, RTSPT, FLV, MPEG-TS, RTMP, WSS); it is reduced to the stream name.
  Non-VRCDN `rtsp://`/`https://`/… URLs are kept as-is.
- **One-click switching.** Click a stream, or press `1`–`9`, to cut to it.
- **Auto-reconnect.** Keeps retrying while a stream is offline and reconnects
  if it drops or stalls, so audio starts by itself when the performer goes
  live.
- **Buffer readout and resync.** Shows how much audio is queued (the part of
  the delay the player is responsible for). *Resync* drops it. *Auto catch-up*
  skips whatever queued up while connecting, plays 10% fast when the queue
  later stays above a threshold, and drops it outright if it gets very large.
- **Output device and volume**, remembered between runs.
- Per-stream connection type: RTSP over TCP (default), RTSP over UDP, FLV or
  MPEG-TS.

## Running it

### Windows

Unzip a release and run `vrcdn-lowlatency-player.exe`. libmpv is inside the
exe; the first run unpacks it to
`%LocalAppData%\vrcdn-lowlatency-player\libmpv`. WebView2 is required and
ships with Windows 10/11.

To use a different libmpv build, put its `libmpv-2.dll` next to the exe; it
takes priority over the built-in one.

### Linux

libmpv is a normal package dependency. On Ubuntu:

```
sudo apt install libmpv2 libwebkit2gtk-4.1-0
```

`LIBMPV_PATH=/path/to/libmpv` overrides where the library is loaded from on
any OS.

## Building

Requirements: Go 1.26+, Node 20+, and the Wails CLI:

```
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
```

### Windows release

```
powershell -ExecutionPolicy Bypass -File scripts\package.ps1
```

This downloads libmpv if needed, builds the exe with libmpv compiled in, and
writes `dist\vrcdn-lowlatency-player-<version>-windows-amd64.zip` holding the
exe and the licence files it must be distributed with.

The steps by hand:

```
go run -C tools/fetch-libmpv .          # libmpv-2.dll -> build/bin and internal/libmpv/bundled
wails build -tags bundled_libmpv        # build/bin/vrcdn-lowlatency-player.exe, about 57 MB
```

`tools/fetch-libmpv` takes the latest
[mpv Windows build](https://github.com/shinchiro/mpv-winbuild-cmake/releases);
`-tag 20261111` pins one. It also runs on Linux and macOS.

### Other builds

| Target | Command | Notes |
| --- | --- | --- |
| Windows, libmpv not built in | `wails build` | An 11 MB exe that needs `libmpv-2.dll` next to it. No C compiler needed. |
| Linux | `wails build -tags webkit2_41` | Needs `gcc`, `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`. Drop the tag on distros that still ship WebKitGTK 4.0. |

Binaries land in `build/bin`. Without `-tags bundled_libmpv`, libmpv is only
looked for at run time and is not needed to build.

## Developing

```
wails dev                      # the real app with live reload
cd frontend && npm run dev     # UI only, in a browser, against a mock backend
go test ./internal/...         # backend tests
go test -tags bundled_libmpv ./internal/...   # ...including the ones that play through the real libmpv
cd frontend && npm run check   # type-check the UI
```

The browser mock takes `?demo=playing|catchup|reconnecting|failed|idle|empty|noengine`.

`cmd/probe` drives the player without the GUI, which is the quickest way to
check libmpv and a stream:

```
go run ./cmd/probe                         # is libmpv found? list audio devices
go run ./cmd/probe some_stream_name        # play it, printing state changes
go run ./cmd/probe -transport flv some_stream_name
go run ./cmd/probe -for 30s -volume 0 some_stream_name   # a silent 30-second check
```

The tests that use the real libmpv play a local, endless WAV stream to a null
audio output, so they are silent and need no stream to be live. They are
skipped when libmpv cannot be loaded.

## How it is put together

```
main.go, app.go        Wails window and the API the UI calls
internal/vrcdn         stream name / URL parsing, URL per connection type
internal/store         saved streams and settings (JSON in the user config dir)
internal/player        Controller: reconnect, stall detection, catch-up policy
                       MPVEngine: the libmpv-backed implementation of Engine
internal/libmpv        cgo-free binding to the libmpv client API (purego),
                       and unpacking of the built-in copy
cmd/probe              headless test tool
tools/fetch-libmpv     downloads libmpv for Windows (its own Go module)
scripts/package.ps1    builds the Windows release zip
frontend/              Svelte 5 + TypeScript UI
```

The `Controller` only knows the `player.Engine` interface, so the policy is
tested against a fake engine and the media backend can be replaced without
touching it.

### Where the low latency comes from

`internal/player/mpv.go` configures libmpv with the explicit form of mpv's
`low-latency` profile, audio only:

- no video, no cache, `cache-pause=no`, `audio-buffer=0`
- `demuxer-lavf-o=fflags=+nobuffer`, minimal stream probing
- RTSP over TCP by default
- the demuxer reads everything the network has delivered
  (`demuxer-readahead-secs`), so the buffer readout shows the whole backlog
  and a resync clears all of it

On top of that the `Controller` starts each stream at its live edge: audio
that queued up while connecting (HTTP-FLV opens with most of a second of it)
is dropped instead of played.

What the player cannot remove is the delay before the audio reaches it
(encoder, VRCDN ingest and edge) and the audio device's own buffer.

### Settings file

`%AppData%\vrcdn-lowlatency-player\config.json` on Windows,
`~/.config/vrcdn-lowlatency-player/config.json` on Linux.

## Licence

GPL-3.0-or-later; see `LICENSE`. The Windows release contains libmpv and
FFmpeg, which are GPL; `THIRD-PARTY-NOTICES.md` lists what is inside and
where its source is.
