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
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allCheckIDs lists every check Verify makes, in order. A rule that stops being
// made, or a new one, shows up here as a deliberate change.
var allCheckIDs = []string{
	"version", "rip-type",
	"read-mode", "accurate-stream", "defeat-cache", "c2",
	"fill-silence", "silent-blocks", "null-samples", "id3",
	"normalize", "encoder", "encoder-verify", "offset",
	"test-copy", "errors", "accuraterip", "whole-disc", "ctdb", "checksum",
}

// want asserts that the check with the given ID has the given status, and
// returns it for further assertions.
func want(t *testing.T, r *Result, id string, s Status) Check {
	t.Helper()
	c, ok := r.Check(id)
	require.True(t, ok, "no check %q", id)
	require.Equal(t, s, c.Status, "check %q: %s", id, c.Detail)
	return c
}

func TestVerifyRealLogs(t *testing.T) {
	for _, name := range allFixtures {
		t.Run(name, func(t *testing.T) {
			l, err := ParseLog(fixture(t, name))
			require.NoError(t, err)

			r := Verify(l)

			var ids []string
			for _, c := range r.Checks {
				ids = append(ids, c.ID)
				assert.NotEmpty(t, c.Name, c.ID)
				assert.NotEmpty(t, c.Detail, c.ID)
			}
			assert.Equal(t, allCheckIDs, ids)
			assert.False(t, r.Failed(), "a good rip must not fail")
			assert.Equal(t, 0, r.Count(StatusError))
		})
	}

	t.Run("a complete rip passes everything", func(t *testing.T) {
		for _, name := range []string{fixtureBlack, fixtureCentral} {
			r := verify(t, fixtureText(t, name))

			assert.Equal(t, StatusPass, r.Worst(), name)
			assert.Equal(t, len(allCheckIDs), r.Count(StatusPass), name)
		}
	})

	t.Run("the log without a CUETools DB section is only warned about", func(t *testing.T) {
		r := verify(t, fixtureText(t, fixtureBday))

		assert.Equal(t, StatusWarning, r.Worst())
		assert.Equal(t, 1, r.Count(StatusWarning))
		c := want(t, r, "ctdb", StatusWarning)
		assert.Contains(t, c.Detail, "plugin")
	})

	t.Run("the details say what was found", func(t *testing.T) {
		r := verify(t, fixtureText(t, fixtureBlack))

		assert.Equal(t, "Secure", want(t, r, "read-mode", StatusPass).Detail)
		assert.Contains(t, want(t, r, "offset", StatusPass).Detail, "+6")
		assert.Contains(t, want(t, r, "offset", StatusPass).Detail, "asus sdrw-08v1m-u")
		assert.Contains(t, want(t, r, "rip-type", StatusPass).Detail, "10 tracks")
		assert.Contains(t, want(t, r, "test-copy", StatusPass).Detail, "9FB18E2F")
		assert.Contains(t, want(t, r, "accuraterip", StatusPass).Detail, "all 10 tracks")
		assert.Contains(t, want(t, r, "accuraterip", StatusPass).Detail, "lowest confidence 200")
		assert.Contains(t, want(t, r, "checksum", StatusPass).Detail, "not verified")
	})
}

func TestVerifySettings(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	for _, rule := range settingRules {
		t.Run(rule.label, func(t *testing.T) {
			t.Run("a wrong value is an error that says what and why", func(t *testing.T) {
				wrong := "No"
				if rule.want == "No" {
					wrong = "Yes"
				}
				if rule.want == "Secure" {
					wrong = "Burst"
				}

				r := verify(t, withSetting(t, good, rule.label, wrong))

				c := want(t, r, rule.id, StatusError)
				assert.Contains(t, c.Detail, wrong)
				assert.Contains(t, c.Detail, "expected "+rule.want)
				assert.Contains(t, c.Detail, rule.why)
				assert.True(t, r.Failed())
			})

			t.Run("a missing line is an error, never a pass", func(t *testing.T) {
				r := verify(t, withoutLine(t, good, rule.label))

				c := want(t, r, rule.id, StatusError)
				assert.Contains(t, c.Detail, "not found")
			})

			t.Run("the right value passes whatever its case", func(t *testing.T) {
				r := verify(t, withSetting(t, good, rule.label, otherCase(rule.want)))

				want(t, r, rule.id, StatusPass)
			})
		})
	}

	t.Run("only the broken setting fails", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Defeat audio cache", "No"))

		assert.Equal(t, 1, r.Count(StatusError))
		want(t, r, "defeat-cache", StatusError)
	})

	t.Run("every rule has a distinct id and a reason", func(t *testing.T) {
		seen := map[string]bool{}
		for _, rule := range settingRules {
			assert.False(t, seen[rule.id], rule.id)
			seen[rule.id] = true
			assert.NotEmpty(t, rule.why, rule.id)
			assert.NotEmpty(t, rule.want, rule.id)
		}
	})
}

// otherCase returns s lower-cased: a different spelling of the same value, to
// show comparisons ignore case.
func otherCase(s string) string { return strings.ToLower(s) }

func TestVerifyNormalization(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	r := verify(t, replaced(t, good,
		"Null samples used in CRC calculations       : Yes",
		"Null samples used in CRC calculations       : Yes\nNormalize to                                : 98 %"))

	c := want(t, r, "normalize", StatusError)
	assert.Contains(t, c.Detail, "changes the audio")
}

func TestVerifyEncoder(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("a different encoder", func(t *testing.T) {
		r := verify(t, replaced(t, good, `FLAC\FLAC.EXE`, `LAME\LAME.EXE`))

		c := want(t, r, "encoder", StatusError)
		assert.Contains(t, c.Detail, "LAME.EXE")
		want(t, r, "encoder-verify", StatusPass)
	})

	t.Run("the path's case and separators do not matter", func(t *testing.T) {
		r := verify(t, replaced(t, good, `Z:\home\mario\.exact-audio-copy\eac\FLAC\FLAC.EXE`, `/usr/bin/flac.exe`))

		want(t, r, "encoder", StatusPass)
	})

	t.Run("without -V", func(t *testing.T) {
		r := verify(t, replaced(t, good, " -V ", " "))

		c := want(t, r, "encoder-verify", StatusError)
		assert.Contains(t, c.Detail, "-V")
		want(t, r, "encoder", StatusPass)
	})

	t.Run("a -V inside a quoted tag is not the option", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Additional command line options", `-6 -T "COMMENT=x -V y" %source%`))

		want(t, r, "encoder-verify", StatusError)
	})

	t.Run("either line missing", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Command line compressor"))
		want(t, r, "encoder", StatusError)

		r = verify(t, withoutLine(t, good, "Additional command line options"))
		want(t, r, "encoder-verify", StatusError)
	})
}

func TestOptionTokens(t *testing.T) {
	assert.Equal(t, []string{"-6", "-V", `-T`, `"A=b c"`, "%source%"},
		optionTokens(`-6 -V -T "A=b c" %source%`))
	assert.Equal(t, []string{"a", "b"}, optionTokens("  a    b  "))
	assert.Empty(t, optionTokens(""))
}

func TestVerifyOffset(t *testing.T) {
	good := fixtureText(t, fixtureBlack)
	const unknownDrive = "Fictional  FX-1  Adapter: 1  ID: 0"

	t.Run("the known drive with its offset", func(t *testing.T) {
		want(t, verify(t, good), "offset", StatusPass)
	})

	t.Run("a different adapter does not matter", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Used drive", "ASUS    SDRW-08V1M-U   Adapter: 4  ID: 2"))

		want(t, r, "offset", StatusPass)
	})

	t.Run("a known drive with another offset is an error", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Read offset correction", "30"))

		c := want(t, r, "offset", StatusError)
		assert.Contains(t, c.Detail, "+30")
		assert.Contains(t, c.Detail, "expected +6")
	})

	t.Run("a negative offset is read as negative", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Read offset correction", "-24"))

		c := want(t, r, "offset", StatusError)
		assert.Contains(t, c.Detail, "-24")
	})

	t.Run("an unknown drive that AccurateRip confirms is a note to add it", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Used drive", unknownDrive))

		c := want(t, r, "offset", StatusInfo)
		assert.Contains(t, c.Detail, "fictional fx-1")
		assert.Contains(t, c.Detail, "AccurateRip confirms offset +6")
		assert.Contains(t, c.Detail, "add it to the drive table")
		assert.False(t, r.Failed())
	})

	t.Run("an unknown drive that AccurateRip cannot confirm is a warning", func(t *testing.T) {
		text := asNotInAccurateRip(t, withSetting(t, good, "Used drive", unknownDrive))

		r := verify(t, text)

		c := want(t, r, "offset", StatusWarning)
		assert.Contains(t, c.Detail, "cannot be verified")
	})

	t.Run("an unknown drive with no AccurateRip results at all", func(t *testing.T) {
		text := withSetting(t, good, "Used drive", unknownDrive)
		text = replaced(t, text, "AccurateRip summary", "Something summary")

		r := verify(t, text)

		want(t, r, "offset", StatusWarning)
	})

	t.Run("a non-numeric offset", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Read offset correction", "six"))

		want(t, r, "offset", StatusError)
	})

	t.Run("a missing offset", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Read offset correction"))

		c := want(t, r, "offset", StatusError)
		assert.Contains(t, c.Detail, "not found")
	})

	t.Run("a plus sign is accepted", func(t *testing.T) {
		r := verify(t, withSetting(t, good, "Read offset correction", "+6"))

		want(t, r, "offset", StatusPass)
	})
}

var reARAccurate = regexp.MustCompile(`(?m)^(Track +\d+) +accurately ripped.*$`)

// asNotInAccurateRip rewrites every accurately-ripped track line to say the
// track is not in the database.
func asNotInAccurateRip(t *testing.T, text string) string {
	t.Helper()
	require.True(t, reARAccurate.MatchString(text))
	return reARAccurate.ReplaceAllString(text, "$1  not present in database")
}

func TestVerifyTestCopy(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("no test pass", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Test CRC"))

		c := want(t, r, "test-copy", StatusError)
		assert.Contains(t, c.Detail, "not tested")
	})

	t.Run("no copy pass", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Copy CRC"))

		c := want(t, r, "test-copy", StatusError)
		assert.Contains(t, c.Detail, "did not complete")
	})

	t.Run("CRCs that differ", func(t *testing.T) {
		r := verify(t, replaced(t, good, "Copy CRC 9FB18E2F", "Copy CRC 9FB18E30"))

		c := want(t, r, "test-copy", StatusError)
		assert.Contains(t, c.Detail, "9FB18E2F")
		assert.Contains(t, c.Detail, "9FB18E30")
		assert.Contains(t, c.Detail, "differ")
	})

	t.Run("no Copy OK", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Copy OK"))

		c := want(t, r, "test-copy", StatusError)
		assert.Contains(t, c.Detail, "Copy OK")
	})
}

func TestVerifyErrors(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("a reported problem", func(t *testing.T) {
		r := verify(t, replaced(t, good, "No errors occurred", "Suspicious position 0:01:23\nTiming problem at 0:02:10"))

		c := want(t, r, "errors", StatusError)
		assert.Contains(t, c.Detail, "Suspicious position 0:01:23")
		assert.Contains(t, c.Detail, "Timing problem at 0:02:10")
	})

	t.Run("a problem is an error even if the log also says no errors", func(t *testing.T) {
		r := verify(t, replaced(t, good, "No errors occurred", "No errors occurred\nMissing sample at 0:01:23"))

		want(t, r, "errors", StatusError)
	})

	t.Run("silence about errors is not a pass", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "No errors occurred"))

		c := want(t, r, "errors", StatusError)
		assert.Contains(t, c.Detail, "No errors occurred")
	})
}

func TestVerifyAccurateRip(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("none of the disc in the database", func(t *testing.T) {
		r := verify(t, asNotInAccurateRip(t, good))

		c := want(t, r, "accuraterip", StatusWarning)
		assert.Contains(t, c.Detail, "not in the AccurateRip database")
		assert.False(t, r.Failed(), "an unknown disc is not a bad rip")
	})

	t.Run("one track that does not match", func(t *testing.T) {
		r := verify(t, regexp.MustCompile(`(?m)^Track  5 .*$`).ReplaceAllString(good,
			"Track  5  cannot be verified as accurate (confidence 2)  [AAAA], AccurateRip returned [BBBB]  (AR v2)"))

		c := want(t, r, "accuraterip", StatusWarning)
		assert.Contains(t, c.Detail, "9 of 10")
		assert.Contains(t, c.Detail, "1 not matching")
	})

	t.Run("a track EAC could not look up", func(t *testing.T) {
		r := verify(t, regexp.MustCompile(`(?m)^Track  3 .*$`).ReplaceAllString(good,
			"Track  3  not fully ripped for AccurateRip lookup"))

		c := want(t, r, "accuraterip", StatusWarning)
		assert.Contains(t, c.Detail, "9 of 10")
		assert.Contains(t, c.Detail, "1 not verifiable")
	})

	t.Run("no results at all", func(t *testing.T) {
		r := verify(t, replaced(t, good, "AccurateRip summary", "Something summary"))

		want(t, r, "accuraterip", StatusWarning)
		want(t, r, "whole-disc", StatusInfo)
	})

	t.Run("results for fewer tracks than the disc has", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "Track 10 "))

		c := want(t, r, "whole-disc", StatusWarning)
		assert.Contains(t, c.Detail, "9 of 10")
		assert.Contains(t, c.Detail, "whole disc")
	})

	t.Run("a log without a table of contents", func(t *testing.T) {
		text := regexp.MustCompile(`(?m)^\s+\d+\s+\|\s+[\d:.]+\s+\|.*$`).ReplaceAllString(good, "")

		r := verify(t, text)

		want(t, r, "rip-type", StatusError)
		want(t, r, "whole-disc", StatusInfo)
	})
}

func TestVerifyCTDB(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("a disc not yet in the database", func(t *testing.T) {
		r := verify(t, replaced(t, good, "] found", "] disk not present in database"))

		want(t, r, "ctdb", StatusInfo)
	})

	t.Run("a track the database does not confirm", func(t *testing.T) {
		r := verify(t, replaced(t, good, "(3472/3497) Accurately ripped", "(0/3497) No match"))

		c := want(t, r, "ctdb", StatusWarning)
		assert.Contains(t, c.Detail, "1 of 10")
	})

	t.Run("alternative results are still accurate", func(t *testing.T) {
		// Track 6 of this log reads "Accurately ripped, or (8/3497) differs in
		// 43 samples": a different pressing's result listed beside the match.
		want(t, verify(t, good), "ctdb", StatusPass)
	})

	t.Run("a section with no tracks", func(t *testing.T) {
		text := regexp.MustCompile(`(?m)^ +\d+ +\| .*$`).ReplaceAllString(good, "")

		r := verify(t, text)

		want(t, r, "ctdb", StatusWarning)
	})
}

func TestVerifyChecksum(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("missing", func(t *testing.T) {
		r := verify(t, withoutLine(t, good, "==== Log checksum"))

		c := want(t, r, "checksum", StatusError)
		assert.Contains(t, c.Detail, "no checksum")
	})

	t.Run("malformed", func(t *testing.T) {
		r := verify(t, regexp.MustCompile(`==== Log checksum \S+ ====`).ReplaceAllString(good, "==== Log checksum 1234 ===="))

		c := want(t, r, "checksum", StatusError)
		assert.Contains(t, c.Detail, "malformed")
	})

	t.Run("it is not verified, so an edited log with its old checksum still passes", func(t *testing.T) {
		// This is why the fixtures can be edited into bad logs and still be
		// useful: nothing here depends on the checksum matching the text.
		r := verify(t, withSetting(t, good, "Defeat audio cache", "No"))

		want(t, r, "checksum", StatusPass)
	})
}

func TestVerifyVersion(t *testing.T) {
	good := fixtureText(t, fixtureBlack)

	t.Run("the tested version", func(t *testing.T) {
		want(t, verify(t, good), "version", StatusPass)
	})

	t.Run("another 1.x version is checked, with a note", func(t *testing.T) {
		r := verify(t, replaced(t, good, "V1.8 from", "V1.9 from"))

		c := want(t, r, "version", StatusInfo)
		assert.Contains(t, c.Detail, "1.9")
		assert.Contains(t, c.Detail, TestedVersion)
		assert.False(t, r.Failed())
	})

	t.Run("a beta version keeps its suffix", func(t *testing.T) {
		r := verify(t, replaced(t, good, "V1.8 from", "V1.0 beta 3 from"))

		c := want(t, r, "version", StatusInfo)
		assert.Contains(t, c.Detail, "1.0 beta 3")
	})
}

func TestResult(t *testing.T) {
	r := &Result{}
	r.add("a", "A", StatusPass, "fine")
	r.add("b", "B", StatusInfo, "note %d", 1)
	r.add("c", "C", StatusWarning, "hmm")

	t.Run("worst", func(t *testing.T) {
		assert.Equal(t, StatusWarning, r.Worst())
		assert.False(t, r.Failed())

		r.add("d", "D", StatusError, "bad")
		assert.Equal(t, StatusError, r.Worst())
		assert.True(t, r.Failed())
	})

	t.Run("count and lookup", func(t *testing.T) {
		assert.Equal(t, 1, r.Count(StatusInfo))
		assert.Equal(t, 0, r.Count(Status(99)))

		c, ok := r.Check("b")
		assert.True(t, ok)
		assert.Equal(t, "note 1", c.Detail)

		_, ok = r.Check("nope")
		assert.False(t, ok)
	})

	t.Run("an empty result passes", func(t *testing.T) {
		assert.Equal(t, StatusPass, (&Result{}).Worst())
	})

	t.Run("status names", func(t *testing.T) {
		assert.Equal(t, "pass", StatusPass.String())
		assert.Equal(t, "info", StatusInfo.String())
		assert.Equal(t, "warning", StatusWarning.String())
		assert.Equal(t, "error", StatusError.String())
		assert.Equal(t, "Status(9)", Status(9).String())
	})

	t.Run("statuses are ordered by severity", func(t *testing.T) {
		assert.Less(t, int(StatusPass), int(StatusInfo))
		assert.Less(t, int(StatusInfo), int(StatusWarning))
		assert.Less(t, int(StatusWarning), int(StatusError))
	})
}
