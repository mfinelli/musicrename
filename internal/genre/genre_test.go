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

package genre

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestList(t *testing.T) {
	t.Run("has Winamp's 192 genres and Christmas", func(t *testing.T) {
		// Winamp's indices 0 to 191, plus Christmas, which some copies of the
		// list have at 148 in place of Abstract.
		assert.Len(t, allowed, 193)
	})

	t.Run("has no empty entry or padding", func(t *testing.T) {
		for _, g := range allowed {
			assert.NotEmpty(t, g)
			assert.Equal(t, strings.TrimSpace(g), g, "%q", g)
		}
	})

	t.Run("has no duplicate, even in another case", func(t *testing.T) {
		seen := map[string]string{}
		for _, g := range allowed {
			key := strings.ToLower(g)
			if prev, ok := seen[key]; ok {
				t.Errorf("%q and %q are the same genre", prev, g)
			}
			seen[key] = g
		}
		// Canonical depends on there being one spelling per name.
		assert.Len(t, byFold, len(allowed))
		assert.Len(t, byExact, len(allowed))
	})

	t.Run("has none with the multi-value separator", func(t *testing.T) {
		for _, g := range allowed {
			assert.NotContains(t, g, ";", "%q", g)
		}
	})
}

func TestValid(t *testing.T) {
	tests := []struct {
		name  string
		genre string
		want  bool
	}{
		{"a plain genre", "Rock", true},
		{"the first genre", "Blues", true},
		{"the last Winamp genre", "Psybient", true},
		{"a genre of two words", "Classic Rock", true},
		{"a genre with a slash", "Folk/Rock", true},
		{"a genre with a plus", "Jazz+Funk", true},
		{"a genre with an ampersand", "R&B", true},
		{"a genre with a full stop", "Alt. Rock", true},
		{"a genre with digits", "Top 40", true},
		{"Abstract, which Winamp has at 148", "Abstract", true},
		{"Christmas, which Wikipedia has at 148", "Christmas", true},
		{"one of Winamp's jokes", "Primus", true},
		{"another of Winamp's jokes", "Porn Groove", true},
		{"Meditative, not EAC's Mediative", "Meditative", true},
		{"Bebop, not EAC's Bebob", "Bebop", true},
		{"A Cappella, not EAC's A Capella", "A Cappella", true},
		{"Afro-Punk, not EAC's Negerpunk", "Afro-Punk", true},
		{"JPop as written, not Jpop", "JPop", true},
		{"Synthpop as written, not SynthPop", "Synthpop", true},

		{"the empty string", "", false},
		{"a genre in lower case", "rock", false},
		{"a genre in upper case", "ROCK", false},
		{"a genre in the wrong case in the middle", "Classic rock", false},
		{"a genre with a leading space", " Rock", false},
		{"a genre with a trailing space", "Rock ", false},
		{"a genre that is not on the list", "Synthwave", false},
		{"Folk-Rock, Winamp 5.63's spelling", "Folk-Rock", false},
		{"Mediative, EAC's spelling", "Mediative", false},
		{"Bebob, EAC's spelling", "Bebob", false},
		{"A Capella, EAC's spelling", "A Capella", false},
		{"Negerpunk, EAC's name", "Negerpunk", false},
		{"Jpop, FFmpeg's and Wikipedia's spelling", "Jpop", false},
		{"SynthPop, FFmpeg's old spelling", "SynthPop", false},
		{"Fast Fusion, FFmpeg's spelling", "Fast Fusion", false},
		{"two genres joined by the separator", "Rock;Pop", false},
		{"two genres joined by the separator and a space", "Rock; Pop", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Valid(tt.genre), "%q", tt.genre)
		})
	}

	t.Run("accepts every genre of the list", func(t *testing.T) {
		for _, g := range allowed {
			assert.True(t, Valid(g), "%q", g)
		}
	})
}

func TestCanonical(t *testing.T) {
	tests := []struct {
		name   string
		genre  string
		want   string
		wantOK bool
	}{
		{"a genre already right", "Rock", "Rock", true},
		{"lower case", "rock", "Rock", true},
		{"upper case", "ROCK", "Rock", true},
		{"a genre of two words in lower case", "classic rock", "Classic Rock", true},
		{"a genre with a slash", "folk/rock", "Folk/Rock", true},
		{"a genre whose spelling has capitals inside", "jpop", "JPop", true},
		{"a genre whose spelling has capitals inside, in upper case", "BRITPOP", "BritPop", true},

		{"nothing", "", "", false},
		{"a genre that is not on the list", "synthwave", "", false},
		{"a different punctuation is not a different case", "folk-rock", "", false},
		{"a different spelling is not a different case", "Jpop2", "", false},
		{"padding is not ignored", " rock", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Canonical(tt.genre)
			assert.Equal(t, tt.wantOK, ok, "%q", tt.genre)
			assert.Equal(t, tt.want, got, "%q", tt.genre)
		})
	}

	t.Run("finds the spelling Valid accepts for every genre of the list", func(t *testing.T) {
		for _, g := range allowed {
			got, ok := Canonical(strings.ToLower(g))
			assert.True(t, ok, "%q", g)
			assert.Equal(t, g, got)
			assert.True(t, Valid(got), "%q", got)
		}
	})
}
