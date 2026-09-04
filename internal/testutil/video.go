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
	"os/exec"
	"path/filepath"
	"testing"
)

// MakeVideoFile generates a short synthetic video file via ffmpeg's lavfi
// test sources: a testsrc pattern at the given dimensions, plus a silent
// anullsrc audio track (unless audioCodec is "", in which case the file
// has no audio stream at all, for exercising audio-detection error paths).
//
// Requires a real ffmpeg binary on PATH.
func MakeVideoFile(t *testing.T, dir, name string, width, height int, videoCodec, audioCodec string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	args := []string{
		"-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=1:size=%dx%d:rate=5", width, height),
	}
	if audioCodec != "" {
		args = append(args, "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo")
	}
	args = append(args, "-c:v", videoCodec)
	if audioCodec != "" {
		args = append(args, "-c:a", audioCodec, "-shortest")
	}
	args = append(args, "-t", "1", path)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("MakeVideoFile: ffmpeg failed: %v\n%s", err, out)
	}

	return path
}

// MakeVideoFileWithTone is like [MakeVideoFile], fixed at 320x240, but
// always carries a real audible sine-tone audio track rather than silence
// (or no audio at all). Use this instead of MakeVideoFile when a test needs
// actual signal to measure (e.g., verifying ReplayGain computation on
// extracted audio actually produces a non-empty gain, which a silent track
// may not exercise the same way).
//
// Requires a real ffmpeg binary on PATH.
func MakeVideoFileWithTone(t *testing.T, dir, name, videoCodec, audioCodec string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	out, err := exec.Command(
		"ffmpeg", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=5",
		"-map", "1:v", "-map", "0:a",
		"-c:v", videoCodec, "-c:a", audioCodec, "-shortest", "-t", "2",
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("MakeVideoFileWithTone: ffmpeg failed: %v\n%s", err, out)
	}

	return path
}
