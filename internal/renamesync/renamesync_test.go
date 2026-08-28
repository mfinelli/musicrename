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

package renamesync

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/planner"
	"github.com/mfinelli/musicrename/internal/playlist"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readSums(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, hasher.SumsFilename))
	require.NoError(t, err)
	return string(data)
}

func md5hex(content string) string {
	sum := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", sum)
}

// renamedAlbum sets up an album directory containing a single audio file at
// oldName with content, generates a real sums.md5 covering it (via
// hasher.Hash, so the fixture matches what the real tool would produce),
// then physically renames the file to newName (simulating what
// executor.Execute has already done by the time Sync is expected to run).
// It returns the album directory and the corresponding MoveOperation.
func renamedAlbum(t *testing.T, oldName, newName, content string) (string, planner.MoveOperation) {
	t.Helper()
	dir := t.TempDir()
	oldPath := filepath.Join(dir, oldName)
	newPath := filepath.Join(dir, newName)

	writeFile(t, oldPath, content)
	require.NoError(t, hasher.Hash(dir, nil))
	require.NoError(t, os.Rename(oldPath, newPath))

	return dir, planner.MoveOperation{OldPath: oldPath, NewPath: newPath}
}

func TestSync(t *testing.T) {
	t.Run("no-op moves are skipped entirely", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "01 track.flac")
		writeFile(t, path, "audio")

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: root,
			DestDir:   root,
			Moves: []planner.MoveOperation{
				{OldPath: path, NewPath: path, IsNoOp: true},
			},
		}}}

		assert.Empty(t, Sync(plan, false, false))
	})

	t.Run("directory-only move needs no follow-up", func(t *testing.T) {
		root := t.TempDir()
		oldDir := filepath.Join(root, "old-album")
		newDir := filepath.Join(root, "new-album")
		require.NoError(t, os.MkdirAll(newDir, 0o755))
		newPath := filepath.Join(newDir, "01 track.flac")
		writeFile(t, newPath, "audio")
		require.NoError(t, hasher.Hash(newDir, nil))
		before := readSums(t, newDir)

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: oldDir,
			DestDir:   newDir,
			Moves: []planner.MoveOperation{
				{OldPath: filepath.Join(oldDir, "01 track.flac"), NewPath: newPath},
			},
		}}}

		warnings := Sync(plan, false, false)
		assert.Empty(t, warnings)
		assert.Equal(t, before, readSums(t, newDir),
			"relative path unchanged; sums.md5 should not be rewritten at all")
	})

	t.Run("real rename updates sums.md5 filename, preserving the hash", func(t *testing.T) {
		dir, op := renamedAlbum(t, "01 old title.flac", "01 new title.flac", "audio bytes")
		expectedHash := md5hex("audio bytes")

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}

		warnings := Sync(plan, false, false)
		assert.Empty(t, warnings)

		got := readSums(t, dir)
		assert.NotContains(t, got, "01 old title.flac")
		assert.Contains(t, got, expectedHash+" *01 new title.flac\n")
	})

	t.Run("case-only rename is treated as a real rename", func(t *testing.T) {
		dir, op := renamedAlbum(t, "Track.flac", "track.flac", "audio")

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}

		assert.Empty(t, Sync(plan, false, false))
		got := readSums(t, dir)
		assert.NotContains(t, got, "Track.flac\n")
		assert.Contains(t, got, "track.flac\n")
	})

	t.Run("no sums.md5 present: silently does nothing, no warning", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := filepath.Join(dir, "01 old.flac")
		newPath := filepath.Join(dir, "01 new.flac")
		writeFile(t, newPath, "audio") // simulate: already moved, no sums.md5 ever existed

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{OldPath: oldPath, NewPath: newPath}},
		}}}

		assert.Empty(t, Sync(plan, false, false))
		_, err := os.Stat(filepath.Join(dir, hasher.SumsFilename))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("sums.md5 exists but lacks an entry for the old name: warns", func(t *testing.T) {
		dir := t.TempDir()
		// sums.md5 covers a different file entirely, not the one being renamed.
		writeFile(t, filepath.Join(dir, "02 other.flac"), "other")
		require.NoError(t, hasher.Hash(dir, nil))

		oldPath := filepath.Join(dir, "01 old.flac")
		newPath := filepath.Join(dir, "01 new.flac")
		writeFile(t, newPath, "audio")

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{OldPath: oldPath, NewPath: newPath}},
		}}}

		warnings := Sync(plan, false, false)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "01 old.flac")
	})

	t.Run("skipMD5 leaves sums.md5 completely untouched", func(t *testing.T) {
		dir, op := renamedAlbum(t, "01 old.flac", "01 new.flac", "audio")
		before := readSums(t, dir)

		assert.Empty(t, Sync(&planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}, true, false))

		assert.Equal(t, before, readSums(t, dir))
	})

	t.Run("race-condition skip: NewPath missing on disk is left alone", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "01 old.flac"), "audio")
		require.NoError(t, hasher.Hash(dir, nil))
		before := readSums(t, dir)

		// NewPath was never actually created (as if the executor skipped
		// this specific move due to a race condition).
		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{
				OldPath: filepath.Join(dir, "01 old.flac"),
				NewPath: filepath.Join(dir, "01 new.flac"),
			}},
		}}}

		assert.Empty(t, Sync(plan, false, false))
		assert.Equal(t, before, readSums(t, dir))
	})

	t.Run("asset (non-audio) rename updates sums.md5 but never touches manifests", func(t *testing.T) {
		dir, op := renamedAlbum(t, "Cover.jpg", "cover.jpg", "image bytes")
		// Give the album an ipod.m3u8 so we can prove it's left alone.
		require.NoError(t, playlist.WriteManifest(dir, "ipod", []string{"01 track.flac"}))

		before, err := playlist.ReadManifest(dir, "ipod")
		require.NoError(t, err)

		assert.Empty(t, Sync(&planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}, false, false))

		got := readSums(t, dir)
		assert.Contains(t, got, "cover.jpg")

		after, err := playlist.ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, before, after, "a non-audio rename must never touch playlist manifests")
	})

	t.Run("audio rename not present in any manifest: no manifest side effects", func(t *testing.T) {
		dir, op := renamedAlbum(t, "01 old.flac", "01 new.flac", "audio")
		// No manifest exists at all in this album.

		assert.Empty(t, Sync(&planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}, false, false))

		_, err := os.Stat(filepath.Join(dir, "ipod.m3u8"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("audio rename present in a manifest: manifest entry renamed and rehashed", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := filepath.Join(dir, "01 old.flac")
		writeFile(t, oldPath, "audio")
		writeFile(t, filepath.Join(dir, "02 other.flac"), "other")
		require.NoError(t, playlist.WriteManifest(dir, "ipod", []string{"01 old.flac"}))
		// sums.md5 generated with the manifest already in place, covering
		// its pre-rename content too, mirroring a real workflow.
		require.NoError(t, hasher.Hash(dir, nil))

		newPath := filepath.Join(dir, "01 new.flac")
		require.NoError(t, os.Rename(oldPath, newPath))

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{OldPath: oldPath, NewPath: newPath}},
		}}}

		warnings := Sync(plan, false, false)
		assert.Empty(t, warnings)

		names, err := playlist.ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 new.flac"}, names)

		// The manifest's own sums.md5 entry must be rehashed to match its
		// new content (this is the regression case: it must not simply
		// carry over the pre-rename hash the way the audio file's
		// filename-only rename does).
		manifestBytes, err := os.ReadFile(filepath.Join(dir, "ipod.m3u8"))
		require.NoError(t, err)
		expectedManifestHash := md5hex(string(manifestBytes))

		got := readSums(t, dir)
		assert.Contains(t, got, expectedManifestHash+"  ipod.m3u8\n")
	})

	t.Run("skipMD5 still updates the manifest but does not rehash it", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := filepath.Join(dir, "01 old.flac")
		writeFile(t, oldPath, "audio")
		require.NoError(t, playlist.WriteManifest(dir, "ipod", []string{"01 old.flac"}))
		require.NoError(t, hasher.Hash(dir, nil))
		staleSums := readSums(t, dir)

		newPath := filepath.Join(dir, "01 new.flac")
		require.NoError(t, os.Rename(oldPath, newPath))

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{OldPath: oldPath, NewPath: newPath}},
		}}}

		assert.Empty(t, Sync(plan, true, false))

		// The manifest content itself is still updated (gated only by
		// skipPlaylists, not skipMD5)...
		names, err := playlist.ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 new.flac"}, names)

		// ...but sums.md5 was explicitly opted out of and must be
		// byte-for-byte untouched, even though it's now stale relative to
		// both the renamed audio file and the rewritten manifest.
		assert.Equal(t, staleSums, readSums(t, dir))
	})

	t.Run("skipPlaylists leaves manifests untouched but still updates sums.md5", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := filepath.Join(dir, "01 old.flac")
		writeFile(t, oldPath, "audio")
		require.NoError(t, playlist.WriteManifest(dir, "ipod", []string{"01 old.flac"}))
		require.NoError(t, hasher.Hash(dir, nil))

		newPath := filepath.Join(dir, "01 new.flac")
		require.NoError(t, os.Rename(oldPath, newPath))

		plan := &planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir,
			Moves: []planner.MoveOperation{{OldPath: oldPath, NewPath: newPath}},
		}}}

		assert.Empty(t, Sync(plan, false, true))

		names, err := playlist.ReadManifest(dir, "ipod")
		require.NoError(t, err)
		assert.Equal(t, []string{"01 old.flac"}, names, "manifest must be untouched")

		got := readSums(t, dir)
		assert.Contains(t, got, "01 new.flac", "the audio file's own sums.md5 entry is still renamed")
	})

	t.Run("both flags set: Sync is a complete no-op, no filesystem reads even attempted", func(t *testing.T) {
		dir, op := renamedAlbum(t, "01 old.flac", "01 new.flac", "audio")
		before := readSums(t, dir)

		assert.Nil(t, Sync(&planner.Plan{Albums: []planner.AlbumPlan{{
			SourceDir: dir, DestDir: dir, Moves: []planner.MoveOperation{op},
		}}}, true, true))

		assert.Equal(t, before, readSums(t, dir))
	})

	t.Run("multiple albums are each processed independently", func(t *testing.T) {
		dir1, op1 := renamedAlbum(t, "01 a.flac", "01 aa.flac", "one")
		dir2, op2 := renamedAlbum(t, "01 b.flac", "01 bb.flac", "two")

		plan := &planner.Plan{Albums: []planner.AlbumPlan{
			{SourceDir: dir1, DestDir: dir1, Moves: []planner.MoveOperation{op1}},
			{SourceDir: dir2, DestDir: dir2, Moves: []planner.MoveOperation{op2}},
		}}

		assert.Empty(t, Sync(plan, false, false))
		assert.Contains(t, readSums(t, dir1), "01 aa.flac")
		assert.Contains(t, readSums(t, dir2), "01 bb.flac")
	})
}
