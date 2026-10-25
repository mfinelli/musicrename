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
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// A split is checked against a rip's log and cue sheet, in one directory, and the
// tracks, in another. The tracks here are FLAC files that are only a header, of
// the right length and with an MD5 of their own, and a fake decoder that
// reports what each header says, so that what is being tested is the check; the
// last tests split real audio and decode it with ffmpeg.

// fakeTracks is a TrackDecoder that gives what a test tells it. By default it is
// honest: each file decodes to what its header says, and the whole has the CRC
// of the log's Copy CRC.
type fakeTracks struct {
	crc      uint32
	override map[string]func(*pcm.Fingerprint) // by the file's name
	err      error
	calls    [][]string
}

func (f *fakeTracks) Fingerprints(_ context.Context, paths ...string) (pcm.Fingerprint, []pcm.Fingerprint, error) {
	f.calls = append(f.calls, paths)

	total := pcm.Fingerprint{CRC32: f.crc}
	var files []pcm.Fingerprint
	for _, p := range paths {
		si, err := readStreamInfo(p)
		if err != nil {
			return total, files, err
		}
		fp := pcm.Fingerprint{Bytes: int64(si.TotalSamples) * 4, MD5: si.MD5}
		if mod := f.override[filepath.Base(p)]; mod != nil {
			mod(&fp)
		}
		files = append(files, fp)
		total.Bytes += fp.Bytes
	}
	return total, files, f.err
}

// fileSpec is a track file of a test split: its name and how many samples it holds.
type fileSpec struct {
	name    string
	samples uint64
}

// makeTracks writes a track file for each spec into a new directory, each only
// a header with its length and an MD5 of its own, and returns the directory.
func makeTracks(t *testing.T, specs []fileSpec) string {
	t.Helper()
	dir := t.TempDir()
	for _, s := range specs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, s.name),
			withMD5(cdFLAC(s.samples), md5.Sum([]byte(s.name))), 0o644))
	}
	return dir
}

// honestTracks is a decoder for a rip whose tracks decode to what the rip says.
func honestTracks(t *testing.T, rip rip) *fakeTracks {
	t.Helper()
	l, err := ParseLog(mustRead(t, rip.logPath()))
	require.NoError(t, err)
	crc, err := strconv.ParseUint(l.Range.CopyCRC, 16, 32)
	require.NoError(t, err)
	return &fakeTracks{crc: uint32(crc)}
}

// checkSplit runs CheckSplit on the rip's log against the tracks in dir.
func checkSplit(t *testing.T, rip rip, dir string, dec TrackDecoder) *Result {
	t.Helper()
	_, res, err := CheckSplit(context.Background(), rip.logPath(), dir, dec)
	require.NoError(t, err)
	return res
}

// cueIndexes reads the INDEX lines of a cue sheet's text without the code under
// test: for each track number, the frames of its INDEX 00 and INDEX 01.
func cueIndexes(text string) (index00, index01 map[int]int) {
	index00, index01 = map[int]int{}, map[int]int{}
	track := regexp.MustCompile(`(?i)TRACK\s+(\d+)`)
	index := regexp.MustCompile(`INDEX\s+(\d+)\s+(\d+):(\d+):(\d+)`)
	current := 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		if m := track.FindStringSubmatch(line); m != nil {
			current, _ = strconv.Atoi(m[1])
		}
		if m := index.FindStringSubmatch(line); m != nil {
			mm, _ := strconv.Atoi(m[2])
			ss, _ := strconv.Atoi(m[3])
			ff, _ := strconv.Atoi(m[4])
			frames := (mm*60+ss)*75 + ff
			if m[1] == "00" {
				index00[current] = frames
			} else {
				index01[current] = frames
			}
		}
	}
	return index00, index01
}

// splitSpecs returns the files of a split of the disc in log, named as the
// workflow names them, with the lengths the cue sheet gives for the way the
// pregaps are kept. It works them out from the table of contents and the cue
// sheet's INDEX 00 lines directly, and not with the code under test.
//
// With a disc number the names carry it ("1-01 ..."). With hidden, the audio
// before the first track is a file of its own, numbered 00.
func splitSpecs(t *testing.T, log []byte, cue string, disc int, hidden, pregapsWithNext bool) []fileSpec {
	t.Helper()
	l, err := ParseLog(log)
	require.NoError(t, err)
	index00, index01 := cueIndexes(cue)

	begin := func(i int) int { // where track i starts, in sectors from the start of the image
		n := l.TOC[i].Number
		switch {
		case i == 0 && hidden:
			return index01[n]
		case i == 0:
			return 0
		case pregapsWithNext:
			if f, ok := index00[n]; ok {
				return f
			}
		}
		return l.TOC[i].Start
	}
	end := l.TOC[len(l.TOC)-1].End + 1

	var specs []fileSpec
	name := func(n int) string { return fmt.Sprintf("%s Track %d.flac", trackLabel(disc, n), n) }
	if hidden {
		specs = append(specs, fileSpec{name(0), uint64(index01[l.TOC[0].Number]) * samplesPerSector})
	}
	for i, tr := range l.TOC {
		next := end
		if i+1 < len(l.TOC) {
			next = begin(i + 1)
		}
		specs = append(specs, fileSpec{name(tr.Number), uint64(next-begin(i)) * samplesPerSector})
	}
	return specs
}

func TestDiscNumber(t *testing.T) {
	for name, want := range map[string]int{
		"Album.log":                             0,
		"The Central Park Concert (disc 1).log": 1,
		"The Central Park Concert (disc 2).log": 2,
		"Album (Disc 10).log":                   10,
		"Album (DISC 3).log":                    3,
		"Album (disc3).log":                     3,
		"Album (disc 2) .log":                   2,
		"Album (disc 2) extra.log":              0,
		"(disc 2) Album.log":                    0,
		"Album (disc).log":                      0,
		"Album [disc 2].log":                    0,
		"/some/dir (disc 5)/Album.log":          0,
		"/some/dir/Album (disc 4).log":          4,
	} {
		assert.Equal(t, want, discNumber(name), name)
	}
}

func TestTrackLabel(t *testing.T) {
	assert.Equal(t, "01", trackLabel(0, 1))
	assert.Equal(t, "00", trackLabel(0, 0))
	assert.Equal(t, "12", trackLabel(0, 12))
	assert.Equal(t, "1-01", trackLabel(1, 1))
	assert.Equal(t, "2-00", trackLabel(2, 0))
	assert.Equal(t, "10-09", trackLabel(10, 9))
}

func TestFindTrackFiles(t *testing.T) {
	write := func(t *testing.T, names ...string) string {
		t.Helper()
		dir := t.TempDir()
		for _, n := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, n), nil, 0o644))
		}
		return dir
	}
	numbers := func(m map[int][]string) []int {
		var out []int
		for n := range m {
			out = append(out, n)
		}
		sort.Ints(out)
		return out
	}

	t.Run("a single disc", func(t *testing.T) {
		dir := write(t, "00 Hidden.flac", "01 One.flac", "02 Two words.flac", "10 Ten.flac")

		got, others, err := findTrackFiles(dir, 0)

		require.NoError(t, err)
		assert.Equal(t, []int{0, 1, 2, 10}, numbers(got))
		assert.Equal(t, 0, others)
		assert.Equal(t, []string{filepath.Join(dir, "02 Two words.flac")}, got[2])
	})

	t.Run("one disc of several that share the directory", func(t *testing.T) {
		dir := write(t, "1-01 A.flac", "1-02 B.flac", "2-01 C.flac", "2-02 D.flac", "2-03 E.flac", "1-00 Hidden.flac")

		got, others, err := findTrackFiles(dir, 1)

		require.NoError(t, err)
		assert.Equal(t, []int{0, 1, 2}, numbers(got))
		assert.Equal(t, 3, others, "the other disc's")
	})

	t.Run("a disc with a number of more than one digit", func(t *testing.T) {
		dir := write(t, "10-01 A.flac", "1-01 B.flac")

		got, others, err := findTrackFiles(dir, 10)

		require.NoError(t, err)
		assert.Equal(t, []int{1}, numbers(got))
		assert.Equal(t, 1, others)
	})

	t.Run("the extension's case does not matter", func(t *testing.T) {
		dir := write(t, "01 One.FLAC", "02 Two.Flac")

		got, _, err := findTrackFiles(dir, 0)

		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, numbers(got))
	})

	t.Run("what is not named as a track is another file", func(t *testing.T) {
		dir := write(t, "01 One.flac", "1 One.flac", "001 One.flac", "01-One.flac", "01.flac", "One.flac", "01 .flac", "1-01 Other disc.flac")

		got, others, err := findTrackFiles(dir, 0)

		require.NoError(t, err)
		assert.Equal(t, []int{1}, numbers(got))
		assert.Equal(t, 7, others)
	})

	t.Run("a file with no disc number is not a track of a numbered disc", func(t *testing.T) {
		dir := write(t, "01 One.flac", "1-01 One.flac")

		got, others, err := findTrackFiles(dir, 1)

		require.NoError(t, err)
		assert.Equal(t, []int{1}, numbers(got))
		assert.Equal(t, 1, others)
	})

	t.Run("only FLAC files, and only files", func(t *testing.T) {
		dir := write(t, "01 One.flac", "01 One.mp3", "01 One.flac.md5", "cover.jpg", "notes.txt")
		require.NoError(t, os.Mkdir(filepath.Join(dir, "02 A directory.flac"), 0o755))

		got, others, err := findTrackFiles(dir, 0)

		require.NoError(t, err)
		assert.Equal(t, []int{1}, numbers(got))
		assert.Equal(t, 0, others)
	})

	t.Run("two files for one track are both kept", func(t *testing.T) {
		dir := write(t, "01 One.flac", "01 Uno.flac")

		got, _, err := findTrackFiles(dir, 0)

		require.NoError(t, err)
		assert.Len(t, got[1], 2)
	})

	t.Run("a directory that is not there", func(t *testing.T) {
		_, _, err := findTrackFiles(filepath.Join(t.TempDir(), "missing"), 0)

		assert.Error(t, err)
	})
}

func TestListLimited(t *testing.T) {
	assert.Equal(t, "", listLimited(nil))
	assert.Equal(t, "01", listLimited([]string{"01"}))
	assert.Equal(t, "01, 02, 03", listLimited([]string{"01", "02", "03"}))
	assert.Equal(t, "01, 02, 03 and 1 more", listLimited([]string{"01", "02", "03", "04"}))
	assert.Equal(t, "01, 02, 03 and 7 more", listLimited([]string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10"}))
}

func TestIsAreWasWere(t *testing.T) {
	assert.Equal(t, "is", isAre(1))
	assert.Equal(t, "are", isAre(2))
	assert.Equal(t, "was", wasWere(1))
	assert.Equal(t, "were", wasWere(3))
}

func TestCheckSplitGoodSplits(t *testing.T) {
	t.Run("a single disc with pregaps kept at the end of the track before", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, false)
		dec := honestTracks(t, rip)

		r := checkSplit(t, rip, makeTracks(t, specs), dec)

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError))
		assert.Equal(t, "10 tracks, 01 to 10", want(t, r, "split-tracks", StatusPass).Detail)
		assert.Equal(t, "all 10 tracks are 44.1 kHz, 16-bit, stereo", want(t, r, "split-format", StatusPass).Detail)
		assert.Equal(t, "the tracks, in order, decode to CRC 9FB18E2F, the log's Copy CRC, and each matches the MD5 in its header",
			want(t, r, "split-audio", StatusPass).Detail)
		assert.Equal(t, "each track is the length the cue sheet gives, with each pregap at the end of the track before it",
			want(t, r, "split-lengths", StatusPass).Detail)
		want(t, r, "checksum", StatusPass)
		want(t, r, "cue-sheet", StatusPass)
		assert.Equal(t, []string{"checksum", "cue-sheet", "split-tracks", "split-format", "split-audio", "split-lengths"}, ids(r))
	})

	t.Run("the whole of the decode is asked for, in the order of the disc", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, false)
		dir := makeTracks(t, specs)
		dec := honestTracks(t, rip)

		checkSplit(t, rip, dir, dec)

		require.Len(t, dec.calls, 1, "one decode of all of them")
		var names []string
		for _, p := range dec.calls[0] {
			names = append(names, filepath.Base(p))
		}
		assert.Equal(t, 10, len(names))
		assert.Equal(t, "01 Track 1.flac", names[0])
		assert.Equal(t, "02 Track 2.flac", names[1], "by track number, not by how the names sort")
		assert.Equal(t, "10 Track 10.flac", names[9])
	})

	t.Run("pregaps at the start of the track they lead into are a warning, not a pass", func(t *testing.T) {
		// Nothing is lost and every track is where the cue sheet puts it, but the
		// workflow appends a pregap to the track before it.
		rip := blackRip(t)
		specs := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, true)

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, "each track is the length the cue sheet gives, but with each pregap at the start of the track it leads into; it should be at the end of the track before it",
			want(t, r, "split-lengths", StatusWarning).Detail)
		assert.Equal(t, 0, r.Count(StatusError))
		assert.Equal(t, 1, r.Count(StatusWarning), "and it is the only finding")
		want(t, r, "split-audio", StatusPass)
		assert.False(t, r.Failed(), "a warning does not fail the run")
		assert.True(t, r.Fails(true), "unless --strict")
	})

	t.Run("the two ways differ for this disc, so a split is one of them", func(t *testing.T) {
		a := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, false)
		b := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, true)

		assert.NotEqual(t, a, b, "Back in Black has pregaps")
	})

	t.Run("one disc of several, which share the directory", func(t *testing.T) {
		rip := centralRip(t)
		specs := splitSpecs(t, fixture(t, fixtureCentral), cueText(t, fixtureCentralCue), 1, false, false)
		specs = append(specs, fileSpec{"2-01 Another disc.flac", 1000}, fileSpec{"2-02 Another disc.flac", 1000})

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError))
		assert.Equal(t, "6 tracks, 1-01 to 1-06", want(t, r, "split-tracks", StatusPass).Detail)
		assert.Equal(t, "2 other FLAC files in the directory are not tracks of this disc, and were left out",
			want(t, r, "split-others", StatusInfo).Detail)
		// This disc has no pregaps, so the two ways are the same, and there is
		// nothing to say about which was used.
		assert.Equal(t, "each track is the length the cue sheet gives", want(t, r, "split-lengths", StatusPass).Detail)
	})

	t.Run("other files of the disc's directory that are not FLAC are not mentioned", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, false)
		dir := makeTracks(t, specs)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "cover.jpg"), nil, 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "01 Track 1.flac.md5"), nil, 0o644))

		r := checkSplit(t, rip, dir, honestTracks(t, rip))

		_, found := r.Check("split-others")
		assert.False(t, found)
		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError))
	})
}

func TestCheckSplitHiddenTrack(t *testing.T) {
	// Transistor's audio starts at sector 12000, 2:40 in, and the cue sheet has an
	// INDEX 00 at 00:00:00 for track 1. The audio before track 1 is in a file
	// named 00.
	log, cue := fixture(t, fixtureTransistor), cueText(t, fixtureTransistorCue)

	t.Run("with a file of its own for the audio before track 1", func(t *testing.T) {
		rip := transistorRip(t)
		specs := splitSpecs(t, log, cue, 0, true, false)
		require.Equal(t, "00 Track 0.flac", specs[0].name)
		require.Equal(t, uint64(12000*588), specs[0].samples, "2:40 of audio")

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError))
		assert.Equal(t, "22 tracks, 00 and 01 to 21", want(t, r, "split-tracks", StatusPass).Detail)
		assert.Contains(t, want(t, r, "split-lengths", StatusPass).Detail, "each track is the length the cue sheet gives")
	})

	t.Run("with pregaps at the start of the track they lead into, which is a warning", func(t *testing.T) {
		rip := transistorRip(t)
		specs := splitSpecs(t, log, cue, 0, true, true)

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, 0, r.Count(StatusError))
		assert.Contains(t, want(t, r, "split-lengths", StatusWarning).Detail, "it should be at the end of the track before it")
		want(t, r, "split-tracks", StatusPass)
	})

	t.Run("a hidden track of one disc of several is 1-00", func(t *testing.T) {
		// The same disc as a disc 1 of a larger album: the files carry the disc, and
		// the audio before track 1 is 1-00.
		rip := buildRip(t, "[1997] Transistor (disc 1)", log, cue)
		specs := splitSpecs(t, log, cue, 1, true, false)
		require.Equal(t, "1-00 Track 0.flac", specs[0].name)
		specs = append(specs, fileSpec{"2-00 Another disc.flac", 1000}, fileSpec{"2-01 Another disc.flac", 1000})

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "%v", r.Findings())
		assert.Equal(t, "22 tracks, 1-00 and 1-01 to 1-21", want(t, r, "split-tracks", StatusPass).Detail)
		assert.Contains(t, want(t, r, "split-others", StatusInfo).Detail, "2 other FLAC files")
	})

	t.Run("and a file for it that is missing is reported as 1-00", func(t *testing.T) {
		rip := buildRip(t, "[1997] Transistor (disc 2)", log, cue)
		specs := splitSpecs(t, log, cue, 2, false, false)

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "split-tracks", StatusWarning).Detail, "there is no 2-00 file")
	})

	t.Run("with no file for it, the audio is in track 1, which is a warning", func(t *testing.T) {
		rip := transistorRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		require.Equal(t, "01 Track 1.flac", specs[0].name)
		require.Equal(t, uint64(25710)*588, specs[0].samples, "track 1 holds everything up to track 2")

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusWarning)
		assert.Contains(t, c.Detail, "21 tracks, 01 to 21")
		assert.Contains(t, c.Detail, "no 00 file")
		assert.Contains(t, c.Detail, "02:40:00 of audio before track 1")
		assert.Equal(t, 1, r.Count(StatusWarning)+r.Count(StatusError), "the audio is all there, which the rest confirm")
		want(t, r, "split-lengths", StatusPass)
		want(t, r, "split-audio", StatusPass)
	})

	t.Run("a file for it when the disc has no such audio", func(t *testing.T) {
		rip := blackRip(t)
		specs := append([]fileSpec{{"00 Hidden.flac", 5000}},
			splitSpecs(t, fixture(t, fixtureBlack), cueText(t, fixtureBlackCue), 0, false, false)...)

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Contains(t, c.Detail, "there is a file for track 00, but the disc has no audio before track 1")
		_, decoded := r.Check("split-audio")
		assert.False(t, decoded, "nothing more is checked of tracks that are not right")
	})

	t.Run("the audio before track 1 left out", func(t *testing.T) {
		// A 00 file whose audio is not all of it: every track is right and the whole
		// is short, which the audio and the length of 00 each say.
		rip := transistorRip(t)
		specs := splitSpecs(t, log, cue, 0, true, false)
		specs[0].samples -= 588 // one sector short

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 00 holds 7055412 samples")
		assert.Contains(t, c.Detail, "1 sector shorter than it should be")
	})
}

func TestCheckSplitTrackFiles(t *testing.T) {
	log, cue := fixture(t, fixtureBlack), cueText(t, fixtureBlackCue)
	good := func(t *testing.T) []fileSpec { return splitSpecs(t, log, cue, 0, false, false) }
	without := func(specs []fileSpec, names ...string) (out []fileSpec) {
		for _, s := range specs {
			keep := true
			for _, n := range names {
				keep = keep && s.name != n
			}
			if keep {
				out = append(out, s)
			}
		}
		return out
	}

	t.Run("a missing track", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, makeTracks(t, without(good(t), "04 Track 4.flac")), honestTracks(t, rip))

		assert.Equal(t, "no file for track 04", want(t, r, "split-tracks", StatusError).Detail)
	})

	t.Run("several missing tracks", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, makeTracks(t, without(good(t), "01 Track 1.flac", "05 Track 5.flac", "06 Track 6.flac", "10 Track 10.flac", "03 Track 3.flac")), honestTracks(t, rip))

		assert.Equal(t, "no file for tracks 01, 03, 05 and 2 more", want(t, r, "split-tracks", StatusError).Detail)
	})

	t.Run("a track the disc does not have", func(t *testing.T) {
		rip := blackRip(t)
		specs := append(good(t), fileSpec{"11 Bonus.flac", 1000})

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		assert.Equal(t, "files for tracks 11, which the disc does not have (it has 10)", want(t, r, "split-tracks", StatusError).Detail)
	})

	t.Run("two files for one track", func(t *testing.T) {
		rip := blackRip(t)
		specs := append(good(t), fileSpec{"03 Another name.flac", 1000})

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Contains(t, c.Detail, "2 files for track 03 (03 Another name.flac and 03 Track 3.flac)")
	})

	t.Run("a missing track and a track the disc does not have", func(t *testing.T) {
		rip := blackRip(t)
		specs := append(without(good(t), "02 Track 2.flac"), fileSpec{"12 Extra.flac", 1000})

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Contains(t, c.Detail, "files for tracks 12, which the disc does not have")
		assert.Contains(t, c.Detail, "no file for track 02")
	})

	t.Run("nothing more is checked of tracks that are not right", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)

		r := checkSplit(t, rip, makeTracks(t, without(good(t), "04 Track 4.flac")), dec)

		assert.Equal(t, []string{"checksum", "cue-sheet", "split-tracks"}, ids(r))
		assert.Empty(t, dec.calls, "no time spent decoding what is already known to be wrong")
	})

	t.Run("an empty directory", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, t.TempDir(), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Equal(t, "no file for tracks 01, 02, 03 and 7 more", c.Detail)
	})

	t.Run("the wrong disc's files", func(t *testing.T) {
		// Disc 2's tracks, with disc 1's log.
		rip := centralRip(t)
		specs := splitSpecs(t, fixture(t, fixtureCentral), cueText(t, fixtureCentralCue), 2, false, false)

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Contains(t, c.Detail, "no file for tracks 1-01, 1-02, 1-03 and 3 more")
		want(t, r, "split-others", StatusInfo)
	})

	t.Run("a directory that cannot be read", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, filepath.Join(t.TempDir(), "missing"), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "split-tracks", StatusError).Detail, "could not read")
	})
}

func TestCheckSplitFormat(t *testing.T) {
	log, cue := fixture(t, fixtureBlack), cueText(t, fixtureBlackCue)

	// formatted makes the files, with one of them written as the given header.
	formatted := func(t *testing.T, name string, header []byte) string {
		dir := makeTracks(t, splitSpecs(t, log, cue, 0, false, false))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), header, 0o644))
		return dir
	}

	t.Run("a track that is not CD audio", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)

		dir := formatted(t, "03 Track 3.flac", flacHeader(48000, 2, 16, 5000))
		r := checkSplit(t, rip, dir, dec)

		c := want(t, r, "split-format", StatusError)
		assert.Equal(t, "03 Track 3.flac: 48 kHz, 16-bit, stereo: a CD is 44.1 kHz, 16-bit, stereo", c.Detail)
		assert.Empty(t, dec.calls)
		assert.Equal(t, []string{"checksum", "cue-sheet", "split-tracks", "split-format"}, ids(r))
	})

	t.Run("a track that is not 16-bit", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, formatted(t, "02 Track 2.flac", flacHeader(44100, 2, 24, 5000)), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "split-format", StatusError).Detail, "02 Track 2.flac: 44.1 kHz, 24-bit, stereo")
	})

	t.Run("a file that is not FLAC", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, formatted(t, "05 Track 5.flac", []byte("this is not audio")), honestTracks(t, rip))

		c := want(t, r, "split-format", StatusError)
		assert.Contains(t, c.Detail, "05 Track 5.flac: not a FLAC file")
	})

	t.Run("a file that cannot be read", func(t *testing.T) {
		rip := blackRip(t)
		dir := makeTracks(t, splitSpecs(t, log, cue, 0, false, false))
		path := filepath.Join(dir, "05 Track 5.flac")
		require.NoError(t, os.Remove(path))
		require.NoError(t, os.Mkdir(path, 0o755)) // a directory with the name of a track

		r := checkSplit(t, rip, dir, honestTracks(t, rip))

		// It is not a regular file, so it is not a track at all.
		assert.Equal(t, "no file for track 05", want(t, r, "split-tracks", StatusError).Detail)
	})

	t.Run("a file that cannot be read", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can read anything")
		}
		rip := blackRip(t)
		dir := makeTracks(t, splitSpecs(t, log, cue, 0, false, false))
		path := filepath.Join(dir, "05 Track 5.flac")
		require.NoError(t, os.Chmod(path, 0))

		r := checkSplit(t, rip, dir, honestTracks(t, rip))

		assert.Contains(t, want(t, r, "split-format", StatusError).Detail, "05 Track 5.flac: cannot read")
	})

	t.Run("several problems, named a few at a time", func(t *testing.T) {
		rip := blackRip(t)
		dir := makeTracks(t, splitSpecs(t, log, cue, 0, false, false))
		for _, n := range []string{"01 Track 1.flac", "02 Track 2.flac", "03 Track 3.flac", "04 Track 4.flac", "05 Track 5.flac"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, n), flacHeader(44100, 1, 16, 5000), 0o644))
		}

		r := checkSplit(t, rip, dir, honestTracks(t, rip))

		c := want(t, r, "split-format", StatusError)
		assert.Contains(t, c.Detail, "01 Track 1.flac: 44.1 kHz, 16-bit, mono; 02 Track 2.flac")
		assert.Contains(t, c.Detail, "; and 2 more: a CD is")
	})
}

func TestCheckSplitAudio(t *testing.T) {
	log, cue := fixture(t, fixtureBlack), cueText(t, fixtureBlackCue)
	dirFor := func(t *testing.T) string { return makeTracks(t, splitSpecs(t, log, cue, 0, false, false)) }

	t.Run("audio that is not the log's", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.crc ^= 1

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "the tracks, in order, decode to CRC 9FB18E2E, not the log's Copy CRC 9FB18E2F")
		assert.Contains(t, c.Detail, "not the audio EAC ripped, or were changed after")
		assert.NotContains(t, c.Detail, "samples, where the disc has", "the length is right")
		assert.True(t, r.Failed())
	})

	t.Run("a different length is said", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[9].samples -= 10
		dec := honestTracks(t, rip)
		dec.crc ^= 1

		r := checkSplit(t, rip, makeTracks(t, specs), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "(111505370 samples, where the disc has 111505380)")
	})

	t.Run("a track that is damaged", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){
			"04 Track 4.flac": func(f *pcm.Fingerprint) { f.MD5[0] ^= 0xff },
		}

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Equal(t, "04 Track 4.flac is damaged: its audio does not match the MD5 in its header", c.Detail,
			"and the whole is as the log says")
	})

	t.Run("a track that decodes to a different length from its header", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){
			"07 Track 7.flac": func(f *pcm.Fingerprint) { f.Bytes -= 4 * 100 },
		}

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "07 Track 7.flac is damaged: it decodes to")
		assert.Contains(t, c.Detail, "in its header")
	})

	t.Run("a track whose header records no length is not damaged for it", func(t *testing.T) {
		// A FLAC written to a pipe cannot say how long it is, and records zero. What
		// it decodes to is its length, and the lengths are checked against that.
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		dir := makeTracks(t, specs)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "03 Track 3.flac"), withMD5(cdFLAC(0), md5.Sum([]byte("03 Track 3.flac"))), 0o644))
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){
			"03 Track 3.flac": func(f *pcm.Fingerprint) { f.Bytes = int64(specs[2].samples) * 4 },
		}

		r := checkSplit(t, rip, dir, dec)

		want(t, r, "split-audio", StatusPass)
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a track that stores no MD5 is not damaged for it", func(t *testing.T) {
		// What decoding gives always has an MD5; a header that has none is not
		// disagreed with.
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		dir := makeTracks(t, specs)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "08 Track 8.flac"), cdFLAC(specs[7].samples), 0o644))
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){"08 Track 8.flac": func(f *pcm.Fingerprint) { f.MD5 = testMD5 }}

		r := checkSplit(t, rip, dir, dec)

		want(t, r, "split-audio", StatusPass)
	})

	t.Run("damage to several tracks is named a few at a time", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){}
		for _, n := range []string{"01 Track 1.flac", "02 Track 2.flac", "03 Track 3.flac", "04 Track 4.flac", "05 Track 5.flac"} {
			dec.override[n] = func(f *pcm.Fingerprint) { f.MD5[3] ^= 1 }
		}

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "01 Track 1.flac is damaged")
		assert.Contains(t, c.Detail, "03 Track 3.flac is damaged")
		assert.NotContains(t, c.Detail, "04 Track 4.flac is damaged")
		assert.Contains(t, c.Detail, "; and 2 more")
	})

	t.Run("damage and audio that is not the log's are both said", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.crc ^= 0xff
		dec.override = map[string]func(*pcm.Fingerprint){"02 Track 2.flac": func(f *pcm.Fingerprint) { f.MD5[0] ^= 1 }}

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "02 Track 2.flac is damaged")
		assert.Contains(t, c.Detail, "not the log's Copy CRC")
	})

	t.Run("tracks that store no MD5", func(t *testing.T) {
		rip := blackRip(t)
		dir := dirFor(t)
		for _, n := range []string{"01 Track 1.flac", "02 Track 2.flac"} {
			si, err := readStreamInfo(filepath.Join(dir, n))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, n), cdFLAC(si.TotalSamples), 0o644))
		}

		r := checkSplit(t, rip, dir, honestTracks(t, rip))

		assert.Equal(t, "the tracks, in order, decode to CRC 9FB18E2F, the log's Copy CRC, and 8 of 10 match the MD5 in their headers, and the rest store none",
			want(t, r, "split-audio", StatusPass).Detail)
	})

	t.Run("a track that does not decode cleanly", func(t *testing.T) {
		rip := blackRip(t)
		dir := dirFor(t)
		dec := honestTracks(t, rip)
		dec.err = &pcm.DecodeError{Path: filepath.Join(dir, "06 Track 6.flac"), Message: "CRC error"}

		r := checkSplit(t, rip, dir, dec)

		assert.Equal(t, "06 Track 6.flac does not decode cleanly: CRC error", want(t, r, "split-audio", StatusError).Detail)
		assert.True(t, r.Failed())
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.err = pcm.ErrNoFFmpeg

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusWarning)
		assert.Contains(t, c.Detail, "ffmpeg was not found")
		assert.Equal(t, 1, r.Count(StatusWarning)+r.Count(StatusError), "the lengths, from the headers, are still checked")
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a decode that could not be made", func(t *testing.T) {
		rip := blackRip(t)
		dec := honestTracks(t, rip)
		dec.err = errors.New("context canceled")

		r := checkSplit(t, rip, dirFor(t), dec)

		c := want(t, r, "split-audio", StatusWarning)
		assert.Contains(t, c.Detail, "could not decode the tracks")
		assert.Contains(t, c.Detail, "context canceled")
		assert.False(t, r.Failed())
	})

	t.Run("with no decoder the tracks are not decoded", func(t *testing.T) {
		rip := blackRip(t)

		r := checkSplit(t, rip, dirFor(t), nil)

		assert.Contains(t, want(t, r, "split-audio", StatusInfo).Detail, "were not decoded")
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a CRC that cannot be compared is not a match", func(t *testing.T) {
		for name, edit := range map[string]func(string) string{
			"null samples left out": func(s string) string {
				return strings.Replace(s, "Null samples used in CRC calculations       : Yes", "Null samples used in CRC calculations       : No", 1)
			},
			"no Copy CRC": func(s string) string { return withoutLine(t, s, "Copy CRC") },
		} {
			text := edit(fixtureText(t, fixtureBlack))
			rip := buildRip(t, "Back in Black", []byte(signed(text)), cue)
			dec := &fakeTracks{crc: 0xDEADBEEF} // whatever it is, it is not compared

			r := checkSplit(t, rip, dirFor(t), dec)

			c := want(t, r, "split-audio", StatusInfo)
			assert.Contains(t, c.Detail, "the log's CRC was not compared (", name)
			assert.Contains(t, c.Detail, "each matches the MD5 in its header", name)
		}
	})

	t.Run("damage is still found when the CRC cannot be compared", func(t *testing.T) {
		text := strings.Replace(fixtureText(t, fixtureBlack), "Null samples used in CRC calculations       : Yes", "Null samples used in CRC calculations       : No", 1)
		rip := buildRip(t, "Back in Black", []byte(signed(text)), cue)
		dec := &fakeTracks{override: map[string]func(*pcm.Fingerprint){"09 Track 9.flac": func(f *pcm.Fingerprint) { f.MD5[0] ^= 1 }}}

		r := checkSplit(t, rip, dirFor(t), dec)

		assert.Equal(t, "09 Track 9.flac is damaged: its audio does not match the MD5 in its header", want(t, r, "split-audio", StatusError).Detail)
	})
}

func TestCheckSplitLengths(t *testing.T) {
	log, cue := fixture(t, fixtureBlack), cueText(t, fixtureBlackCue)

	t.Run("the audio of a split that is cut a sample late is still all there", func(t *testing.T) {
		// Moving a boundary by a sample leaves the whole as it was, so the whole
		// passes, and the lengths are what find it.
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[2].samples++ // track 3 takes a sample of track 4
		specs[3].samples--

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		want(t, r, "split-audio", StatusPass)
		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, fmt.Sprintf("track 03 holds %d samples", specs[2].samples))
		assert.Contains(t, c.Detail, "1 sample longer than it should be")
		assert.Contains(t, c.Detail, "; and 1 more track differ")
	})

	t.Run("a split cut the other way with one boundary wrong is judged against that way", func(t *testing.T) {
		// Closer to pregaps at the start of the track they lead into than to
		// the other way, so what the cue sheet gives is quoted for that.
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, true)
		specs[8].samples++ // track 9
		specs[9].samples--

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, fmt.Sprintf("track 09 holds %d samples, and the cue sheet gives %d", specs[8].samples, specs[8].samples-1))
		assert.Contains(t, c.Detail, "1 sample longer than it should be")
		assert.Contains(t, c.Detail, "; and 1 more track differ")
	})

	t.Run("a wrong track shorter than either way is measured against the nearer", func(t *testing.T) {
		// Track 2 is 8 sectors longer for pregaps at the start of the track they lead
		// into than for the other way, so this one, 5 samples under the shorter, is
		// far under the longer and nearest the shorter.
		rip := blackRip(t)
		a := splitSpecs(t, log, cue, 0, false, false)
		a[1].samples -= 5
		a[2].samples += 5

		r := checkSplit(t, rip, makeTracks(t, a), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 02 holds")
		assert.Contains(t, c.Detail, "5 samples shorter than it should be")
	})

	t.Run("a boundary a sector early", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[4].samples -= 588
		specs[5].samples += 588

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 05")
		assert.Contains(t, c.Detail, "1 sector shorter than it should be")
	})

	t.Run("both ways are given when the track has a pregap that decides it", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[0].samples++ // track 1, ahead of track 2, which has a pregap
		specs[1].samples--

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "with each pregap at the end of the track before it, or")
		assert.Contains(t, c.Detail, "with each at the start of its own")
	})

	t.Run("the last track too long, and the audio with it", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[9].samples += 588

		r := checkSplit(t, rip, makeTracks(t, specs), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 10")
		assert.Contains(t, c.Detail, "1 sector longer than it should be")
		assert.NotContains(t, c.Detail, "more")
	})

	t.Run("pregaps handled one way for some tracks and the other for others", func(t *testing.T) {
		rip := blackRip(t)
		a := splitSpecs(t, log, cue, 0, false, false)
		b := splitSpecs(t, log, cue, 0, false, true)
		mixed := append(append([]fileSpec(nil), a[:4]...), b[4:]...)
		require.NotEqual(t, a, mixed)
		require.NotEqual(t, b, mixed)

		r := checkSplit(t, rip, makeTracks(t, mixed), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "the tracks are not all cut the same way")
		assert.Contains(t, c.Detail, "have each pregap at the end of the track before it, and ")
		assert.Contains(t, c.Detail, "at the start of the track it leads into")
		assert.NotContains(t, c.Detail, "holds", "no track is wrong, it is the mix")
	})

	t.Run("a mix says which tracks are cut which way", func(t *testing.T) {
		// Tracks 1 to 4 are cut with the pregap at the start of the track it leads
		// into, and 5 onwards with it at the end of the track before. A track whose
		// length is the same either way (no pregap near it) says nothing, so the
		// groups are of the tracks that do.
		rip := blackRip(t)
		a := splitSpecs(t, log, cue, 0, false, false)
		b := splitSpecs(t, log, cue, 0, false, true)
		mixed := append(append([]fileSpec(nil), b[:4]...), a[4:]...)

		r := checkSplit(t, rip, makeTracks(t, mixed), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Regexp(t, `^the tracks are not all cut the same way: .* have each pregap at the end of the track before it, and .* at the start of the track it leads into$`, c.Detail)
		assert.Equal(t, "the tracks are not all cut the same way: 05, 06, 07 and 1 more have each pregap at the end of the track before it, "+
			"and 01, 02, 03 and 1 more at the start of the track it leads into", c.Detail)
	})

	t.Run("a track that is neither way is reported before a mix", func(t *testing.T) {
		rip := blackRip(t)
		a := splitSpecs(t, log, cue, 0, false, false)
		b := splitSpecs(t, log, cue, 0, false, true)
		mixed := append(append([]fileSpec(nil), a[:4]...), b[4:]...)
		mixed[8].samples++ // and track 9 matches neither
		mixed[9].samples--

		r := checkSplit(t, rip, makeTracks(t, mixed), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 09 holds")
		assert.Contains(t, c.Detail, "; and 1 more track differ")
		assert.NotContains(t, c.Detail, "not all cut the same way")
	})

	t.Run("the nearer of the ways is the one a wrong track is measured against", func(t *testing.T) {
		// Track 2 has a pregap, so the ways differ for it. Cut a sample past the way
		// that keeps the pregap with its own track, it is 1 sample over that and
		// many more than that over the other.
		rip := blackRip(t)
		b := splitSpecs(t, log, cue, 0, false, true)
		b[1].samples++
		b[2].samples--

		r := checkSplit(t, rip, makeTracks(t, b), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 02 holds")
		assert.Contains(t, c.Detail, "1 sample longer than it should be")
	})

	t.Run("a length that is not recorded and not decoded is not known", func(t *testing.T) {
		rip := blackRip(t)
		dir := makeTracks(t, splitSpecs(t, log, cue, 0, false, false))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "03 Track 3.flac"), withMD5(cdFLAC(0), testMD5), 0o644))

		r := checkSplit(t, rip, dir, nil)

		assert.Contains(t, want(t, r, "split-lengths", StatusInfo).Detail, "03 Track 3.flac is not known")
	})

	t.Run("the decode is believed over the header", func(t *testing.T) {
		// The header of a track says one length and the audio is another, which the
		// audio check names; the lengths are of the audio that is there.
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		dec := honestTracks(t, rip)
		dec.override = map[string]func(*pcm.Fingerprint){"05 Track 5.flac": func(f *pcm.Fingerprint) { f.Bytes -= 4 * 588 }}

		r := checkSplit(t, rip, makeTracks(t, specs), dec)

		want(t, r, "split-audio", StatusError)
		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, fmt.Sprintf("track 05 holds %d samples", specs[4].samples-588))
		assert.Contains(t, c.Detail, "1 sector shorter")
	})

	t.Run("the header is used when nothing was decoded", func(t *testing.T) {
		rip := blackRip(t)
		specs := splitSpecs(t, log, cue, 0, false, false)
		specs[0].samples += 588
		specs[1].samples -= 588

		r := checkSplit(t, rip, makeTracks(t, specs), nil)

		want(t, r, "split-lengths", StatusError)
	})
}

func TestCheckSplitCueAndLog(t *testing.T) {
	log, cue := fixture(t, fixtureBlack), cueText(t, fixtureBlackCue)
	dir := func(t *testing.T) string { return makeTracks(t, splitSpecs(t, log, cue, 0, false, false)) }

	t.Run("no cue sheet", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".cue")))
		dec := honestTracks(t, rip)

		r := checkSplit(t, rip, dir(t), dec)

		c := want(t, r, "cue-sheet", StatusError)
		assert.Contains(t, c.Detail, `no "Back in Black.cue" next to the log`)
		assert.Equal(t, []string{"checksum", "cue-sheet"}, ids(r))
		assert.Empty(t, dec.calls)
	})

	t.Run("a cue sheet that is not valid", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".cue"), []byte("INDEX 01 00:00:00\n"), 0o644))
		dec := honestTracks(t, rip)

		r := checkSplit(t, rip, dir(t), dec)

		c := want(t, r, "cue-sheet", StatusError)
		assert.Contains(t, c.Detail, "INDEX before any TRACK")
		assert.Empty(t, dec.calls)
	})

	t.Run("a cue sheet that cannot be read", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.Remove(rip.path(".cue")))
		require.NoError(t, os.Mkdir(rip.path(".cue"), 0o755)) // something of that name that is not a file

		r := checkSplit(t, rip, dir(t), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "cue-sheet", StatusError).Detail, "could not read")
	})

	t.Run("text that is not a cue sheet says nothing of the tracks", func(t *testing.T) {
		rip := blackRip(t)
		require.NoError(t, os.WriteFile(rip.path(".cue"), []byte("this is not a cue sheet"), 0o644))

		r := checkSplit(t, rip, dir(t), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "split-tracks", StatusError).Detail, "cannot tell whether there is audio before track 1")
	})

	t.Run("a cue sheet that has no INDEX 01 for the first track", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "    INDEX 01 00:00:00\n", "")

		r := checkSplit(t, rip, dir(t), honestTracks(t, rip))

		c := want(t, r, "split-tracks", StatusError)
		assert.Contains(t, c.Detail, "cannot tell whether there is audio before track 1")
	})

	t.Run("a cue sheet with no INDEX 01 for another track", func(t *testing.T) {
		rip := blackRip(t)
		rewriteCue(t, rip, "INDEX 01 17:37:12", "INDEX 05 17:37:12")

		r := checkSplit(t, rip, dir(t), honestTracks(t, rip))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "cannot tell how long the tracks should be")
		assert.Contains(t, c.Detail, "no INDEX 01 for track 5")
	})

	t.Run("a log that has been edited since EAC wrote it", func(t *testing.T) {
		// The log is what the tracks are compared with, so it has to be what EAC
		// wrote; the rest is still checked.
		text := strings.Replace(fixtureText(t, fixtureBlack), "AC/DC / Back in Black", "AC/DC / Back in Blue", 1)
		rip := buildRip(t, "Back in Black", []byte(text), cue)

		r := checkSplit(t, rip, dir(t), honestTracks(t, rip))

		assert.Contains(t, want(t, r, "checksum", StatusError).Detail, "does not match the log")
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a log that cannot be read", func(t *testing.T) {
		_, _, err := CheckSplit(context.Background(), filepath.Join(t.TempDir(), "missing.log"), t.TempDir(), nil)

		assert.ErrorContains(t, err, "could not read")
	})

	t.Run("a log that is not supported", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x.log")
		require.NoError(t, os.WriteFile(path, []byte("not a log"), 0o644))

		_, _, err := CheckSplit(context.Background(), path, t.TempDir(), nil)

		var unsupported *UnsupportedError
		assert.ErrorAs(t, err, &unsupported)
	})

	t.Run("it returns the log for the caller to describe", func(t *testing.T) {
		rip := blackRip(t)

		l, _, err := CheckSplit(context.Background(), rip.logPath(), dir(t), nil)

		require.NoError(t, err)
		assert.Equal(t, "AC/DC / Back in Black", l.Title)
	})
}

// synthetic is a disc small enough to write out: its tracks, and the cue
// sheet's INDEX 00 and INDEX 01 for each, in frames.
func synthetic(tracks []TOCTrack, index01 []int, index00 map[int]int) (*Log, *Cue) {
	c := &Cue{}
	for i, tr := range tracks {
		ct := CueTrack{Number: tr.Number, Type: "AUDIO"}
		if f, ok := index00[tr.Number]; ok {
			ct.Indexes = append(ct.Indexes, CueIndex{Number: 0, Frames: f})
		}
		ct.Indexes = append(ct.Indexes, CueIndex{Number: 1, Frames: index01[i]})
		c.Tracks = append(c.Tracks, ct)
	}
	return &Log{TOC: tracks}, c
}

func TestExpectedLengths(t *testing.T) {
	// Three tracks of 1000 sectors, and a pregap of 100 before track 3.
	tracks := []TOCTrack{{1, 0, 999}, {2, 1000, 1999}, {3, 2000, 2999}}
	l, c := synthetic(tracks, []int{0, 1000, 2000}, map[int]int{3: 1900})
	const s = samplesPerSector

	t.Run("pregaps at the end of the track before", func(t *testing.T) {
		got, err := expectedLengths(l, c, false, false)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 1000 * s, 2: 1000 * s, 3: 1000 * s}, got)
	})

	t.Run("pregaps at the start of the track they lead into", func(t *testing.T) {
		got, err := expectedLengths(l, c, false, true)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 1000 * s, 2: 900 * s, 3: 1100 * s}, got)
	})

	t.Run("a disc with no pregaps is the same either way", func(t *testing.T) {
		l, c := synthetic(tracks, []int{0, 1000, 2000}, nil)
		a, err := expectedLengths(l, c, false, false)
		require.NoError(t, err)
		b, err := expectedLengths(l, c, false, true)
		require.NoError(t, err)

		assert.Equal(t, a, b)
	})

	t.Run("audio before track 1 in a file of its own", func(t *testing.T) {
		// Track 1 starts 300 sectors in, so the image is 3300 sectors.
		tracks := []TOCTrack{{1, 300, 1299}, {2, 1300, 2299}, {3, 2300, 3299}}
		l, c := synthetic(tracks, []int{300, 1300, 2300}, map[int]int{1: 0})

		got, err := expectedLengths(l, c, true, false)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{0: 300 * s, 1: 1000 * s, 2: 1000 * s, 3: 1000 * s}, got)
	})

	t.Run("audio before track 1 in track 1", func(t *testing.T) {
		tracks := []TOCTrack{{1, 300, 1299}, {2, 1300, 2299}, {3, 2300, 3299}}
		l, c := synthetic(tracks, []int{300, 1300, 2300}, map[int]int{1: 0})

		got, err := expectedLengths(l, c, false, false)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 1300 * s, 2: 1000 * s, 3: 1000 * s}, got)
	})

	t.Run("the first track's pregap does not move it when there is a file for the audio before it", func(t *testing.T) {
		tracks := []TOCTrack{{1, 300, 1299}, {2, 1300, 2299}}
		l, c := synthetic(tracks, []int{300, 1300}, map[int]int{1: 0})

		a, err := expectedLengths(l, c, true, false)
		require.NoError(t, err)
		b, err := expectedLengths(l, c, true, true)
		require.NoError(t, err)

		assert.Equal(t, a, b, "track 1 begins at its INDEX 01 either way")
	})

	t.Run("an image that begins at track 1, leaving the audio before it out", func(t *testing.T) {
		// The cue sheet is measured from the start of the image, which is track 1.
		tracks := []TOCTrack{{1, 300, 1299}, {2, 1300, 2299}}
		l, c := synthetic(tracks, []int{0, 1000}, nil)

		got, err := expectedLengths(l, c, false, false)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 1000 * s, 2: 1000 * s}, got)
	})

	t.Run("a disc that does not start at track 1", func(t *testing.T) {
		tracks := []TOCTrack{{3, 0, 999}, {4, 1000, 1999}}
		l, c := synthetic(tracks, []int{0, 1000}, nil)

		got, err := expectedLengths(l, c, false, false)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{3: 1000 * s, 4: 1000 * s}, got)
	})

	t.Run("one track", func(t *testing.T) {
		l, c := synthetic([]TOCTrack{{1, 0, 4999}}, []int{0}, nil)

		got, err := expectedLengths(l, c, false, true)

		require.NoError(t, err)
		assert.Equal(t, map[int]int64{1: 5000 * s}, got)
	})

	t.Run("what the cue sheet cannot say", func(t *testing.T) {
		t.Run("a track it does not have", func(t *testing.T) {
			l, c := synthetic(tracks, []int{0, 1000, 2000}, nil)
			c.Tracks = c.Tracks[:2]

			_, err := expectedLengths(l, c, false, false)

			assert.ErrorContains(t, err, "no track 3")
		})
		t.Run("a track with no INDEX 01", func(t *testing.T) {
			l, c := synthetic(tracks, []int{0, 1000, 2000}, nil)
			c.Tracks[1].Indexes = nil

			_, err := expectedLengths(l, c, false, false)

			assert.ErrorContains(t, err, "no INDEX 01 for track 2")
		})
		t.Run("a track that ends before it starts", func(t *testing.T) {
			l, c := synthetic(tracks, []int{0, 2500, 2000}, nil)

			_, err := expectedLengths(l, c, false, false)

			assert.ErrorContains(t, err, "end of track")
		})
		t.Run("a first track later than the log puts it", func(t *testing.T) {
			l, c := synthetic([]TOCTrack{{1, 100, 1099}}, []int{500}, nil)

			_, err := expectedLengths(l, c, false, false)

			assert.ErrorContains(t, err, "later than the log does")
		})
	})
}

func TestCheckSplitLengthsDirectly(t *testing.T) {
	// Three tracks of 1000 sectors, and an INDEX 00 for track 3 that is before
	// track 2 even starts: nonsense, which only the second way of cutting the
	// disc depends on.
	tracks := []TOCTrack{{1, 0, 999}, {2, 1000, 1999}, {3, 2000, 2999}}
	l, c := synthetic(tracks, []int{0, 1000, 2000}, map[int]int{3: 500})
	files := []splitTrack{{1, "01 a.flac"}, {2, "02 b.flac"}, {3, "03 c.flac"}}
	const s = samplesPerSector

	t.Run("a split that fits the way that can be worked out is right", func(t *testing.T) {
		r := &Result{}

		checkSplitLengths(r, l, c, files, map[int]int64{1: 1000 * s, 2: 1000 * s, 3: 1000 * s}, 0)

		assert.Equal(t, "each track is the length the cue sheet gives, with each pregap at the end of the track before it",
			want(t, r, "split-lengths", StatusPass).Detail)
	})

	t.Run("one that fits neither is told only of the way that can be worked out", func(t *testing.T) {
		r := &Result{}

		checkSplitLengths(r, l, c, files, map[int]int64{1: 1000*s + 1, 2: 1000*s - 1, 3: 1000 * s}, 0)

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, fmt.Sprintf("track 01 holds %d samples, and the cue sheet gives %d: 1 sample longer", 1000*s+1, 1000*s))
		assert.NotContains(t, c.Detail, "or ")
	})

	t.Run("a log with no track 1 of the cue sheet's", func(t *testing.T) {
		r := &Result{}
		l, c := synthetic(tracks, []int{0, 1000, 2000}, nil)
		c.Tracks = c.Tracks[1:]

		checkSplitLengths(r, l, c, files, map[int]int64{1: 1000 * s, 2: 1000 * s, 3: 1000 * s}, 0)

		assert.Contains(t, want(t, r, "split-lengths", StatusError).Detail, "cannot tell how long the tracks should be: the cue sheet has no track 1")
	})
}

func TestImageSamples(t *testing.T) {
	t.Run("a whole disc", func(t *testing.T) {
		l, c := synthetic([]TOCTrack{{1, 0, 999}, {2, 1000, 1999}}, []int{0, 1000}, nil)

		assert.Equal(t, int64(2000)*samplesPerSector, imageSamples(l, c))
	})

	t.Run("with audio before the first track", func(t *testing.T) {
		l, c := synthetic([]TOCTrack{{1, 300, 1299}}, []int{300}, nil)

		assert.Equal(t, int64(1300)*samplesPerSector, imageSamples(l, c))
	})

	t.Run("an image that leaves it out", func(t *testing.T) {
		l, c := synthetic([]TOCTrack{{1, 300, 1299}}, []int{0}, nil)

		assert.Equal(t, int64(1000)*samplesPerSector, imageSamples(l, c))
	})

	t.Run("what cannot be worked out", func(t *testing.T) {
		l, c := synthetic([]TOCTrack{{1, 100, 1099}}, []int{500}, nil)
		assert.Equal(t, int64(0), imageSamples(l, c), "a cue sheet that starts the image before the disc")

		c.Tracks = nil
		assert.Equal(t, int64(0), imageSamples(l, c), "no track")
		assert.Equal(t, int64(0), imageSamples(&Log{}, c), "no table of contents")
	})
}

func TestPreAudioFrames(t *testing.T) {
	l, c := synthetic([]TOCTrack{{1, 300, 1299}, {2, 1300, 2299}}, []int{300, 1300}, nil)

	frames, ok := preAudioFrames(l, c)
	assert.True(t, ok)
	assert.Equal(t, 300, frames)

	_, ok = preAudioFrames(&Log{}, c)
	assert.False(t, ok)

	c.Tracks[0].Indexes = nil
	_, ok = preAudioFrames(l, c)
	assert.False(t, ok)
}

// tinyDisc is a disc of three short tracks, made to be written out, split into
// real FLAC files and decoded: a log and cue sheet for it, and the audio.
type tinyDisc struct {
	rip   rip
	pcm   []byte
	tr    []TOCTrack
	cue   string
	start int // sector track 1 starts at
}

// withTOC returns the log text with its table of contents replaced by tracks.
func withTOC(t *testing.T, text string, tracks []TOCTrack) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	row := regexp.MustCompile(`^\s+\d+\s+\|\s+[\d:.]+\s+\|`)

	var out []string
	done := false
	for i := 0; i < len(lines); i++ {
		if !done && strings.HasPrefix(strings.TrimSpace(lines[i]), "---------") && i > 0 && strings.Contains(lines[i-1], "Start sector") {
			out = append(out, lines[i])
			for _, tr := range tracks {
				out = append(out, fmt.Sprintf("      %2d  |  0:00.00 |  0:00.00 |   %6d    |  %6d   ", tr.Number, tr.Start, tr.End))
			}
			for i+1 < len(lines) && row.MatchString(lines[i+1]) {
				i++
			}
			done = true
			continue
		}
		out = append(out, lines[i])
	}
	require.True(t, done, "the table of contents should have been found")
	return strings.Join(out, "\n")
}

// newTinyDisc builds a disc whose audio starts at sector start (so that there
// are start sectors before track 1, 0 for most discs), with three tracks of 1000
// sectors, and a pregap of 100 sectors before track 3. Its Copy CRC is that of
// its audio.
func newTinyDisc(t *testing.T, start int) tinyDisc {
	t.Helper()

	tracks := []TOCTrack{{1, start, start + 999}, {2, start + 1000, start + 1999}, {3, start + 2000, start + 2999}}
	sectors := tracks[2].End + 1
	data := testAudio(sectors*samplesPerSector, 41)

	text := withTOC(t, fixtureText(t, fixtureBlack), tracks)
	text = strings.ReplaceAll(text, blackCopyCRC, fmt.Sprintf("%08X", crc32.ChecksumIEEE(data)))

	frames := func(n int) string { return msf(n) }
	var cue strings.Builder
	cue.WriteString("FILE \"Back in Black.flac\" WAVE\r\n")
	for i, tr := range tracks {
		fmt.Fprintf(&cue, "  TRACK %02d AUDIO\r\n", tr.Number)
		switch {
		case i == 0 && start > 0:
			fmt.Fprintf(&cue, "    INDEX 00 %s\r\n", frames(0))
		case tr.Number == 3:
			fmt.Fprintf(&cue, "    INDEX 00 %s\r\n", frames(tr.Start-100))
		}
		fmt.Fprintf(&cue, "    INDEX 01 %s\r\n", frames(tr.Start))
	}

	return tinyDisc{
		rip:   buildRip(t, "Back in Black", []byte(signed(text)), cue.String()),
		pcm:   data,
		tr:    tracks,
		cue:   cue.String(),
		start: start,
	}
}

// split writes the disc's audio as FLAC files named as tracks, divided at the given
// sample offsets (where each track after the first begins, and track 00 before
// the first if hidden), and returns the directory.
func (d tinyDisc) split(t *testing.T, hidden bool, boundaries []int) string {
	t.Helper()
	dir := t.TempDir()

	var names []string
	if hidden {
		names = append(names, "00 Hidden.flac")
	}
	for _, tr := range d.tr {
		names = append(names, fmt.Sprintf("%02d Track %d.flac", tr.Number, tr.Number))
	}
	require.Len(t, boundaries, len(names)-1)

	from := 0
	for i, name := range names {
		to := len(d.pcm) / 4
		if i < len(boundaries) {
			to = boundaries[i]
		}
		testutil.MakePCMFlac(t, dir, name, d.pcm[from*4:to*4])
		from = to
	}
	return dir
}

func TestCheckSplitWithFFmpeg(t *testing.T) {
	ctx := context.Background()
	const s = samplesPerSector

	run := func(t *testing.T, d tinyDisc, dir string) *Result {
		t.Helper()
		_, r, err := CheckSplit(ctx, d.rip.logPath(), dir, pcm.Decoder{})
		require.NoError(t, err)
		return r
	}

	t.Run("a split cut at each track's INDEX 01", func(t *testing.T) {
		d := newTinyDisc(t, 0)

		r := run(t, d, d.split(t, false, []int{1000 * s, 2000 * s}))

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "%v", r.Findings())
		assert.Contains(t, want(t, r, "split-audio", StatusPass).Detail, "decode to CRC "+fmt.Sprintf("%08X", crc32.ChecksumIEEE(d.pcm)))
		assert.Contains(t, want(t, r, "split-lengths", StatusPass).Detail, "pregap at the end of the track before it")
	})

	t.Run("a split cut at each pregap", func(t *testing.T) {
		d := newTinyDisc(t, 0)

		r := run(t, d, d.split(t, false, []int{1000 * s, 1900 * s}))

		// Right, and so a warning and not an error, but not how the workflow splits.
		assert.Equal(t, 0, r.Count(StatusError), "%v", r.Findings())
		assert.Contains(t, want(t, r, "split-lengths", StatusWarning).Detail, "it should be at the end of the track before it")
		assert.False(t, r.Failed())
	})

	t.Run("a boundary a sample late leaves the audio as it was, and the lengths find it", func(t *testing.T) {
		d := newTinyDisc(t, 0)

		r := run(t, d, d.split(t, false, []int{1000*s + 1, 2000 * s}))

		want(t, r, "split-audio", StatusPass)
		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 01 holds 588001 samples")
		assert.Contains(t, c.Detail, "1 sample longer than it should be")
		assert.True(t, r.Failed())
	})

	t.Run("audio before track 1 in a file of its own", func(t *testing.T) {
		d := newTinyDisc(t, 300)

		r := run(t, d, d.split(t, true, []int{300 * s, 1300 * s, 2300 * s}))

		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "%v", r.Findings())
		assert.Equal(t, "4 tracks, 00 and 01 to 03", want(t, r, "split-tracks", StatusPass).Detail)
	})

	t.Run("audio in a track that is not the log's", func(t *testing.T) {
		d := newTinyDisc(t, 0)
		other := d
		other.pcm = testAudio(len(d.pcm)/4, 42) // the same length, other samples

		r := run(t, d, other.split(t, false, []int{1000 * s, 2000 * s}))

		c := want(t, r, "split-audio", StatusError)
		assert.Contains(t, c.Detail, "not the log's Copy CRC")
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a track that is damaged", func(t *testing.T) {
		d := newTinyDisc(t, 0)
		dir := d.split(t, false, []int{1000 * s, 2000 * s})
		path := filepath.Join(dir, "02 Track 2.flac")
		data := mustRead(t, path)
		data[len(data)/2] ^= 0x04
		require.NoError(t, os.WriteFile(path, data, 0o644))

		r := run(t, d, dir)

		c := want(t, r, "split-audio", StatusError)
		assert.Regexp(t, `02 Track 2\.flac (does not decode cleanly|is damaged)`, c.Detail)
		assert.True(t, r.Failed())
	})

	t.Run("a track that is missing a sample", func(t *testing.T) {
		d := newTinyDisc(t, 0)

		r := run(t, d, d.split(t, false, []int{1000 * s, 2000*s - 1}))

		c := want(t, r, "split-lengths", StatusError)
		assert.Contains(t, c.Detail, "track 02")
		assert.Contains(t, c.Detail, "1 sample shorter than it should be")
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		d := newTinyDisc(t, 0)
		dir := d.split(t, false, []int{1000 * s, 2000 * s})
		t.Setenv("PATH", t.TempDir())

		r := run(t, d, dir)

		assert.Contains(t, want(t, r, "split-audio", StatusWarning).Detail, "ffmpeg was not found")
		want(t, r, "split-lengths", StatusPass)
		assert.False(t, r.Failed())
	})
}

func TestTrackListAndAbs(t *testing.T) {
	assert.Equal(t, "02, 04", trackList(0, []int{2, 4}))
	assert.Equal(t, "1-02, 1-04", trackList(1, []int{2, 4}))
	assert.Equal(t, "01, 02, 03 and 2 more", trackList(0, []int{1, 2, 3, 4, 5}))
	assert.Equal(t, "", trackList(0, nil))

	assert.Equal(t, int64(5), abs(5))
	assert.Equal(t, int64(5), abs(-5))
	assert.Equal(t, int64(0), abs(0))
}
