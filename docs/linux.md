# Linux (Ubuntu): what a port would take

Written 2026-11-14 as a desk investigation. **The Linux build has never
been tried.** The details about PulseAudio and PipeWire are from memory and
need confirming. Start here when Linux work begins, then correct this file.
A first build can be tried from WSL (Ubuntu 22.04) on the main development
PC.

See [macos.md](macos.md) for the same exercise on the Mac.

## What should carry over as it is

The listening side is mostly platform-neutral Go and Svelte: the player
controller (reconnect, UDP/TCP/MPEG-TS fallback, catch-up), the level
meter, the stream list with export, import and reordering, and settings in
the user's config folder. Wails v3 targets Linux (WebKitGTK), and
`build/linux` holds its generated packaging files, never used.

Code that already has a non-Windows path, never run:

- `internal/libmpv/load_unix.go`: finds a system libmpv.
- `internal/pcmpipe/pipe_other.go`: a FIFO in place of the named pipe.
- `internal/sender/proc_other.go`, `coreaudio_other.go`,
  `internal/loopback/loopback_other.go`, `internal/secret/secret_other.go`:
  stubs that say "not available".

## What is Windows-only and needs a Linux answer

| Piece | Windows | On Ubuntu |
|---|---|---|
| Player library | Compiled into the exe (`bundled_libmpv`) | The system package (`libmpv2`, `libmpv1` on older releases) as a dependency |
| Encrypted keys | DPAPI (`internal/secret`) | Secret Service over D-Bus (GNOME Keyring, KWallet) |
| Sender inputs | DirectShow (`av://dshow:`) | PulseAudio sources (`av://pulse:`), which PipeWire also provides. The bundled Windows engine has the pulse input compiled in; the distribution's libmpv must be checked for it |
| CoreAudio AAC | Found in an iTunes install | Not available. FFmpeg AAC only; the Encoder choice already hides itself |
| Touch dragging | `app-region` (`html.native-drag`) | Wails's own mouse dragging; touch not expected to matter |
| Packaging | Zip with one exe (`scripts/package.ps1`) | AppImage or `.deb` (`build/linux/nfpm`) |

Two of these gate the rest:

- **No key store, no sender.** Private streams and send targets cannot be
  saved without `secret.Protect`, and the UI hides them when
  `secret.Supported()` is false. A machine with no keyring running (a bare
  window manager, a server) needs a decision: refuse, or store keys
  unprotected and say so.
- **`sender.Supported()` is Windows only**, so the Send tab is hidden.

## Things specific to Linux

- **The distribution's libmpv decides what works.** Its version and the
  FFmpeg it was built against vary by release. Ubuntu 22.04 ships an older
  mpv than the one bundled on Windows; options the player sets that it
  doesn't know are already ignored at start-up, but behaviour may differ.
- **The frameless, see-through window** with a rounded border depends on
  the compositor. On X11 without one, or some Wayland setups, the corners
  may be black or the window may not be see-through at all.
- **WebKitGTK** is a different browser engine from WebView2. The CSS is
  plain, but nothing has been looked at in it.
- **RTSP over UDP and local firewalls** (`ufw`): untested.

## Sending one program's sound

On the `program-audio` branch for Windows (WASAPI process loopback; see
`docs/sender.md` on that branch). Linux should be the easy platform for
this:

- Every program's playback is already a separate stream ("sink input")
  that can be listed by application name and recorded by itself, without
  rerouting it, so the user still hears it.
- The standard tools do both: `pactl list sink-inputs` to list, `parec
  --monitor-stream=<index>` to record one. They work on PulseAudio (Ubuntu
  22.04) and on PipeWire through its PulseAudio layer (22.10 and later).
- So the port is: run those tools, name the input `app:<application>` as
  on Windows, and pipe the raw audio into `sender.PCMInput`, which exists
  for this. No permission prompts.
- **Everything the PC plays** is simpler still: every output has a
  "monitor" source, which is an ordinary PulseAudio source, so it should
  appear among the sender's inputs with no new code once those are listed.

## Rough size

- **Listening only:** small. A first build, packaging, and fixing what the
  first run turns up.
- **Everything:** medium. Add the key store, sender inputs and program
  capture.
