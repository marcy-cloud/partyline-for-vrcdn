# Third-party software

PartyLine for VRCDN is licensed under the GNU General Public License,
version 3 or (at your option) any later version; see `LICENSE`.

PartyLine is based on
[VRCDN-LowLatency-Player](https://github.com/jazzy348/VRCDN-LowLatency-Player/tree/main)
by jazzy348 and was rebuilt from scratch by Marcy.cloud. It shares no code
with the original.

Release builds for Windows contain the software below. The licence texts of
the Go modules, Svelte and the fonts are in the `licenses` folder of a release.

## libmpv (inside the Windows exe)

The Windows exe contains an unmodified `libmpv-2.dll`, which it unpacks to
`%LocalAppData%\partyline\libmpv` on first run. The DLL is
taken from the
[mpv-winbuild-cmake](https://github.com/shinchiro/mpv-winbuild-cmake/releases)
builds (the `mpv-dev-x86_64` archive). The exact build in a given release is
named at the end of that release's copy of this file.

- **mpv** — GPL-2.0-or-later. Source: <https://github.com/mpv-player/mpv>.
  The commit is the `git-…` part of the archive name.
- **FFmpeg**, statically linked into the DLL, configured with `--enable-gpl
  --enable-version3`, which makes the combined library GPL-3.0-or-later.
  Source: <https://git.ffmpeg.org/ffmpeg.git>.
- The other libraries linked into the DLL (x264, x265, rubberband, libass,
  libplacebo, OpenSSL and more) and the exact versions used are set by the
  build scripts: <https://github.com/shinchiro/mpv-winbuild-cmake>, at the
  tag matching the release date in the archive name.

To use a different libmpv, put its `libmpv-2.dll` next to the exe or set
`LIBMPV_PATH`; either takes priority over the built-in copy.

## Go modules

| Module | Licence |
| --- | --- |
| github.com/wailsapp/wails/v3 | MIT |
| github.com/ebitengine/purego | Apache-2.0 |
| github.com/go-ole/go-ole | MIT |
| golang.org/x/sys | BSD-3-Clause |
| Go standard library and runtime | BSD-3-Clause |

## User interface

| Package | Licence |
| --- | --- |
| Svelte (runtime compiled into the UI) | MIT |
| @wailsio/runtime (part of Wails) | MIT |
| Fredoka, Nunito and VT323 fonts (via Fontsource) | OFL-1.1 |
