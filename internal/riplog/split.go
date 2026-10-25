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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mfinelli/musicrename/internal/pcm"
)

// TrackDecoder fingerprints audio files, each on its own and all together as
// one stretch of audio. pcm.Decoder is one; tests use a fake.
type TrackDecoder interface {
	// Fingerprints decodes the files in order. total is the fingerprint of
	// them as one stretch of audio, and files that of each on its own. See
	// pcm.Decoder.Fingerprints for what it returns on an error.
	Fingerprints(ctx context.Context, paths ...string) (total pcm.Fingerprint, files []pcm.Fingerprint, err error)
}

// A split album is a directory of FLAC files, one per track, named as the
// workflow names them (see WORKFLOW.md):
//
//	01 Title.flac       a single disc
//	1-01 Title.flac     disc 1 of several, which share the directory
//	00 Title.flac       audio before track 1, a hidden track or a pregap
//
// The disc a log is for is read from its name, as "(disc 2)", the way EAC and the
// rest of the workflow name the logs of a multi-disc rip.
var (
	reDisc        = regexp.MustCompile(`(?i)\(disc\s*(\d+)\)\s*$`)
	reSingleTrack = regexp.MustCompile(`(?i)^(\d{2}) .+\.flac$`)
	reMultiTrack  = regexp.MustCompile(`(?i)^(\d+)-(\d{2}) .+\.flac$`)
)

// discNumber returns the disc the log at logPath is for, 0 for a single disc.
func discNumber(logPath string) int {
	stem := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath))
	if m := reDisc.FindStringSubmatch(stem); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// trackLabel is how a track's number is written in a file's name.
func trackLabel(disc, n int) string {
	if disc == 0 {
		return fmt.Sprintf("%02d", n)
	}
	return fmt.Sprintf("%d-%02d", disc, n)
}

// splitTrack is one file of a split.
type splitTrack struct {
	Number int
	Path   string
}

// findTrackFiles lists the FLAC files in dir that are tracks of the given disc,
// by track number (more than one file for a number is a mistake, so each maps
// to all of them), and counts the other FLAC files there, which belong to other
// discs or are not named as tracks at all.
func findTrackFiles(dir string, disc int) (byNumber map[int][]string, others int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}

	byNumber = map[int][]string{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.EqualFold(filepath.Ext(e.Name()), ".flac") {
			continue
		}

		number := -1
		if disc == 0 {
			if m := reSingleTrack.FindStringSubmatch(e.Name()); m != nil {
				number, _ = strconv.Atoi(m[1])
			}
		} else if m := reMultiTrack.FindStringSubmatch(e.Name()); m != nil {
			if d, _ := strconv.Atoi(m[1]); d == disc {
				number, _ = strconv.Atoi(m[2])
			}
		}

		if number < 0 {
			others++
			continue
		}
		byNumber[number] = append(byNumber[number], filepath.Join(dir, e.Name()))
	}
	return byNumber, others, nil
}

// CheckSplit checks that the FLAC files in dir are a faithful split of the rip
// the log at logPath describes: that there is a file for every track, that each
// is CD audio, that the tracks together hold the audio EAC ripped, undamaged,
// and that each track starts and ends where the cue sheet beside the log says it
// should. A split can be cut a few samples early or late at every boundary and
// still hold exactly the audio of the rip, so the lengths are checked as well as
// the audio.
//
// dec decodes the tracks; nil leaves their audio unchecked. It returns the
// log, which describes the disc, and the findings.
func CheckSplit(ctx context.Context, logPath, dir string, dec TrackDecoder) (*Log, *Result, error) {
	l, err := readLog(logPath)
	if err != nil {
		return nil, nil, err
	}

	r := &Result{}
	// The log is the reference the tracks are compared with, so it is checked
	// to be what EAC wrote.
	checkChecksum(r, l)

	c := splitCue(r, logPath)
	if c == nil {
		return l, r, nil
	}

	disc := discNumber(logPath)
	tracks, ok := checkSplitTracks(r, l, c, dir, disc)
	if !ok {
		return l, r, nil
	}

	infos, ok := checkSplitFormat(r, tracks, disc)
	if !ok {
		return l, r, nil
	}

	// A length is what the decode finds if there was one, and what the file's
	// header says if not.
	lengths := make(map[int]int64, len(tracks))
	for i, t := range tracks {
		lengths[t.Number] = int64(infos[i].TotalSamples)
	}
	if decoded := checkSplitAudio(ctx, r, l, c, tracks, infos, dec); decoded != nil {
		for i, t := range tracks {
			lengths[t.Number] = decoded[i]
		}
	}

	checkSplitLengths(r, l, c, tracks, lengths, disc)
	return l, r, nil
}

// splitCue finds and reads the cue sheet beside the log, which says where each
// track begins and where its pregap does. It returns nil if there is none.
func splitCue(r *Result, logPath string) *Cue {
	const id, name = "cue-sheet", "Cue sheet"

	dir := filepath.Dir(logPath)
	want := strings.TrimSuffix(filepath.Base(logPath), filepath.Ext(logPath)) + ".cue"

	path := findFile(dir, want)
	if path == "" {
		r.add(id, name, StatusError,
			"no %q next to the log: it says where each track should begin and end", want)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.add(id, name, StatusError, "could not read %q: %v", filepath.Base(path), err)
		return nil
	}
	c, err := ParseCue(data)
	if err != nil {
		r.add(id, name, StatusError, "%s: %v", filepath.Base(path), err)
		return nil
	}
	r.add(id, name, StatusPass, "%s", filepath.Base(path))
	return c
}

// preAudioFrames is how much of the image comes before track 1: audio, such as a
// hidden track in the pregap, that no track of the disc claims. It is zero for
// most discs. ok is false if the cue sheet does not say where track 1 begins.
func preAudioFrames(l *Log, c *Cue) (frames int, ok bool) {
	if len(l.TOC) == 0 {
		return 0, false
	}
	for _, t := range c.Tracks {
		if t.Number == l.TOC[0].Number {
			return t.Index(1)
		}
	}
	return 0, false
}

// checkSplitTracks confirms there is one file for each track of the disc, and
// for track 00 if there is audio before track 1. It returns the files in the
// order of the disc, track 00 first, and whether they are right, which the
// rest cannot be checked without.
func checkSplitTracks(r *Result, l *Log, c *Cue, dir string, disc int) ([]splitTrack, bool) {
	const id, name = "split-tracks", "Track files"

	byNumber, others, err := findTrackFiles(dir, disc)
	if err != nil {
		r.add(id, name, StatusError, "could not read %s: %v", dir, err)
		return nil, false
	}
	if others > 0 {
		r.add("split-others", "Other files", StatusInfo,
			"%d other FLAC %s in the directory %s not tracks of this disc, and %s left out",
			others, plural(int64(others), "file"), isAre(others), wasWere(others))
	}

	pre, ok := preAudioFrames(l, c)
	if !ok {
		r.add(id, name, StatusError, "cannot tell whether there is audio before track 1: the cue sheet has no INDEX 01 for it")
		return nil, false
	}

	numbers := make([]int, 0, len(byNumber))
	for n := range byNumber {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)

	onDisc := map[int]bool{}
	for _, t := range l.TOC {
		onDisc[t.Number] = true
	}

	var problems []string
	var unexpected []string
	for _, n := range numbers {
		files := byNumber[n]
		switch {
		case len(files) > 1:
			problems = append(problems, fmt.Sprintf("%d files for track %s (%s and %s)",
				len(files), trackLabel(disc, n), filepath.Base(files[0]), filepath.Base(files[1])))
		case n == 0 && pre == 0:
			problems = append(problems, fmt.Sprintf("there is a file for track %s, but the disc has no audio before track 1", trackLabel(disc, 0)))
		case n != 0 && !onDisc[n]:
			unexpected = append(unexpected, trackLabel(disc, n))
		}
	}
	if len(unexpected) > 0 {
		problems = append(problems, fmt.Sprintf("files for tracks %s, which the disc does not have (it has %d)",
			listLimited(unexpected), len(l.TOC)))
	}

	var missing []string
	for _, t := range l.TOC {
		if len(byNumber[t.Number]) == 0 {
			missing = append(missing, trackLabel(disc, t.Number))
		}
	}
	if len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("no file for %s %s", plural(int64(len(missing)), "track"), listLimited(missing)))
	}

	if len(problems) > 0 {
		r.add(id, name, StatusError, "%s", strings.Join(problems, "; "))
		return nil, false
	}

	var tracks []splitTrack
	if len(byNumber[0]) == 1 {
		tracks = append(tracks, splitTrack{Number: 0, Path: byNumber[0][0]})
	}
	for _, t := range l.TOC {
		tracks = append(tracks, splitTrack{Number: t.Number, Path: byNumber[t.Number][0]})
	}

	first, last := l.TOC[0].Number, l.TOC[len(l.TOC)-1].Number
	detail := fmt.Sprintf("%d tracks, %s to %s", len(tracks), trackLabel(disc, first), trackLabel(disc, last))
	if len(tracks) > len(l.TOC) {
		detail = fmt.Sprintf("%d tracks, %s and %s to %s", len(tracks), trackLabel(disc, 0), trackLabel(disc, first), trackLabel(disc, last))
	}

	if pre > 0 && tracks[0].Number != 0 {
		// The audio is not lost, since the lengths and the audio are checked
		// after this, but the workflow keeps it in a file of its own.
		r.add(id, name, StatusWarning,
			"%s; there is no %s file, but there is %s of audio before track 1, which should be a file of its own",
			detail, trackLabel(disc, 0), msf(pre))
		return tracks, true
	}
	r.add(id, name, StatusPass, "%s", detail)
	return tracks, true
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// listLimited joins names with commas, naming no more than maxListed.
func listLimited(names []string) string {
	if len(names) <= maxListed {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxListed], ", "), len(names)-maxListed)
}

// checkSplitFormat confirms every track is CD audio, and returns their headers.
func checkSplitFormat(r *Result, tracks []splitTrack, disc int) ([]streamInfo, bool) {
	const id, name = "split-format", "Track format"

	infos := make([]streamInfo, len(tracks))
	var problems []string
	for i, t := range tracks {
		si, err := readStreamInfo(t.Path)
		switch {
		case errors.Is(err, errNotFLAC):
			problems = append(problems, fmt.Sprintf("%s: %v", filepath.Base(t.Path), err))
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: cannot read: %v", filepath.Base(t.Path), err))
		case si.SampleRate != cdSampleRate || si.Channels != cdChannels || si.BitsPerSample != cdBits:
			problems = append(problems, fmt.Sprintf("%s: %s, %d-bit, %s", filepath.Base(t.Path),
				rateName(si.SampleRate), si.BitsPerSample, channelsName(si.Channels)))
		}
		infos[i] = si
	}

	if len(problems) > 0 {
		detail := strings.Join(problems[:min(len(problems), maxListed)], "; ")
		if len(problems) > maxListed {
			detail += fmt.Sprintf("; and %d more", len(problems)-maxListed)
		}
		r.add(id, name, StatusError, "%s: a CD is %s, %d-bit, %s", detail,
			rateName(cdSampleRate), cdBits, channelsName(cdChannels))
		return nil, false
	}
	r.add(id, name, StatusPass, "all %d tracks are %s, %d-bit, %s",
		len(tracks), rateName(cdSampleRate), cdBits, channelsName(cdChannels))
	return infos, true
}

// checkSplitAudio decodes the tracks and confirms that each is undamaged, by what
// its own header records of it, and that together they hold the audio EAC
// ripped, by the log's Copy CRC. It returns how many samples each track decoded
// to, or nil if they were not decoded.
func checkSplitAudio(ctx context.Context, r *Result, l *Log, c *Cue, tracks []splitTrack,
	infos []streamInfo, dec TrackDecoder) []int64 {
	const id, name = "split-audio", "Audio"

	if dec == nil {
		r.add(id, name, StatusInfo, "the tracks were not decoded, so their audio was not checked")
		return nil
	}

	paths := make([]string, len(tracks))
	for i, t := range tracks {
		paths[i] = t.Path
	}

	total, files, err := dec.Fingerprints(ctx, paths...)
	var decodeErr *pcm.DecodeError
	switch {
	case errors.Is(err, pcm.ErrNoFFmpeg):
		r.add(id, name, StatusWarning, "ffmpeg was not found, so the audio was not checked")
		return nil
	case errors.As(err, &decodeErr):
		r.add(id, name, StatusError, "%s does not decode cleanly: %s", filepath.Base(decodeErr.Path), decodeErr.Message)
		return nil
	case err != nil:
		r.add(id, name, StatusWarning, "could not decode the tracks, so their audio was not checked: %v", err)
		return nil
	}

	// What each file says of itself: a difference is damage to that file.
	var damaged []string
	withMD5 := 0
	for i, f := range files {
		var why []string
		if want := infos[i].TotalSamples; want != 0 && uint64(f.Samples()) != want {
			why = append(why, fmt.Sprintf("it decodes to %d samples, not the %d in its header", f.Samples(), want))
		}
		if infos[i].HasMD5() {
			withMD5++
			if f.MD5 != infos[i].MD5 {
				why = append(why, "its audio does not match the MD5 in its header")
			}
		}
		if len(why) > 0 {
			damaged = append(damaged, fmt.Sprintf("%s is damaged: %s", filepath.Base(tracks[i].Path), strings.Join(why, ", and ")))
		}
	}

	var problems []string
	if len(damaged) > 0 {
		problems = append(problems, strings.Join(damaged[:min(len(damaged), maxListed)], "; "))
		if len(damaged) > maxListed {
			problems[0] += fmt.Sprintf("; and %d more", len(damaged)-maxListed)
		}
	}

	reason := crcNotComparable(l)
	if reason == "" && !strings.EqualFold(total.CRC(), l.Range.CopyCRC) {
		detail := fmt.Sprintf("the tracks, in order, decode to CRC %s, not the log's Copy CRC %s: they are not the audio EAC ripped, or were changed after",
			total.CRC(), l.Range.CopyCRC)
		if want := imageSamples(l, c); want > 0 && total.Samples() != want {
			detail += fmt.Sprintf(" (%d samples, where the disc has %d)", total.Samples(), want)
		}
		problems = append(problems, detail)
	}
	if len(problems) > 0 {
		r.add(id, name, StatusError, "%s", strings.Join(problems, "; "))
		return decodedSamples(files)
	}

	md5 := "each matches the MD5 in its header"
	if withMD5 < len(files) {
		md5 = fmt.Sprintf("%d of %d match the MD5 in their headers, and the rest store none", withMD5, len(files))
	}
	if reason != "" {
		// Nothing says the audio is the rip's, so it is not that it is.
		r.add(id, name, StatusInfo, "the log's CRC was not compared (%s); %s", reason, md5)
	} else {
		r.add(id, name, StatusPass, "the tracks, in order, decode to CRC %s, the log's Copy CRC, and %s", total.CRC(), md5)
	}
	return decodedSamples(files)
}

// decodedSamples returns how many samples each file decoded to.
func decodedSamples(files []pcm.Fingerprint) []int64 {
	out := make([]int64, len(files))
	for i, f := range files {
		out[i] = f.Samples()
	}
	return out
}

// crcNotComparable says why the log's Copy CRC cannot be compared with decoded
// audio, or "" if it can: it is the CRC of all the samples only if EAC was told
// to count null samples in it.
func crcNotComparable(l *Log) string {
	if v, _ := l.Setting("Null samples used in CRC calculations"); !strings.EqualFold(v, "Yes") {
		return "it leaves out null samples"
	}
	if l.Range.CopyCRC == "" {
		return "the log has none"
	}
	return ""
}

// imageSamples is how many samples the disc's image holds, from the log and the
// cue sheet, or 0 if that cannot be worked out.
func imageSamples(l *Log, c *Cue) int64 {
	pre, ok := preAudioFrames(l, c)
	if !ok || len(l.TOC) == 0 {
		return 0
	}
	start := l.TOC[0].Start - pre
	if start < 0 {
		return 0
	}
	return expectedImageSamples(l, start)
}

// cuePosition is where a track starts in the image, in frames: its INDEX 01, and
// its INDEX 00, if the cue sheet has one, where its pregap starts.
type cuePosition struct {
	index01, index00 int
	hasIndex00       bool
}

// cuePositions reads where each track of the log starts from the cue sheet.
func cuePositions(l *Log, c *Cue) ([]cuePosition, error) {
	out := make([]cuePosition, len(l.TOC))
	for i, t := range l.TOC {
		var found bool
		for _, ct := range c.Tracks {
			if ct.Number != t.Number {
				continue
			}
			found = true
			var ok bool
			if out[i].index01, ok = ct.Index(1); !ok {
				return nil, fmt.Errorf("the cue sheet gives no INDEX 01 for track %d", t.Number)
			}
			out[i].index00, out[i].hasIndex00 = ct.Index(0)
		}
		if !found {
			return nil, fmt.Errorf("the cue sheet has no track %d", t.Number)
		}
	}
	return out, nil
}

// expectedLengths is how many samples each track of a split should hold, from
// where the cue sheet puts the tracks, keyed by track number.
//
// A track's pregap is the silence or hidden audio before it, which a split has
// to put somewhere. It can go at the end of the track before, so that every track
// begins at its INDEX 01 (pregapsWithNext false), or at the start of the track it
// leads into, so that it begins at its INDEX 00 (true). A track with no INDEX 00
// has no pregap, and begins at its INDEX 01 either way.
//
// Audio before track 1 goes in a track 00 of its own if hidden is set, which
// begins at the start of the image, and track 1 at its INDEX 01. Without one,
// track 1 begins at the start of the image and holds it.
func expectedLengths(l *Log, c *Cue, hidden, pregapsWithNext bool) (map[int]int64, error) {
	pos, err := cuePositions(l, c)
	if err != nil {
		return nil, err
	}
	pre := pos[0].index01
	if start := l.TOC[0].Start - pre; start < 0 {
		return nil, fmt.Errorf("the cue sheet puts track %d later than the log does", l.TOC[0].Number)
	}
	total := int(imageSamples(l, c) / samplesPerSector)

	// begins is where track i starts in the image, in frames.
	begins := func(i int) int {
		if i == 0 {
			if hidden {
				return pos[0].index01
			}
			return 0
		}
		if pregapsWithNext && pos[i].hasIndex00 {
			return pos[i].index00
		}
		return pos[i].index01
	}

	lengths := map[int]int64{}
	if hidden {
		lengths[0] = int64(pre) * samplesPerSector
	}
	for i, t := range l.TOC {
		end := total
		if i+1 < len(l.TOC) {
			end = begins(i + 1)
		}
		if end <= begins(i) {
			return nil, fmt.Errorf("the cue sheet puts the end of track %d before its start", t.Number)
		}
		lengths[t.Number] = int64(end-begins(i)) * samplesPerSector
	}
	return lengths, nil
}

// checkSplitLengths confirms each track is as long as the cue sheet says it
// should be. The audio of a split can be exactly the rip's while every boundary
// is a few samples off, which no check on the audio as a whole can see, and which
// puts the end of one track at the start of the next.
func checkSplitLengths(r *Result, l *Log, c *Cue, tracks []splitTrack, actual map[int]int64, disc int) {
	const id, name = "split-lengths", "Track lengths"

	for _, t := range tracks {
		if actual[t.Number] == 0 {
			r.add(id, name, StatusInfo, "the length of %s is not known, so the lengths were not checked",
				filepath.Base(t.Path))
			return
		}
	}

	hidden := tracks[0].Number == 0
	withPrev, err := expectedLengths(l, c, hidden, false)
	if err != nil {
		r.add(id, name, StatusError, "cannot tell how long the tracks should be: %v", err)
		return
	}
	// The other way uses the cue sheet's INDEX 00 lines, which can be nonsense
	// (the cue pregap check of riplog is the one that says so). A way that
	// cannot be worked out is not one the split can be, and is left out.
	withNext, err := expectedLengths(l, c, hidden, true)
	if err != nil {
		withNext = nil
	}

	// Each track is as long as one of the ways says, or both, or neither.
	var onlyPrev, onlyNext, wrong []int
	for _, t := range tracks {
		n := t.Number
		fitsPrev := actual[n] == withPrev[n]
		fitsNext := withNext != nil && actual[n] == withNext[n]
		switch {
		case fitsPrev && fitsNext:
		case fitsPrev:
			onlyPrev = append(onlyPrev, n)
		case fitsNext:
			onlyNext = append(onlyNext, n)
		default:
			wrong = append(wrong, n)
		}
	}

	switch {
	case len(wrong) == 0 && len(onlyNext) == 0:
		if len(onlyPrev) == 0 {
			r.add(id, name, StatusPass, "each track is the length the cue sheet gives")
		} else {
			r.add(id, name, StatusPass, "each track is the length the cue sheet gives, with each pregap at the end of the track before it")
		}
		return
	case len(wrong) == 0 && len(onlyPrev) == 0:
		r.add(id, name, StatusPass, "each track is the length the cue sheet gives, with each pregap at the start of the track it leads into")
		return
	case len(wrong) == 0:
		// Every track is right for one way or the other, but not the same one.
		r.add(id, name, StatusError,
			"the tracks are not all cut the same way: %s have each pregap at the end of the track before it, and %s at the start of the track it leads into",
			trackList(disc, onlyPrev), trackList(disc, onlyNext))
		return
	}

	// A track that is neither way is the mistake. Say what is wrong with the first,
	// against whichever of the ways it is nearer.
	n := wrong[0]
	nearest := withPrev[n]
	if withNext != nil && abs(actual[n]-withNext[n]) < abs(actual[n]-withPrev[n]) {
		nearest = withNext[n]
	}
	diff := actual[n] - nearest
	how := "longer"
	if diff < 0 {
		diff, how = -diff, "shorter"
	}

	gives := fmt.Sprintf("the cue sheet gives %d", withPrev[n])
	if withNext != nil && withPrev[n] != withNext[n] {
		gives = fmt.Sprintf("the cue sheet gives %d with each pregap at the end of the track before it, or %d with each at the start of its own",
			withPrev[n], withNext[n])
	}
	detail := fmt.Sprintf("track %s holds %d samples, and %s: %s %s than it should be",
		trackLabel(disc, n), actual[n], gives, describeSize(diff), how)
	if len(wrong) > 1 {
		detail += fmt.Sprintf("; and %d more %s differ", len(wrong)-1, plural(int64(len(wrong)-1), "track"))
	}
	r.add(id, name, StatusError, "%s", detail)
}

// trackList names tracks by their labels, a few at a time.
func trackList(disc int, numbers []int) string {
	labels := make([]string, len(numbers))
	for i, n := range numbers {
		labels[i] = trackLabel(disc, n)
	}
	return listLimited(labels)
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
