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
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// TestedVersion is the EAC version this package has been tried against. A log
// from another version is still checked, with a note that the check has not
// been tried on it.
const TestedVersion = "1.8"

// Status is the outcome of one check.
type Status int

const (
	// StatusPass means the check holds.
	StatusPass Status = iota
	// StatusInfo is a note that needs no action.
	StatusInfo
	// StatusWarning means something could not be confirmed, or looks unusual,
	// but nothing is known to be wrong.
	StatusWarning
	// StatusError means a setting is wrong, or the log shows a problem with the
	// rip, or the log cannot be verified as it should be.
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusInfo:
		return "info"
	case StatusWarning:
		return "warning"
	case StatusError:
		return "error"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

// Check is the outcome of one check on a log.
type Check struct {
	// ID is a short stable identifier such as "read-mode".
	ID string
	// Name is a human-readable label such as "Read mode".
	Name   string
	Status Status
	// Detail says what was found and, if the check did not pass, why that
	// matters.
	Detail string
}

// Result is the outcome of checking one log.
type Result struct {
	Checks []Check
}

// Worst returns the most serious status among the checks.
func (r *Result) Worst() Status {
	worst := StatusPass
	for _, c := range r.Checks {
		if c.Status > worst {
			worst = c.Status
		}
	}
	return worst
}

// Count returns how many checks have status s.
func (r *Result) Count(s Status) int {
	n := 0
	for _, c := range r.Checks {
		if c.Status == s {
			n++
		}
	}
	return n
}

// Failed reports whether any check is an error.
func (r *Result) Failed() bool {
	return r.Worst() == StatusError
}

// Check returns the check with the given ID.
func (r *Result) Check(id string) (Check, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

func (r *Result) add(id, name string, s Status, format string, args ...any) {
	r.Checks = append(r.Checks, Check{ID: id, Name: name, Status: s, Detail: fmt.Sprintf(format, args...)})
}

// settingRule is one setting of the profile: a line in the log's header that
// must have a given value.
type settingRule struct {
	id    string
	label string // as the log writes it
	want  string
	// why says what goes wrong otherwise, for the report.
	why string
}

// settingRules are the settings a rip must have been made with. They are the
// profile. Every one is a "label : value" line EAC writes in its header; a rip
// with any of them wrong should be redone, because the error is in the audio,
// not just in the log.
var settingRules = []settingRule{
	{"read-mode", "Read mode", "Secure",
		"only secure mode re-reads sectors until they agree"},
	{"accurate-stream", "Utilize accurate stream", "Yes",
		"without it the drive's reading can drift and goes unnoticed"},
	{"defeat-cache", "Defeat audio cache", "Yes",
		"otherwise re-reads may be served from the drive's cache and hide errors"},
	{"c2", "Make use of C2 pointers", "No",
		"a drive's own error report can be wrong, and trusting it skips re-reads"},
	{"fill-silence", "Fill up missing offset samples with silence", "Yes",
		"otherwise audio is shifted rather than padded at the ends"},
	{"silent-blocks", "Delete leading and trailing silent blocks", "No",
		"it removes audio, so the result no longer matches the disc"},
	{"null-samples", "Null samples used in CRC calculations", "Yes",
		"otherwise CRCs differ from those other rippers and AccurateRip compute"},
	{"id3", "Add ID3 tag", "No",
		"ID3 tags do not belong in FLAC files, which use Vorbis comments"},
}

// Requirement is one setting the profile requires of every log: the label the
// log uses for it, the value it must have, and what goes wrong otherwise.
type Requirement struct {
	Label string
	Want  string
	Why   string
}

// Requirements returns the settings every log must have, in the order they are
// checked. The drive-dependent settings (read offset and overread) are not
// here, since their values are per drive, nor are the checks on the outcome of
// the rip.
func Requirements() []Requirement {
	out := make([]Requirement, len(settingRules))
	for i, rule := range settingRules {
		out[i] = Requirement{Label: rule.label, Want: rule.want, Why: rule.why}
	}
	return out
}

// Verify checks a parsed log against the profile.
func Verify(l *Log) *Result {
	r := &Result{}

	checkVersion(r, l)
	checkRipType(r, l)
	for _, rule := range settingRules {
		checkSetting(r, l, rule)
	}
	checkNormalization(r, l)
	checkEncoder(r, l)
	checkOffset(r, l)
	checkOverread(r, l)
	checkTestCopy(r, l)
	checkErrors(r, l)
	checkAccurateRip(r, l)
	checkWholeDisc(r, l)
	checkCTDB(r, l)
	checkChecksum(r, l)

	return r
}

func checkVersion(r *Result, l *Log) {
	if l.Version == TestedVersion {
		r.add("version", "EAC version", StatusPass, "EAC %s", l.Version)
		return
	}
	r.add("version", "EAC version", StatusInfo,
		"EAC %s; these checks have only been tried with %s", l.Version, TestedVersion)
}

func checkRipType(r *Result, l *Log) {
	if len(l.TOC) == 0 {
		r.add("rip-type", "Rip type", StatusError,
			"the log has no table of contents, so what was ripped cannot be confirmed")
		return
	}
	r.add("rip-type", "Rip type", StatusPass,
		"image rip: %d tracks ripped as one range", len(l.TOC))
}

func checkSetting(r *Result, l *Log, rule settingRule) {
	got, ok := l.Setting(rule.label)
	switch {
	case !ok:
		r.add(rule.id, rule.label, StatusError,
			"%q not found in the log, so it cannot be verified", rule.label)
	case strings.EqualFold(got, rule.want):
		r.add(rule.id, rule.label, StatusPass, "%s", got)
	default:
		r.add(rule.id, rule.label, StatusError,
			"%s, expected %s: %s", got, rule.want, rule.why)
	}
}

func checkNormalization(r *Result, l *Log) {
	if l.Normalization {
		r.add("normalize", "Normalization", StatusError,
			"the log records normalization, which changes the audio")
		return
	}
	r.add("normalize", "Normalization", StatusPass, "none recorded")
}

// optionTokens splits a command line into arguments, keeping a double-quoted
// stretch (such as a tag value that contains spaces) together as one.
func optionTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for _, c := range s {
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteRune(c)
		case c == ' ' && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func checkEncoder(r *Result, l *Log) {
	comp, ok := l.Setting("Command line compressor")
	if !ok {
		r.add("encoder", "Encoder", StatusError, "\"Command line compressor\" not found in the log")
	} else if name := path.Base(strings.ReplaceAll(comp, `\`, "/")); strings.EqualFold(name, "flac.exe") {
		r.add("encoder", "Encoder", StatusPass, "%s", name)
	} else {
		r.add("encoder", "Encoder", StatusError, "%s, expected flac.exe", name)
	}

	opts, ok := l.Setting("Additional command line options")
	if !ok {
		r.add("encoder-verify", "FLAC verification", StatusError,
			"\"Additional command line options\" not found in the log")
		return
	}
	for _, t := range optionTokens(opts) {
		if t == "-V" {
			r.add("encoder-verify", "FLAC verification", StatusPass, "-V is set")
			return
		}
	}
	r.add("encoder-verify", "FLAC verification", StatusError,
		"-V is not set: without it the encoder does not check that the FLAC decodes back to the same audio")
}

// arConfirmsAll reports whether AccurateRip accurately matched every track.
func arConfirmsAll(l *Log) bool {
	if len(l.AR) == 0 {
		return false
	}
	for _, t := range l.AR {
		if t.Status != ARAccurate {
			return false
		}
	}
	return true
}

func checkOffset(r *Result, l *Log) {
	raw, ok := l.Setting("Read offset correction")
	if !ok {
		r.add("offset", "Read offset", StatusError,
			"\"Read offset correction\" not found in the log, so it cannot be verified")
		return
	}
	got, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(raw), "+"))
	if err != nil {
		r.add("offset", "Read offset", StatusError, "%q is not a number", raw)
		return
	}

	drive := normalizeDrive(l.Drive)
	prof, known := knownDrives[drive]
	switch {
	case known && got == prof.Offset:
		r.add("offset", "Read offset", StatusPass, "%+d for %s", got, drive)
	case known:
		r.add("offset", "Read offset", StatusError,
			"%+d for %s, expected %+d: every track is shifted by the difference", got, drive, prof.Offset)
	case arConfirmsAll(l):
		r.add("offset", "Read offset", StatusInfo,
			"%q is not a known drive, but AccurateRip confirms offset %+d: add it to the drive table", drive, got)
	default:
		r.add("offset", "Read offset", StatusWarning,
			"%q is not a known drive and AccurateRip did not confirm the offset (%+d), so it cannot be verified", drive, got)
	}
}

// overreadLabel is how the log names the overread setting.
const overreadLabel = "Overread into Lead-In and Lead-Out"

// checkOverread confirms the overread setting is what is known for the drive.
// Whether it should be on depends on the drive, so for a drive that is not in
// the table there is nothing to compare it with.
func checkOverread(r *Result, l *Log) {
	drive := normalizeDrive(l.Drive)
	prof, known := knownDrives[drive]
	if !known {
		r.add("overread", "Overread", StatusInfo,
			"%q is not a known drive, so whether overread should be on is not verified", drive)
		return
	}

	raw, ok := l.Setting(overreadLabel)
	if !ok {
		r.add("overread", "Overread", StatusError,
			"%q not found in the log, so it cannot be verified", overreadLabel)
		return
	}

	var got bool
	switch {
	case strings.EqualFold(raw, "Yes"):
		got = true
	case strings.EqualFold(raw, "No"):
		got = false
	default:
		r.add("overread", "Overread", StatusError, "%q is neither Yes nor No", raw)
		return
	}

	switch {
	case got == prof.Overread:
		r.add("overread", "Overread", StatusPass, "%s for %s", raw, drive)
	case prof.Overread:
		r.add("overread", "Overread", StatusError,
			"%s, but %s can overread: without it the samples the offset needs from beyond the ends of the disc are filled with silence instead of read",
			raw, drive)
	default:
		r.add("overread", "Overread", StatusError,
			"%s, but %s is recorded as not overreading: if it was turned on deliberately, update the drive table",
			raw, drive)
	}
}

func checkTestCopy(r *Result, l *Log) {
	rg := l.Range
	switch {
	case rg.TestCRC == "":
		r.add("test-copy", "Test & Copy", StatusError,
			"no Test CRC: the disc was copied but not tested, so nothing confirms the copy was read consistently")
	case rg.CopyCRC == "":
		r.add("test-copy", "Test & Copy", StatusError, "no Copy CRC: the copy pass did not complete")
	case rg.TestCRC != rg.CopyCRC:
		r.add("test-copy", "Test & Copy", StatusError,
			"test CRC %s and copy CRC %s differ: the disc did not read the same way twice", rg.TestCRC, rg.CopyCRC)
	case !rg.CopyOK:
		r.add("test-copy", "Test & Copy", StatusError, "the log does not say \"Copy OK\"")
	default:
		r.add("test-copy", "Test & Copy", StatusPass, "test and copy CRCs match (%s)", rg.CopyCRC)
	}
}

func checkErrors(r *Result, l *Log) {
	rg := l.Range
	switch {
	case rg.NoErrors && len(rg.Problems) == 0:
		r.add("errors", "Errors", StatusPass, "none reported")
	case len(rg.Problems) > 0:
		r.add("errors", "Errors", StatusError, "%s", strings.Join(rg.Problems, "; "))
	default:
		r.add("errors", "Errors", StatusError, "the log does not say \"No errors occurred\"")
	}
}

func checkAccurateRip(r *Result, l *Log) {
	if len(l.AR) == 0 {
		r.add("accuraterip", "AccurateRip", StatusWarning, "the log has no AccurateRip results")
		return
	}

	var accurate, mismatch, absent, other int
	lowest := 0
	for _, t := range l.AR {
		switch t.Status {
		case ARAccurate:
			accurate++
			if lowest == 0 || t.Confidence < lowest {
				lowest = t.Confidence
			}
		case ARMismatch:
			mismatch++
		case ARNotPresent:
			absent++
		default:
			other++
		}
	}

	n := len(l.AR)
	switch {
	case accurate == n:
		r.add("accuraterip", "AccurateRip", StatusPass,
			"all %d tracks accurately ripped (lowest confidence %d)", n, lowest)
	case absent == n:
		r.add("accuraterip", "AccurateRip", StatusWarning,
			"the disc is not in the AccurateRip database, so the rip is not independently confirmed")
	default:
		r.add("accuraterip", "AccurateRip", StatusWarning,
			"%d of %d tracks accurately ripped (%d not matching, %d not in the database, %d not verifiable)",
			accurate, n, mismatch, absent, other)
	}
}

func checkWholeDisc(r *Result, l *Log) {
	if len(l.AR) == 0 || len(l.TOC) == 0 {
		r.add("whole-disc", "Whole disc", StatusInfo,
			"cannot confirm the whole disc was ripped without AccurateRip results")
		return
	}
	if len(l.AR) == len(l.TOC) {
		r.add("whole-disc", "Whole disc", StatusPass, "results cover all %d tracks", len(l.TOC))
		return
	}
	r.add("whole-disc", "Whole disc", StatusWarning,
		"AccurateRip results cover %d of %d tracks: was the whole disc ripped?", len(l.AR), len(l.TOC))
}

func checkCTDB(r *Result, l *Log) {
	if l.CTDB == nil {
		r.add("ctdb", "CUETools DB", StatusWarning,
			"no CUETools DB result in the log (is the plugin running?)")
		return
	}
	if !l.CTDB.Found {
		r.add("ctdb", "CUETools DB", StatusInfo, "the disc was not yet in the database; the result was submitted")
		return
	}

	unconfirmed := 0
	for _, t := range l.CTDB.Tracks {
		if !strings.Contains(strings.ToLower(t), "accurately ripped") {
			unconfirmed++
		}
	}
	switch {
	case len(l.CTDB.Tracks) == 0:
		r.add("ctdb", "CUETools DB", StatusWarning, "the log lists no tracks for the database result")
	case unconfirmed == 0:
		r.add("ctdb", "CUETools DB", StatusPass, "all %d tracks accurately ripped", len(l.CTDB.Tracks))
	default:
		r.add("ctdb", "CUETools DB", StatusWarning,
			"%d of %d tracks not confirmed by the database", unconfirmed, len(l.CTDB.Tracks))
	}
}

var reHex64 = regexp.MustCompile(`^[0-9A-Fa-f]{64}$`)

// checkChecksum confirms the log carries EAC's checksum line. It does not
// verify it: that would show the log is unedited, which matters for a log
// from someone else and not for one's own. A missing or malformed checksum on
// a fresh rip means the log was edited or cut short, or came from an old EAC.
func checkChecksum(r *Result, l *Log) {
	switch {
	case l.Checksum == "":
		r.add("checksum", "Log checksum", StatusError,
			"the log has no checksum: it was edited, cut short, or written by an old EAC")
	case !reHex64.MatchString(l.Checksum):
		r.add("checksum", "Log checksum", StatusError, "the checksum is malformed (%q)", truncate(l.Checksum, 20))
	default:
		r.add("checksum", "Log checksum", StatusPass, "present (not verified)")
	}
}
