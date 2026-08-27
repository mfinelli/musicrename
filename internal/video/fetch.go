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

// Package video implements the "completely separate" music-video tree
// described in DESIGN.md: fetching raw videos via yt-dlp, and filing them
// into the bucketed video-root hierarchy.
package video

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ytdlpRunner abstracts the actual yt-dlp invocation so Fetch's surrounding
// logic (URL cleaning, info.json parsing, info.txt writing) can be tested
// without touching the network or requiring yt-dlp to be installed. The
// production implementation (execYtdlpRunner) shells out to the real binary;
// tests supply a fake that writes canned output files instead.
type ytdlpRunner interface {
	// Run invokes yt-dlp against cleanURL, writing its output (including the
	// --write-info-json sidecar) into dir using outputTemplate as the -o
	// value.
	Run(ctx context.Context, cleanURL, outputTemplate, dir string) error
}

// execYtdlpRunner shells out to the real yt-dlp binary on PATH.
type execYtdlpRunner struct{}

func (execYtdlpRunner) Run(ctx context.Context, cleanURL, outputTemplate, dir string) error {
	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--write-info-json",
		"-o", outputTemplate,
		cleanURL,
	)
	cmd.Dir = dir
	// yt-dlp's own progress/status output is forwarded directly to the
	// terminal rather than captured; Fetch does not depend on parsing it.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ytdlpInfo is the subset of yt-dlp's --write-info-json output that Fetch
// uses. The full schema is large and shifts between yt-dlp versions; only
// fields actually consumed are declared here so unrelated schema changes
// don't break decoding.
type ytdlpInfo struct {
	WebpageURL  string `json:"webpage_url"`
	Title       string `json:"title"`
	Uploader    string `json:"uploader"`
	Channel     string `json:"channel"`
	UploadDate  string `json:"upload_date"` // YYYYMMDD
	Description string `json:"description"`
}

// Result describes the outcome of a successful Fetch.
type Result struct {
	// VideoPath is the absolute path to the downloaded video file.
	VideoPath string
	// InfoPath is the absolute path to the generated info.txt.
	InfoPath string
}

// CleanURL extracts a YouTube video id from raw and returns both a canonical
// https://www.youtube.com/watch?v=<id> URL and the bare id. Supported forms
// are youtube.com/watch?v=<id>, youtube.com/shorts/<id>,
// youtube.com/embed/<id>, and the youtu.be/<id> short-link form. Every other
// query parameter (playlist, timestamp, tracking parameters, etc.) is
// discarded.
func CleanURL(raw string) (cleanURL, id string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	switch {
	case host == "youtu.be":
		id = strings.Trim(u.Path, "/")
	case strings.HasSuffix(host, "youtube.com"):
		id = u.Query().Get("v")
		if id == "" {
			// /shorts/<id> and /embed/<id> carry the id in the path rather
			// than the "v" query parameter.
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed") {
				id = parts[1]
			}
		}
	default:
		return "", "", fmt.Errorf("unrecognized host %q", u.Hostname())
	}

	if id == "" {
		return "", "", fmt.Errorf("could not extract a video id from %q", raw)
	}

	return "https://www.youtube.com/watch?v=" + id, id, nil
}

// Fetch downloads the video at rawURL into dir via the real yt-dlp binary
// and writes a generated info.txt alongside it. dir must already exist.
func Fetch(ctx context.Context, rawURL, dir string) (*Result, error) {
	return fetch(ctx, execYtdlpRunner{}, rawURL, dir)
}

// fetch contains Fetch's logic against an injectable runner so it can be
// exercised in tests without a real yt-dlp binary or network access.
func fetch(ctx context.Context, r ytdlpRunner, rawURL, dir string) (*Result, error) {
	cleanURL, id, err := CleanURL(rawURL)
	if err != nil {
		return nil, err
	}

	// Pin the output basename to the (already known) video id so the
	// downloaded file can be located deterministically afterward, without
	// guessing at yt-dlp's own title-sanitization rules. The extension is
	// left to yt-dlp's own format selection.
	outputTemplate := id + ".%(ext)s"

	if err := r.Run(ctx, cleanURL, outputTemplate, dir); err != nil {
		return nil, fmt.Errorf("yt-dlp: %w", err)
	}

	jsonPath := filepath.Join(dir, id+".info.json")
	info, err := readInfoJSON(jsonPath)
	if err != nil {
		return nil, err
	}

	videoPath, err := findVideoFile(dir, id)
	if err != nil {
		return nil, err
	}

	infoTxtPath := filepath.Join(dir, "info.txt")
	if err := writeInfoTxt(infoTxtPath, info); err != nil {
		return nil, err
	}

	// The raw JSON is not kept: it is large, version-fragile, and
	// everything useful from it has already been folded into info.txt.
	if err := os.Remove(jsonPath); err != nil {
		return nil, fmt.Errorf("removing %s: %w", jsonPath, err)
	}

	return &Result{VideoPath: videoPath, InfoPath: infoTxtPath}, nil
}

// findVideoFile locates the single downloaded video file matching "id.*" in
// dir, excluding the "id.info.json" sidecar that --write-info-json produces.
func findVideoFile(dir, id string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, id+".*"))
	if err != nil {
		return "", fmt.Errorf("globbing for downloaded file: %w", err)
	}

	var candidates []string
	for _, m := range matches {
		if strings.EqualFold(filepath.Ext(m), ".json") {
			continue
		}
		candidates = append(candidates, m)
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no downloaded video file found for id %q in %s", id, dir)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf(
			"ambiguous download: multiple files matched id %q in %s: %v",
			id, dir, candidates,
		)
	}
}

// readInfoJSON reads and decodes yt-dlp's --write-info-json output at path.
func readInfoJSON(path string) (*ytdlpInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var info ytdlpInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return &info, nil
}

// writeInfoTxt writes a small, human-readable, labeled plain-text summary of
// info to path. This is not the raw yt-dlp JSON; only the url, title,
// uploader, and upload date as labeled fields, followed by the description.
func writeInfoTxt(path string, info *ytdlpInfo) error {
	uploader := info.Uploader
	if uploader == "" {
		uploader = info.Channel
	}

	var b strings.Builder
	fmt.Fprintf(&b, "url:      %s\n", info.WebpageURL)
	fmt.Fprintf(&b, "title:    %s\n", info.Title)
	fmt.Fprintf(&b, "uploader: %s\n", uploader)
	fmt.Fprintf(&b, "uploaded: %s\n", formatUploadDate(info.UploadDate))
	b.WriteString("\nDescription:\n")
	b.WriteString(strings.TrimRight(info.Description, "\n"))
	b.WriteString("\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// formatUploadDate converts yt-dlp's YYYYMMDD upload_date into YYYY-MM-DD.
// A value that doesn't parse as an 8-digit date is returned unchanged rather
// than causing an error: this is a display nicety, not something worth
// aborting a fetch over.
func formatUploadDate(raw string) string {
	t, err := time.Parse("20060102", raw)
	if err != nil {
		return raw
	}
	return t.Format("2006-01-02")
}
