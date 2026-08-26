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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanURL(t *testing.T) {
	t.Run("standard watch url", func(t *testing.T) {
		clean, id, err := CleanURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
		require.NoError(t, err)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", clean)
		assert.Equal(t, "dQw4w9WgXcQ", id)
	})

	t.Run("strips playlist and tracking params", func(t *testing.T) {
		clean, id, err := CleanURL(
			"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLxyz&t=42s&si=abc123",
		)
		require.NoError(t, err)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", clean)
		assert.Equal(t, "dQw4w9WgXcQ", id)
	})

	t.Run("youtu.be short link", func(t *testing.T) {
		clean, id, err := CleanURL("https://youtu.be/dQw4w9WgXcQ?si=abc123")
		require.NoError(t, err)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", clean)
		assert.Equal(t, "dQw4w9WgXcQ", id)
	})

	t.Run("shorts url", func(t *testing.T) {
		clean, id, err := CleanURL("https://www.youtube.com/shorts/dQw4w9WgXcQ")
		require.NoError(t, err)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", clean)
		assert.Equal(t, "dQw4w9WgXcQ", id)
	})

	t.Run("embed url", func(t *testing.T) {
		clean, id, err := CleanURL("https://www.youtube.com/embed/dQw4w9WgXcQ")
		require.NoError(t, err)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", clean)
		assert.Equal(t, "dQw4w9WgXcQ", id)
	})

	t.Run("unrecognized host", func(t *testing.T) {
		_, _, err := CleanURL("https://example.com/watch?v=dQw4w9WgXcQ")
		assert.Error(t, err)
	})

	t.Run("no id present", func(t *testing.T) {
		_, _, err := CleanURL("https://www.youtube.com/watch")
		assert.Error(t, err)
	})

	t.Run("malformed url", func(t *testing.T) {
		_, _, err := CleanURL("://not a url")
		assert.Error(t, err)
	})
}

func TestFormatUploadDate(t *testing.T) {
	assert.Equal(t, "2009-10-25", formatUploadDate("20091025"))
	// Malformed input is passed through unchanged rather than erroring.
	assert.Equal(t, "not-a-date", formatUploadDate("not-a-date"))
	assert.Equal(t, "", formatUploadDate(""))
}

// fakeRunner simulates yt-dlp by writing the info.json and video file that a
// real invocation would produce, without touching the network.
type fakeRunner struct {
	// info is marshaled to <id>.info.json.
	info ytdlpInfo
	// ext is the extension used for the fake downloaded video file (e.g.
	// "mp4"), simulating yt-dlp's format-selection choice.
	ext string
	// called records the arguments Run was invoked with, for assertions.
	called bool
	gotDir string
	gotURL string
}

func (f *fakeRunner) Run(_ context.Context, cleanURL, outputTemplate, dir string) error {
	f.called = true
	f.gotURL = cleanURL
	f.gotDir = dir

	// The real yt-dlp expands outputTemplate itself; the fake only needs to
	// know the id, which is the fixed prefix Fetch always uses.
	id := outputTemplate[:len(outputTemplate)-len(".%(ext)s")]

	data, err := json.Marshal(f.info)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, id+".info.json"), data, 0o644); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, id+"."+f.ext), []byte("fake video data"), 0o644)
}

func TestFetch(t *testing.T) {
	t.Run("happy path writes info.txt and locates the video file", func(t *testing.T) {
		dir := t.TempDir()
		runner := &fakeRunner{
			info: ytdlpInfo{
				WebpageURL:  "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				Title:       "Never Gonna Give You Up",
				Uploader:    "Rick Astley",
				UploadDate:  "20091025",
				Description: "We're no strangers to love...\n",
			},
			ext: "mp4",
		}

		result, err := fetch(context.Background(), runner, "https://youtu.be/dQw4w9WgXcQ", dir)
		require.NoError(t, err)

		assert.True(t, runner.called)
		assert.Equal(t, "https://www.youtube.com/watch?v=dQw4w9WgXcQ", runner.gotURL)
		assert.Equal(t, dir, runner.gotDir)

		assert.Equal(t, filepath.Join(dir, "dQw4w9WgXcQ.mp4"), result.VideoPath)
		assert.FileExists(t, result.VideoPath)

		assert.Equal(t, filepath.Join(dir, "info.txt"), result.InfoPath)
		content, err := os.ReadFile(result.InfoPath)
		require.NoError(t, err)
		assert.Equal(t,
			"url:      https://www.youtube.com/watch?v=dQw4w9WgXcQ\n"+
				"title:    Never Gonna Give You Up\n"+
				"uploader: Rick Astley\n"+
				"uploaded: 2009-10-25\n"+
				"\nDescription:\nWe're no strangers to love...\n",
			string(content),
		)

		// The raw .info.json is removed once its fields have been folded
		// into info.txt.
		_, err = os.Stat(filepath.Join(dir, "dQw4w9WgXcQ.info.json"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("falls back to channel when uploader is empty", func(t *testing.T) {
		dir := t.TempDir()
		runner := &fakeRunner{
			info: ytdlpInfo{
				WebpageURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
				Title:      "A Video",
				Channel:    "Some Channel",
				UploadDate: "20091025",
			},
			ext: "webm",
		}

		result, err := fetch(context.Background(), runner, "https://youtu.be/dQw4w9WgXcQ", dir)
		require.NoError(t, err)

		content, err := os.ReadFile(result.InfoPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "uploader: Some Channel\n")
	})

	t.Run("invalid url never invokes the runner", func(t *testing.T) {
		dir := t.TempDir()
		runner := &fakeRunner{}

		_, err := fetch(context.Background(), runner, "https://example.com/not-youtube", dir)
		assert.Error(t, err)
		assert.False(t, runner.called)
	})

	t.Run("runner failure is propagated", func(t *testing.T) {
		dir := t.TempDir()
		runner := &failingRunner{}

		_, err := fetch(context.Background(), runner, "https://youtu.be/dQw4w9WgXcQ", dir)
		assert.Error(t, err)
	})

	t.Run("missing video file after a successful run is an error", func(t *testing.T) {
		dir := t.TempDir()
		// infoOnlyRunner writes the info.json but never the video file,
		// simulating e.g. a yt-dlp version that changed its output naming.
		runner := &infoOnlyRunner{info: ytdlpInfo{Title: "X"}}

		_, err := fetch(context.Background(), runner, "https://youtu.be/dQw4w9WgXcQ", dir)
		assert.Error(t, err)
	})
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, string, string) error {
	return assert.AnError
}

type infoOnlyRunner struct{ info ytdlpInfo }

func (r *infoOnlyRunner) Run(_ context.Context, _, outputTemplate, dir string) error {
	id := outputTemplate[:len(outputTemplate)-len(".%(ext)s")]
	data, err := json.Marshal(r.info)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".info.json"), data, 0o644)
}
