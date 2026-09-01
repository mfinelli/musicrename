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

// Package checker audits a music library for misconfigurations and deviations
// from the naming and metadata conventions enforced by the rename command.
//
// The three entry points cover the three modes exposed by the cobra command:
//
//   - [CheckLibrary] – the target is a library root (no audio files directly
//     inside). All album directories are discovered recursively and the full
//     check suite runs on each, including path-conformance checks.
//   - [CheckAlbum] – the target is a single album directory (directly contains
//     audio files). All checks run; path-conformance checks require a non-empty
//     libraryRoot argument.
//   - [CheckTrack] – the target is a single audio file. Only per-track checks
//     run; directory-level checks (artwork, sums.md5, unknown files,
//     path conformance) are skipped because album context is unavailable.
//
// All three functions return a [Result] that groups [Warning] values by album
// directory. The cobra command is responsible for presenting the warnings to
// the user; this package only collects them.
package checker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.senan.xyz/taglib"

	"github.com/mfinelli/musicrename/internal/hasher"
	"github.com/mfinelli/musicrename/internal/metadata"
	"github.com/mfinelli/musicrename/internal/planner"
	"github.com/mfinelli/musicrename/internal/playlist"
	"github.com/mfinelli/musicrename/internal/target"
)

// Warning represents a single finding discovered during a check run.
type Warning struct {
	// Path is the absolute path of the file or directory the finding
	// relates to. For album-level findings this is the album root directory;
	// for track-level findings it is the audio file path.
	Path string
	// Message describes the finding in human-readable form.
	Message string
}

// AlbumResult groups all warnings discovered for a single album directory.
type AlbumResult struct {
	// AlbumPath is the absolute path to the album directory.
	AlbumPath string
	// Warnings holds all findings for this album. Track-level warnings use
	// the audio file path; album-level warnings use the album directory path.
	Warnings []Warning
}

// Result is the complete output of a check run.
type Result struct {
	// Albums contains one entry per album directory processed during the run,
	// in the order they were discovered.
	Albums []AlbumResult
}

// HasWarnings reports whether any findings were discovered across all albums.
// The cobra command uses this to decide the process exit code.
func (r *Result) HasWarnings() bool {
	for _, a := range r.Albums {
		if len(a.Warnings) > 0 {
			return true
		}
	}
	return false
}

// CheckLibrary scans root as a library root directory, discovers all album
// directories recursively, and runs the full check suite on each. Because the
// library root is known, path-conformance checks (album directory path and
// per-file destination paths) are performed in addition to all other checks.
func CheckLibrary(root string) (*Result, error) {
	albums, err := metadata.ProcessLibrary(root)
	if err != nil {
		return nil, fmt.Errorf("scanning library at %s: %w", root, err)
	}

	result := &Result{}
	for _, album := range albums {
		ar, err := checkAlbum(album, root)
		if err != nil {
			return nil, err
		}
		result.Albums = append(result.Albums, *ar)
	}
	return result, nil
}

// CheckAlbum runs the full check suite on the album directory at albumPath.
// albumPath must directly contain audio files; if it does not, an error is
// returned. When libraryRoot is non-empty, path-conformance checks are
// performed (album directory path and per-file destination paths). When
// libraryRoot is empty, path-conformance is skipped.
func CheckAlbum(albumPath, libraryRoot string) (*AlbumResult, error) {
	albums, err := metadata.ProcessLibrary(albumPath)
	if err != nil {
		return nil, fmt.Errorf("scanning album at %s: %w", albumPath, err)
	}

	// ProcessLibrary may discover nested albums if sub-directories contain
	// audio files. We only want the album rooted at albumPath itself.
	var album *metadata.Album
	for _, a := range albums {
		if a.RootPath == albumPath {
			album = a
			break
		}
	}
	if album == nil {
		return nil, fmt.Errorf("no audio files found directly in %s", albumPath)
	}

	return checkAlbum(album, libraryRoot)
}

// CheckTrack runs track-level checks only on the single audio file at
// filePath. Directory-level checks (artwork, sums.md5, unknown files, path
// conformance) are skipped because album context is not available for a
// single-file invocation.
func CheckTrack(filePath string) (*AlbumResult, error) {
	reader := metadata.NewReader()
	track := &metadata.Track{Path: filePath}
	if err := reader.ReadTrack(track); err != nil {
		return nil, fmt.Errorf("reading track %s: %w", filePath, err)
	}

	ar := &AlbumResult{AlbumPath: filepath.Dir(filePath)}
	checkTrackTags(track, ar)
	checkTrackAudio(track, ar)
	checkTrackFilename(track, ar)
	return ar, nil
}

// checkAlbum is the internal implementation shared by CheckLibrary and
// CheckAlbum. It runs every check category for a single album.
func checkAlbum(album *metadata.Album, libraryRoot string) (*AlbumResult, error) {
	ar := &AlbumResult{AlbumPath: album.RootPath}

	for _, track := range album.Tracks {
		checkTrackTags(track, ar)
		checkTrackAudio(track, ar)
	}

	checkAlbumTags(album, ar)
	checkArtwork(album, ar)
	checkIntegrity(album, ar)
	checkUnknownFiles(album, ar)
	checkNaming(album, libraryRoot, ar)
	checkPlaylists(album, ar)

	return ar, nil
}

// checkTrackTags emits warnings for missing per-track metadata tags. It is
// called for every audio file regardless of the invocation mode.
func checkTrackTags(track *metadata.Track, ar *AlbumResult) {
	if track.Title == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing TITLE tag",
		})
	}
	if track.TrackNumber == nil {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing TRACKNUMBER tag",
		})
	}
	if track.Year == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing DATE tag",
		})
	}
	if track.Artist == "" && track.AlbumArtist == "" {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: "missing both ARTIST and ALBUMARTIST tags",
		})
	}
}

// checkTrackAudio performs a second read-only pass over each audio file to
// check for tags and properties not captured by the primary metadata scan.
//
// Specifically it checks for:
//   - missing REPLAYGAIN_TRACK_GAIN tag
//   - missing REPLAYGAIN_ALBUM_GAIN tag
//   - embedded artwork (detected via Properties().Images)
//
// This is a deliberate design choice: metadata.Track stays focused on the
// fields needed for path planning. Checker-specific audio attributes are
// read here in a separate pass rather than expanding the shared data model.
// The WASM call is read-only and inexpensive relative to the overall check run.
func checkTrackAudio(track *metadata.Track, ar *AlbumResult) {
	file, err := taglib.OpenReadOnly(track.Path)
	if err != nil {
		// The primary scan phase already records a warning for unreadable
		// files; skip silently here to avoid duplicating it.
		return
	}
	defer file.Close()

	if tags := file.Tags(); tags != nil {
		if len(tags["REPLAYGAIN_TRACK_GAIN"]) == 0 {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: "missing REPLAYGAIN_TRACK_GAIN tag",
			})
		}
		if len(tags["REPLAYGAIN_ALBUM_GAIN"]) == 0 {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: "missing REPLAYGAIN_ALBUM_GAIN tag",
			})
		}
	}

	if props := file.Properties(); len(props.Images) > 0 {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    track.Path,
			Message: fmt.Sprintf("embedded artwork detected (%d image(s))", len(props.Images)),
		})
	}
}

// checkTrackFilename warns when the file's current basename does not match
// what the rename planner would produce from its tags. This check runs in
// track mode where no library root is available, so only the filename
// component (not the full path) is compared.
//
// A synthetic library root is passed to PlanAlbum so that expected paths can
// be computed; only the resulting basename is inspected, so the root value
// does not affect the outcome.
func checkTrackFilename(track *metadata.Track, ar *AlbumResult) {
	// Resolve artist the same way ProcessLibrary does: prefer AlbumArtist,
	// fall back to Artist. If neither is set the missing-artist warning from
	// checkTrackTags already covers the problem; skip silently here.
	resolvedArtist := track.AlbumArtist
	if resolvedArtist == "" {
		resolvedArtist = track.Artist
	}
	if resolvedArtist == "" {
		return
	}

	albumDir := filepath.Dir(track.Path)
	album := metadata.NewAlbum(albumDir)
	album.ResolvedArtist = resolvedArtist
	album.Tracks = []*metadata.Track{track}

	// Any non-empty root works; we only compare basenames below.
	syntheticRoot := filepath.Dir(albumDir)
	albumPlan, err := planner.PlanAlbum(syntheticRoot, album)
	if err != nil {
		// Tag issues that prevent planning are already flagged by
		// checkTrackTags; skip silently to avoid a confusing second warning.
		return
	}

	for _, move := range albumPlan.Moves {
		if move.OldPath != track.Path {
			continue
		}
		expectedBase := filepath.Base(move.NewPath)
		actualBase := filepath.Base(track.Path)
		if expectedBase != actualBase {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    track.Path,
				Message: fmt.Sprintf("filename does not match spec (expected: %s)", expectedBase),
			})
		}
		break
	}
}

// checkAlbumTags emits warnings for album-level tag problems: inconsistent
// ALBUMARTIST or ALBUM across tracks, partial DISCNUMBER coverage, and
// duplicate track numbers within the same disc.
func checkAlbumTags(album *metadata.Album, ar *AlbumResult) {
	if len(album.Tracks) == 0 {
		return
	}

	first := album.Tracks[0]

	// Inconsistent ALBUMARTIST: all tracks must agree.
	for _, t := range album.Tracks[1:] {
		if t.AlbumArtist != first.AlbumArtist {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    album.RootPath,
				Message: "inconsistent ALBUMARTIST tags across tracks",
			})
			break
		}
	}

	// Inconsistent ALBUM: all tracks must agree.
	for _, t := range album.Tracks[1:] {
		if t.Album != first.Album {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    album.RootPath,
				Message: "inconsistent ALBUM tags across tracks",
			})
			break
		}
	}

	// Partial DISCNUMBER: if any track has the tag, all must.
	tracksWithDisc := 0
	for _, t := range album.Tracks {
		if t.DiscNumber > 0 {
			tracksWithDisc++
		}
	}
	if tracksWithDisc > 0 && tracksWithDisc < len(album.Tracks) {
		ar.Warnings = append(ar.Warnings, Warning{
			Path: album.RootPath,
			Message: fmt.Sprintf(
				"partial DISCNUMBER tags: %d of %d tracks have a disc number",
				tracksWithDisc, len(album.Tracks),
			),
		})
	}

	// Duplicate track numbers within the same disc. Tracks without a
	// TrackNumber tag (nil) are excluded (they are already flagged by
	// checkTrackTags and cannot form meaningful duplicates).
	type discTrack struct{ disc, track int }
	seen := make(map[discTrack]string) // key -> first file path that used it
	for _, t := range album.Tracks {
		if t.TrackNumber == nil {
			continue
		}
		key := discTrack{t.DiscNumber, *t.TrackNumber}
		if prev, dup := seen[key]; dup {
			var discPart string
			if t.DiscNumber > 0 {
				discPart = fmt.Sprintf(" on disc %d", t.DiscNumber)
			}
			ar.Warnings = append(ar.Warnings, Warning{
				Path: t.Path,
				Message: fmt.Sprintf(
					"duplicate track number %d%s (also used by %s)",
					*t.TrackNumber, discPart, prev,
				),
			})
		} else {
			seen[key] = t.Path
		}
	}
}

// checkArtwork warns when the album has no primary artwork file (static or
// animated), or when there's more than one of a given kind. Exactly one
// static image (folder.jpg/.jpeg/.png) paired with exactly one animated
// image/video (folder.webp/.mp4) is a supported fallback combination and
// does not produce a warning.
func checkArtwork(album *metadata.Album, ar *AlbumResult) {
	static := album.Assets[metadata.CatPrimaryArt]
	animated := album.Assets[metadata.CatPrimaryArtAnimated]

	switch {
	case len(static) == 0 && len(animated) == 0:
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: "missing primary artwork (folder.jpg, folder.png, folder.webp, or folder.mp4)",
		})
	case len(static) > 1:
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("multiple static primary artwork files found (%d)", len(static)),
		})
	case len(animated) > 1:
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("multiple animated primary artwork files found (%d)", len(animated)),
		})
	}
}

// checkIntegrity warns when sums.md5 is absent from the album directory.
// When present, it also warns about any file in the album with no
// corresponding entry recorded in sums.md5, and any entry recorded in
// sums.md5 with no corresponding file in the album. Verification of the
// checksums themselves is out of scope; the user can run `md5sum -c sums.md5`
// directly for that.
func checkIntegrity(album *metadata.Album, ar *AlbumResult) {
	sumsPath := filepath.Join(album.RootPath, hasher.SumsFilename)
	if _, err := os.Stat(sumsPath); os.IsNotExist(err) {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: "missing " + hasher.SumsFilename,
		})
		return
	}

	missingFromSums, missingOnDisk, err := hasher.DiffEntries(album.RootPath, hasher.SumsFilename)
	if err != nil {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("could not verify %s entries: %v", hasher.SumsFilename, err),
		})
		return
	}

	for _, name := range missingFromSums {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    filepath.Join(album.RootPath, name),
			Message: fmt.Sprintf("not recorded in %s", hasher.SumsFilename),
		})
	}
	for _, name := range missingOnDisk {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("%s references %q which does not exist", hasher.SumsFilename, name),
		})
	}
}

// checkUnknownFiles warns for every file that the metadata scanner
// categorised as CatUnknown. Files inside extras/ are categorised as
// CatExtras by the scanner and are never flagged here.
func checkUnknownFiles(album *metadata.Album, ar *AlbumResult) {
	for _, path := range album.Assets[metadata.CatUnknown] {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    path,
			Message: "unknown file not in extras/",
		})
	}
}

// checkNaming checks whether the album's current on-disk paths match the
// paths that the planner would compute for a fully conformant library.
//
// Filename conformance (basename only) is checked in all modes. When
// libraryRoot is non-empty (library mode), the album directory path is
// also checked against the full expected hierarchy. When libraryRoot is
// empty (album mode), a synthetic root is derived from the album's parent
// directory so that expected filenames can still be computed; the album
// directory check is skipped because the surrounding hierarchy is unknown.
//
// One warning is emitted when the album directory path does not match the
// expected destination (library mode only), and one warning per file whose
// current basename differs from its expected basename.
func checkNaming(album *metadata.Album, libraryRoot string, ar *AlbumResult) {
	// Determine the root to pass to the planner. In library mode the real
	// root gives us full-path information. In album mode we synthesise one
	// from the album's parent so filename computation still works; the
	// resulting full paths are meaningless and are never compared directly.
	root := libraryRoot
	if root == "" {
		root = filepath.Dir(album.RootPath)
	}

	albumPlan, err := planner.PlanAlbum(root, album)
	if err != nil {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("could not compute expected path for conformance check: %v", err),
		})
		return
	}

	// Album directory check requires a real library root; skip in album mode.
	if libraryRoot != "" && album.RootPath != albumPlan.DestDir {
		ar.Warnings = append(ar.Warnings, Warning{
			Path:    album.RootPath,
			Message: fmt.Sprintf("album directory does not match spec (expected: %s)", albumPlan.DestDir),
		})
	}

	for _, move := range albumPlan.Moves {
		// In library mode, IsNoOp captures full-path equality and is the
		// right signal. In album mode the synthetic root makes the full
		// paths meaningless, so compare basenames only.
		var wrong bool
		if libraryRoot != "" {
			wrong = !move.IsNoOp
		} else {
			wrong = filepath.Base(move.OldPath) != filepath.Base(move.NewPath)
		}
		if wrong {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    move.OldPath,
				Message: fmt.Sprintf("filename does not match spec (expected: %s)", filepath.Base(move.NewPath)),
			})
		}
	}
}

// checkPlaylists warns about album-local target selection manifests
// ({target}.m3u8) found in the album:
//
//   - A manifest for an unrecognized target name (e.g. a stray "xbox.m3u8"
//     which is most likely a typo or leftover cruft, since target names are a
//     small, hardcoded set, internal/target).
//   - For a manifest whose target name *is* recognized, any entry that no
//     longer matches a filename among the tracks currently found in the
//     album (the same "stale entry" condition `playlist select` detects
//     interactively, surfaced here as a passive audit finding instead). Not
//     checked for an unrecognized-target manifest, since that manifest is
//     already flagged as a whole.
//   - A duplicate entry: the same line appearing more than once. Unlike
//     the same concern in a library-wide playlist, a repeated track in an
//     album-local manifest is never legitimate because a manifest is a
//     selection of an album's tracks for one target, not an ordered mix where
//     a deliberate repeat could make sense. Re-running `playlist select` on the same
//     target already fixes it as a side effect: its selection model is
//     keyed by filename, so it can't represent (and therefore can't
//     write back) two rows for the same track. Reported once per
//     duplicated name regardless of how many times it repeats.
//
// This does not check the library-wide playlists/ tree which has no
// per-album scope and is audited separately by `musicrename playlist check`
// instead.
func checkPlaylists(album *metadata.Album, ar *AlbumResult) {
	trackNames := make(map[string]bool, len(album.Tracks))
	for _, t := range album.Tracks {
		trackNames[filepath.Base(t.Path)] = true
	}

	for _, path := range album.Assets[metadata.CatRootText] {
		if strings.ToLower(filepath.Ext(path)) != ".m3u8" {
			continue
		}

		targetName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

		if !target.Valid(targetName) {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    path,
				Message: fmt.Sprintf("playlist manifest for unrecognized target %q", targetName),
			})
			continue
		}

		names, err := playlist.ReadManifest(album.RootPath, targetName)
		if err != nil {
			ar.Warnings = append(ar.Warnings, Warning{
				Path:    path,
				Message: fmt.Sprintf("could not read manifest: %v", err),
			})
			continue
		}

		counts := make(map[string]int, len(names))
		for _, n := range names {
			counts[n]++
		}

		reportedDuplicate := make(map[string]bool, len(names))
		for _, n := range names {
			if !trackNames[n] {
				ar.Warnings = append(ar.Warnings, Warning{
					Path:    path,
					Message: fmt.Sprintf("stale entry %q: no matching track found in album", n),
				})
			}
			if counts[n] > 1 && !reportedDuplicate[n] {
				ar.Warnings = append(ar.Warnings, Warning{
					Path:    path,
					Message: fmt.Sprintf("duplicate entry %q (appears %d times)", n, counts[n]),
				})
				reportedDuplicate[n] = true
			}
		}
	}
}

// PlaylistWarning represents a single finding discovered while auditing the
// library-wide playlists/ tree, via [CheckPlaylists].
type PlaylistWarning struct {
	// Path is the absolute path of the playlist file the finding relates to.
	Path string
	// Message describes the finding in human-readable form.
	Message string
}

// PlaylistResult is the complete output of a [CheckPlaylists] run.
type PlaylistResult struct {
	Warnings []PlaylistWarning
}

// HasWarnings reports whether any findings were discovered.
func (r *PlaylistResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// CheckPlaylists audits the library-wide playlists/ tree rooted at
// libraryRootRoot: walked recursively, since playlists live flat as
// playlists/*.m3u8 and any subdirectories are purely organizational
// and reports:
//
//   - An entry whose path (relative to libraryRootRoot) does not resolve to
//     an actual file anywhere under it.
//   - An unrecognized target name inside a #TARGETS: directive.
//   - Two or more playlist files sharing the same #NAVIDROME-ID directive.
//     Under the one-file-per-playlist structure this is never legitimate, so
//     any duplicate is reported unconditionally (target scope is expressed
//     via the #TARGETS: directive inside a single canonical file).
//   - A directive (#PLAYLIST:, #NAVIDROME-ID:, #TARGETS:, or #SORT:) that
//     appears more than once within a single file. Every reader silently
//     resolves this one way or another (see [playlist.DuplicateDirectives]),
//     so it's never a hard error, only a passive finding.
//   - The file's directives appearing in a different relative order than
//     [playlist.WriteGlobalPlaylist] itself would write them in (see
//     [playlist.CheckDirectiveOrder]) which is only a consistency finding,
//     since every reader in this package is prefix-based and
//     order-independent; this exists purely so every playlist file in
//     the tree looks the same as one `musicrename` itself would have
//     produced.
//   - A missing playlists/sums.md5, and once present, any file under the
//     tree with no corresponding entry recorded in it, or any entry recorded
//     in it with no corresponding file (a pure listing comparison via
//     [hasher.DiffEntries], performed without hashing anything). Skipped
//     entirely if the playlists/ tree doesn't exist at all, or contains no
//     .m3u8 file yet, matching this function's "no directory, no findings"
//     stance rather than demanding a sums.md5 for a tree with nothing
//     meaningful in it yet.
//
// A libraryRootRoot with no playlists/ directory at all is not an error; it
// simply produces an empty PlaylistResult.
//
// This does not check album-local target manifests ({target}.m3u8 inside an
// album directory) those follow CheckAlbum/CheckLibrary's per-album scope
// model instead, via checkPlaylists.
func CheckPlaylists(libraryRootRoot string) (*PlaylistResult, error) {
	result := &PlaylistResult{}

	// id -> every playlist file path that declares it, for the duplicate
	// #NAVIDROME-ID check below.
	navidromeIDs := make(map[string][]string)

	// Tracks whether WalkTree found at least one .m3u8 file at all, so an
	// empty (or .m3u8-free) playlists/ tree doesn't get flagged for a
	// missing sums.md5 it has no real need for yet.
	var sawPlaylist bool

	err := playlist.WalkTree(libraryRootRoot, func(path string) error {
		sawPlaylist = true

		entries, err := playlist.ReadEntries(path)
		if err != nil {
			result.Warnings = append(result.Warnings, PlaylistWarning{
				Path:    path,
				Message: fmt.Sprintf("could not read playlist: %v", err),
			})
			return nil
		}
		for _, entry := range entries {
			if _, statErr := os.Stat(filepath.Join(libraryRootRoot, entry)); statErr != nil {
				result.Warnings = append(result.Warnings, PlaylistWarning{
					Path:    path,
					Message: fmt.Sprintf("entry %q does not resolve to a file", entry),
				})
			}
		}

		if id, ok, err := playlist.ReadNavidromeID(path); err == nil && ok {
			navidromeIDs[id] = append(navidromeIDs[id], path)
		}

		if names, ok, err := playlist.ReadTargets(path); err == nil && ok {
			for _, name := range names {
				if !target.Valid(name) {
					result.Warnings = append(result.Warnings, PlaylistWarning{
						Path:    path,
						Message: fmt.Sprintf("#TARGETS: references unrecognized target %q", name),
					})
				}
			}
		}

		if dups, err := playlist.DuplicateDirectives(path); err == nil {
			for _, prefix := range dups {
				result.Warnings = append(result.Warnings, PlaylistWarning{
					Path:    path,
					Message: fmt.Sprintf("duplicate %s directive", prefix),
				})
			}
		}

		if ok, got, want, err := playlist.CheckDirectiveOrder(path); err == nil && !ok {
			result.Warnings = append(result.Warnings, PlaylistWarning{
				Path: path,
				Message: fmt.Sprintf(
					"directives out of order: found %s, expected %s",
					strings.Join(got, ","), strings.Join(want, ","),
				),
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for id, paths := range navidromeIDs {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		for _, p := range paths {
			others := make([]string, 0, len(paths)-1)
			for _, other := range paths {
				if other != p {
					others = append(others, other)
				}
			}
			result.Warnings = append(result.Warnings, PlaylistWarning{
				Path: p,
				Message: fmt.Sprintf(
					"duplicate #NAVIDROME-ID %s also used by %s",
					id, strings.Join(others, ", "),
				),
			})
		}
	}

	playlistsDir := playlist.Dir(libraryRootRoot)
	if _, statErr := os.Stat(playlistsDir); statErr == nil {
		sumsPath := filepath.Join(playlistsDir, hasher.SumsFilename)
		switch _, sumsErr := os.Stat(sumsPath); {
		case os.IsNotExist(sumsErr):
			if sawPlaylist {
				result.Warnings = append(result.Warnings, PlaylistWarning{
					Path:    playlistsDir,
					Message: "missing " + hasher.SumsFilename,
				})
			}
		case sumsErr == nil:
			missingFromSums, missingOnDisk, derr := hasher.DiffEntries(playlistsDir, hasher.SumsFilename)
			if derr != nil {
				result.Warnings = append(result.Warnings, PlaylistWarning{
					Path:    playlistsDir,
					Message: fmt.Sprintf("could not verify %s entries: %v", hasher.SumsFilename, derr),
				})
			} else {
				for _, name := range missingFromSums {
					result.Warnings = append(result.Warnings, PlaylistWarning{
						Path:    filepath.Join(playlistsDir, name),
						Message: fmt.Sprintf("not recorded in %s", hasher.SumsFilename),
					})
				}
				for _, name := range missingOnDisk {
					result.Warnings = append(result.Warnings, PlaylistWarning{
						Path:    playlistsDir,
						Message: fmt.Sprintf("%s references %q which does not exist", hasher.SumsFilename, name),
					})
				}
			}
		}
	}

	sort.Slice(result.Warnings, func(i, j int) bool {
		if result.Warnings[i].Path != result.Warnings[j].Path {
			return result.Warnings[i].Path < result.Warnings[j].Path
		}
		return result.Warnings[i].Message < result.Warnings[j].Message
	})

	return result, nil
}
