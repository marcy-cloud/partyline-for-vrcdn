# VRCDN: what the service can do

Notes taken from the VRCDN wiki on 2026-11-11, so we don't have to re-read
it. Start page: <https://wiki.vrcdn.live/en/stream/Getting-Started>. The wiki
is rendered by JavaScript, so a plain HTTP fetch returns only the page title;
use a real browser to re-read it.

Everything under "From the wiki" is what the wiki says. "Not in the wiki"
lists what it is silent on, and "From our own testing" is what we measured.

## From the wiki

### Accounts and stream types

- Streaming needs a Patreon pledge of at least the "VRCDN 40" tier. The
  control panel is <https://panel.vrcdn.live/>.
- **Live**: the account's main stream. Up to 3 forwarders (restreaming to
  Twitch, YouTube and so on).
- **Guest**: extra stream keys that forward to the account's live stream URL,
  for events with several broadcasters. Only one guest key can be live at a
  time, and while one is live the live key cannot be used. A guest can have 1
  forwarder.
- **Private**: "Can be used to create a low latency link between a DJ, VJ,
  LD, or other parts of a stream pipeline." Its URL can be randomised before
  streaming. The stream key and the URL are identical, so it must not be
  shared publicly. Extra private keys cost 1 GBP a month each, by support
  ticket.
- Viewer limits (concurrent viewers, "CCU") are per account, combine the live
  and private streams, and depend on the Patreon tier. Only viewers pulling
  directly from VRCDN count. The wiki gives no numbers.

### DJ to VJ relay (the case PartyLine serves)

Page: <https://wiki.vrcdn.live/en/stream/relay>

- VRCDN's recommended setup: the DJ streams to the account's **private key**
  with normal settings; the VJ pulls it into OBS.
- The receiver URL they document is `rtmp://stream.vrcdn.live/live/[PrivateKey]`,
  played with OBS's "VLC Video Source" (needs 64-bit VLC installed) or, less
  reliably, "Media Source".
- For multi-performer events, performers stream to a private key and the
  operator brings it into the main OBS, so the public URL stays stable.

### Sending (ingest)

- Protocol: RTMP. OBS has a built-in service called "VRCDN - Live".
- Default ingest host: `ingest.vrcdn.live`. Regional ingests, all
  `rtmp://<region>.ingest.vrcdn.live/live`:

  | Region | Host prefix |
  | --- | --- |
  | London | `uk` |
  | Frankfurt | `de` |
  | North America, north east (Toronto) | `use` |
  | North America, central (Dallas) | `usc` |
  | North America, north west (Fremont) | `usw` |
  | São Paulo | `br` |
  | East Japan | `jpe` |
  | West Japan | `jpw` |
  | Sydney | `au` |

- SRT ingest is mentioned as "if available on your plan and setup": more
  resilient than RTMP on an unstable upload, slightly more latency. No URL is
  given.
- If a stream is stuck as connected, press Disconnect in the control panel.
  Regenerate a key that has been exposed.

### Encoder settings

- Video: H.264 only (NVENC, AMD, Apple VT or x264). Recommended 3,500 kbps,
  CBR. **Keyframe interval 1 second is required.** B-frames 0. Do not use the
  x264 "zerolatency" tune or NVENC "Ultra Low Latency" tuning; AVPro mishandles
  them. 30 fps and 1280x720 are suggested when streams drift out of sync.
- Audio: **48 kHz**; other sample rates are "not supported by most VR
  platforms". The sample config uses **320 kbps**.
- **Limits: 6,000 kbps video and 320 kbps audio.** The limit is on the
  combined bitrate, with some headroom. Going over shows a warning for 5
  minutes and then the stream is disconnected until Disconnect is pressed.
  Bitrate flags are not strikes and expire after 24 hours. A temporary higher
  limit can be bought by support ticket.
- Audio level: peak near 0 dB without clipping; about -5 dB peak is the
  suggested target. VRCDN does not modify the audio.
- "If using CoreAudio AAC, try switching back to FFmpeg AAC. CoreAudio may
  sound slightly better in some cases, but it may be less stable."

### Playing

- **RTSP is the lowest-latency output, PC only.** VRChat writes it
  `rtspt://stream.vrcdn.live/live/<name>`; other players drop the extra `t`:
  `rtsp://stream.vrcdn.live/live/<name>`. It uses port 554, which some ISPs,
  routers and firewalls block or throttle.
- **MPEG-TS** is for standalone headsets (Quest, Pico) and mixed instances,
  since RTSP is not supported on most standalone platforms.
- **RTMP** playback exists: `rtmp://stream.vrcdn.live/live/<name>`. The wiki
  uses it as the comparison test when RTSP fails.
- MP4 is named once as an output; the control panel lists the full set.
- **Public demo stream: `vrcdn`**, for example
  `rtsp://stream.vrcdn.live/live/vrcdn`. Useful as a test stream that is not
  anyone's own.
- In VRChat the world's video player must use AVPro with "Use Low Latency"
  enabled, which only the world's creator can set.
- Low-latency playback has little client-side buffering, so jitter, Wi-Fi and
  powerline networking cause stutter that ordinary streaming hides.
- Latency test: a millisecond clock at <https://panel.vrcdn.live/tools/clock>
  to put in the stream and compare against playback.
- Test world: <https://vrchat.com/home/world/wrld_a52637bc-159c-43b6-9e1a-5a384d927190>.

### Rules

Page: <https://wiki.vrcdn.live/en/stream/NetworkRules>

- Stay under 6,000 kbps (with "wiggle room").
- Only stream content you have the right to broadcast; nothing illegal,
  malicious or pornographic.
- "Do not abuse our streaming network, API, website, or any service or
  product from us."
- Three strikes policy exists; details are not given.

### Other

- File hosting: uploads of video, audio, images and strings, re-encoded to
  H.264 and AAC, with aliases for stable URLs. Limits depend on tier.
- Known issues: some routers (Liberty Global, HUMAX, ARRIS; Eero DNS
  blocking) have trouble with VRCDN; OBS hardware encoders can overshoot the
  bitrate.
- Support: <https://support.vrcdn.live/>, support@vrcdn.live, Discord
  <https://discord.gg/vrcdn>.

## Not in the wiki

- **Audio-only streams.** The wiki says nothing, but they work; see "From
  our own testing".
- **Audio codecs.** AAC is implied by the OBS settings; nothing is said about
  Opus or MP3.
- **Stated latency figures** for any output. We measured our own: see
  [latency.md](latency.md).
- **An HTTP API.** The rules mention one, but no endpoints are documented.
  (The original Electron player polled a viewer-count API.)
- **Viewer numbers per tier**, and any stream duration limit.
- **RTSP over UDP.** The wiki only discusses RTSP on port 554.
- **Third-party players.** Not addressed beyond the "do not abuse" rule.

## From our own testing

On one stream, from one Windows PC, on 2026-11-10 and 2026-11-11:

- RTSP over UDP, RTSP over TCP, HTTP-FLV
  (`https://stream.vrcdn.live/live/<name>.live.flv`) and MPEG-TS
  (`.live.ts`) all play through libmpv.
- Queued audio in the player while playing: 0–21 ms over RTSP, 20–40 ms over
  MPEG-TS, and about 800 ms at start over FLV until dropped. This is the
  player's own queue, not the delay from the DJ.
- The delay from the DJ, measured on 2026-11-13: about 80-105 ms from
  PartyLine's sender to the listener's decoder, of which VRCDN and the
  network are 36-57 ms. Details and caveats in [latency.md](latency.md).
- An offline or nonexistent stream answers 401 on the HTTP outputs, after
  about 4 seconds (404 has also been seen). Over
  RTSP it does not answer at all, so the player cannot tell "offline" from
  "RTSP is blocked" by RTSP alone.
- The `vrcdn` demo stream plays over MPEG-TS, FLV and RTSP.
- The UDP to TCP fallback has now been seen for real: on a network that let
  RTSP through over TCP but not UDP, the demo stream started over TCP after
  the 4-second wait.
- **Whether RTSP works depends on the listener's network** (one PC, three
  networks, 2026-11-11):

  | Network | RTSP over UDP | RTSP over TCP | MPEG-TS / FLV |
  | --- | --- | --- | --- |
  | Coffee-shop Wi-Fi, Tailscale on | no response | no response | plays |
  | Home, Tailscale on | no response | plays | plays |
  | Home, Tailscale off | plays | plays | plays |

  On the blocked networks port 554 still accepted connections; nothing came
  back. So "no response over RTSP" usually means the network, not VRCDN.
- The UDP to TCP fallback has now been seen for real: on a network that let
  RTSP through over TCP but not UDP, the demo stream started over TCP after
  the 4-second wait.
- **RTSP can stop answering while the stream is up.** On 2026-11-11 (around
  16:40 local) a live stream that had played over RTSP hours earlier, with
  the same build, got no answer over RTSP on UDP or TCP, while MPEG-TS and
  FLV played it normally. Port 554 accepted connections on all four edge
  addresses. The PC was on coffee-shop Wi-Fi with Tailscale running at the
  time, and RTSP had worked earlier from another network, so the local
  network is the likely cause (public Wi-Fi often filters RTSP). Not
  confirmed. It matches the wiki's "RTSPT doesn't work, but MPEG-TS does".
- **VRCDN accepts audio-only streams** (AAC in FLV over RTMP, no video
  track), and serves them on RTSP, FLV and MPEG-TS, about 1 second after
  sending starts. A wrong key is refused with a 403, "Invalid stream key or
  user disabled". See [sender.md](sender.md).

## What this means for PartyLine

- **Private streams are the intended source.** A private key is both the key
  and the stream name, so it is a secret. A stream saved as "Private" has
  its name encrypted in the settings file (as the sender's key is) and
  masked as ••• wherever the player would show it.
- **A sender has clear targets**: RTMP to `ingest.vrcdn.live` or a regional
  ingest, AAC at 48 kHz, 320 kbps or less, and no video needed.
- **RTSP on port 554 being blocked is common enough to be in the wiki.** Our
  fallback goes UDP then TCP, both RTSP. A network that blocks RTSP entirely
  needs FLV or MPEG-TS, which the user has to pick by hand today.
- **The `vrcdn` demo stream** stands in for a live stream in manual tests.
  See [testing.md](testing.md).
- **Offline is detected over HTTP** (built 2026-11-11). Beside every
  connection attempt the player requests the stream's `.live.ts` URL. A 401
  or 404 means offline. VRCDN takes about 4 seconds to give that answer and
  about 1 second to start a live stream, so the first fallback waits for it.
- **UDP gets two tries before TCP** (2026-11-14). On a network where UDP
  works, a start over it now and then produced no audio within the
  4 seconds allowed, and the player then used TCP for 10 minutes. Timed on
  the demo stream: 30 UDP starts took 2.4 to 3.1 seconds (TCP the same), so
  the margin is thin, and one start in about 45 that day got nothing in
  4 seconds. Why that one was slow is not known; two brand-new copies of
  the exe did not reproduce it. A network already found to drop UDP gets
  one try when its 10 minutes are up, not two.
- **The default setting falls back to MPEG-TS** when RTSP gets no response
  on UDP or TCP. This is run-time only; the saved connection is not changed.
