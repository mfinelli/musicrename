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

package video

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
)

// place adds a video (and its musicvideo.nfo, and info.txt if withInfo) at
// videoRoot/relDir, using relDir verbatim as the directory (i.e. it need not
// match what destination() would compute; that's the point when testing
// drift detection).
func place(t *testing.T, videoRoot, relDir string, nfo NFO, withInfo bool) string {
	t.Helper()
	dir := filepath.Join(videoRoot, relDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("data"), 0o644))
	require.NoError(t, writeNFO(nfoPath(dir), nfo))
	if withInfo {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "info.txt"), []byte("url: x\n"), 0o644))
	}
	return dir
}

// hashHex returns the lowercase hex MD5 digest of content, matching the
// format sums.md5 uses. Used to build hand-written sums.md5 fixtures without
// duplicating the hashing logic under test.
func hashHex(t *testing.T, content string) string {
	t.Helper()
	h := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", h)
}

func TestScan(t *testing.T) {
	t.Run("a correctly-placed video is a no-op", func(t *testing.T) {
		root := t.TempDir()
		_, err := Add(root, AddInput{
			SourcePath: mustWriteVideo(t, "raw.mp4"),
			Artist:     "Beyoncé",
			Title:      "Crazy in Love",
		})
		require.NoError(t, err)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		assert.True(t, plan.Moves[0].IsNoOp)
		assert.Equal(t, plan.Moves[0].OldDir, plan.Moves[0].NewDir)
		assert.Empty(t, plan.Warnings)
	})

	t.Run("a video whose nfo no longer matches its directory is a move", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong artist", "wrong title"), NFO{
			Artist: "Beyoncé",
			Title:  "Crazy in Love",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		m := plan.Moves[0]
		assert.False(t, m.IsNoOp)
		assert.False(t, m.IsCaseOnly)
		assert.Equal(t, oldDir, m.OldDir)
		assert.Equal(t, filepath.Join(root, "b", "beyonce", "crazy in love"), m.NewDir)
		assert.Equal(t, "b", m.Bucket)
	})

	t.Run("case-only drift is detected", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("b", "beyonce", "CRAZY IN LOVE"), NFO{
			Artist: "Beyoncé",
			Title:  "Crazy in Love",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		assert.True(t, plan.Moves[0].IsCaseOnly)
		assert.False(t, plan.Moves[0].IsNoOp)
	})

	t.Run("info.txt presence is detected", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("a", "artist", "title"), NFO{
			Artist: "Artist", Title: "Title",
		}, true)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		assert.True(t, plan.Moves[0].HasInfoTxt)
	})

	t.Run("a video directory with no nfo is a warning, not a move", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "orphan")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("data"), 0o644))

		plan, err := Scan(root)
		require.NoError(t, err)
		assert.Empty(t, plan.Moves)
		require.Len(t, plan.Warnings, 1)
		assert.Contains(t, plan.Warnings[0], "no musicvideo.nfo")
	})

	t.Run("a directory with multiple video files is a warning, not a move", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "ambiguous")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "one.mp4"), []byte("data"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "two.mkv"), []byte("data"), 0o644))
		require.NoError(t, writeNFO(nfoPath(dir), NFO{Artist: "A", Title: "T"}))

		plan, err := Scan(root)
		require.NoError(t, err)
		assert.Empty(t, plan.Moves)
		require.Len(t, plan.Warnings, 1)
		assert.Contains(t, plan.Warnings[0], "multiple video files")
	})

	t.Run("multiple videos under the same artist are all planned", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("b", "beyonce", "crazy in love"), NFO{
			Artist: "Beyoncé", Title: "Crazy in Love",
		}, false)
		place(t, root, filepath.Join("b", "beyonce", "halo-wrong"), NFO{
			Artist: "Beyoncé", Title: "Halo",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)
		assert.Len(t, plan.Moves, 2)
	})

	t.Run("more than one derived audio file is a warning, not a move", func(t *testing.T) {
		root := t.TempDir()
		dir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.m4a"), []byte("a"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "video.opus"), []byte("a"), 0o644))

		plan, err := Scan(root)
		require.NoError(t, err)
		assert.Empty(t, plan.Moves)
		require.Len(t, plan.Warnings, 1)
		assert.Contains(t, plan.Warnings[0], "multiple derived audio files")
	})
}

func TestExecute(t *testing.T) {
	t.Run("moves the video, nfo, and info.txt together", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, true)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)

		newDir := plan.Moves[0].NewDir
		assert.FileExists(t, filepath.Join(newDir, "title.mp4"))
		assert.FileExists(t, filepath.Join(newDir, NFOFilename))
		assert.FileExists(t, filepath.Join(newDir, "info.txt"))

		// The old leaf directory, and its now-empty ancestors up to (but
		// not including) root, are cleaned up.
		_, err = os.Stat(oldDir)
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(root, "w"))
		assert.True(t, os.IsNotExist(err))
		assert.DirExists(t, root)
	})

	t.Run("does not remove an ancestor still used by a sibling video", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("b", "beyonce", "crazy in love"), NFO{
			Artist: "Beyoncé", Title: "Crazy in Love",
		}, false)
		place(t, root, filepath.Join("b", "beyonce", "halo-wrong"), NFO{
			Artist: "Beyoncé", Title: "Halo",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)

		_, err = Execute(plan, root, false, nil)
		require.NoError(t, err)

		// "beyonce" must survive: "Crazy in Love" was already correctly
		// placed there (a no-op) even though "Halo" moved out of a
		// differently-named sibling directory.
		assert.DirExists(t, filepath.Join(root, "b", "beyonce"))
		assert.DirExists(t, filepath.Join(root, "b", "beyonce", "crazy in love"))
		assert.DirExists(t, filepath.Join(root, "b", "beyonce", "halo"))
	})

	t.Run("no-op moves are skipped", func(t *testing.T) {
		root := t.TempDir()
		correctDir := place(t, root, filepath.Join("a", "artist", "title"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)

		var calls int
		_, err = Execute(plan, root, false, func(RenameMove) { calls++ })
		require.NoError(t, err)
		assert.Zero(t, calls)
		assert.DirExists(t, correctDir)
	})

	t.Run("progress is called once per real move", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)

		var moved []RenameMove
		_, err = Execute(plan, root, false, func(m RenameMove) { moved = append(moved, m) })
		require.NoError(t, err)
		assert.Len(t, moved, 1)
	})

	t.Run("race condition at destination is a warning, not a failure", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)

		// Simulate something appearing at the destination between planning
		// and execution.
		require.NoError(t, os.MkdirAll(plan.Moves[0].NewDir, 0o755))

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "race condition")
	})

	t.Run("sums.md5 travels with the directory when present", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, hasher.SumsFilename),
			[]byte(hashHex(t, "data")+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		newDir := plan.Moves[0].NewDir

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)

		assert.FileExists(t, filepath.Join(newDir, hasher.SumsFilename))
		_, err = os.Stat(filepath.Join(oldDir, hasher.SumsFilename))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("sums.md5 entry filename is updated in place, hash unchanged", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		videoHash := hashHex(t, "data")
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, hasher.SumsFilename),
			[]byte(videoHash+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		_, err = Execute(plan, root, false, nil)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(newDir, hasher.SumsFilename))
		require.NoError(t, err)
		assert.NotContains(t, string(got), "video.mp4")
		assert.Contains(t, string(got), videoHash+" *title.mp4\n")
	})

	t.Run("skipMD5 moves sums.md5 but does not rewrite its entry", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, hasher.SumsFilename),
			[]byte(hashHex(t, "data")+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		_, err = Execute(plan, root, true, nil)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(newDir, hasher.SumsFilename))
		require.NoError(t, err)
		assert.Contains(t, string(got), "video.mp4",
			"skipMD5 must still move the file but leave its content alone")
	})

	t.Run("no sums.md5 present is not an error", func(t *testing.T) {
		root := t.TempDir()
		place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)

		plan, err := Scan(root)
		require.NoError(t, err)

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)
	})

	t.Run("derived audio file travels with the directory", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(filepath.Join(oldDir, "video.m4a"), []byte("audio"), 0o644))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)

		assert.FileExists(t, filepath.Join(newDir, "title.m4a"))
		assert.NoFileExists(t, filepath.Join(oldDir, "video.m4a"))
	})

	t.Run("derived audio file and its sums.md5 entry are renamed when title changes", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(filepath.Join(oldDir, "video.m4a"), []byte("audio"), 0o644))
		audioHash := hashHex(t, "audio")
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, hasher.SumsFilename),
			[]byte(hashHex(t, "data")+" *video.mp4\n"+audioHash+" *video.m4a\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		_, err = Execute(plan, root, false, nil)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(newDir, hasher.SumsFilename))
		require.NoError(t, err)
		assert.NotContains(t, string(got), "video.m4a")
		assert.Contains(t, string(got), audioHash+" *title.m4a\n")
	})

	t.Run("audio.src.md5's entry is renamed alongside sums.md5 when title changes", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(filepath.Join(oldDir, "video.m4a"), []byte("audio"), 0o644))
		videoHash := hashHex(t, "data")
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, AudioSrcSumsFilename),
			[]byte(videoHash+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		result, err := Execute(plan, root, false, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Warnings)

		sums, existed, err := hasher.ReadSums(newDir, AudioSrcSumsFilename)
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, videoHash, sums["title.mp4"])
		_, oldStillPresent := sums["video.mp4"]
		assert.False(t, oldStillPresent)
	})

	t.Run("audio.src.md5 travels with the directory but its entry is untouched when only artist changes", func(t *testing.T) {
		root := t.TempDir()
		// The old directory is under the wrong artist entirely (a
		// different bucket/artist path than Correct Artist sanitizes to),
		// so a real move happens but the title (and so the sanitized
		// video filename) is the same before and after, the same
		// distinction the existing sums.md5 tests above exercise for the
		// video's entry, extended here to audio.src.md5.
		oldDir := place(t, root, filepath.Join("w", "totally wrong", "video"), NFO{
			Artist: "Correct Artist", Title: "Video",
		}, false)
		videoHash := hashHex(t, "data")
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, AudioSrcSumsFilename),
			[]byte(videoHash+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		require.Len(t, plan.Moves, 1)
		require.False(t, plan.Moves[0].IsNoOp)
		require.False(t, plan.Moves[0].IsCaseOnly)
		require.Equal(t, "video.mp4", filepath.Base(plan.Moves[0].NewVideoPath),
			"fixture must keep the video's own filename unchanged so this actually exercises the oldBase == newBase path")
		newDir := plan.Moves[0].NewDir

		_, err = Execute(plan, root, false, nil)
		require.NoError(t, err)

		sums, existed, err := hasher.ReadSums(newDir, AudioSrcSumsFilename)
		require.NoError(t, err)
		require.True(t, existed)
		assert.Equal(t, videoHash, sums["video.mp4"])
	})

	t.Run("skipMD5 moves audio.src.md5 but does not rewrite its entry", func(t *testing.T) {
		root := t.TempDir()
		oldDir := place(t, root, filepath.Join("w", "wrong", "wrong"), NFO{
			Artist: "Artist", Title: "Title",
		}, false)
		require.NoError(t, os.WriteFile(
			filepath.Join(oldDir, AudioSrcSumsFilename),
			[]byte(hashHex(t, "data")+" *video.mp4\n"),
			0o644,
		))

		plan, err := Scan(root)
		require.NoError(t, err)
		newDir := plan.Moves[0].NewDir

		_, err = Execute(plan, root, true, nil)
		require.NoError(t, err)

		got, err := os.ReadFile(filepath.Join(newDir, AudioSrcSumsFilename))
		require.NoError(t, err)
		assert.Contains(t, string(got), "video.mp4",
			"skipMD5 must still move the file but leave its content alone")
	})
}

// mustWriteVideo creates a temp source video file for use with Add.
func mustWriteVideo(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	return path
}
