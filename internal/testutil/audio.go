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

// Package testutil holds real-fixture test helpers (ffmpeg/ffprobe-backed)
// shared across this module's internal packages. It is a plain intentionally
// a non-_test.go package: Go test helpers can't be imported across packages,
// so anything meant to be reused between, say, internal/metadata's and
// internal/checker's tests has to live in an importable package instead.
// Nothing outside test files should ever import this package; it pulls in
// "testing" as a real dependency and every helper calls t.Fatalf on failure,
// neither of which belongs in non-test code.
package testutil

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// MakeAudioFile generates a short silent audio file at dir/name via a real
// ffmpeg invocation, carrying tags (nil or empty for none). The codec is
// inferred from name's extension (.flac, .mp3, .m4a); any other extension
// fails the test immediately via t.Fatalf.
//
// For FLAC, use uppercase Vorbis comment key names (TITLE, ARTIST,
// ALBUMARTIST, DATE, TRACKNUMBER, DISCNUMBER). For MP3 and M4A, ffmpeg's
// lowercase generic keys (title, artist, album, album_artist, track, date)
// are more reliable.
//
// Requires a real ffmpeg binary on PATH.
func MakeAudioFile(t *testing.T, dir, name string, tags map[string]string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	ext := strings.ToLower(filepath.Ext(name))

	var codec string
	switch ext {
	case ".flac":
		codec = "flac"
	case ".mp3":
		codec = "libmp3lame"
	case ".m4a":
		codec = "aac"
	default:
		t.Fatalf("MakeAudioFile: unsupported extension %q", ext)
	}

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "anullsrc=r=44100:cl=stereo",
		"-t", "1",
		"-c:a", codec,
	}
	for k, v := range tags {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, path)

	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("MakeAudioFile: ffmpeg failed: %v\n%s", err, out)
	}

	return path
}

// MakeToneFile generates a short real audible tone (not silence) at
// dir/name via a real ffmpeg invocation. Unlike [MakeAudioFile], this
// carries no tags and always produces actual signal, for tests that
// measure loudness (e.g., replaygain) where a silent source has nothing to
// measure. The codec is inferred from name's extension: "libopus" for
// .opus, "aac" otherwise.
//
// Requires a real ffmpeg binary on PATH; not skippable.
func MakeToneFile(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	ext := strings.ToLower(filepath.Ext(name))
	codec := "aac"
	if ext == ".opus" {
		codec = "libopus"
	}

	out, err := exec.Command(
		"ffmpeg", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", codec,
		path,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("MakeToneFile: ffmpeg failed: %v\n%s", err, out)
	}

	return path
}
