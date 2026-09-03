# Video Conversion Research Notes

Working notes for the iPod/Rockbox video conversion pipeline referenced in
`DESIGN.md` §6.5. This is raw research and open questions, not settled design —
the settled decisions that came out of it (no metadata embedding,
error-on-unsupported-target, MPEG-2/MPEG-PS as the real target format) live in
`DESIGN.md` itself. This file exists so the sources, exact commands, and the
places they disagree with each other aren't lost between now and whenever real
on-device testing happens.

**Target device:** iPod Video 5.5G, LCD 320x240.

## Sources

1. Rockbox's `MPEGplayer` wiki page:
   <https://www.rockbox.org/wiki/PluginMpegplayer>
2. A Reddit thread (r/IpodClassic) of someone doing real hardware testing on an
   iPod Classic (iFlash Quad SSD mod, not the same device as ours, but the same
   MPEGplayer software decoder):
   <https://www.reddit.com/r/IpodClassic/comments/15q7ia6/>
3. WinFF's bundled presets (`I5GFS`/`I5GWS`, "RB Apple iPod Video
   Fullscreen"/"Widescreen") — the only presets in WinFF actually targeting this
   exact device. WinFF itself hasn't been updated in years, so treat its numbers
   as a starting point to verify, not ground truth.

## What's confirmed, consistent across all three sources

- **Codec/container is not negotiable:** MPEGplayer decodes MPEG-1/MPEG-2 video
  only, entirely in software on the main CPU — no hardware video decoding is
  used even on devices (like ours) whose original Apple firmware has it. No
  modern codec (H.264, VP9, etc. — what `yt-dlp` sources actually are) is
  supported. Real target: `mpeg2video` + MP3 audio (`libmp3lame`), 44.1kHz,
  muxed as an MPEG Program Stream, `.mpg` extension.
- **No evidence of metadata support anywhere.** The wiki describes MPEGplayer
  purely as a file-browser "viewer plugin," launched by selecting a file
  (recognized by extension: `.mpg`/`.mpeg`/`.mpv`/`.m2v`, or via "Open With..."
  otherwise) — nothing about title/artist display anywhere on the page. Nothing
  in the Reddit thread mentions it either. Basis for `DESIGN.md`'s "no metadata
  embedding, filename only" decision.
- **Match output framerate to source, don't force a fixed value.** The Reddit
  tester explicitly moved off 30fps once he noticed `ffmpeg` was just
  duplicating frames to hit it from a 23.9fps source.
- **Skip deinterlacing for our pipeline.** The Reddit thread's `-deinterlace`/
  `bwdif` discussion is entirely about interlaced sources (broadcast TV, DVD
  rips). Our source pipeline is exclusively `yt-dlp`/YouTube, which is
  essentially always already progressive — applying a deinterlace filter to
  progressive content is unnecessary work at best, and one commenter found their
  test file looked _better_ with it removed entirely.

## Where the sources disagree — needs real on-device testing to resolve

- **Bitrate:** WinFF's `I5GFS`/`I5GWS` presets use `-b:v 400k` / `-b:a 128k`.
  The Reddit tester's real-world testing (§ below) found `4096k` video / `320k`
  audio was needed for full quality with zero frame drops — roughly **10x**
  WinFF's numbers. WinFF hasn't been updated in years, so its preset may just be
  stale/conservative, but that's a guess either way — not confirmed.
- **Fullscreen (4:3) resolution:** WinFF's `I5GFS` preset (built for our exact
  device) targets full `320x240`. But the Reddit tester found real frame-drop
  problems at full `320x240` 4:3 — down to 11fps in his testing — even at
  bitrates _below_ WinFF's already-low number, and only recovered full framerate
  by deliberately shrinking to `280x210` with black borders. This was on an iPod
  Classic, not our 5.5G — related hardware/software (same MPEGplayer decoder)
  but not identical, so it's unconfirmed whether the same cliff applies to our
  device. The wiki's performance table shows a real fps drop for 4:3 content
  across every device it lists too (e.g. its iPod Video row: 37fps for 16:9 vs.
  14fps for the 1:1 column is blank, but 4:3 generally scores much lower than
  16:9 across the whole table) — consistent with there being a real effect, just
  not with an exact number for our device.
- **Widescreen resolution:** wiki and Reddit both suggest `320x180`; WinFF's
  `I5GWS` preset (again, built for our exact device) uses `320x176` instead.
  `176` is cleanly divisible by MPEG-2's 16px macroblock size where `180` isn't,
  so there may be a real encoder-correctness reason to prefer WinFF's number
  here, independent of the frame-drop question above.

## Reddit thread: concrete data points

Real hardware testing (iPod Classic, iFlash Quad), widescreen 16:9 cartoon
source, varying video bitrate to find the ceiling:

| Video bitrate | Output data rate (measured) | File size | Notes                                                            |
| ------------- | --------------------------- | --------- | ---------------------------------------------------------------- |
| 1024k         | 1018kbps                    | 207MB     | visible artifacts                                                |
| 2048k         | 1241kbps                    | 242MB     | clear jump in quality over 1024k                                 |
| 4096k         | 1275kbps                    | 247MB     | small further improvement over 2048k                             |
| 8192k         | 1275kbps                    | 247MB     | identical output to 4096k — flatlines here, while costing 1-2fps |

Audio: 320kbps MP3, downmixed from 32-bit/48kHz source, "sounds great."

4:3 resolution testing (same tester, same device), holding bitrate at 4096k/320k
and framerate at 23.9fps (the source's actual framerate):

| Resolution                                            | Result                                 |
| ----------------------------------------------------- | -------------------------------------- |
| 320x240 (full 4:3)                                    | ~11fps, unusable                       |
| 300x225                                               | 18fps, still dropping                  |
| 280x210                                               | stable 23.9fps, no drops — landed here |
| 240x180 (not shown in his post, mentioned in passing) | stable, but very small screen area     |

His conclusion: decode cost tracks total pixel/macroblock count much more than
aspect ratio or "fits the screen" — 16:9 at 320x180 (fewer total pixels) had
zero problems, while 4:3 at the nominally-similar 320x240 fell over badly. This
is a steep, non-linear cliff between 300x225 and 280x210 in his testing, not a
gentle slope.

### His working `ffmpeg` commands

16:9 widescreen:

```
ffmpeg -i vidin.ext -deinterlace -vcodec mpeg2video -ac 2 -ar 44100 -acodec mp3 -r "FPS" -s 320x180 -vb 4096k -ab 320k -f mpeg vidout.mpg
```

4:3 (with black border for performance):

```
ffmpeg -i vidin.ext -deinterlace -vcodec mpeg2video -ac 2 -ar 44100 -acodec mp3 -r "FPS" -s 280x210 -vb 4096k -ab 320k -f mpeg vidout.mpg
```

Widescreen source cropped to 4:3 (with black border):

```
ffmpeg -i vidin.ext -filter:v "crop=ih/3*4:ih" -deinterlace -vcodec mpeg2video -ac 2 -ar 44100 -acodec mp3 -r "FPS" -s 280x210 -vb 4096k -ab 320k -f mpeg vidout.mpg
```

(Drop `-deinterlace` per our decision above — it's not applicable to our
progressive-source pipeline, and current `ffmpeg` versions have removed the flag
anyway, per the comment thread — `bwdif=mode=send_field:parity=auto:deint=all`
is the modern replacement if it's ever needed.)

## WinFF presets (`I5GFS`/`I5GWS`, "iPod 5th Gen")

```xml
<I5GFS>
  <label>RB Apple iPod Video Fullscreen</label>
  <params>-acodec libmp3lame -b:a 128k -ar 44100 -vcodec mpeg2video -vf scale=320:240 -b:v 400k -strict -1</params>
  <extension>mpg</extension>
  <category>Rockbox</category>
</I5GFS>
<I5GWS>
  <label>RB Apple iPod Video Widescreen</label>
  <params>-acodec libmp3lame -b:a 128k -ar 44100 -vcodec mpeg2video -vf scale=320:176 -b:v 400k -strict -1</params>
  <extension>mpg</extension>
  <category>Rockbox</category>
</I5GWS>
```

## Testing plan for when real conversion happens

1. Start from WinFF's device-targeted values (`400k`/`128k`, `320x240`
   fullscreen / `320x176` widescreen) as a first real-hardware baseline, since
   they're the only numbers actually built for this exact device.
2. If fullscreen 4:3 drops frames the way it did in the Reddit testing, fall
   back toward the Reddit-tested `280x210` approach (bumping bitrate up toward
   `4096k`/`320k` at the same time, since that pairing is what actually held
   steady in his testing — the two variables were only ever tested together, not
   independently).
3. Confirm whether `176` vs. `180` (widescreen height) actually matters in
   practice on this device, or whether `180` decodes and displays fine despite
   not being macroblock-aligned.
4. Once real numbers are confirmed, fold them back into `DESIGN.md` §6.5 as
   settled values, and this file's resolution/bitrate open questions can be
   marked resolved (or this file retired entirely if nothing else remains open).
