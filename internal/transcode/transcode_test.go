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

package transcode

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/target"
)

// fakeRunner records the args it was invoked with, for assertions, rather
// than actually shelling out to ffmpeg.
type fakeRunner struct {
	called  bool
	gotArgs []string
	err     error
}

func (f *fakeRunner) Run(_ context.Context, args []string) error {
	f.called = true
	f.gotArgs = args
	return f.err
}

func TestTranscode(t *testing.T) {
	params := target.EncodeParams{Codec: "libmp3lame", Args: []string{"-q:a", "0"}, Ext: ".mp3"}

	t.Run("builds the expected ffmpeg arguments", func(t *testing.T) {
		r := &fakeRunner{}
		err := transcode(context.Background(), r, "src.flac", "dst.mp3", params)
		require.NoError(t, err)
		require.True(t, r.called)

		assert.Equal(t, []string{
			"-y",
			"-i", "src.flac",
			"-map_metadata", "-1",
			"-vn",
			"-c:a", "libmp3lame",
			"-q:a", "0",
			"dst.mp3",
		}, r.gotArgs)
	})

	t.Run("strips metadata and drops any embedded picture stream", func(t *testing.T) {
		r := &fakeRunner{}
		require.NoError(t, transcode(context.Background(), r, "src.flac", "dst.mp3", params))

		assert.Contains(t, r.gotArgs, "-map_metadata")
		idx := indexOf(r.gotArgs, "-map_metadata")
		require.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "-1", r.gotArgs[idx+1])

		assert.Contains(t, r.gotArgs, "-vn")
	})

	t.Run("propagates a runner failure", func(t *testing.T) {
		r := &fakeRunner{err: errors.New("boom")}
		err := transcode(context.Background(), r, "src.flac", "dst.mp3", params)
		assert.Error(t, err)
	})

	t.Run("works with encode params that have no extra Args", func(t *testing.T) {
		r := &fakeRunner{}
		bare := target.EncodeParams{Codec: "libmp3lame", Ext: ".mp3"}
		err := transcode(context.Background(), r, "src.flac", "dst.mp3", bare)
		require.NoError(t, err)
		assert.Equal(t, []string{
			"-y", "-i", "src.flac", "-map_metadata", "-1", "-vn", "-c:a", "libmp3lame", "dst.mp3",
		}, r.gotArgs)
	})
}

func indexOf(s []string, v string) int {
	for i, e := range s {
		if e == v {
			return i
		}
	}
	return -1
}
