# macOS: what a port would take

Written 2026-11-14 as a desk investigation. **Nothing here has been built or
run on a Mac**, and there is no Mac to test on from the main development PC.
The API details are from memory and need confirming before anyone commits
to them. Start here when Mac work begins, then correct this file.

See [linux.md](linux.md) for the same exercise on Ubuntu.

## What should carry over as it is

The listening side is mostly platform-neutral Go and Svelte: the player
controller (reconnect, UDP/TCP/MPEG-TS fallback, catch-up), the level
meter, the stream list with export, import and reordering, and settings in
the user's config folder. Wails v3 targets macOS (WebKit), and `build/darwin`
holds its generated project files, never used.

Code that already has a non-Windows path, never run:

- `internal/libmpv/load_unix.go`: finds a system libmpv.
- `internal/pcmpipe/pipe_other.go`: a FIFO in place of the named pipe.
- `internal/sender/proc_other.go`, `coreaudio_other.go`,
  `internal/loopback/loopback_other.go`, `internal/secret/secret_other.go`:
  stubs that say "not available".

## What is Windows-only and needs a Mac answer

| Piece | Windows | On a Mac |
|---|---|---|
| Player library | Compiled into the exe (`bundled_libmpv`) | Bundle `libmpv.dylib` and everything it links to inside the `.app`, or require `brew install mpv` |
| Encrypted keys | DPAPI (`internal/secret`) | Keychain |
| Sender inputs | DirectShow (`av://dshow:`) | AVFoundation (`av://avfoundation:`), device list from its log lines the way DirectShow's is |
| CoreAudio AAC | Found in an iTunes install, called through `syscall` | Part of the OS (AudioToolbox). Either call the same AudioConverter API, or use FFmpeg's `aac_at` encoder if the libmpv build has it, which would need none of `coreaudio_windows.go` |
| Touch dragging | `app-region` (`html.native-drag`) | Not needed; Wails's own mouse dragging |
| Packaging | Zip with one exe (`scripts/package.ps1`) | `.app`, signed and notarised |

Two of these gate the rest:

- **No key store, no sender.** Private streams and send targets cannot be
  saved without `secret.Protect`, and the UI hides them when
  `secret.Supported()` is false.
- **`sender.Supported()` is Windows only**, so the Send tab is hidden.

## Things specific to the Mac

- **Signing.** An unsigned app is blocked by Gatekeeper. Signing and
  notarising need a paid Apple Developer account. Every bundled dylib has
  to be signed too.
- **Permissions.** Capturing from an input device shows the microphone
  prompt and needs `NSMicrophoneUsageDescription` in `Info.plist`. The
  sender runs in a helper process (the app started again with
  `--send-worker`); whether the prompt and the grant attach to the helper
  the same way needs checking.
- **Two chip families.** Apple Silicon and Intel: a universal build, or
  two.
- **The frameless, see-through window** with its own buttons: untested on
  WebKit. The traffic-light buttons are on the left on a Mac; ours are on
  the right.
- **Bundling libmpv.** A Homebrew mpv links dozens of dylibs with absolute
  paths; they have to be copied in and their paths rewritten. This is
  likely the fiddliest part of a listen-only port.

## Sending one program's sound

On the `program-audio` branch for Windows (WASAPI process loopback; see
`docs/sender.md` on that branch). The Mac has no equivalent that is both
old and simple:

- **ScreenCaptureKit, macOS 13 and later.** Captures one application's
  audio and lists running applications. It is a Swift/Objective-C API with
  delegates, so in practice a small Swift helper shipped in the app,
  writing raw PCM to a pipe: the shape `sender/app.go` already expects.
  Needs the Screen Recording permission, which is a lot to ask of a DJ who
  only wants to send audio.
- **Core Audio process taps, macOS 14.4 and later.** A tap on one
  process's output, with a milder audio-capture permission
  (`NSAudioCaptureUsageDescription`). A C API, so perhaps callable without
  a helper, but involved (a tap plus an aggregate device), and it leaves
  out older systems.
- **Before macOS 13:** not possible without a virtual audio device such as
  BlackHole, which the user installs and routes to by hand. That already
  works as an ordinary input device.

## Rough size

- **Listening only:** medium. The work is bundling the player library,
  signing, and whatever the first run turns up.
- **Everything:** large. Add the key store, sender inputs with their
  permission, CoreAudio, and the program-capture helper.
