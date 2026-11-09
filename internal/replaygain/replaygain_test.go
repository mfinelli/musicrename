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
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/testutil"
)

// fakeRunner records the args it was invoked with (for assertions) rather
// than actually shelling out to rsgain.
type fakeRunner struct {
	called  bool
	gotArgs []string
	err     error

	// onRun, if set, is called with the arguments and its error is returned
	// instead of err, so a test can have the fake "rsgain" change files.
	onRun func(args []string) error
}

func (f *fakeRunner) Run(_ context.Context, args []string) error {
	f.called = true
	f.gotArgs = args
	if f.onRun != nil {
		return f.onRun(args)
	}
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
			"-t",
			"track.m4a",
		}, r.gotArgs)
	})

	t.Run("measures true peak", func(t *testing.T) {
		r := &fakeRunner{}
		require.NoError(t, compute(context.Background(), r, "track.m4a"))
		assert.Contains(t, r.gotArgs, "-t")
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
	return testutil.ProbeText(t, path, "format_tags:stream_tags")
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

func TestScanAlbum(t *testing.T) {
	// album makes real (silent) FLAC files, since the tags of the first are
	// read back after a scan.
	album := func(t *testing.T, names ...string) []string {
		t.Helper()
		dir := t.TempDir()
		var paths []string
		for _, n := range names {
			paths = append(paths, testutil.MakeAudioFile(t, dir, n, nil))
		}
		return paths
	}

	// rewrite changes the content of path, as rsgain writing new tags would.
	rewrite := func(t *testing.T, path string) {
		t.Helper()
		require.NoError(t, taglib.WriteTags(path, map[string][]string{
			"REPLAYGAIN_TRACK_GAIN": {"-1.00 dB"},
		}, 0))
	}

	t.Run("scans every file together, as an album, with true peaks and quietly", func(t *testing.T) {
		paths := album(t, "01.flac", "02.flac")
		r := &fakeRunner{}
		_, err := scanAlbum(context.Background(), r, paths)
		require.NoError(t, err)

		assert.Equal(t, append([]string{
			"custom", "-a", "-s", "i", "-l", "-18", "-t", "-q",
		}, paths...), r.gotArgs)
	})

	t.Run("reports the files whose content changed, in the order given", func(t *testing.T) {
		paths := album(t, "01.flac", "02.flac", "03.flac")
		r := &fakeRunner{onRun: func([]string) error {
			rewrite(t, paths[2])
			rewrite(t, paths[0])
			return nil
		}}

		res, err := scanAlbum(context.Background(), r, paths)
		require.NoError(t, err)
		assert.Equal(t, []string{paths[0], paths[2]}, res.Changed)
	})

	t.Run("a file rewritten with the same content is not a change", func(t *testing.T) {
		// rsgain does this to a file whose tags were already right: the bytes
		// are the same and only the modification time moves.
		paths := album(t, "01.flac", "02.flac")
		r := &fakeRunner{onRun: func([]string) error {
			later := time.Now().Add(time.Hour)
			for _, p := range paths {
				require.NoError(t, os.Chtimes(p, later, later))
			}
			return nil
		}}

		res, err := scanAlbum(context.Background(), r, paths)
		require.NoError(t, err)
		assert.Empty(t, res.Changed)
	})

	t.Run("an album that needed no changes reports none", func(t *testing.T) {
		res, err := scanAlbum(context.Background(), &fakeRunner{}, album(t, "01.flac", "02.flac"))
		require.NoError(t, err)
		assert.Empty(t, res.Changed)
	})

	t.Run("reports the album values the files carry", func(t *testing.T) {
		dir := t.TempDir()
		paths := []string{
			testutil.MakeAudioFile(t, dir, "01.flac", map[string]string{
				"REPLAYGAIN_ALBUM_GAIN": "-7.31 dB", "REPLAYGAIN_ALBUM_PEAK": "0.987654",
			}),
			testutil.MakeAudioFile(t, dir, "02.flac", nil),
		}
		res, err := scanAlbum(context.Background(), &fakeRunner{}, paths)
		require.NoError(t, err)
		assert.Equal(t, "-7.31 dB", res.AlbumGain)
		assert.Equal(t, "0.987654", res.AlbumPeak)
	})

	t.Run("a runner failure is an error and reports nothing", func(t *testing.T) {
		_, err := scanAlbum(context.Background(), &fakeRunner{err: errors.New("boom")}, album(t, "01.flac"))
		assert.ErrorContains(t, err, "boom")
	})

	t.Run("a missing file is an error before rsgain runs", func(t *testing.T) {
		r := &fakeRunner{}
		_, err := scanAlbum(context.Background(), r, []string{"/nonexistent/01.flac"})
		assert.Error(t, err)
		assert.False(t, r.called)
	})

	t.Run("no files is an error", func(t *testing.T) {
		r := &fakeRunner{}
		_, err := scanAlbum(context.Background(), r, nil)
		assert.Error(t, err)
		assert.False(t, r.called)
	})
}

// readRG returns the first value of each ReplayGain tag of path.
func readRG(t *testing.T, path string) map[string]string {
	t.Helper()
	tags, err := taglib.ReadTags(path)
	require.NoError(t, err)
	out := make(map[string]string)
	for _, k := range []string{
		"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK",
		"REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK",
	} {
		if v := tags[k]; len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// TestScanAlbumReal runs ScanAlbum against a real rsgain, checking what the
// fake-runner tests above can't: that rsgain accepts the arguments, writes all
// four tags, calculates one album value over every file, and leaves an
// already-tagged album alone.
func TestScanAlbumReal(t *testing.T) {
	t.Run("tags every file with its own track values and the album's", func(t *testing.T) {
		dir := t.TempDir()
		paths := []string{
			testutil.MakeToneFileAt(t, dir, "01.flac", -6),
			testutil.MakeToneFileAt(t, dir, "02.flac", -12),
			testutil.MakeToneFileAt(t, dir, "03.flac", -18),
		}

		res, err := ScanAlbum(context.Background(), paths)
		require.NoError(t, err)
		assert.Equal(t, paths, res.Changed)

		first := readRG(t, paths[0])
		for _, k := range []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_TRACK_PEAK", "REPLAYGAIN_ALBUM_GAIN", "REPLAYGAIN_ALBUM_PEAK"} {
			assert.NotEmpty(t, first[k], k)
		}
		assert.Equal(t, first["REPLAYGAIN_ALBUM_GAIN"], res.AlbumGain)
		assert.Equal(t, first["REPLAYGAIN_ALBUM_PEAK"], res.AlbumPeak)

		trackGains := map[string]bool{}
		for _, p := range paths {
			rg := readRG(t, p)
			assert.Equal(t, first["REPLAYGAIN_ALBUM_GAIN"], rg["REPLAYGAIN_ALBUM_GAIN"], "one album gain for every track")
			assert.Equal(t, first["REPLAYGAIN_ALBUM_PEAK"], rg["REPLAYGAIN_ALBUM_PEAK"], "one album peak for every track")
			trackGains[rg["REPLAYGAIN_TRACK_GAIN"]] = true
		}
		assert.Len(t, trackGains, 3, "tracks of different loudness have different track gains")
	})

	t.Run("scanning an album that is already right changes nothing", func(t *testing.T) {
		dir := t.TempDir()
		paths := []string{
			testutil.MakeToneFileAt(t, dir, "01.flac", -6),
			testutil.MakeToneFileAt(t, dir, "02.flac", -12),
		}
		_, err := ScanAlbum(context.Background(), paths)
		require.NoError(t, err)

		res, err := ScanAlbum(context.Background(), paths)
		require.NoError(t, err)
		assert.Empty(t, res.Changed)
	})

	t.Run("a track added later moves the album values, so every track is rewritten", func(t *testing.T) {
		dir := t.TempDir()
		paths := []string{
			testutil.MakeToneFileAt(t, dir, "01.flac", -6),
			testutil.MakeToneFileAt(t, dir, "02.flac", -12),
		}
		first, err := ScanAlbum(context.Background(), paths)
		require.NoError(t, err)

		// Louder than the rest, so it counts toward the album's loudness; a
		// much quieter one would be gated out of it.
		paths = append(paths, testutil.MakeToneFileAt(t, dir, "03.flac", 0))
		res, err := ScanAlbum(context.Background(), paths)
		require.NoError(t, err)

		assert.NotEqual(t, first.AlbumGain, res.AlbumGain)
		assert.Equal(t, paths, res.Changed, "the old tracks carry the album gain, so they changed too")
		for _, p := range paths {
			assert.Equal(t, res.AlbumGain, readRG(t, p)["REPLAYGAIN_ALBUM_GAIN"])
		}
	})

	t.Run("a file that doesn't exist is an error", func(t *testing.T) {
		_, err := ScanAlbum(context.Background(), []string{"/nonexistent/01.flac"})
		assert.Error(t, err)
	})
}
