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
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mfinelli/musicrename/internal/sanitize"
)

// NFOFilename is the fixed sidecar filename within a video's directory.
const NFOFilename = "musicvideo.nfo"

// VideoExtensions lists every recognized video file extension without the
// leading dot (e.g. "mp4"), in a stable order. videoExts, used for fast
// lookup elsewhere in this file, is derived from this slice so the two can
// never drift apart.
var VideoExtensions = []string{"mp4", "webm", "mkv"}

// videoExts is the set of file extensions (with the leading dot) recognized
// as video files, derived from VideoExtensions.
var videoExts = dottedExtSet(VideoExtensions)

// IsVideoExt reports whether ext (including the leading dot, e.g. ".mp4")
// is a recognized video file extension. Comparison is case-insensitive.
func IsVideoExt(ext string) bool {
	return videoExts[strings.ToLower(ext)]
}

// dottedExtSet builds a lookup set (e.g. {".mp4": true}) from a slice of
// bare extensions (e.g. []string{"mp4"}), for extension slices that are
// authored without their leading dot (the form cobra's
// ShellCompDirectiveFilterFileExt expects) but are also checked against
// filepath.Ext's dotted output elsewhere.
func dottedExtSet(exts []string) map[string]bool {
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set["."+e] = true
	}
	return set
}

// DottedExtList renders a bare extension slice (e.g. VideoExtensions) as a
// comma-separated, dotted list (".mp4, .webm, .mkv") for error messages and
// help text.
func DottedExtList(exts []string) string {
	dotted := make([]string, len(exts))
	for i, e := range exts {
		dotted[i] = "." + e
	}
	return strings.Join(dotted, ", ")
}

// videoFilesIn returns every file directly inside dir with a recognized
// video extension, in directory-entry order. Shared primitive behind
// dirHasVideoFile (edit.go) and SoleVideoFile (rename.go).
func videoFilesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}

// NFO is the machine-written sidecar schema.
type NFO struct {
	XMLName xml.Name `xml:"musicvideo"`
	Title   string   `xml:"title"`
	Artist  string   `xml:"artist"`
	Album   string   `xml:"album,omitempty"`
	Year    string   `xml:"year,omitempty"`
}

// AddInput carries the raw (pre-sanitization) fields for Add. Artist and
// Title are required; Album and Year are optional.
type AddInput struct {
	// SourcePath is the absolute path to the raw video file to ingest.
	SourcePath string
	Artist     string
	Title      string
	Album      string
	Year       string
}

// AddResult describes where Add filed the video.
type AddResult struct {
	// Dir is the video's destination directory.
	Dir       string
	VideoPath string
	NFOPath   string
	// InfoPath is the destination info.txt path, or "" if no info.txt was
	// found alongside the source video to carry along.
	InfoPath string
}

// Add sanitizes Artist/Title, computes the destination under videoRoot,
// moves the source video file into place, and writes musicvideo.nfo using
// the raw (unsanitized) field values.
//
// Add errors if the destination directory already exists (there is
// no overwrite/force path); a pre-existing destination most likely means a
// duplicate import or an artist/title typo. The video file and, if present,
// an info.txt sitting alongside it (as written by video.Fetch) are moved
// into place; any other sibling files are left untouched.
func Add(videoRoot string, in AddInput) (*AddResult, error) {
	artist := strings.TrimSpace(in.Artist)
	title := strings.TrimSpace(in.Title)
	if artist == "" {
		return nil, fmt.Errorf("artist is required")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	if _, err := os.Stat(in.SourcePath); err != nil {
		return nil, fmt.Errorf("source video: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(in.SourcePath))
	if !IsVideoExt(ext) {
		return nil, fmt.Errorf(
			"unsupported video extension %q (expected one of: %s)",
			ext, DottedExtList(VideoExtensions),
		)
	}

	_, dir, videoPath, err := destination(videoRoot, artist, title, ext)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("destination already exists: %s", dir)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("checking %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}

	if err := moveVideoFile(in.SourcePath, videoPath); err != nil {
		return nil, fmt.Errorf("moving video into place: %w", err)
	}

	// Carry along an info.txt sitting next to the source video, if present
	// (this is the normal case after "video fetch"). Its absence is not an
	// error: add is also used for videos sourced some other way.
	var destInfoTxt string
	srcInfoTxt := filepath.Join(filepath.Dir(in.SourcePath), "info.txt")
	if _, statErr := os.Stat(srcInfoTxt); statErr == nil {
		destInfoTxt = filepath.Join(dir, "info.txt")
		if err := moveVideoFile(srcInfoTxt, destInfoTxt); err != nil {
			return nil, fmt.Errorf("moving info.txt into place: %w", err)
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("checking %s: %w", srcInfoTxt, statErr)
	}

	nfoPath := filepath.Join(dir, NFOFilename)
	nfo := NFO{
		Title:  title,
		Artist: artist,
		Album:  strings.TrimSpace(in.Album),
		Year:   strings.TrimSpace(in.Year),
	}
	if err := writeNFO(nfoPath, nfo); err != nil {
		return nil, fmt.Errorf("writing nfo: %w", err)
	}

	return &AddResult{Dir: dir, VideoPath: videoPath, NFOPath: nfoPath, InfoPath: destInfoTxt}, nil
}

// destination computes the bucket ("a"-"z" or "0"), target directory, and
// video file path for artist/title under videoRoot, mirroring internal/
// planner's bucketing logic for audio (shared sanitize package, shared
// bucket-override map) without the ALBUMARTISTSORT/disc/track machinery
// that doesn't apply to a single flat video. bucket is returned separately
// (rather than only embedded in dir) so callers such as Rename's dry-run
// output can group by it without re-deriving it from the computed path.
func destination(videoRoot, artist, title, ext string) (bucket, dir, videoPath string, err error) {
	sanArtist := sanitize.CleanStringResult(artist, sanitize.ArtistOverride)
	truncArtist := sanitize.Truncate(sanArtist.Value, 60)
	if truncArtist == "" {
		return "", "", "", fmt.Errorf("artist %q sanitizes to an empty string", artist)
	}

	afp, err := artistFolderPath(artist, truncArtist)
	if err != nil {
		return "", "", "", fmt.Errorf("artist path: %w", err)
	}
	bucket = strings.SplitN(afp, string(filepath.Separator), 2)[0]

	sanTitle := sanitize.CleanStringResult(title, sanitize.TrackOverride)
	truncTitle := sanitize.Truncate(sanTitle.Value, 40)
	if truncTitle == "" {
		return "", "", "", fmt.Errorf("title %q sanitizes to an empty string", title)
	}

	dir = filepath.Join(videoRoot, afp, truncTitle)
	videoPath = filepath.Join(dir, truncTitle+ext)
	return bucket, dir, videoPath, nil
}

// artistFolderPath returns the bucket+artist path component (e.g.
// "b/beyonce"), preferring the hardcoded bucket-override map (shared with
// audio) over the standard first-letter derivation. There is no
// ALBUMARTISTSORT equivalent for video so the artist string is used as
// given directly for bucketing.
func artistFolderPath(rawArtist, truncArtist string) (string, error) {
	if bucket, ok := sanitize.BucketOverride(rawArtist); ok {
		return filepath.Join(bucket, truncArtist), nil
	}
	return sanitize.GetFirstLetterPath(truncArtist)
}

// writeNFO marshals nfo via encoding/xml and writes it to path with an XML
// declaration header.
func writeNFO(path string, nfo NFO) error {
	data, err := xml.MarshalIndent(nfo, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding nfo: %w", err)
	}

	var b strings.Builder
	b.WriteString(xml.Header)
	b.Write(data)
	b.WriteString("\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// moveVideoFile moves src to dst, falling back to copy-then-delete when the
// two paths are on different filesystems (EXDEV).
//
// This intentionally duplicates internal/executor's equivalent logic rather
// than depending on it: executor operates on planner.Plan/MoveOperation, an
// audio-specific shape, and its case-only-rename handling and empty-source-
// directory cleanup do not apply here; Add's destination is always freshly
// created (never a case-only variant of an existing one), and sibling files
// left behind in the source directory (e.g. a yt-dlp thumbnail) are
// deliberately never touched, let alone cleaned up.
func moveVideoFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	if linkErr, ok := err.(*os.LinkError); ok {
		if linkErr.Err == syscall.EXDEV {
			return copyAndDeleteFile(src, dst)
		}
	}

	return err
}

// copyAndDeleteFile copies src to dst preserving file permissions, then
// removes src. Used as a fallback for cross-device moves where os.Rename
// fails. If the copy fails partway through, dst is removed to avoid leaving
// a partial file on disk.
func copyAndDeleteFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	// No defer for destFile: it is closed explicitly below so the close
	// error (where buffered writes may surface) can be captured.

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		destFile.Close()
		os.Remove(dst)
		return err
	}

	if err := destFile.Close(); err != nil {
		os.Remove(dst)
		return err
	}

	return os.Remove(src)
}
