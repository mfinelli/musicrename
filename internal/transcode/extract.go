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
	"os"
	"os/exec"
	"slices"
	"strings"
)

// codecExtensions maps an audio codec name, as reported by ffprobe's
// stream=codec_name, to the file extension a remuxed derived audio file
// should use. This is a real remux target list, not an exhaustive list of
// every codec ffprobe could ever report (ExtensionForCodec errors on anything
// not listed here rather than guessing). aac and alac both land in .m4a
// (the standard container for either), which is why DerivedAudioExtensions
// below deduplicates rather than returning one extension per codec.
var codecExtensions = map[string]string{
	"aac":    ".m4a",
	"alac":   ".m4a",
	"opus":   ".opus",
	"vorbis": ".ogg",
	"mp3":    ".mp3",
	"flac":   ".flac",
}

// ExtensionForCodec returns the file extension (including the leading dot) a
// remuxed derived audio file should use for the given ffprobe codec name.
// ok is false for a codec not in the known remux target list.
func ExtensionForCodec(codec string) (ext string, ok bool) {
	ext, ok = codecExtensions[codec]
	return ext, ok
}

// DerivedAudioExtensions returns every distinct extension
// [ExtensionForCodec] could ever produce. This is the vocabulary
// internal/video's derived-audio file discovery uses to recognize a
// candidate file by extension alone, without needing to re-probe or import
// this package's codec-name-keyed map directly.
func DerivedAudioExtensions() []string {
	seen := make(map[string]bool, len(codecExtensions))
	var exts []string
	for _, ext := range codecExtensions {
		if !seen[ext] {
			seen[ext] = true
			exts = append(exts, ext)
		}
	}
	slices.Sort(exts)
	return exts
}

// IsDerivedAudioExt reports whether ext (lowercase, including the leading
// dot) is one [ExtensionForCodec] could produce.
func IsDerivedAudioExt(ext string) bool {
	return slices.Contains(DerivedAudioExtensions(), ext)
}

// probeRunner abstracts the actual ffprobe invocation so ProbeAudioCodec's
// surrounding logic can be tested without a real ffprobe binary, mirroring
// runner's role for ffmpeg above.
type probeRunner interface {
	Run(ctx context.Context, args []string) (stdout string, err error)
}

// execProbeRunner shells out to the real ffprobe binary on PATH.
type execProbeRunner struct{}

func (execProbeRunner) Run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// ProbeAudioCodec reports the codec name (ffprobe's stream=codec_name, e.g.,
// "aac", "opus") of src's first audio stream. Returns an error if src has no
// audio stream at all (a silent video) or if ffprobe itself fails.
func ProbeAudioCodec(ctx context.Context, src string) (string, error) {
	return probeAudioCodec(ctx, execProbeRunner{}, src)
}

func probeAudioCodec(ctx context.Context, r probeRunner, src string) (string, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		src,
	}

	out, err := r.Run(ctx, args)
	if err != nil {
		return "", fmt.Errorf("ffprobe: %w", err)
	}

	codec := strings.TrimSpace(out)
	if codec == "" {
		// A missing audio stream is not an ffprobe failure (with
		// -select_streams a:0 it exits 0 and simply prints nothing).
		return "", fmt.Errorf("no audio stream in %s", src)
	}
	return codec, nil
}

// RemuxAudio extracts src's (a video file) audio stream into dst via a true
// remux (-c:a copy) i.e., no re-encoding, so dst's container/extension must
// already match src's actual audio codec (see ExtensionForCodec). dst's
// parent directory must already exist; if dst already exists it is
// overwritten.
//
// Metadata is stripped (-map_metadata -1) and the video stream is dropped
// (-vn) the same as Audio does for its own reasons (there, discarding a
// stale embedded picture; here, discarding the actual video stream itself).
// Tags are written afterward as a separate step from musicvideo.nfo, not
// carried over from whatever the source video happened to have.
func RemuxAudio(ctx context.Context, src, dst string) error {
	return remuxAudio(ctx, execRunner{}, src, dst)
}

func remuxAudio(ctx context.Context, r runner, src, dst string) error {
	args := []string{
		"-y", // overwrite dst if it already exists
		"-i", src,
		"-map_metadata", "-1",
		"-vn",
		"-c:a", "copy",
		dst,
	}

	if err := r.Run(ctx, args); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}
