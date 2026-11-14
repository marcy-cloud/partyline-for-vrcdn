# What PartyLine is for

Context from Marcy (DJ), November 2026. Read this before deciding what a
feature should do.

## The setting

DJs and VJs perform together in VR (VRChat). The audio and the visuals reach
the audience by different routes, and people in the middle of that chain need
the DJ's audio early.

## The pipeline

```
DJ ──audio──▶ VRCDN (DJ's stream) ──▶ VJ ──▶ visualiser ──▶ VRCDN (VJ's own stream) ──▶ audience
                                  └─▶ dancers (listen to the same pre-feed)
```

1. **The DJ sends audio over VRCDN**, on a stream meant only for the VJ and
   the dancers. This is the "pre-feed". The audience does not listen to it.
2. **The VJ (video jockey) takes that audio** into visualiser software such
   as Resolume or Synesthesia, which makes visuals that react to the music.
3. **The VJ streams the result, video with the audio, over their own
   separate VRCDN stream.** That is what the audience sees and hears.
4. **Dancers who may appear in the visuals listen to the same pre-feed**, so
   their dancing is in time with the music when the VJ picks them up.

## What PartyLine solves

Only the first link: getting the DJ's audio to the VJ and the dancers as fast
as possible. Everything after the VJ's visualiser is out of scope.

## What follows from that

- **Latency is the product.** Every design choice should favour the shortest
  delay from the DJ to the listener. A glitch is better than a delay that
  creeps up, which is why the player drops or speeds through queued audio.
  - From a show, 2026-11-14: a DJ's connection kept dropping out for a few
    seconds at a time. Each time, the audio that had queued up arrived in a
    rush and the player played it 10% fast to catch up, for many seconds:
    tempo off, and still behind. The listener had to press Resync by hand.
    Reproduced: a 2 second dropout cost 11 seconds of fast playback. Playing
    fast is now only for being a little behind (about half a second at
    most); a backlog after a dropout is skipped at once, and skipping goes
    on until the stream has been steady for a second. The same 2 second
    dropout is back at live in 0.3 seconds. (`recovery_live_test.go`)
- **The listeners are performers, not an audience.** VJs and dancers are
  busy mid-show. Switching streams between DJs must be one click or one key,
  and the app must recover by itself when a stream drops or a DJ goes live.
- **Audio only.** The DJ's stream may carry video, but nobody on this link
  needs it. The player never decodes it.
- **The VJ feeds the audio into other software.** Choosing the output device
  matters (a virtual cable into Resolume or Synesthesia is typical), and the
  level meter shows that signal is arriving before the volume control.
- **Dancers are less technical users.** The app should work from a single
  download with nothing to install or configure beyond a stream name.
- **Sync with the audience is the VJ's job, not ours.** The audience hears
  the VJ's stream, later. PartyLine should never add delay to "line things
  up".

## Ideas this context makes sensible

- **A sender** for the DJ: push audio only to VRCDN without OBS, with
  known-good settings. Built as the Send tab; see [sender.md](sender.md).
- **VRCDN private streams** are what VRCDN itself recommends for this DJ to
  VJ link (see [vrcdn.md](vrcdn.md)). PartyLine treats a private key like any
  other stream name today.
