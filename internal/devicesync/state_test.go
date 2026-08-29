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

package devicesync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/playlist"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0755))
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0644))
}

func TestLibraryRoots(t *testing.T) {
	t.Run("finds every real library root, excluding reserved names and hidden dirs", func(t *testing.T) {
		root := t.TempDir()
		mkdirs(t, root, "main", "christmas", "playlists", "videos", ".git")

		roots, err := LibraryRoots(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"christmas", "main"}, roots)
	})

	t.Run("ignores files at the top level", func(t *testing.T) {
		root := t.TempDir()
		mkdirs(t, root, "main")
		touch(t, filepath.Join(root, "README.md"))

		roots, err := LibraryRoots(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"main"}, roots)
	})

	t.Run("empty library-root-root yields no roots", func(t *testing.T) {
		root := t.TempDir()
		roots, err := LibraryRoots(root)
		require.NoError(t, err)
		assert.Empty(t, roots)
	})

	t.Run("errors when libraryRootRoot does not exist", func(t *testing.T) {
		_, err := LibraryRoots(filepath.Join(t.TempDir(), "missing"))
		assert.Error(t, err)
	})
}

func TestFindPrimaryArt(t *testing.T) {
	t.Run("finds folder.jpg", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, filepath.Join(dir, "folder.jpg"))
		name, found, err := findPrimaryArt(dir)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "folder.jpg", name)
	})

	t.Run("finds folder.jpeg and folder.png too", func(t *testing.T) {
		for _, name := range []string{"folder.jpeg", "folder.png"} {
			dir := t.TempDir()
			touch(t, filepath.Join(dir, name))
			got, found, err := findPrimaryArt(dir)
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, name, got)
		}
	})

	t.Run("matches case-insensitively but returns the actual on-disk name", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, filepath.Join(dir, "Folder.JPG"))
		name, found, err := findPrimaryArt(dir)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "Folder.JPG", name)
	})

	t.Run("ignores animated art entirely", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, filepath.Join(dir, "folder.webp"))
		touch(t, filepath.Join(dir, "folder.mp4"))
		_, found, err := findPrimaryArt(dir)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("not found is not an error", func(t *testing.T) {
		dir := t.TempDir()
		_, found, err := findPrimaryArt(dir)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("a nonexistent album directory is not-found, not an error", func(t *testing.T) {
		_, found, err := findPrimaryArt(filepath.Join(t.TempDir(), "missing"))
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestDesiredState(t *testing.T) {
	t.Run("errors on an unrecognized target", func(t *testing.T) {
		root := t.TempDir()
		_, err := DesiredState(root, "chromecast")
		assert.Error(t, err)
	})

	t.Run("includes an album manifest entry", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, playlist.WriteManifest(album, "sdcard", []string{"01 track.flac"}))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "main", result.Entries[0].Root)
		assert.Equal(t, "b/beyonce/2003 album/01 track.flac", result.Entries[0].Rel)
		assert.Empty(t, result.Warnings)
	})

	t.Run("a stale manifest entry is skipped with a warning, not included", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		require.NoError(t, os.MkdirAll(album, 0755))
		require.NoError(t, playlist.WriteManifest(album, "sdcard", []string{"01 gone.flac"}))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "01 gone.flac")
	})

	t.Run("a manifest for a different target is not included", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, playlist.WriteManifest(album, "sdcard", []string{"01 track.flac"}))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
	})

	t.Run("a global playlist entry with a matching #TARGETS: is included", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "main", "a", "artist", "album", "01 track.flac"))
		mkdirs(t, root, "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrip.m3u8"),
			[]byte("#TARGETS:ipod,sdcard\nmain/a/artist/album/01 track.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "main", result.Entries[0].Root)
		assert.Equal(t, "a/artist/album/01 track.flac", result.Entries[0].Rel)
	})

	t.Run("a global playlist entry with a non-matching #TARGETS: is excluded", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "main", "a", "artist", "album", "01 track.flac"))
		mkdirs(t, root, "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrip.m3u8"),
			[]byte("#TARGETS:sdcard\nmain/a/artist/album/01 track.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
	})

	t.Run("a global playlist entry with no #TARGETS: directive applies to every target", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "main", "a", "artist", "album", "01 track.flac"))
		mkdirs(t, root, "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrip.m3u8"),
			[]byte("main/a/artist/album/01 track.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		require.Len(t, result.Entries, 1)
	})

	t.Run("the same file from both a manifest and a playlist appears once", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, playlist.WriteManifest(album, "sdcard", []string{"01 track.flac"}))

		mkdirs(t, root, "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrip.m3u8"),
			[]byte("main/a/artist/album/01 track.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		assert.Len(t, result.Entries, 1)
	})

	t.Run("an unresolvable global playlist entry is skipped with a warning", func(t *testing.T) {
		root := t.TempDir()
		mkdirs(t, root, "main", "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "roadtrip.m3u8"),
			[]byte("main/a/artist/album/missing.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		require.Len(t, result.Warnings, 1)
		assert.Contains(t, result.Warnings[0], "missing.flac")
	})

	t.Run("videos root is never scanned for manifests", func(t *testing.T) {
		root := t.TempDir()
		videoAlbum := filepath.Join(root, "videos", "a", "artist", "title")
		touch(t, filepath.Join(videoAlbum, "title.mp4"))
		require.NoError(t, playlist.WriteManifest(videoAlbum, "sdcard", []string{"title.mp4"}))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		assert.Empty(t, result.Entries, "videos/ must never contribute to desired state")
	})

	t.Run("results are sorted by (root, relative path)", func(t *testing.T) {
		root := t.TempDir()
		touch(t, filepath.Join(root, "main", "z", "artist", "album", "01 track.flac"))
		touch(t, filepath.Join(root, "christmas", "a", "artist", "album", "01 track.flac"))
		touch(t, filepath.Join(root, "main", "a", "artist", "album", "01 track.flac"))

		require.NoError(t, playlist.WriteManifest(
			filepath.Join(root, "main", "z", "artist", "album"), "sdcard", []string{"01 track.flac"},
		))
		require.NoError(t, playlist.WriteManifest(
			filepath.Join(root, "christmas", "a", "artist", "album"), "sdcard", []string{"01 track.flac"},
		))
		require.NoError(t, playlist.WriteManifest(
			filepath.Join(root, "main", "a", "artist", "album"), "sdcard", []string{"01 track.flac"},
		))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		require.Len(t, result.Entries, 3)
		assert.Equal(t, "christmas", result.Entries[0].Root)
		assert.Equal(t, "main", result.Entries[1].Root)
		assert.Equal(t, "a/artist/album/01 track.flac", result.Entries[1].Rel)
		assert.Equal(t, "main", result.Entries[2].Root)
		assert.Equal(t, "z/artist/album/01 track.flac", result.Entries[2].Rel)
	})

	t.Run("empty library produces an empty, non-error result", func(t *testing.T) {
		root := t.TempDir()
		mkdirs(t, root, "main")

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)
		assert.Empty(t, result.Entries)
		assert.Empty(t, result.Warnings)
	})

	t.Run("external-art target: an album with a selected track gets its artwork too", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.flac"))
		touch(t, filepath.Join(album, "folder.jpg"))
		require.NoError(t, playlist.WriteManifest(album, "ipod", []string{"01 track.flac"}))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)

		var rels []string
		for _, e := range result.Entries {
			rels = append(rels, e.Rel)
		}
		assert.Contains(t, rels, "b/beyonce/2003 album/01 track.flac")
		assert.Contains(t, rels, "b/beyonce/2003 album/folder.jpg")
		assert.Len(t, rels, 2)
	})

	t.Run("embedding target never gets an external artwork entry", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.mp3"))
		touch(t, filepath.Join(album, "folder.jpg"))
		require.NoError(t, playlist.WriteManifest(album, "sdcard", []string{"01 track.mp3"}))

		result, err := DesiredState(root, "sdcard")
		require.NoError(t, err)

		require.Len(t, result.Entries, 1)
		assert.Equal(t, "b/beyonce/2003 album/01 track.mp3", result.Entries[0].Rel)
	})

	t.Run("an album with zero selected tracks gets no artwork entry either", func(t *testing.T) {
		root := t.TempDir()
		// An album with artwork but no manifest at all (nothing selected).
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.flac"))
		touch(t, filepath.Join(album, "folder.jpg"))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)
		assert.Empty(t, result.Entries, "no selected tracks means no artwork either")
	})

	t.Run("an album with no artwork file present contributes no artwork entry, no warning", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "b", "beyonce", "2003 album")
		touch(t, filepath.Join(album, "01 track.flac"))
		require.NoError(t, playlist.WriteManifest(album, "ipod", []string{"01 track.flac"}))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)
		require.Len(t, result.Entries, 1)
		assert.Equal(t, "b/beyonce/2003 album/01 track.flac", result.Entries[0].Rel)
		assert.Empty(t, result.Warnings, "missing artwork is check's concern, not a DesiredState warning")
	})

	t.Run("two albums with selected tracks each get their own artwork, no cross-contamination", func(t *testing.T) {
		root := t.TempDir()
		albumA := filepath.Join(root, "main", "a", "artist-a", "album")
		albumB := filepath.Join(root, "main", "b", "artist-b", "album")
		touch(t, filepath.Join(albumA, "01 track.flac"))
		touch(t, filepath.Join(albumA, "folder.jpg"))
		touch(t, filepath.Join(albumB, "01 track.flac"))
		touch(t, filepath.Join(albumB, "folder.png"))
		require.NoError(t, playlist.WriteManifest(albumA, "ipod", []string{"01 track.flac"}))
		require.NoError(t, playlist.WriteManifest(albumB, "ipod", []string{"01 track.flac"}))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)

		var rels []string
		for _, e := range result.Entries {
			rels = append(rels, e.Rel)
		}
		assert.Contains(t, rels, "a/artist-a/album/folder.jpg")
		assert.Contains(t, rels, "b/artist-b/album/folder.png")
		assert.Len(t, rels, 4)
	})

	t.Run("artwork reached via both a manifest track and a playlist track is added once", func(t *testing.T) {
		root := t.TempDir()
		album := filepath.Join(root, "main", "a", "artist", "album")
		touch(t, filepath.Join(album, "01 track.flac"))
		touch(t, filepath.Join(album, "02 track.flac"))
		touch(t, filepath.Join(album, "folder.jpg"))
		require.NoError(t, playlist.WriteManifest(album, "ipod", []string{"01 track.flac"}))

		mkdirs(t, root, "playlists")
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "playlists", "mix.m3u8"),
			[]byte("main/a/artist/album/02 track.flac\n"),
			0644,
		))

		result, err := DesiredState(root, "ipod")
		require.NoError(t, err)

		artCount := 0
		for _, e := range result.Entries {
			if e.Rel == "a/artist/album/folder.jpg" {
				artCount++
			}
		}
		assert.Equal(t, 1, artCount)
	})
}
