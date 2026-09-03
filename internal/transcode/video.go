/*
 * Copyright © 2026 Mario Finelli
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package transcode

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mfinelli/musicrename/internal/target"
)

// VideoExt is the file extension used for a video transcoded via
// [TranscodeVideo]. TranscodeVideo itself doesn't enforce this (ffmpeg's
// -f mpeg flag controls the actual output format regardless of dst's
// extension, so this is purely a naming convention) but every caller that
// picks dst should use it, for one shared source of truth.
const VideoExt = ".mpg"

const (
	// aspectRatioMin/aspectRatioMax bound the range of source aspect
	// ratios ChooseVideoScale will attempt to fit into either preset.
	// Outside this range (e.g., a portrait/vertical video, or something
	// absurdly ultra-wide) force-fitting into either preset would look
	// broken so this refuses outright rather than silently degrading.
	aspectRatioMin = 1.0
	aspectRatioMax = 2.5
	// aspectRatioThreshold is the midpoint between 4:3 (1.333...) and
	// 16:9 (1.778...) which is the natural boundary for picking whichever
	// of the two presets a source's aspect ratio is actually closer to.
	aspectRatioThreshold = (4.0/3.0 + 16.0/9.0) / 2
)

// ProbeAspectRatio returns src's display aspect ratio (width/height,
// accounting for non-square pixels via ffprobe's display_aspect_ratio when
// it's available and parses cleanly) as a plain float64.
func ProbeAspectRatio(ctx context.Context, src string) (float64, error) {
	return probeAspectRatio(ctx, execProbeRunner{}, src)
}

func probeAspectRatio(ctx context.Context, r probeRunner, src string) (float64, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,display_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		src,
	}

	out, err := r.Run(ctx, args)
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}

	var width, height int
	var dar string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "width":
			width, _ = strconv.Atoi(val)
		case "height":
			height, _ = strconv.Atoi(val)
		case "display_aspect_ratio":
			dar = val
		}
	}

	if ratio, ok := parseRatioString(dar); ok {
		return ratio, nil
	}
	// display_aspect_ratio absent, or not a clean "W:H" string. Fall back
	// to raw pixel dimensions, which don't account for non-square pixels
	// but are right the overwhelming majority of the time in practice.
	if width > 0 && height > 0 {
		return float64(width) / float64(height), nil
	}
	return 0, fmt.Errorf("could not determine aspect ratio for %s", src)
}

// parseRatioString parses an ffprobe "W:H" ratio string (e.g. "16:9") into
// a plain float64. ok is false for anything that doesn't parse cleanly,
// including "N/A" for an unset ratio, a zero denominator (division by zero),
// or a zero numerator (a meaningless 0 aspect ratio for any actual video);
// any of these should fall back to raw width/height instead, not be used
// directly.
func parseRatioString(s string) (float64, bool) {
	w, h, found := strings.Cut(s, ":")
	if !found {
		return 0, false
	}
	wf, err1 := strconv.ParseFloat(w, 64)
	hf, err2 := strconv.ParseFloat(h, 64)
	if err1 != nil || err2 != nil || hf == 0 || wf == 0 {
		return 0, false
	}
	return wf / hf, true
}

// ChooseVideoScale picks whichever of settings' two scale targets
// (Fullscreen/Widescreen) is closer to ratio, erroring if ratio falls
// outside [aspectRatioMin, aspectRatioMax].
func ChooseVideoScale(ratio float64, settings target.VideoTranscodeSettings) (target.VideoScale, error) {
	if ratio < aspectRatioMin || ratio > aspectRatioMax {
		return target.VideoScale{}, fmt.Errorf(
			"aspect ratio %.3f is outside the supported range (%.2f-%.2f)",
			ratio, aspectRatioMin, aspectRatioMax,
		)
	}
	if ratio >= aspectRatioThreshold {
		return settings.Widescreen, nil
	}
	return settings.Fullscreen, nil
}

// TranscodeVideo produces dst (MPEG-2 video / MP3 audio muxed as an MPEG
// Program Stream (for Rockbox's MPEGplayer plugin which is the only video
// format it decodes at all ) from src, using settings' bitrates and picking
// Fullscreen or Widescreen scale automatically from src's aspect ratio
// (ChooseVideoScale). Framerate is intentionally never forced (no -r):
// omitting it preserves src's framerate exactly, matching testing in VIDEO.md
// that found forcing a fixed rate just meant ffmpeg duplicating frames to
// reach it. dst's parent directory must already exist; if dst already exists
// it's overwritten.
func TranscodeVideo(ctx context.Context, src, dst string, settings target.VideoTranscodeSettings) error {
	return transcodeVideo(ctx, execProbeRunner{}, execRunner{}, src, dst, settings)
}

func transcodeVideo(ctx context.Context, pr probeRunner, r runner, src, dst string, settings target.VideoTranscodeSettings) error {
	ratio, err := probeAspectRatio(ctx, pr, src)
	if err != nil {
		return err
	}
	scale, err := ChooseVideoScale(ratio, settings)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}

	args := []string{
		"-y", // overwrite dst if it already exists
		"-i", src,
		"-vcodec", "mpeg2video",
		"-vf", fmt.Sprintf("scale=%d:%d", scale.Width, scale.Height),
		"-b:v", fmt.Sprintf("%dk", settings.VideoBitrateKbps),
		"-acodec", "libmp3lame",
		"-b:a", fmt.Sprintf("%dk", settings.AudioBitrateKbps),
		"-ar", "44100",
		"-f", "mpeg",
		dst,
	}

	if err := r.Run(ctx, args); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
