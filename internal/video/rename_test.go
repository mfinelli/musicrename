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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		result, err := Execute(plan, root, nil)
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

		_, err = Execute(plan, root, nil)
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
		_, err = Execute(plan, root, func(RenameMove) { calls++ })
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
		_, err = Execute(plan, root, func(m RenameMove) { moved = append(moved, m) })
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

		result, err := Execute(plan, root, nil)
		require.NoError(t, err)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "race condition")
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
