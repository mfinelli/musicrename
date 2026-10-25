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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mfinelli/musicrename/internal/pcm"
	"github.com/mfinelli/musicrename/internal/testutil"
)

// cueMeta is what a cue sheet says of a track.
type cueMeta struct{ title, performer, isrc string }

// titledCue gives a cue sheet the album and track metadata a real one has.
func titledCue(cue, album, performer string, tracks []cueMeta) string {
	cue = strings.Replace(cue, "FILE ", fmt.Sprintf("PERFORMER \"%s\"\r\nTITLE \"%s\"\r\nFILE ", performer, album), 1)
	for i, m := range tracks {
		marker := fmt.Sprintf("  TRACK %02d AUDIO\r\n", i+1)
		add := marker
		if m.title != "" {
			add += fmt.Sprintf("    TITLE \"%s\"\r\n", m.title)
		}
		if m.performer != "" {
			add += fmt.Sprintf("    PERFORMER \"%s\"\r\n", m.performer)
		}
		if m.isrc != "" {
			add += fmt.Sprintf("    ISRC %s\r\n", m.isrc)
		}
		cue = strings.Replace(cue, marker, add, 1)
	}
	return cue
}

var splitMeta = []cueMeta{
	{"One", "Artist One", "AAAA00000001"},
	{"Two", "Guest", "AAAA00000002"},
	{"Three", "", ""},
}

// splitDisc is a tiny disc, with start sectors before track 1, whose cue sheet has
// the metadata of splitMeta, and whose image is real audio.
func splitDisc(t *testing.T, start int, meta []cueMeta) tinyDisc {
	t.Helper()
	d := newTinyDisc(t, start)
	d.cue = titledCue(d.cue, "The Album", "Album Artist", meta)
	require.NoError(t, os.WriteFile(d.rip.path(".cue"), []byte(d.cue), 0o644))
	testutil.MakePCMFlac(t, d.rip.dir, "Back in Black.flac", d.pcm)
	return d
}

// names lists the files in a directory, with those that are not tracks, which is
// what is left of a split that was not finished.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func releaseID(t *testing.T) string {
	t.Helper()
	id, problem := parseReleaseURL(testReleaseURL)
	require.Empty(t, problem)
	return id
}

func TestSplitWritesTheTracks(t *testing.T) {
	ctx := context.Background()
	const s = samplesPerSector
	release := releaseID(t)

	split := func(t *testing.T, d tinyDisc, opts SplitOptions) (string, *Result) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "WORKING", "The Album") // not there
		l, r, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, opts)
		require.NoError(t, err)
		require.NotNil(t, l)
		return out, r
	}
	clean := func(t *testing.T, r *Result) {
		t.Helper()
		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "%v", r.Findings())
	}
	tagsOf := func(t *testing.T, dir, name string) []string {
		t.Helper()
		_, c := testutil.FLACComments(t, filepath.Join(dir, name))
		return c
	}

	t.Run("a disc, track by track, with what the cue sheet says of each", func(t *testing.T) {
		d := splitDisc(t, 0, splitMeta)

		out, r := split(t, d, SplitOptions{})

		clean(t, r)
		assert.Equal(t, fmt.Sprintf("3 tracks written to %s", out), want(t, r, "split-write", StatusPass).Detail)
		assert.Equal(t, "the image has the CRC the log records, "+blackCopyCRCOf(t, d), want(t, r, "split-crc", StatusPass).Detail)
		assert.Equal(t, []string{"01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out), "and nothing else")

		assert.Equal(t, []string{"TITLE=One", "ARTIST=Artist One", "ALBUM=The Album", "ALBUMARTIST=Album Artist",
			"TRACKNUMBER=1", "ISRC=AAAA00000001", "MUSICBRAINZ_ALBUMID=" + release}, tagsOf(t, out, "01 one.flac"))
		assert.Equal(t, []string{"TITLE=Three", "ARTIST=Album Artist", "ALBUM=The Album", "ALBUMARTIST=Album Artist",
			"TRACKNUMBER=3", "MUSICBRAINZ_ALBUMID=" + release}, tagsOf(t, out, "03 three.flac"),
			"the album's artist where the track has none, and no empty ISRC")
		for _, name := range names(t, out) {
			vendor, comments := testutil.FLACComments(t, filepath.Join(out, name))
			assert.NotContains(t, vendor, "Lavf")
			for _, c := range comments {
				assert.NotContains(t, c, "ExactAudioCopy", "its comment is not carried over")
				assert.False(t, strings.HasPrefix(c, "COMMENT=") || strings.HasPrefix(c, "GENRE=") || strings.HasPrefix(c, "DATE="), c)
			}
		}
	})

	t.Run("and the split is one that is checked on its own, later, and is right", func(t *testing.T) {
		d := splitDisc(t, 0, splitMeta)
		out, _ := split(t, d, SplitOptions{})

		_, r, err := CheckSplit(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		clean(t, r)
		want(t, r, "split-audio", StatusPass)
		want(t, r, "split-lengths", StatusPass)
	})

	t.Run("a pregap before track 1 is part of it", func(t *testing.T) {
		d := splitDisc(t, 33, splitMeta)

		out, r := split(t, d, SplitOptions{})

		clean(t, r)
		assert.Equal(t, []string{"01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out))
		assert.Contains(t, want(t, r, "split-pregap", StatusInfo).Detail, "the 00:00:33 before track 1 is kept in track 1")
		fp, err := pcm.Decoder{}.Fingerprint(ctx, filepath.Join(out, "01 one.flac"))
		require.NoError(t, err)
		assert.Equal(t, int64((33+1000)*s), fp.Samples(), "it begins at the start of the image")
	})

	t.Run("a hidden track is a file of its own", func(t *testing.T) {
		d := splitDisc(t, 450, splitMeta)

		out, r := split(t, d, SplitOptions{})

		clean(t, r)
		assert.Equal(t, []string{"00 hidden track.flac", "01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out))
		assert.Equal(t, []string{"TITLE=Hidden track", "ARTIST=Album Artist", "ALBUM=The Album", "ALBUMARTIST=Album Artist",
			"TRACKNUMBER=0", "MUSICBRAINZ_ALBUMID=" + release}, tagsOf(t, out, "00 hidden track.flac"))
		fp, err := pcm.Decoder{}.Fingerprint(ctx, filepath.Join(out, "00 hidden track.flac"))
		require.NoError(t, err)
		assert.Equal(t, int64(450*s), fp.Samples())
		_, has := r.Check("split-pregap")
		assert.False(t, has, "it is not a pregap kept in track 1")
	})

	t.Run("what the option says", func(t *testing.T) {
		short := splitDisc(t, 33, splitMeta)
		out, r := split(t, short, SplitOptions{HiddenTrack: HiddenYes})
		clean(t, r)
		assert.Equal(t, []string{"00 hidden track.flac", "01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out))

		long := splitDisc(t, 450, splitMeta)
		out, r = split(t, long, SplitOptions{HiddenTrack: HiddenNo})
		clean(t, r)
		assert.Equal(t, []string{"01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out))
		assert.Contains(t, want(t, r, "split-pregap", StatusInfo).Detail, ", as --hidden-track=no says")
	})

	t.Run("the discs of a multi-disc album go into one directory", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "Album")
		for _, disc := range []int{1, 2} {
			d := newTinyDisc(t, 0)
			stem := fmt.Sprintf("Back in Black (disc %d)", disc)
			cue := strings.Replace(titledCue(d.cue, "The Album", "Album Artist", splitMeta), "Back in Black.flac", stem+".flac", 1)
			logBytes, err := os.ReadFile(d.rip.logPath())
			require.NoError(t, err)
			rip := buildRip(t, stem, logBytes, cue)
			testutil.MakePCMFlac(t, rip.dir, stem+".flac", d.pcm)

			_, r, err := Split(ctx, rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

			require.NoError(t, err)
			clean(t, r)
			if disc == 2 {
				assert.Contains(t, want(t, r, "split-others", StatusInfo).Detail, "3 other FLAC files")
			}
			assert.Equal(t, []string{"DISCNUMBER=" + fmt.Sprint(disc)}, filterTags(tagsOf(t, out, fmt.Sprintf("%d-02 two.flac", disc)), "DISCNUMBER"))
		}
		assert.Equal(t, []string{"1-01 one.flac", "1-02 two.flac", "1-03 three.flac", "2-01 one.flac", "2-02 two.flac", "2-03 three.flac"}, names(t, out))
	})

	t.Run("a hidden track on a disc of several is 1-00", func(t *testing.T) {
		d := newTinyDisc(t, 450)
		stem := "Back in Black (disc 1)"
		cue := strings.Replace(titledCue(d.cue, "The Album", "Album Artist", splitMeta), "Back in Black.flac", stem+".flac", 1)
		logBytes, err := os.ReadFile(d.rip.logPath())
		require.NoError(t, err)
		rip := buildRip(t, stem, logBytes, cue)
		testutil.MakePCMFlac(t, rip.dir, stem+".flac", d.pcm)
		out := filepath.Join(t.TempDir(), "Album")

		_, r, err := Split(ctx, rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		clean(t, r)
		assert.Equal(t, []string{"1-00 hidden track.flac", "1-01 one.flac", "1-02 two.flac", "1-03 three.flac"}, names(t, out))
	})

	t.Run("a directory that is there is used, and what is in it is left alone", func(t *testing.T) {
		d := splitDisc(t, 0, splitMeta)
		out := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(out, "notes.txt"), []byte("mine"), 0o644))

		_, r, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		clean(t, r)
		assert.Equal(t, []string{"01 one.flac", "02 two.flac", "03 three.flac", "notes.txt"}, names(t, out))
	})

	t.Run("without a release, the tracks have none and it is said", func(t *testing.T) {
		d := splitDisc(t, 0, splitMeta)
		require.NoError(t, os.Remove(filepath.Join(d.rip.dir, releaseFile)))

		out, r := split(t, d, SplitOptions{})

		assert.Equal(t, 0, r.Count(StatusError))
		assert.Contains(t, want(t, r, "split-release", StatusInfo).Detail, "no usable musicbrainz.txt beside the log")
		for _, c := range tagsOf(t, out, "01 one.flac") {
			assert.False(t, strings.HasPrefix(c, "MUSICBRAINZ_ALBUMID="))
		}

		for name, content := range map[string]string{"garbage": "not a url", "a release group": "https://musicbrainz.org/release-group/0a4a8b6e-0000-4000-8000-000000000000"} {
			require.NoError(t, os.WriteFile(filepath.Join(d.rip.dir, releaseFile), []byte(content), 0o644))
			out, r := split(t, d, SplitOptions{})
			_, has := r.Check("split-release")
			assert.True(t, has, name)
			for _, c := range tagsOf(t, out, "01 one.flac") {
				assert.False(t, strings.HasPrefix(c, "MUSICBRAINZ_ALBUMID="), name)
			}
		}
	})

	t.Run("a log whose CRC leaves out null samples is split, and says it was not compared", func(t *testing.T) {
		d := newTinyDisc(t, 0)
		text := replaceOnce(t, logWith(t, d, setup{asus, 6, false}), "Null samples used in CRC calculations       : Yes", "Null samples used in CRC calculations       : No")
		rip := buildRip(t, "Back in Black", []byte(signed(text)), titledCue(d.cue, "The Album", "Album Artist", splitMeta))
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", d.pcm)
		out := filepath.Join(t.TempDir(), "o")

		_, r, err := Split(ctx, rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		assert.Contains(t, want(t, r, "split-crc", StatusInfo).Detail, "was not compared with the log's, as it leaves out null samples")
		assert.Len(t, names(t, out), 3)
	})
}

// blackCopyCRCOf is the CRC the log of a tiny disc records.
func blackCopyCRCOf(t *testing.T, d tinyDisc) string {
	t.Helper()
	l, err := ParseLog(mustRead(t, d.rip.logPath()))
	require.NoError(t, err)
	return l.Range.CopyCRC
}

// filterTags returns the comments that are of the key.
func filterTags(comments []string, key string) []string {
	var out []string
	for _, c := range comments {
		if strings.HasPrefix(c, key+"=") {
			out = append(out, c)
		}
	}
	return out
}

func TestSplitFileNamesAndTagsFromAwkwardTitles(t *testing.T) {
	ctx := context.Background()
	name := regexp.MustCompile(`^\d{2} .+\.flac$`)

	t.Run("whatever a title is, the file is one the check finds", func(t *testing.T) {
		long := strings.Repeat("Very Long Title ", 20)
		d := splitDisc(t, 0, []cueMeta{{"AC/DC: Back In Black?", "", ""}, {"???", "", ""}, {long, "", ""}})
		out := filepath.Join(t.TempDir(), "o")

		_, r, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		assert.Equal(t, 0, r.Count(StatusWarning)+r.Count(StatusError), "%v", r.Findings())
		got := names(t, out)
		require.Len(t, got, 3)
		for _, n := range got {
			assert.Regexp(t, name, n)
			assert.NotContains(t, n, "/")
			assert.LessOrEqual(t, len([]rune(n)), 46, "the budget of a line of sums.md5: %q", n)
			assert.False(t, strings.HasPrefix(n, "."))
		}
		assert.Equal(t, "02 track 2.flac", got[1], "a title that is nothing once cleaned is not left out")
		_, c := testutil.FLACComments(t, filepath.Join(out, got[0]))
		assert.Equal(t, "TITLE=AC/DC: Back In Black?", c[0], "the tag keeps what the title says")
	})

	t.Run("a title with no text at all", func(t *testing.T) {
		d := splitDisc(t, 0, []cueMeta{{"", "", ""}, {"Two", "", ""}, {"Three", "", ""}})
		out := filepath.Join(t.TempDir(), "o")

		_, r, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		assert.Equal(t, 0, r.Count(StatusError), "%v", r.Findings())
		assert.Equal(t, "01 track 1.flac", names(t, out)[0])
		_, c := testutil.FLACComments(t, filepath.Join(out, "01 track 1.flac"))
		assert.NotContains(t, strings.Join(c, "|"), "TITLE=")
	})

	t.Run("text in the Windows code page is UTF-8 in the tags", func(t *testing.T) {
		d := newTinyDisc(t, 0)
		cue := titledCue(d.cue, "B'day", "Beyonc\xe9", []cueMeta{{"Caf\xe9 \xd8rjan", "Beyonc\xe9 & Shakira", ""}, {"Two", "", ""}, {"Three", "", ""}})
		require.NoError(t, os.WriteFile(d.rip.path(".cue"), []byte(cue), 0o644))
		testutil.MakePCMFlac(t, d.rip.dir, "Back in Black.flac", d.pcm)
		out := filepath.Join(t.TempDir(), "o")

		_, r, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		require.NoError(t, err)
		assert.Equal(t, 0, r.Count(StatusError), "%v", r.Findings())
		got := names(t, out)
		_, c := testutil.FLACComments(t, filepath.Join(out, got[0]))
		assert.Equal(t, "TITLE=Café Ørjan", c[0])
		assert.Equal(t, "ARTIST=Beyoncé & Shakira", c[1])
		assert.Equal(t, "ALBUMARTIST=Beyoncé", filterTags(c, "ALBUMARTIST")[0])
	})
}

func TestSplitRefuses(t *testing.T) {
	ctx := context.Background()
	d := splitDisc(t, 0, splitMeta)

	refused := func(t *testing.T, logPath, out string, dec Splitter) *Result {
		t.Helper()
		_, r, err := Split(ctx, logPath, out, dec, SplitOptions{})
		require.NoError(t, err)
		assert.True(t, r.Failed())
		return r
	}
	failure := func(t *testing.T, r *Result) string {
		t.Helper()
		return want(t, r, "split-write", StatusError).Detail
	}

	t.Run("a file that is to be made is there already", func(t *testing.T) {
		out := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(out, "01 one.flac"), []byte("mine"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(out, "02 two.flac"), []byte("mine"), 0o644))

		r := refused(t, d.rip.logPath(), out, pcm.Decoder{})

		assert.Equal(t, "nothing was written: 2 files are already there: 01 one.flac, 02 two.flac", failure(t, r))
		assert.Equal(t, []string{"01 one.flac", "02 two.flac"}, names(t, out), "and they are not touched")
		assert.Equal(t, []byte("mine"), mustRead(t, filepath.Join(out, "01 one.flac")))
	})

	t.Run("one file is there", func(t *testing.T) {
		out := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(out, "03 three.flac"), 0o755)) // even a directory

		r := refused(t, d.rip.logPath(), out, pcm.Decoder{})

		assert.Equal(t, "nothing was written: 1 file is already there: 03 three.flac", failure(t, r))
	})

	t.Run("the directory of the rip, which is its archive", func(t *testing.T) {
		before := names(t, d.rip.dir)

		r := refused(t, d.rip.logPath(), d.rip.dir, pcm.Decoder{})

		assert.Contains(t, failure(t, r), "not to be written into the directory of the rip")
		assert.Equal(t, before, names(t, d.rip.dir))
	})

	t.Run("the directory of the rip by another name", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink(d.rip.dir, link))

		r := refused(t, d.rip.logPath(), link, pcm.Decoder{})

		assert.Contains(t, failure(t, r), "not to be written into the directory of the rip")
	})

	t.Run("a log that has been changed since EAC wrote it", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", []byte(replaceOnce(t, signed(logWith(t, d, setup{asus, 6, false})), "ASUS", "ASUZ")), d.cue)
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", d.pcm)
		out := filepath.Join(t.TempDir(), "o")

		r := refused(t, rip.logPath(), out, pcm.Decoder{})

		assert.Equal(t, "nothing was written: the log is not as EAC wrote it", failure(t, r))
		assert.Contains(t, want(t, r, "checksum", StatusError).Detail, "checksum")
		assert.NoDirExists(t, out)
	})

	t.Run("an image that is not what EAC ripped", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		other := testAudio(len(d.pcm)/4, 999) // the same length, other samples
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", other)
		out := filepath.Join(t.TempDir(), "WORKING", "o")

		r := refused(t, rip.logPath(), out, pcm.Decoder{})

		assert.Regexp(t, `^nothing was written: the image decodes to CRC [0-9A-F]{8}, and the log's Copy CRC is [0-9A-F]{8}, so it is not what EAC ripped$`, failure(t, r))
		assert.NoDirExists(t, out, "not even the directory")
		assert.NoDirExists(t, filepath.Dir(out), "nor the one it was in")
	})

	t.Run("an image that is shorter than the disc", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", d.pcm[:len(d.pcm)-588*4])
		out := filepath.Join(t.TempDir(), "o")

		r := refused(t, rip.logPath(), out, pcm.Decoder{})

		assert.Regexp(t, `^nothing was written: Back in Black.flac holds \d+ samples, and the disc has \d+$`, failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an image that is longer than the disc", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		testutil.MakePCMFlac(t, rip.dir, "Back in Black.flac", append(append([]byte(nil), d.pcm...), d.pcm[:588*4]...))
		out := filepath.Join(t.TempDir(), "o")

		r := refused(t, rip.logPath(), out, pcm.Decoder{})

		assert.Regexp(t, `^nothing was written: Back in Black.flac holds \d+ samples, and the disc has \d+$`, failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an image that is not CD audio", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		require.NoError(t, os.WriteFile(rip.path(".flac"), flacHeader(48000, 2, 24, uint64(len(d.pcm)/4)), 0o644))

		r := refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})

		assert.Contains(t, failure(t, r), "is 48 kHz, 24-bit, stereo, and not CD audio")
	})

	t.Run("an image that is not a FLAC file, and one that is not there", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		require.NoError(t, os.WriteFile(rip.path(".flac"), []byte("not flac"), 0o644))
		assert.Contains(t, failure(t, refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})), "not a FLAC file")

		require.NoError(t, os.Remove(rip.path(".flac")))
		assert.Contains(t, failure(t, refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})),
			"the cue sheet does not name one FLAC image that is beside the log")
	})

	t.Run("a cue sheet without a track the disc has", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), strings.Replace(d.cue, "TRACK 03 AUDIO", "TRACK 07 AUDIO", 1))
		out := filepath.Join(t.TempDir(), "o")

		r := refused(t, rip.logPath(), out, pcm.Decoder{})

		assert.Equal(t, "nothing was written: the cue sheet has no track 3", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("a cue sheet that puts track 1 later than the log does", func(t *testing.T) {
		late := splitDisc(t, 450, splitMeta)
		rip := buildRip(t, "Back in Black", mustRead(t, late.rip.logPath()), strings.Replace(late.cue, "INDEX 01 00:06:00", "INDEX 01 00:07:00", 1))

		r := refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})

		assert.Equal(t, "nothing was written: the cue sheet and the log do not say how long the image is", failure(t, r))
	})

	t.Run("an image that cannot be read", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		require.NoError(t, os.Remove(rip.path(".flac")))
		require.NoError(t, os.Mkdir(rip.path(".flac"), 0o755)) // there, and not a file

		r := refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})

		assert.Contains(t, failure(t, r), "Back in Black.flac cannot be read")
	})

	t.Run("a place that cannot be made a directory", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

		r := refused(t, d.rip.logPath(), filepath.Join(blocker, "sub"), pcm.Decoder{})

		assert.Contains(t, failure(t, r), "could not be made")
	})

	t.Run("a rip with no cue sheet", func(t *testing.T) {
		rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		require.NoError(t, os.Remove(rip.path(".cue")))

		r := refused(t, rip.logPath(), filepath.Join(t.TempDir(), "o"), pcm.Decoder{})

		assert.Equal(t, "nothing was written: the cue sheet cannot be used", failure(t, r))
		want(t, r, "cue-sheet", StatusError)
	})

	t.Run("no ffmpeg", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "o")
		t.Setenv("PATH", t.TempDir())

		r := refused(t, d.rip.logPath(), out, pcm.Decoder{})

		assert.Equal(t, "nothing was written: ffmpeg was not found", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("a context that is cancelled", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "o")
		ctx, cancel := context.WithCancel(ctx)
		cancel()

		_, _, err := Split(ctx, d.rip.logPath(), out, pcm.Decoder{}, SplitOptions{})

		assert.ErrorIs(t, err, context.Canceled)
		assert.NoDirExists(t, out)
	})

	t.Run("a log that cannot be read", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "x.log")
		require.NoError(t, os.WriteFile(bad, []byte("not a log"), 0o644))

		_, _, err := Split(ctx, bad, t.TempDir(), pcm.Decoder{}, SplitOptions{})

		assert.Error(t, err)
	})
}

// fakeSplitter is a Splitter that gives the audio it is told to, and fails as it
// is told to.
type fakeSplitter struct {
	*fakeTracks
	audio      []byte
	endsWith   error // what the stream ends with, if not io.EOF
	failAt     int   // the call of Encode, from 1, that fails
	failWith   error
	noFileAt   int // the call of Encode that does not write its file
	calls      int
	encoded    []string
	closed     bool
	tagsAtCall map[int][]pcm.Tag
	// returnReadError is whether Encode gives the error its reader failed with, as it
	// should; one that does not must not hide it.
	returnReadError bool
	// onEncode is called as each Encode begins, with its number from 1.
	onEncode func(call int)
}

type endingReader struct {
	data []byte
	end  error
}

func (e *endingReader) Read(p []byte) (int, error) {
	if len(e.data) == 0 {
		if e.end != nil {
			return 0, e.end
		}
		return 0, io.EOF
	}
	n := copy(p, e.data)
	e.data = e.data[n:]
	return n, nil
}

func (f *fakeSplitter) Stream(_ context.Context, _ string) io.ReadCloser {
	return &closeRecorder{Reader: &endingReader{data: f.audio, end: f.endsWith}, closed: &f.closed}
}

type closeRecorder struct {
	io.Reader
	closed *bool
}

func (c *closeRecorder) Close() error { *c.closed = true; return nil }

func (f *fakeSplitter) Encode(_ context.Context, r io.Reader, path string, tags []pcm.Tag) error {
	f.calls++
	if f.onEncode != nil {
		f.onEncode(f.calls)
	}
	if f.tagsAtCall == nil {
		f.tagsAtCall = map[int][]pcm.Tag{}
	}
	f.tagsAtCall[f.calls] = tags
	if _, err := io.Copy(io.Discard, r); err != nil && f.returnReadError {
		return err
	}
	if f.calls == f.failAt {
		return f.failWith
	}
	if f.calls != f.noFileAt {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			return err
		}
		f.encoded = append(f.encoded, path)
	}
	return nil
}

func TestSplitFailuresPartWay(t *testing.T) {
	ctx := context.Background()
	d := splitDisc(t, 0, splitMeta)
	rip := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue) // an image that is only a header: it is the fake's audio that is decoded
	run := func(t *testing.T, f *fakeSplitter) (string, *Result, error) {
		t.Helper()
		f.fakeTracks = &fakeTracks{crc: 0}
		out := filepath.Join(t.TempDir(), "WORKING", "o")
		_, r, err := Split(ctx, rip.logPath(), out, f, SplitOptions{})
		return out, r, err
	}
	failure := func(t *testing.T, r *Result) string { return want(t, r, "split-write", StatusError).Detail }

	t.Run("a track that cannot be written takes away what was made", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 2, failWith: &pcm.EncodeError{Path: "x", Message: "no space left"}}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: could not write x: no space left", failure(t, r))
		assert.NoDirExists(t, out)
		assert.NoDirExists(t, filepath.Dir(out))
		assert.True(t, f.closed, "the decode is stopped")
		assert.Equal(t, 2, f.calls, "and no more is attempted")
	})

	t.Run("an image that ends before it should", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm[:len(d.pcm)-1000*4]}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Regexp(t, `^nothing was written: the image ends before 03 three.flac does: it is \d+ samples short$`, failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an image with more audio than the disc has", func(t *testing.T) {
		f := &fakeSplitter{audio: append(append([]byte(nil), d.pcm...), 1, 2, 3, 4)}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: the image holds more audio than the cue sheet and the log say the disc has", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an image that does not decode cleanly at its end", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, endsWith: &pcm.DecodeError{Path: "x", Message: "CRC error"}}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: the image does not decode cleanly: CRC error", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an image that does not decode cleanly in the middle", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm[:1500*588*4], endsWith: &pcm.DecodeError{Path: "x", Message: "invalid residual"}}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: the image does not decode cleanly: invalid residual", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("the same, from an encoder that says what its reader failed with", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm[:1500*588*4], endsWith: &pcm.DecodeError{Path: "x", Message: "invalid residual"}, returnReadError: true}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: the image does not decode cleanly: invalid residual", failure(t, r))
		assert.NoDirExists(t, out)
	})

	t.Run("an encoder that fails when the stream does not", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 2, failWith: errors.New("encoder died")}

		_, r, _ := run(t, f)

		assert.Equal(t, "nothing was written: encoder died", failure(t, r))
	})

	t.Run("a log that goes away while the split is made", func(t *testing.T) {
		// What is written is in place by then, and what cannot be said is an error.
		own := buildRip(t, "Back in Black", mustRead(t, d.rip.logPath()), d.cue)
		out := filepath.Join(t.TempDir(), "o")
		f := &fakeSplitter{fakeTracks: &fakeTracks{}, audio: d.pcm, onEncode: func(call int) {
			if call == 3 {
				require.NoError(t, os.Remove(own.logPath()))
			}
		}}

		_, _, err := Split(ctx, own.logPath(), out, f, SplitOptions{})

		assert.Error(t, err)
		assert.Equal(t, []string{"01 one.flac", "02 two.flac", "03 three.flac"}, names(t, out))
	})

	t.Run("something that is none of those", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 1, failWith: errors.New("disk on fire")}

		_, r, err := run(t, f)

		require.NoError(t, err)
		assert.Equal(t, "nothing was written: disk on fire", failure(t, r))
	})

	t.Run("no ffmpeg, from the encoder", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 1, failWith: pcm.ErrNoFFmpeg}

		_, r, _ := run(t, f)

		assert.Equal(t, "nothing was written: ffmpeg was not found", failure(t, r))
	})

	t.Run("a file that is not there to be put in place takes away the ones that were", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, noFileAt: 3}

		out, r, err := run(t, f)

		require.NoError(t, err)
		assert.Regexp(t, `^nothing was written: 03 three.flac could not be put in place: `, failure(t, r))
		assert.NoDirExists(t, out, "01 and 02 are taken away, and the directory")
	})

	t.Run("the tracks are written under names that are not tracks until all are", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 3, failWith: errors.New("stop")}

		_, _, _ = run(t, f)

		for _, p := range f.encoded {
			assert.True(t, strings.HasPrefix(filepath.Base(p), ".") && strings.HasSuffix(p, ".flac.partial"), p)
		}
		assert.Len(t, f.encoded, 2)
	})

	t.Run("a directory that was there is left there", func(t *testing.T) {
		out := t.TempDir()
		f := &fakeSplitter{fakeTracks: &fakeTracks{}, audio: d.pcm, failAt: 2, failWith: errors.New("stop")}

		_, r, err := Split(ctx, rip.logPath(), out, f, SplitOptions{})

		require.NoError(t, err)
		assert.True(t, r.Failed())
		assert.DirExists(t, out)
		assert.Empty(t, names(t, out))
	})

	t.Run("each is given the tags of its track, in order", func(t *testing.T) {
		f := &fakeSplitter{audio: d.pcm, failAt: 3, failWith: errors.New("stop")}

		_, _, _ = run(t, f)

		assert.Contains(t, f.tagsAtCall[1], pcm.Tag{Key: "TITLE", Value: "One"})
		assert.Contains(t, f.tagsAtCall[2], pcm.Tag{Key: "TITLE", Value: "Two"})
		assert.Contains(t, f.tagsAtCall[3], pcm.Tag{Key: "TRACKNUMBER", Value: "3"})
	})
}

func TestPlanPieces(t *testing.T) {
	const s = samplesPerSector
	d := newTinyDisc(t, 450)
	log, err := ParseLog(mustRead(t, d.rip.logPath()))
	require.NoError(t, err)
	c, err := ParseCue([]byte(titledCue(d.cue, "The Album", "Album Artist", splitMeta)))
	require.NoError(t, err)

	t.Run("with the audio before track 1 a track of its own", func(t *testing.T) {
		got, err := planPieces(log, c, 0, true, "")

		require.NoError(t, err)
		require.Len(t, got, 4)
		assert.Equal(t, []int{0, 1, 2, 3}, []int{got[0].number, got[1].number, got[2].number, got[3].number})
		assert.Equal(t, []int64{450 * s, 1000 * s, 1000 * s, 1000 * s}, []int64{got[0].samples, got[1].samples, got[2].samples, got[3].samples})
		assert.Equal(t, "00 hidden track.flac", got[0].name)
		assert.Equal(t, "01 one.flac", got[1].name)
	})

	t.Run("with it in track 1", func(t *testing.T) {
		got, err := planPieces(log, c, 0, false, "")

		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, int64((450+1000)*s), got[0].samples)
	})

	t.Run("the pieces are the whole of the image, end to end", func(t *testing.T) {
		for _, separate := range []bool{true, false} {
			got, err := planPieces(log, c, 0, separate, "")
			require.NoError(t, err)
			var total int64
			for _, p := range got {
				total += p.samples
			}
			assert.Equal(t, imageSamples(log, c), total, "separate %v", separate)
		}
	})

	t.Run("on a disc of several", func(t *testing.T) {
		got, err := planPieces(log, c, 3, true, "")

		require.NoError(t, err)
		assert.Equal(t, "3-00 hidden track.flac", got[0].name)
		assert.Equal(t, "3-02 two.flac", got[2].name)
		assert.Contains(t, got[2].tags, pcm.Tag{Key: "DISCNUMBER", Value: "3"})
	})

	t.Run("a cue sheet that does not have a track the disc has", func(t *testing.T) {
		missing, err := ParseCue([]byte(strings.Replace(d.cue, "  TRACK 03 AUDIO", "  TRACK 07 AUDIO", 1)))
		require.NoError(t, err)

		_, err = planPieces(log, missing, 0, false, "")

		assert.ErrorContains(t, err, "the cue sheet has no track 3")
	})
}

func TestTagsFor(t *testing.T) {
	c := &Cue{Title: "The Album", Performer: "The Band"}

	t.Run("everything there is", func(t *testing.T) {
		got := tagsFor(c, CueTrack{Title: "Song", Performer: "Singer", ISRC: "X1"}, 5, 2, "Song", "rel-id")

		assert.Equal(t, []pcm.Tag{
			{Key: "TITLE", Value: "Song"}, {Key: "ARTIST", Value: "Singer"}, {Key: "ALBUM", Value: "The Album"},
			{Key: "ALBUMARTIST", Value: "The Band"}, {Key: "TRACKNUMBER", Value: "5"}, {Key: "DISCNUMBER", Value: "2"},
			{Key: "ISRC", Value: "X1"}, {Key: "MUSICBRAINZ_ALBUMID", Value: "rel-id"},
		}, got)
	})

	t.Run("a disc that is not one of several has no disc number", func(t *testing.T) {
		got := tagsFor(c, CueTrack{}, 1, 0, "x", "")

		assert.Empty(t, filterTagsOf(got, "DISCNUMBER"))
	})

	t.Run("nothing is empty", func(t *testing.T) {
		got := tagsFor(&Cue{}, CueTrack{}, 0, 0, "", "")

		assert.Equal(t, []pcm.Tag{{Key: "TRACKNUMBER", Value: "0"}}, got)
	})

	t.Run("the track's artist, or the album's", func(t *testing.T) {
		assert.Equal(t, "The Band", filterTagsOf(tagsFor(c, CueTrack{}, 1, 0, "x", ""), "ARTIST")[0].Value)
		assert.Equal(t, "Guest", filterTagsOf(tagsFor(c, CueTrack{Performer: "Guest"}, 1, 0, "x", ""), "ARTIST")[0].Value)
	})
}

func filterTagsOf(tags []pcm.Tag, key string) []pcm.Tag {
	var out []pcm.Tag
	for _, t := range tags {
		if t.Key == key {
			out = append(out, t)
		}
	}
	return out
}

func TestTagText(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"plain":                {"Hello", "Hello"},
		"UTF-8":                {"Café 日本語 ♥", "Café 日本語 ♥"},
		"spaces around":        {"  Hello  ", "Hello"},
		"spaces inside":        {"a   b \t c", "a b c"},
		"a line break":         {"one\r\ntwo\nthree", "one two three"},
		"control characters":   {"a\x00b\x07c\x1fd\x7fe", "a b c d e"},
		"not UTF-8":            {"caf\xe9", "caf\uFFFD"},
		"nothing":              {"", ""},
		"only space":           {" \t\r\n ", ""},
		"an equals sign":       {"a=b", "a=b"},
		"quotes and backslash": {`say "hi" \ bye`, `say "hi" \ bye`},
	} {
		assert.Equal(t, tc.want, tagText(tc.in), name)
	}
}

func TestReleaseIDOf(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, releaseFile), []byte(content), 0o644))
		return dir
	}
	const id = "0a4a8b6e-1234-4abc-8def-000000000000"

	assert.Equal(t, id, releaseIDOf(write(t, "https://musicbrainz.org/release/"+id+"\n")))
	assert.Equal(t, id, releaseIDOf(write(t, "\ufeff\nhttps://musicbrainz.org/release/"+id+"/cover-art")), "a byte order mark, and more after it")
	assert.Equal(t, "", releaseIDOf(write(t, "")))
	assert.Equal(t, "", releaseIDOf(write(t, "not a url")))
	assert.Equal(t, "", releaseIDOf(write(t, "https://musicbrainz.org/release-group/"+id)))
	assert.Equal(t, "", releaseIDOf(t.TempDir()), "no file")
}

func TestSplitHelpers(t *testing.T) {
	t.Run("splitFailure", func(t *testing.T) {
		assert.Equal(t, "ffmpeg was not found", splitFailure(pcm.ErrNoFFmpeg))
		assert.Equal(t, "the image does not decode cleanly: bad", splitFailure(&pcm.DecodeError{Path: "p", Message: "bad"}))
		assert.Equal(t, "could not write p: bad", splitFailure(&pcm.EncodeError{Path: "p", Message: "bad"}))
		assert.Equal(t, "other", splitFailure(errors.New("other")))
		assert.Equal(t, "the image does not decode cleanly: bad", splitFailure(fmt.Errorf("wrapped: %w", &pcm.DecodeError{Message: "bad"})))
	})

	t.Run("countingReader", func(t *testing.T) {
		c := &countingReader{r: bytes.NewReader(make([]byte, 1000))}
		_, err := io.CopyN(io.Discard, c, 600)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, c)
		require.NoError(t, err)
		assert.Equal(t, int64(1000), c.n)
	})

	t.Run("sameDirectory", func(t *testing.T) {
		a, b := t.TempDir(), t.TempDir()
		link := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Symlink(a, link))

		same, err := sameDirectory(a, a)
		require.NoError(t, err)
		assert.True(t, same)
		same, err = sameDirectory(a, link)
		require.NoError(t, err)
		assert.True(t, same, "by another name")
		same, err = sameDirectory(a, b)
		require.NoError(t, err)
		assert.False(t, same)
		_, err = sameDirectory(a, filepath.Join(a, "missing"))
		assert.Error(t, err, "one that is not there")
		_, err = sameDirectory(filepath.Join(a, "missing"), a)
		assert.Error(t, err)
	})

	t.Run("missingDirectories", func(t *testing.T) {
		root := t.TempDir()

		assert.Empty(t, missingDirectories(root))
		assert.Equal(t, []string{filepath.Join(root, "a", "b"), filepath.Join(root, "a")}, missingDirectories(filepath.Join(root, "a", "b")), "deepest first")
		assert.Equal(t, []string{filepath.Join(root, "a")}, missingDirectories(filepath.Join(root, "a")))
	})
}
