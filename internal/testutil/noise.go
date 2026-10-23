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

package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFFmpeg runs ffmpeg with args and fails the test (naming the calling
// helper) if it exits non-zero.
func runFFmpeg(t *testing.T, helper string, args ...string) {
	t.Helper()

	out, err := exec.Command("ffmpeg", append([]string{"-y", "-v", "error"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: ffmpeg failed: %v\n%s", helper, err, out)
	}
}

// MakeNoiseFlac generates a stereo 16-bit FLAC of brown noise at dir/name,
// seconds long at sampleRate Hz. Unlike [MakeToneFile], whose single tone has
// no broadband content, noise fills the whole spectrum, which is what tests of
// spectral analysis (for example, finding a lossy encoder's lowpass) need: a
// lossless file made this way has content all the way up to the Nyquist
// frequency.
//
// Requires a real ffmpeg binary on PATH.
func MakeNoiseFlac(t *testing.T, dir, name string, sampleRate, seconds int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	runFFmpeg(t, "MakeNoiseFlac",
		"-f", "lavfi",
		"-i", fmt.Sprintf("anoisesrc=color=brown:amplitude=0.9:sample_rate=%d:duration=%d:seed=7",
			sampleRate, seconds),
		"-ac", "2",
		"-sample_fmt", "s16",
		"-c:a", "flac",
		path,
	)
	return path
}

// MakeLossyTranscodeFlac makes a fake lossless file: it encodes src with a
// lossy codec (encodeArgs are ffmpeg output options such as "-c:a",
// "libmp3lame", "-b:a", "128k"; ext is the lossy container's extension
// including the dot, e.g. ".mp3"), decodes the result, and stores that as a
// 16-bit FLAC at dir/name. The intermediate lossy file is removed.
//
// Requires a real ffmpeg binary on PATH with the requested encoder.
func MakeLossyTranscodeFlac(t *testing.T, src, dir, name, ext string, encodeArgs ...string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	lossy := filepath.Join(dir, strings.TrimSuffix(name, filepath.Ext(name))+".lossy"+ext)

	runFFmpeg(t, "MakeLossyTranscodeFlac", append(append([]string{"-i", src}, encodeArgs...), lossy)...)
	defer os.Remove(lossy)

	runFFmpeg(t, "MakeLossyTranscodeFlac",
		"-i", lossy,
		"-sample_fmt", "s16",
		"-c:a", "flac",
		path,
	)
	return path
}

// MakeHiResFlac re-encodes src as a 24-bit FLAC at dir/name. When sampleRate is
// non-zero the audio is first resampled to it; with zero the sample rate is
// kept, which pads a 16-bit source out to a 24-bit container without adding
// any real resolution.
//
// Requires a real ffmpeg binary on PATH.
func MakeHiResFlac(t *testing.T, src, dir, name string, sampleRate int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	args := []string{"-i", src}
	if sampleRate != 0 {
		args = append(args, "-af", fmt.Sprintf("aresample=%d", sampleRate))
	}
	args = append(args,
		"-sample_fmt", "s32",
		"-bits_per_raw_sample", "24",
		"-c:a", "flac",
		path,
	)

	runFFmpeg(t, "MakeHiResFlac", args...)
	return path
}
