# Spotting fake lossless files: `mrr spectrum`

`mrr spectrum` checks whether a FLAC (or ALAC) file is really lossless, or
whether it is a lossy file (an MP3, an AAC, ...) that someone converted to FLAC
afterwards. Converting to FLAC makes a file bigger and look better, but it
cannot put back what the lossy encoder threw away. This command looks for the
traces of what was thrown away.

This document explains how it works from the ground up, assuming no background
in audio signal processing, and records why each choice was made. If you only
want to use the command, the next section is all you need.

- [Using it](#using-it)
- [The idea in one page](#the-idea-in-one-page)
- [Background: spectra, decibels and sample rates](#background-spectra-decibels-and-sample-rates)
- [How the analysis works, step by step](#how-the-analysis-works-step-by-step)
- [From measurements to a verdict](#from-measurements-to-a-verdict)
- [Other checks](#other-checks)
- [Choices and why](#choices-and-why)
- [What was measured](#what-was-measured)
- [What it cannot do](#what-it-cannot-do)
- [Tuning it](#tuning-it)
- [Reading a spectrogram by eye](#reading-a-spectrogram-by-eye)
- [Glossary](#glossary)

---

## Using it

```sh
mrr spectrum album/                  # every track in one album
mrr spectrum "01 track.flac"         # one track
mrr spectrum album/ --png out/       # also write a spectrogram image per track
mrr spectrum album/ --png out/ --annotated   # ...with the cutoff marked on it
mrr spectrum album/ --png out/ --annotated --fixed-scale   # ...same scale
mrr spectrum album/ -v               # show the numbers behind each verdict
```

It only works on one track or one album at a time, not an entire library. Expect
about a second or two per track. Files that are openly lossy (`.mp3`, AAC
`.m4a`) are skipped, since there is nothing to find out about them.

Example output:

```text
Analyzing album...

  album
  01 genuine.flac  44.1 kHz · 16-bit · stereo
    ✓ ok — full bandwidth; content reaches 22.1 kHz
    ↳ spectrogram: out/01 genuine.png
  02 from mp3.flac  44.1 kHz · 16-bit · stereo
    ✗ likely fake — content ends at about 16.1 kHz (level drops 43 dB across the edge), typical of a lossy encoder's lowpass
  03 from mp3 320.flac  44.1 kHz · 16-bit · stereo
    ⚠ suspect — content ends at about 20.2 kHz (level drops 43 dB across the edge); could be a lossy encode or a mastering filter

───────────────────────────────
3 tracks · 2 findings · 4.02s
```

Each track gets one of four verdicts:

| Verdict        | Meaning                                                                                                                       |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `ok`           | Nothing suggests it is anything but what it claims to be. This is **not proof**; see [What it cannot do](#what-it-cannot-do). |
| `suspect`      | It shows something lossy encoding causes, but a genuine lossless file can show it too. Look at the spectrogram.               |
| `likely fake`  | Strong evidence of a lossy transcode or an upsample.                                                                          |
| `inconclusive` | Too little audio to judge (a silent or very short track).                                                                     |

The command exits non-zero if any track is `suspect` or `likely fake`, like
`mrr check` does, so it can be used in scripts. If a track cannot be decoded at
all that is an error and also exits non-zero.

It's a read-only command. It doesn't change the album or any files; the only
thing it writes is the spectrogram images (into the directory you give with
`--png`).

---

## The idea in one page

A lossy encoder saves space by discarding sound that people are unlikely to
notice. The biggest and most reliable thing it discards is the very high
frequencies. A typical 128 kbps MP3 keeps nothing above about 16 kHz; it simply
cuts everything over that line. Different encoders and bitrates cut at different
frequencies, but nearly all of them cut somewhere.

Convert that MP3 to FLAC and you get a perfect, lossless copy of the
_already-damaged_ audio. The file is now several times bigger and says "FLAC",
but the high frequencies are still missing, because nothing can recreate them.
The cut is still there, and it is detectable.

A genuine lossless recording usually looks different. Real recordings tend to
have at least a little energy up near the top of what the format can hold,
fading out gradually. A fake has a wall: lots of content below a certain
frequency and, above it, nothing but the faint hiss of rounding error.

So the whole job is: **measure how much energy there is at each frequency, and
look for a wall.** Everything else in this document is detail about doing that
reliably.

A person does this by opening the file in an audio editor and looking at a
spectrogram (a picture of frequency against time). `mrr spectrum` does the same
measurement automatically, and can also draw the picture for the cases where you
want to judge by eye.

---

## Background: spectra, decibels and sample rates

You can skip this if you are comfortable with these ideas.

### Frequency and the spectrum

Sound is a mix of vibrations at different rates. The rate is the **frequency**,
measured in hertz (Hz, vibrations per second). A deep bass note is around 50-100
Hz; a voice sits around 100 Hz-4 kHz (1 kHz = 1000 Hz); cymbals and "air" reach
10-20 kHz; most adults cannot hear much above 16-18 kHz.

The **spectrum** of a piece of audio says how much energy it has at each
frequency. A **spectrogram** shows how that changes over time: time runs left to
right, frequency runs bottom to top, and brightness is loudness.

### Sample rate and the Nyquist frequency

Digital audio is a long list of measurements, taken `sample rate` times per
second: 44,100 for CDs (44.1 kHz), 48,000 (48 kHz) for DVDs and video, and
96,000 or 192,000 for "hi-res" audio.

A basic theorem says a given sample rate can only represent frequencies up to
**half** the rate. That limit is the **Nyquist frequency**: 22.05 kHz for CDs,
24 kHz for 48 kHz audio, 48 kHz for 96 kHz audio, and so on. The top of any
spectrum we draw is the Nyquist frequency.

### Decibels

Loudness spans an enormous range, so it is measured on a logarithmic scale in
**decibels (dB)**. Every 10 dB is a factor of 10 in power, and 20 dB is a factor
of 10 in amplitude. Differences are what matter, and the numbers here are
usually negative because they are measured against a reference.

This document uses **dBFS-style** levels: 0 dB is a full-scale sine wave (the
loudest steady tone the format can hold) and everything else is below that. A
level of -60 dB is a thousandth of the amplitude; -129 dB is vanishingly quiet.

### The noise floor of a file

A 16-bit file stores each measurement as a whole number between about -32,768
and 32,767. Rounding to a whole number adds a tiny error, which sounds like a
very faint, steady hiss: the **quantization noise floor**. Its level depends on
the bit depth (a 24-bit file's floor is far lower) and, as we will see, on how
finely the spectrum is sliced.

In our measurements, with 4096-sample windows, the floor of a 16-bit file sits
at about **-129 dB**. In a transcode, the region above the cutoff contains
_only_ this floor, because the encoder removed everything else. That is why the
wall in a fake file is so tall: music at -60 to -100 dB falling to a floor at
-129.

---

## How the analysis works, step by step

### 1. Decode

`ffmpeg` decodes the file to raw 32-bit floating point samples, piped straight
into the analysis. Everything after this point is ordinary Go code with no
dependency on ffmpeg.

It is decoded at the file's **own sample rate and channel count**. Resampling or
mixing down would reshape the spectrum, which is the thing actually being
measured. 32-bit floats can represent every whole number up to 24 bits exactly,
so no information about the original samples is lost (this matters for the
bit-depth check, below).

### 2. Cut into frames

The audio is cut into short, overlapping chunks called **frames**. We measure
the spectrum of each frame and average the results. Why not analyze the whole
track at once? Because we want the _typical_ spectrum, and averaging many short
measurements gives a smooth, stable result in which the structure of the file
stands out above the moment-to-moment noise of the music. This method (cut,
transform, average) is called **Welch's method**.

Frames overlap by half: frame 1 covers samples 0-4095, frame 2 covers 2048-6143,
frame 3 covers 4096-8191, and so on. The reason for the overlap is explained
under the window, below.

Frames that are nearly silent (below -75 dB RMS) are skipped. Digital silence,
the start of a fade-in and the tail of a fade-out would otherwise dilute the
average with frames that say nothing about the file. A channel needs at least 20
usable frames (about a second of audio) to be judged at all.

### 3. The FFT

The tool that turns a chunk of samples into a spectrum is the **Fast Fourier
Transform (FFT)**. You give it N samples and it returns N/2 + 1 numbers, the
energy in each of N/2 + 1 evenly spaced frequency **bins**. The bins are spaced
`sample rate / N` apart. With N = 4096 at 44.1 kHz, each bin is about 10.8 Hz
wide.

The classic fast version needs N to be a power of two (1024, 2048, 4096, ...),
which is one reason window sizes below are always powers of two. The FFT in this
tool is a short, self-contained Go implementation, checked in the tests
(`TestFFT`, "matches a naive DFT") against a direct, slow implementation of the
textbook definition.

### 4. The window, and why it matters so much

This is the part that needs the most explaining. It is also the part where a
wrong choice would make the whole tool useless.

**The problem.** The FFT behaves as if the chunk you give it repeats forever. If
the chunk starts at one value and ends at a very different one, the endless
repetition has a sudden jump at every seam, like a click. A click contains
energy at _every_ frequency. So a loud bass note, cut off abruptly at the edges
of a frame, appears to spill a thin layer of energy across the entire spectrum.
This is called **spectral leakage**.

**Why it matters here.** We are looking for a very quiet region (the area above
a lossy cutoff, at about -129 dB) next to loud music. Leakage from the loud
parts can easily be louder than the quiet region and fill it in, hiding the
wall.

Here is a real measurement. This is the same file, a 128 kbps MP3 stored as
FLAC, whose true spectrum has nothing above 16.8 kHz:

| Analysis                    | Level at 15 kHz | Level at 20 kHz | Wall height     |
| --------------------------- | --------------- | --------------- | --------------- |
| No window (chunks cut flat) | -61 dB          | **-85 dB**      | about 24 dB     |
| Hann window                 | -59 dB          | **-129 dB**     | about **70 dB** |

Without a window, leakage fills the empty region with energy that is not there
and shrinks a 70 dB wall to 24 dB. With the Hann window the wall is as tall as
it really is.

**The fix.** Before taking the FFT, each frame is multiplied by a **window
function**: a smooth curve that starts at zero, rises to one in the middle and
falls back to zero. The edges of the frame are faded out, so there is no seam
and no click. The **Hann window** is a simple bell shape,
`0.5 - 0.5 * cos(...)`, and a standard choice for this kind of measurement. It
has excellent leakage behavior far from a loud tone, which is exactly the
situation here: its leakage dies away very quickly, so loud bass does not
contaminate the high frequencies.

**Why frames overlap.** The window fades the edges to zero, so samples near the
edge of a frame count for almost nothing in that frame. Overlapping frames by
half means every sample is near the middle of some frame, so every part of the
audio is weighed fairly. With a Hann window, 50% overlap is the standard choice.

### 5. How big a window? (4096, 8192, 16384...)

The window length is a trade-off, and you cannot have both:

- A **longer** window gives **narrower bins**, so finer detail in frequency. It
  also blurs time: it cannot tell you _when_ within that stretch something
  happened.
- A **shorter** window has the opposite properties.

We want to find a sharp edge in frequency and we do not care about timing at all
(we are averaging it all away), so longer is better, up to a point. 4096 samples
gives bins about 10.8 Hz wide at 44.1 kHz. That is far finer than we need to
find an edge, but short enough to give plenty of frames to average.

Bin width is `sample rate / window`, so a fixed window size would give a
different resolution at every sample rate. To keep it constant the window grows
with the sample rate:

| Sample rate | Window (samples) | Bin width | Nyquist (top of the spectrum) |
| ----------- | ---------------- | --------- | ----------------------------- |
| 44.1 kHz    | 4096             | 10.8 Hz   | 22.05 kHz                     |
| 48 kHz      | 4096             | 11.7 Hz   | 24 kHz                        |
| 88.2 kHz    | 8192             | 10.8 Hz   | 44.1 kHz                      |
| 96 kHz      | 8192             | 11.7 Hz   | 48 kHz                        |
| 176.4 kHz   | 16384            | 10.8 Hz   | 88.2 kHz                      |
| 192 kHz     | 16384            | 11.7 Hz   | 96 kHz                        |

The rule is "the smallest power of two, at least 4096, that keeps bins at or
under 12 Hz". Everything is therefore analyzed at about the same frequency
resolution, whatever the sample rate.

One side effect: the _level_ reported for a flat noise floor drops by about 3 dB
every time the window doubles (the same noise is spread across twice as many
bins). A 24-bit file's floor reads far lower still. The thresholds are about
_differences_ in level, not absolute levels, so this doesn't affect the
verdicts.

### 6. Averaging, and what a level means

Each frame's spectrum is turned into **power** (energy per bin), scaled so a
full-scale sine reads exactly 0 dB, and the powers are averaged over all frames.
The result is one number per bin: the long-term average power at that frequency.

### 7. Condense into bands

Looking at individual bins would be noisy: neighboring bins jitter by several
dB. So the spectrum is condensed into **bands** about 100 Hz wide (nine
neighboring bins at 44.1 kHz), averaging the level in dB within each band. A
44.1 kHz file becomes a curve of about 220 points from 0 to 22 kHz. This curve
is what everything after this point works on.

### 8. Look for the wall

A wall is a place where the level drops a long way, quickly, and stays down. To
find one the tool slides along the curve and, at each position, compares:

- the **average level of the 1 kHz just below**, with
- the **average level of the 1 kHz just above**.

Where the first is much higher than the second, there is a drop. Rules:

1. Only edges above 5 kHz are considered. (Nothing interesting happens below
   that.)
2. A drop of **at least 15 dB** counts as an edge. Real music's spectrum does
   not fall that fast over 1 kHz without a reason.
3. If several neighboring positions all qualify, they are one edge; the steepest
   position locates it.
4. The edge reported is the **lowest-frequency** one. What matters is where the
   audio first ends, not the biggest step somewhere above it.
5. The **cutoff** is the frequency where the curve crosses halfway between the
   level before the edge and the level after it (halfway in dB).
6. The **drop** is how many dB the level falls across the edge.

Rule 4 matters more than it seems at first glance. A resampler or encoder can
leave several smaller steps in the noise floor above the real edge. Picking the
biggest drop anywhere can land on one of those instead of the real cutoff;
picking the lowest picks the right one.

The 1 kHz comparison window is a deliberate compromise. Narrower (500 Hz) missed
the gentle, several-kHz-wide edge that ffmpeg's default resampler leaves when
upsampling a CD file to 96 kHz. 1 kHz sees that edge and still shows a lossy
encoder's sharp wall at full height.

```text
level (dB)
   -60 ┤▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
       ┤                                      ▀▄
   -80 ┤                                        ▀▄       ← the "edge"
       ┤                                          ▀▄      cutoff ≈ midpoint
  -100 ┤                                            ▀▄
       ┤                                              ▀▄
  -129 ┤                                                ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
       └──────────────────┬──────────────┬───────────────────┬──────── Hz
                          0           16k  17k              22.05k
                                (content)       (nothing: just the noise floor)
```

---

## From measurements to a verdict

Finding a wall is only half the job. Whether it means anything depends on
_where_ it is and how sharp it is. A wall at 16 kHz is a classic MP3 lowpass. A
wall at 21.5 kHz in a 44.1 kHz file is just the ordinary filter every CD
recording has near the top of its range. These are the rules, in order, applied
to the best channel's wall:

| Situation                                                               | Result                                                    |
| ----------------------------------------------------------------------- | --------------------------------------------------------- |
| No wall found                                                           | `ok`                                                      |
| Wall at or above 95% of Nyquist                                         | `ok` (normal anti-alias filtering at the top of the band) |
| Hi-res file (88.2 kHz or more), strong wall at ≤ 27 kHz, below 19.5 kHz | `likely fake`: lossy-style lowpass                        |
| Hi-res file, strong wall at ≤ 27 kHz, 19.5 kHz or above                 | `likely fake`: **upsampled**                              |
| Hi-res file, weaker wall at ≤ 27 kHz                                    | `suspect`                                                 |
| Hi-res file, wall above 27 kHz                                          | `ok` (a mastering filter on genuinely extended audio)     |
| Other file, wall at 21 kHz or above                                     | `ok`                                                      |
| Other file, strong wall (25 dB or more) below 19.5 kHz                  | `likely fake`                                             |
| Everything else (weaker wall; or wall between 19.5 and 21 kHz)          | `suspect`                                                 |

Where "strong" means the level drops by at least 25 dB across the edge.

Several of these thresholds are worth explaining.

**19.5-21 kHz is always only `suspect`.** Many encodes at high bitrates (320
kbps MP3, Opus, Dolby Digital, some Vorbis) cut at about 20 kHz. But a number of
genuine CDs, especially from the early digital era, were deliberately filtered
at 20 kHz too. The two look exactly the same in a spectrum, however strong the
wall. Guessing would produce either a stream of false accusations or a stream of
missed transcodes, so this zone is reported as what it is: ambiguous. Look at
the spectrogram, and at whatever else you know about where the file came from.

**27 kHz for hi-res.** A CD-quality recording upsampled to 96 kHz has nothing
above 22.05 kHz, plus whatever the resampler leaves behind. A sloppy resampler,
including ffmpeg's default one, leaves a gentle slope that ends at about 26 kHz.
The limit is set at 27 kHz to catch that. A real hi-res recording usually
reaches well above 30 kHz; one that stops at 26 kHz is hard to explain as
anything but an upsample.

**The 95% of Nyquist rule.** Digital audio always needs a filter near the top of
its range, so a drop at 21-22 kHz in a CD file is expected and means nothing. It
is also why a file at an unusual sample rate (say 32 kHz, with a Nyquist of 16
kHz) is not flagged for having a 15.5 kHz edge.

---

## Other checks

### Hi-res files that are not

If someone sells a "96 kHz / 24-bit" file that was made by upsampling a CD, it
has a wall at about 22 kHz, with nothing above, in a file that has room up to 48
kHz. That is the "upsampled" finding above. It applies to 88.2, 96, 176.4 and
192 kHz files. A hi-res file made from a lossy source shows a lossy-style wall
(a 128 kbps MP3 upsampled to 96 kHz, for example, has the 16 kHz wall).

### 24-bit files that only hold 16 bits

A 24-bit file has 24 bits available for each sample. If it was made by padding a
16-bit file, the extra eight bits are zero in every single sample. The tool
combines (bitwise OR) every sample in the file and counts how many low-order
bits never switch on. If a "24-bit" file turns out to use 16 or fewer bits, that
is reported as `suspect`.

This only detects pure padding. If the audio was changed after padding (volume
adjustment, a different bit depth conversion with dither), the low bits are
filled with something and the check passes. It also reports `suspect`, not
`likely fake`: a 16-bit file stored in a 24-bit container is not lossy, merely
not what the label implies.

### More than one channel

Every channel is analyzed. The verdict uses the channel with the **widest
bandwidth**. If either channel reaches the top of the band, the file as a whole
is not band-limited, so one full-bandwidth channel is enough to make it `ok`.

Two kinds of channel are left out:

- **The LFE channel** in a 2.1, 5.1 or 7.1 file. It is a bass-only channel,
  usually intentionally filtered at about 120 Hz. It has no cliff to find above
  5 kHz (it has nothing there at all), so it would look like a perfectly
  full-bandwidth channel and mask a transcode in all the others. There is a test
  showing exactly this failure.
- **Silent channels** (fewer than 20 usable frames).

This matters for DVD rips in particular.

---

## Choices and why

A record of the decisions, so they can be revisited.

**Decoding with ffmpeg, analysis in Go.** ffmpeg is already a hard dependency
for transcoding and replay gain, so using it to decode adds nothing. The
analysis itself is plain Go with no external dependencies.

**Averaging instead of looking at moments.** A lossy encoder's cutoff is
constant through the whole file; individual moments of the music are not.
Averaging makes the constant part stand out. The cost is that a cutoff that
_changes_ during a file (some encoders cut lower on difficult passages) shows up
as a slope, not a wall.

**Mean level in dB, not mean power (within a band).** Averaging dB values keeps
one loud bin in a band from dominating its neighbors.

**The lowest wall.** Described above.

**1 kHz comparison windows.** Described above.

**No table of encoder signatures.** It would be tempting to report "looks like
MP3 128k" by matching the cutoff to a table. But the measured cutoff for the
same encoder moved by up to 0.7 kHz depending on the audio (see below), and the
same cutoff comes from different encoders. A guess presented as a fact is worse
than reporting the frequency and the drop and leaving the interpretation to you.

**Three levels of verdict, and an ambiguous zone.** A heuristic that claims
certainty is wrong some of the time; one that admits uncertainty is useful. The
split between `suspect` and `likely fake` is intentionally cautious, so that
`likely fake` can be trusted.

**Two ways to draw the spectrogram.** By default ffmpeg draws it. That adds
almost no code, handles every sample rate and channel count, and produces the
familiar picture with labelled axes, which is the one people already know how to
read. The picture is independent of the tool's measurement; they should agree,
and when they do not that is interesting in itself.

With `--annotated` the tool draws its own instead, from the same measurements as
the verdict, with the detected cutoff marked on it. The analysis keeps only the
time-averaged spectrum (that is all it needs), and a picture needs the full grid
of frequency against time. Rather than keep that grid during every analysis,
drawing it is a second pass over the audio, made only on request. Its cost is
about that of the analysis itself, and its memory is bounded however long the
track is: as the audio grows, neighboring time columns are merged, so at most
about a thousand columns of 512 frequency rows are ever held. The second pass
uses the same channel, window and silence handling as the analysis, so the
picture shows what the analysis saw.

**Two color scales for annotated spectrograms.** On a fixed scale a quiet
recording looks nearly black, which hides the edge the picture exists to show.
Scaling each picture to its own loudness keeps the shape visible for every file,
so that is the default, and the fixed scale is there for the case where
comparing files matters more than seeing each one at its best. The fixed range
is -140 to -20 dB: a full-scale sine reads 0 dB, and the loudest bins of real
music sit below about -20 dB, so the top 20 dB would be wasted; and the bottom
is just under the rounding-noise floor of a 16-bit file, so that floor is
visible as a faint color rather than lost.

**Thresholds are compiled in and are not configurable at runtime.** They are all
in one place (`spectrum.Thresholds`) with documentation on each, and are not
exposed as command-line flags. They were set from measurements on synthetic
signals (below). Adjusting them is a one-line change, and it is better to learn
from real files over time what actually needs adjusting before inventing a
configuration surface.

---

## What was measured

The defaults were chosen by encoding synthetic test signals with real encoders,
decoding them back to 16-bit FLAC and measuring the result, together with
lossless controls that must _not_ be flagged. The signals are noise shaped
roughly like music (flat to 500 Hz, then falling steadily) at three levels of
high-frequency content, which matters because quiet high frequencies make a wall
harder to see:

| Signal | Level at 16 kHz | Wall height vs. the -129 dB floor |
| ------ | --------------- | --------------------------------- |
| Loud   | about -60 dB    | about 70 dB                       |
| Medium | about -80 dB    | about 50 dB                       |
| Quiet  | about -103 dB   | about 25 dB                       |

Cutoffs found, by encoder (ffmpeg's encoders at 44.1 kHz unless noted):

| Encoder and setting                                            | Cutoff found                                          | Wall (dB)       | Verdict                      |
| -------------------------------------------------------------- | ----------------------------------------------------- | --------------- | ---------------------------- |
| MP3 (LAME) 128 kbps                                            | 16.0 - 16.8 kHz                                       | 24 - 57         | likely fake                  |
| MP3 192 kbps                                                   | 18.7 - 18.8 kHz (loud, medium)                        | 38 - 58         | likely fake                  |
| MP3 320 kbps                                                   | 20.0 - 20.2 kHz                                       | 18 - 59         | suspect                      |
| MP3 VBR V0                                                     | none (loud, medium); 16.0 kHz (quiet)                 | 20 (quiet only) | ok (missed); suspect (quiet) |
| AAC 128 kbps                                                   | 17.3 kHz                                              | 25 - 64         | likely fake                  |
| AAC 256 kbps                                                   | none                                                  | -               | ok (missed)                  |
| Vorbis q3                                                      | 17.2 - 17.3 kHz                                       | 18 - 66         | likely fake / suspect        |
| Vorbis q5                                                      | 17.0 - 20.4 kHz                                       | 22 - 64         | suspect                      |
| Vorbis q8                                                      | none or about 21 kHz (loud, medium); 17.8 kHz (quiet) | 20 (quiet only) | ok (missed); suspect (quiet) |
| Opus 128/192 kbps (decodes at 48 kHz)                          | 20.1 - 20.4 kHz                                       | 21 - 49         | suspect                      |
| Dolby Digital (AC-3) 192/448 kbps, 48 kHz (medium signal only) | 20.6 kHz                                              | 43              | suspect                      |
| Upsampled to 96 / 192 kHz (ffmpeg default resampler)           | 25.8 kHz                                              | 38 - 41         | likely fake (upsampled)      |

Controls that were correctly **not** flagged: a full-band lossless file (no
wall, at all three levels); a lossless file with an intentional 21 kHz lowpass;
a 48 kHz lossless file; a CD file converted to 48 kHz; real 96 kHz and 192 kHz
files with content to the top of the band.

Three things to take from this table:

- Low and medium bitrates are detected reliably and with a large margin.
- High-bitrate encodes (MP3 V0, AAC 256, Vorbis q8) usually leave no wall to
  find, and are then indistinguishable from lossless by this method. (With very
  quiet high frequencies the _content_ can end below the encoder's cutoff, which
  is why V0 and Vorbis q8 do show up on the quiet signal.)
- Everything that cuts at about 20 kHz (320 kbps MP3, Opus, AC-3, some Vorbis)
  lands in the ambiguous zone and cannot be called either way.

**An important limit on all of this:** these were synthetic signals. Real music
has tonal peaks, transients and a spectrum with its own character, and none was
available for these tests. The numbers should hold for the _shape_ of the
problem, but the thresholds have not yet been tried on real files. That is the
main reason they live in one place and why `-v` prints the underlying numbers.

---

## What it cannot do

- **It cannot prove a file is lossless.** `ok` means "no wall found", nothing
  more. A high-bitrate lossy encode has none to find.
- **It cannot tell a 20 kHz encode from a 20 kHz mastering filter.** See above.
- **Some genuine files will be flagged.** Recordings with a deliberate lowpass
  at 15-19 kHz (some old digital masters, certain broadcast or tape sources,
  some low-fidelity productions) have a real wall that was put there on purpose.
- **A lossy file converted to a different lossless format** (FLAC to ALAC, say)
  is still lossy underneath and is found the same way; but a file that was
  lossy-encoded, then _processed_ (heavy EQ or noise added above the cutoff) can
  hide the wall.
- **It only looks at the spectrum.** There are other telltale signs of lossy
  origin (in how the MP3 codec splits time into short blocks, for example) that
  specialist tools such as auCDtect look for. They are far more complicated and
  were left out. The spectrum catches the common cases.
- **Very short or very quiet tracks** give a noisy spectrum and may be
  `inconclusive`.

When the tool is unsure, `--png` exists so that you can look.

---

## Tuning it

All numbers live in `spectrum.Thresholds` (`internal/spectrum/thresholds.go`),
each documented. If it flags too much or too little on your own files, these are
the ones to try:

| If...                                                             | Try                                                     |
| ----------------------------------------------------------------- | ------------------------------------------------------- |
| Genuine files are flagged (false alarms)                          | Raise `MinDropDB` (15) or `StrongDropDB` (25)           |
| Known transcodes are missed                                       | Lower `MinDropDB`; check with `-v` how big the wall is  |
| Genuine files with an 18-19.5 kHz filter are called `likely fake` | Lower `LossyEdgeHz` (19500), so less is called "likely" |
| Hi-res fakes slip through                                         | Raise `HiResLimitHz` (27000)                            |
| Genuine hi-res files are flagged                                  | Lower `HiResLimitHz`                                    |
| Quiet classical music is `inconclusive`                           | Lower `SilenceDB` (-75)                                 |

`-v` prints, per track and per channel, the cutoff, the size of the drop and the
number of frames, which is the information needed to choose a value.

**Speed.** The analysis takes about a second for a three-and-a-half minute
stereo track (measured on a 2.8 GHz server CPU), plus ffmpeg's decoding time. A
spectrogram image, either kind, adds up to about a second more.

---

## Reading a spectrogram by eye

A spectrogram plots **time** across, **frequency** up, and **loudness** as color
(bright is loud, dark is quiet). The axis on the right gives the colors' dB
values.

What to look for:

- **A flat ceiling.** A genuine recording fades out toward the top. A lossy
  transcode has a straight horizontal line, with color below it and solid black
  above it, running the whole length of the track. This is the thing the tool is
  measuring.
- **Where the ceiling is, against the sample rate.** 16 kHz in a CD file is a
  classic low-bitrate MP3. A black band across the top third of a 96 kHz file is
  an upsampled CD.
- **Holes.** MP3 and AAC sometimes drop out small patches in the upper
  frequencies from frame to frame, giving a patchy, "swiss cheese" look just
  below the ceiling. The tool does not look for these but your eye can.
- **Dither or a single noise floor.** A pale uniform haze above the ceiling is
  usually just dither or rounding noise. It does not change the verdict.

By default the picture is drawn by ffmpeg independently of the measurement, so
if the verdict and the picture disagree, trust the picture, and treat the
disagreement as something worth investigating.

### The annotated spectrogram

With `--annotated` the picture is drawn by the tool itself, from the same data
as the verdict, and marks what the tool concluded:

- **A cyan line** at the detected cutoff, labelled with its frequency and the
  size of the drop. In a transcode it should sit right on the upper edge of the
  colored area. If it does not match where your eye puts the edge, either the
  measurement is off or the file is unusual.
- **A dashed white line at 22.05 kHz, on hi-res files only**, labelled "CD
  bandwidth". Content stopping at or not far above it is what an upsampled CD
  looks like.
- **No cyan line** means no cutoff was found, as in a genuine file.
- **The header** gives the file, its format, the channel drawn (the one the
  verdict was based on) and the verdict in color: green for `ok`, yellow for
  `suspect`, red for `likely fake`.

There are two color scales, and the footer of every picture says which one it
uses.

- **Relative to the file (the default).** The top of the scale is the loudness
  reached by all but the loudest 0.1% of the picture, and the scale runs 110 dB
  down from there. Every picture uses its whole range of color however loud or
  quiet the file is, which shows the shape of each file as clearly as possible.
  The cost is that **pictures of different files cannot be compared by color**:
  the same audio at two different volumes produces identical pictures, and only
  the numbers on the color bar differ.
- **Fixed (`--fixed-scale`, which requires `--annotated`).** The scale is -140
  dB to -20 dB for every picture, so a given color always means the same level.
  A quiet recording looks darker than a loud one, and a lossy file's empty
  region looks darker than a genuine file's quiet one. Use it to compare tracks,
  for example to see at a glance which tracks of an album are quieter, or
  whether the high frequencies of two masterings really differ. Levels above -20
  dB show as the brightest color and levels below -140 dB as black. A 16-bit
  file's rounding-noise floor (about -129 dB) shows as a faint dark blue,
  distinct from the pure black of anything quieter, such as a 24-bit file's far
  lower floor.

Levels are per FFT bin, and the window grows with the sample rate (see
[How big a window?](#5-how-big-a-window-4096-8192-16384)), which lowers the
level of noise-like sound by about 3 dB each time the window doubles. Pictures
of files at the same sample rate, which is the usual case for an album, are
exactly comparable on the fixed scale. Pictures of files at different sample
rates are comparable only roughly: tonal peaks line up, but noise-like sound
reads a few dB lower at the higher rate.

---

## Glossary

**Bin.** One of the evenly spaced frequency slots an FFT reports.

**Band.** A group of neighboring bins averaged together (here about 100 Hz).

**Cliff / wall / edge.** A place where the spectrum drops a long way, fast.

**Cutoff.** The frequency at which that happens.

**dB / dBFS.** Decibels; decibels relative to the loudest possible steady tone.

**FFT.** Fast Fourier Transform: the algorithm that turns a short stretch of
audio into a spectrum.

**Frame.** One short chunk of audio (4096 samples at 44.1 kHz) analyzed at a
time.

**Hann window.** A smooth fade-in-fade-out curve applied to each frame to
prevent spectral leakage.

**Lowpass.** A filter that removes everything above a frequency. Lossy encoders
apply one.

**LFE.** Low-frequency effects: the bass-only ".1" channel of a 5.1 or 7.1 mix.

**Nyquist frequency.** Half the sample rate: the highest frequency the format
can hold.

**Quantization noise.** The tiny rounding error from storing samples as whole
numbers; sets the noise floor of the file.

**Spectral leakage.** Energy from a loud part of the spectrum spilling into
quiet parts because of how a chunk of audio was cut.

**Spectrogram.** A picture of the spectrum over time.

**Transcode.** Converting audio from one encoding to another. Here: lossy to
lossless, which cannot restore what was lost.

**Upsampling.** Converting audio to a higher sample rate. It does not add
content; a 44.1 kHz recording upsampled to 96 kHz still has nothing above 22.05
kHz.

**Welch's method.** Cutting audio into overlapping, windowed frames, measuring
each, and averaging the results.
