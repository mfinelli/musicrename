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

package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/planner"
)

// makeFile creates a file at path with the given content, creating any
// necessary parent directories. It fails the test immediately on any error.
func makeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

// assertGone asserts that path does not exist on the filesystem.
func assertGone(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expected %s to not exist", path)
}

func TestExecute_BasicMoves(t *testing.T) {
	t.Run("file is moved from source to destination", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "source", "01 track one.flac")
		dst := filepath.Join(root, "a", "artist", "[2003] album", "01 track one.flac")
		makeFile(t, src, "audio data")

		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: filepath.Dir(src),
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		warnings, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.FileExists(t, dst)
		assertGone(t, src)
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "audio data", string(got))
	})

	t.Run("no-op move leaves file untouched", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "a", "artist", "[2003] album", "01 track one.flac")
		makeFile(t, file, "audio data")

		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: filepath.Dir(file),
				DestDir:   filepath.Dir(file),
				Moves: []planner.MoveOperation{
					{OldPath: file, NewPath: file, IsNoOp: true},
				},
			}},
		}

		warnings, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.FileExists(t, file)
		got, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Equal(t, "audio data", string(got))
	})

	t.Run("case-only move exercises the two-step rename path", func(t *testing.T) {
		// On Linux (case-sensitive FS) a case-change is a rename between two
		// distinct paths, which still exercises moveCaseInsensitive via Execute.
		root := t.TempDir()
		dir := filepath.Join(root, "a", "artist", "album")
		src := filepath.Join(dir, "Track One.flac")
		dst := filepath.Join(dir, "track one.flac")
		makeFile(t, src, "audio data")

		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: dir,
				DestDir:   dir,
				Moves: []planner.MoveOperation{
					{OldPath: src, NewPath: dst, IsCaseOnly: true},
				},
			}},
		}

		warnings, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.FileExists(t, dst)
		assertGone(t, src)
	})

	t.Run("artwork/ subdirectory is created automatically", func(t *testing.T) {
		// ensureDir only creates the album root; per-op MkdirAll must handle
		// artwork/, scans/, and extras/ subdirectories.
		root := t.TempDir()
		src := filepath.Join(root, "source", "cover.jpg")
		albumDest := filepath.Join(root, "a", "artist", "[2003] album")
		dst := filepath.Join(albumDest, "artwork", "cover.jpg")
		makeFile(t, src, "image data")

		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: filepath.Dir(src),
				DestDir:   albumDest,
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		warnings, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.Empty(t, warnings)
		assert.FileExists(t, dst)
		assertGone(t, src)
	})
}

func TestExecute_RaceCondition(t *testing.T) {
	t.Run("pre-existing destination emits a warning and leaves both files intact", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "source", "01 track.flac")
		dst := filepath.Join(root, "a", "artist", "album", "01 track.flac")
		makeFile(t, src, "source audio")
		makeFile(t, dst, "existing audio") // simulate file appearing after planning

		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: filepath.Dir(src),
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		warnings, err := Execute(plan, root, nil)
		require.NoError(t, err)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], dst)

		// Source must be untouched; destination must retain its original content.
		assert.FileExists(t, src)
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "existing audio", string(got))
	})
}

func TestExecute_EmptyDirCleanup(t *testing.T) {
	t.Run("empty source directory is removed after all files are moved", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "source", "album")
		src := filepath.Join(srcDir, "01 track.flac")
		makeFile(t, src, "audio")

		dst := filepath.Join(root, "a", "artist", "album", "01 track.flac")
		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: srcDir,
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		_, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assertGone(t, srcDir)
	})

	t.Run("cleanup bubbles up through empty parent directories", func(t *testing.T) {
		root := t.TempDir()
		srcParent := filepath.Join(root, "source")
		srcDir := filepath.Join(srcParent, "album")
		src := filepath.Join(srcDir, "01 track.flac")
		makeFile(t, src, "audio")

		dst := filepath.Join(root, "a", "artist", "album", "01 track.flac")
		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: srcDir,
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		_, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assertGone(t, srcDir)
		assertGone(t, srcParent)
		assert.DirExists(t, root)
	})

	t.Run("source directory with remaining files is not removed", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "source")
		src := filepath.Join(srcDir, "01 track.flac")
		leftover := filepath.Join(srcDir, "unknown.pdf")
		makeFile(t, src, "audio")
		makeFile(t, leftover, "pdf content") // not included in plan

		dst := filepath.Join(root, "a", "artist", "album", "01 track.flac")
		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: srcDir,
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		_, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.DirExists(t, srcDir)
		assert.FileExists(t, leftover)
	})

	t.Run("library root is never removed", func(t *testing.T) {
		root := t.TempDir()
		srcDir := filepath.Join(root, "source")
		src := filepath.Join(srcDir, "01 track.flac")
		makeFile(t, src, "audio")

		dst := filepath.Join(root, "a", "artist", "album", "01 track.flac")
		plan := &planner.Plan{
			Albums: []planner.AlbumPlan{{
				SourceDir: srcDir,
				DestDir:   filepath.Dir(dst),
				Moves:     []planner.MoveOperation{{OldPath: src, NewPath: dst}},
			}},
		}

		_, err := Execute(plan, root, nil)
		require.NoError(t, err)
		assert.DirExists(t, root)
	})
}

func TestCleanupEmpty(t *testing.T) {
	t.Run("removes an empty directory", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "empty")
		require.NoError(t, os.Mkdir(dir, 0755))

		cleanupEmpty(dir, root)

		assertGone(t, dir)
	})

	t.Run("stops at a non-empty directory", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "nonempty")
		makeFile(t, filepath.Join(dir, "file.txt"), "content")

		cleanupEmpty(dir, root)

		assert.DirExists(t, dir)
	})

	t.Run("never removes the library root even when empty", func(t *testing.T) {
		root := t.TempDir()

		cleanupEmpty(root, root)

		assert.DirExists(t, root)
	})

	t.Run("bubbles up through multiple empty ancestor directories", func(t *testing.T) {
		root := t.TempDir()
		deep := filepath.Join(root, "a", "b", "c")
		require.NoError(t, os.MkdirAll(deep, 0755))

		cleanupEmpty(deep, root)

		assertGone(t, filepath.Join(root, "a"))
		assert.DirExists(t, root)
	})

	t.Run("stops climbing when an ancestor is kept non-empty by a sibling", func(t *testing.T) {
		root := t.TempDir()
		makeFile(t, filepath.Join(root, "a", "sibling.txt"), "content")
		dir := filepath.Join(root, "a", "b")
		require.NoError(t, os.Mkdir(dir, 0755))

		cleanupEmpty(dir, root)

		assertGone(t, dir)
		assert.DirExists(t, filepath.Join(root, "a"))
	})
}

func TestEnsureDir(t *testing.T) {
	t.Run("creates a new directory tree when none exists", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "a", "artist", "[2003] album")

		require.NoError(t, ensureDir(dir))

		assert.DirExists(t, dir)
	})

	t.Run("is a no-op when directory already exists with correct casing", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "existing")
		require.NoError(t, os.Mkdir(dir, 0755))

		assert.NoError(t, ensureDir(dir))
		assert.DirExists(t, dir)
	})

	// NOTE: The case-mismatch correction path in ensureDir requires a
	// case-insensitive filesystem (macOS HFS+) and cannot be exercised on
	// Linux. Manual verification on macOS is required for that branch.
}

func TestCopyAndDelete(t *testing.T) {
	t.Run("copies file content to destination and removes source", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "source.flac")
		dst := filepath.Join(root, "dest.flac")
		makeFile(t, src, "audio content")

		require.NoError(t, copyAndDelete(src, dst))

		assertGone(t, src)
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "audio content", string(got))
	})

	t.Run("preserves source file permissions on destination", func(t *testing.T) {
		root := t.TempDir()
		src := filepath.Join(root, "source.flac")
		dst := filepath.Join(root, "dest.flac")
		require.NoError(t, os.WriteFile(src, []byte("data"), 0600))

		require.NoError(t, copyAndDelete(src, dst))

		info, err := os.Stat(dst)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	// NOTE: The partial-write cleanup path (removing dst when io.Copy fails
	// mid-stream) requires injecting an I/O error and cannot be triggered
	// reliably against a real filesystem without additional test infrastructure.
}

func TestMoveCaseInsensitive(t *testing.T) {
	t.Run("moves file to the new path via a temporary intermediate", func(t *testing.T) {
		// On Linux (case-sensitive FS), a case-change is a rename between two
		// distinct paths; the two-step rename logic is still fully exercised.
		root := t.TempDir()
		src := filepath.Join(root, "Track One.flac")
		dst := filepath.Join(root, "track one.flac")
		makeFile(t, src, "audio")

		op := planner.MoveOperation{OldPath: src, NewPath: dst, IsCaseOnly: true}
		require.NoError(t, moveCaseInsensitive(op))

		assert.FileExists(t, dst)
		assertGone(t, src)
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, "audio", string(got))
	})
}
