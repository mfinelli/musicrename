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

package riplog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindLogs(t *testing.T) {
	touch := func(dir string, names ...string) {
		for _, n := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, n), nil, 0o644))
		}
	}

	t.Run("finds the logs in name order", func(t *testing.T) {
		dir := t.TempDir()
		touch(dir, "Album (disc 2).log", "Album (disc 1).log", "Album (disc 1).cue", "Album (disc 1).flac", "notes.txt")

		got, err := FindLogs(dir)

		require.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(dir, "Album (disc 1).log"),
			filepath.Join(dir, "Album (disc 2).log"),
		}, got)
	})

	t.Run("the extension's case does not matter", func(t *testing.T) {
		dir := t.TempDir()
		touch(dir, "A.LOG", "B.Log")

		got, err := FindLogs(dir)

		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("does not descend into subdirectories or count directories", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, "scans"), 0o755))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "looks like a.log"), 0o755))
		touch(filepath.Join(dir, "scans"), "inner.log")
		touch(dir, "outer.log")

		got, err := FindLogs(dir)

		require.NoError(t, err)
		assert.Equal(t, []string{filepath.Join(dir, "outer.log")}, got)
	})

	t.Run("a directory with no logs", func(t *testing.T) {
		dir := t.TempDir()
		touch(dir, "Album.flac")

		got, err := FindLogs(dir)

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a directory that cannot be read", func(t *testing.T) {
		_, err := FindLogs(filepath.Join(t.TempDir(), "missing"))

		assert.Error(t, err)
	})
}

func TestLogDescribe(t *testing.T) {
	t.Run("a real log", func(t *testing.T) {
		l, err := ParseLog(fixture(t, fixtureBlack))
		require.NoError(t, err)

		assert.Equal(t, "AC/DC / Back in Black (EAC 1.8, 10 tracks)", l.Describe())
	})

	t.Run("track counts", func(t *testing.T) {
		assert.Equal(t, "X (EAC 1.8, 1 track)", (&Log{Title: "X", Version: "1.8", TOC: make([]TOCTrack, 1)}).Describe())
		assert.Equal(t, "X (EAC 1.8, no tracks)", (&Log{Title: "X", Version: "1.8"}).Describe())
	})

	t.Run("no title", func(t *testing.T) {
		assert.Equal(t, "untitled (EAC 1.8, no tracks)", (&Log{Version: "1.8"}).Describe())
	})
}

func TestResultFindings(t *testing.T) {
	r := &Result{}
	r.add("a", "A", StatusPass, "ok")
	r.add("b", "B", StatusInfo, "note")
	r.add("c", "C", StatusPass, "ok")
	r.add("d", "D", StatusError, "bad")

	got := r.Findings()

	require.Len(t, got, 2)
	assert.Equal(t, "b", got[0].ID, "in the order they were made")
	assert.Equal(t, "d", got[1].ID)
	assert.Empty(t, (&Result{}).Findings())
}

func TestPassSummary(t *testing.T) {
	build := func(statuses ...Status) *Result {
		r := &Result{}
		for _, s := range statuses {
			r.add("x", "X", s, "d")
		}
		return r
	}

	assert.Equal(t, "all 3 checks passed", build(StatusPass, StatusPass, StatusPass).PassSummary())
	assert.Equal(t, "all 1 check passed", build(StatusPass).PassSummary())
	assert.Equal(t, "2 other checks passed", build(StatusPass, StatusWarning, StatusPass).PassSummary())
	assert.Equal(t, "1 other check passed", build(StatusPass, StatusError).PassSummary())
	assert.Equal(t, "no checks passed", build(StatusError, StatusWarning).PassSummary())
	assert.Equal(t, "no checks passed", build().PassSummary())

	t.Run("on real logs", func(t *testing.T) {
		assert.Equal(t, "all 21 checks passed", verify(t, fixtureText(t, fixtureBlack)).PassSummary())
		assert.Equal(t, "20 other checks passed", verify(t, fixtureText(t, fixtureBday)).PassSummary())
	})
}

func TestResultFails(t *testing.T) {
	build := func(s Status) *Result {
		r := &Result{}
		r.add("a", "A", StatusPass, "ok")
		r.add("b", "B", s, "d")
		return r
	}

	tests := []struct {
		worst         Status
		strict, plain bool
	}{
		{StatusPass, false, false},
		{StatusInfo, false, false},
		{StatusWarning, true, false},
		{StatusError, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.worst.String(), func(t *testing.T) {
			r := build(tc.worst)

			assert.Equal(t, tc.strict, r.Fails(true), "strict")
			assert.Equal(t, tc.plain, r.Fails(false), "not strict")
		})
	}
}
