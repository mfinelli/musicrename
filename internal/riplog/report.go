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
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FindLogs returns the EAC log files directly inside dir (those named *.log,
// whatever the case of the extension), in name order. It does not descend into
// subdirectories. A rip directory holds one log per disc, so a multi-disc
// release yields several.
func FindLogs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var logs []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".log") {
			logs = append(logs, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(logs)
	return logs, nil
}

// Describe returns a one-line description of the log: the artist and album,
// then the EAC version and the number of tracks. For example:
// "AC/DC / Back in Black (EAC 1.8, 10 tracks)".
func (l *Log) Describe() string {
	tracks := "no tracks"
	switch n := len(l.TOC); n {
	case 0:
	case 1:
		tracks = "1 track"
	default:
		tracks = fmt.Sprintf("%d tracks", n)
	}

	title := l.Title
	if title == "" {
		title = "untitled"
	}
	return fmt.Sprintf("%s (EAC %s, %s)", title, l.Version, tracks)
}

// Findings returns the checks that did not simply pass (notes, warnings and
// errors), in the order they were made.
func (r *Result) Findings() []Check {
	var out []Check
	for _, c := range r.Checks {
		if c.Status != StatusPass {
			out = append(out, c)
		}
	}
	return out
}

// PassSummary says how many checks passed, in a form to follow a list of the
// ones that did not: "all 20 checks passed" when every check passed, otherwise
// "19 other checks passed".
func (r *Result) PassSummary() string {
	passed := r.Count(StatusPass)
	noun := "checks"
	if passed == 1 {
		noun = "check"
	}

	switch {
	case passed == 0:
		return "no checks passed"
	case passed == len(r.Checks):
		return fmt.Sprintf("all %d %s passed", passed, noun)
	default:
		return fmt.Sprintf("%d other %s passed", passed, noun)
	}
}

// Fails reports whether the result should make a run fail: any error, or,
// when strict is set, any warning as well. Notes never do.
func (r *Result) Fails(strict bool) bool {
	w := r.Worst()
	return w == StatusError || (strict && w == StatusWarning)
}
