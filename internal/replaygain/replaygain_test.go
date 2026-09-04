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

package replaygain

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// fakeRunner records the args it was invoked with (for assertions) rather
// than actually shelling out to rsgain.
type fakeRunner struct {
	called  bool
	gotArgs []string
	err     error
}

func (f *fakeRunner) Run(_ context.Context, args []string) error {
	f.called = true
	f.gotArgs = args
	return f.err
}

func TestCompute(t *testing.T) {
	t.Run("builds the expected rsgain arguments", func(t *testing.T) {
		r := &fakeRunner{}
		err := compute(context.Background(), r, "track.m4a")
		require.NoError(t, err)
		require.True(t, r.called)

		assert.Equal(t, []string{
			"custom",
			"-s", "i",
			"-l", "-18",
			"track.m4a",
		}, r.gotArgs)
	})

	t.Run("never requests album gain", func(t *testing.T) {
		r := &fakeRunner{}
		require.NoError(t, compute(context.Background(), r, "track.m4a"))
		assert.NotContains(t, r.gotArgs, "-a")
	})

	t.Run("propagates a runner failure", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		err := compute(context.Background(), r, "track.m4a")
		assert.Error(t, err)
	})
}

// probeFormatTags reads path's tags via a direct real ffprobe call,
// checking both format-level and stream-level tags: REPLAYGAIN_* lands at
// the format level for MP4/M4A but at the *stream* level for Ogg/Opus, a
// quirk of ffprobe's metadata model, not something safe to assume is uniform
// across containers.
func probeFormatTags(t *testing.T, path string) string {
	t.Helper()

	out, err := exec.Command(
		"ffprobe", "-v", "error",
		"-show_entries", "format_tags:stream_tags",
		"-of", "default=noprint_wrappers=1",
		path,
	).Output()
	if err != nil {
		t.Fatalf("probeFormatTags: ffprobe failed: %v", err)
	}
	return string(out)
}

// TestComputeReal exercises Compute against a real rsgain binary rather
// than only a fake runner's canned response (the fake-runner tests above
// confirm the argument-building/error-handling logic; these confirm rsgain
// actually accepts those arguments and really does write the tags).
func TestComputeReal(t *testing.T) {
	t.Run("writes REPLAYGAIN tags to an m4a file", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeToneFile(t, dir, "track.m4a")

		before := probeFormatTags(t, path)
		assert.NotContains(t, before, "REPLAYGAIN_TRACK_GAIN")

		require.NoError(t, Compute(context.Background(), path))

		after := probeFormatTags(t, path)
		assert.Contains(t, after, "REPLAYGAIN_TRACK_GAIN")
		assert.Contains(t, after, "REPLAYGAIN_TRACK_PEAK")
	})

	t.Run("writes REPLAYGAIN tags to an opus file", func(t *testing.T) {
		dir := t.TempDir()
		path := testutil.MakeToneFile(t, dir, "track.opus")

		require.NoError(t, Compute(context.Background(), path))

		after := probeFormatTags(t, path)
		assert.Contains(t, after, "REPLAYGAIN_TRACK_GAIN")
		assert.Contains(t, after, "REPLAYGAIN_TRACK_PEAK")
	})

	t.Run("errors on a nonexistent file", func(t *testing.T) {
		err := Compute(context.Background(), "/nonexistent/track.m4a")
		assert.Error(t, err)
	})
}
