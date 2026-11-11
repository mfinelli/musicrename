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

// Package genre is the list of genres the library allows in its GENRE tag.
//
// The list is closed and is kept in code, like the other short lists the
// project keeps there (the rip log settings, the known drives): a genre is
// either on it, spelled exactly as it is here, or it is a finding of
// "mrr check". Matching is exact and case-sensitive, so "Rock" is allowed and
// "rock" is not. A genre is stored as text; the numbers ID3v1 gave genres
// (shown in the comments beside the entries, for reference only) are never
// written.
//
// # Source
//
// The list is Winamp's extended ID3v1 list, indices 0 to 191, which is the de
// facto standard: the original ID3v1 list (Eric Kemp, 1999) stopped at 79 and
// Winamp added the rest, the last of it in 5.6 (November 2010). Exact Audio
// Copy's genre menu is the same list as far as index 147 (Synthpop) and has
// nothing after it. The entries that are Winamp's jokes (Primus, Porn
// Groove, Pranks, Polsk Punk, ...) are kept to stay true to Winamp.
//
// # Where the lists differ
//
// The copies of this list found in various tools do not agree on every
// spelling, and Winamp itself is defunct, so there is no authority to ask. The
// spellings here are the project's choice, made as follows. A difference from
// a tool is also noted beside the entry it concerns.
//
//   - Meditative, Bebop, A Cappella: the correct spellings, which the Winamp
//     5.63 list has and EAC's menu gets wrong (Mediative, Bebob, A Capella).
//   - Afro-Punk: Winamp's name for what EAC's menu calls Negerpunk.
//   - Folk/Rock: the Winamp 5.63 list has Folk-Rock. EAC and squell/id3 have
//     Folk/Rock, which is kept. The slash does not clash with the separator
//     that would be used for several genres (below).
//   - JPop: FFmpeg and Wikipedia have Jpop. Synthpop is the same everywhere
//     but in FFmpeg, which had SynthPop until 2019.
//   - Abstract and Christmas: both are allowed, although they are the same
//     index (148) in different copies of the list. Wikipedia has Christmas
//     there; Winamp 5.63, FFmpeg and squell/id3 have Abstract.
//   - Fast-Fusion: FFmpeg spells it with a space, Fast Fusion.
//   - Wikipedia writes the list in sentence case (Thrash metal, Art rock);
//     every other source, and this list, in title case.
//
// # Several genres
//
// A track has one genre for now. If several are ever supported, they would be
// separated with a semicolon (";"), so no allowed genre may contain one;
// TestList checks that. Until then a value that has one is simply not on the
// list.
package genre

import "strings"

// allowed is every allowed genre, spelled as it is to be written, in Winamp's
// order.
var allowed = []string{
	// ID3v1 (Eric Kemp, 0-79).
	"Blues",             // 0
	"Classic Rock",      // 1
	"Country",           // 2
	"Dance",             // 3
	"Disco",             // 4
	"Funk",              // 5
	"Grunge",            // 6
	"Hip-Hop",           // 7
	"Jazz",              // 8
	"Metal",             // 9
	"New Age",           // 10
	"Oldies",            // 11
	"Other",             // 12
	"Pop",               // 13
	"R&B",               // 14
	"Rap",               // 15
	"Reggae",            // 16
	"Rock",              // 17
	"Techno",            // 18
	"Industrial",        // 19
	"Alternative",       // 20
	"Ska",               // 21
	"Death Metal",       // 22
	"Pranks",            // 23
	"Soundtrack",        // 24
	"Euro-Techno",       // 25
	"Ambient",           // 26
	"Trip-Hop",          // 27
	"Vocal",             // 28
	"Jazz+Funk",         // 29
	"Fusion",            // 30
	"Trance",            // 31
	"Classical",         // 32
	"Instrumental",      // 33
	"Acid",              // 34
	"House",             // 35
	"Game",              // 36
	"Sound Clip",        // 37
	"Gospel",            // 38
	"Noise",             // 39
	"Alt. Rock",         // 40
	"Bass",              // 41
	"Soul",              // 42
	"Punk",              // 43
	"Space",             // 44
	"Meditative",        // 45 — EAC: "Mediative"
	"Instrumental Pop",  // 46
	"Instrumental Rock", // 47
	"Ethnic",            // 48
	"Gothic",            // 49
	"Darkwave",          // 50
	"Techno-Industrial", // 51
	"Electronic",        // 52
	"Pop-Folk",          // 53
	"Eurodance",         // 54
	"Dream",             // 55
	"Southern Rock",     // 56
	"Comedy",            // 57
	"Cult",              // 58
	"Gangsta Rap",       // 59
	"Top 40",            // 60
	"Christian Rap",     // 61
	"Pop/Funk",          // 62
	"Jungle",            // 63
	"Native American",   // 64
	"Cabaret",           // 65
	"New Wave",          // 66
	"Psychedelic",       // 67
	"Rave",              // 68
	"Showtunes",         // 69
	"Trailer",           // 70
	"Lo-Fi",             // 71
	"Tribal",            // 72
	"Acid Punk",         // 73
	"Acid Jazz",         // 74
	"Polka",             // 75
	"Retro",             // 76
	"Musical",           // 77
	"Rock & Roll",       // 78
	"Hard Rock",         // 79

	// Winamp extensions, 1998 and earlier (80-147).
	"Folk",                   // 80
	"Folk/Rock",              // 81 — Winamp 5.63: "Folk-Rock"; EAC and squell/id3: "Folk/Rock"
	"National Folk",          // 82
	"Swing",                  // 83
	"Fast-Fusion",            // 84 — FFmpeg: "Fast Fusion"
	"Bebop",                  // 85 — EAC: "Bebob"; FFmpeg used that too until 2019
	"Latin",                  // 86
	"Revival",                // 87
	"Celtic",                 // 88
	"Bluegrass",              // 89
	"Avantgarde",             // 90
	"Gothic Rock",            // 91
	"Progressive Rock",       // 92
	"Psychedelic Rock",       // 93
	"Symphonic Rock",         // 94
	"Slow Rock",              // 95
	"Big Band",               // 96
	"Chorus",                 // 97
	"Easy Listening",         // 98
	"Acoustic",               // 99
	"Humour",                 // 100
	"Speech",                 // 101
	"Chanson",                // 102
	"Opera",                  // 103
	"Chamber Music",          // 104
	"Sonata",                 // 105
	"Symphony",               // 106
	"Booty Bass",             // 107
	"Primus",                 // 108
	"Porn Groove",            // 109
	"Satire",                 // 110
	"Slow Jam",               // 111
	"Club",                   // 112
	"Tango",                  // 113
	"Samba",                  // 114
	"Folklore",               // 115
	"Ballad",                 // 116
	"Power Ballad",           // 117
	"Rhythmic Soul",          // 118
	"Freestyle",              // 119
	"Duet",                   // 120
	"Punk Rock",              // 121
	"Drum Solo",              // 122
	"A Cappella",             // 123 — EAC: "A Capella"
	"Euro-House",             // 124
	"Dance Hall",             // 125
	"Goa",                    // 126
	"Drum & Bass",            // 127
	"Club-House",             // 128
	"Hardcore",               // 129
	"Terror",                 // 130
	"Indie",                  // 131
	"BritPop",                // 132
	"Afro-Punk",              // 133 — EAC: "Negerpunk"
	"Polsk Punk",             // 134
	"Beat",                   // 135
	"Christian Gangsta Rap",  // 136
	"Heavy Metal",            // 137
	"Black Metal",            // 138
	"Crossover",              // 139
	"Contemporary Christian", // 140
	"Christian Rock",         // 141
	"Merengue",               // 142
	"Salsa",                  // 143
	"Thrash Metal",           // 144
	"Anime",                  // 145
	"JPop",                   // 146 — FFmpeg, Wikipedia: "Jpop"
	"Synthpop",               // 147 — FFmpeg used "SynthPop" until 2019

	// Winamp 5.6 extensions (148-191).
	"Abstract", // 148
	// Christmas is index 148 in Wikipedia's list, where Winamp 5.63, FFmpeg and
	// squell/id3 have Abstract. It has no index of its own here: both are allowed.
	"Christmas",           // 148 (Wikipedia)
	"Art Rock",            // 149
	"Baroque",             // 150
	"Bhangra",             // 151
	"Big Beat",            // 152
	"Breakbeat",           // 153
	"Chillout",            // 154
	"Downtempo",           // 155
	"Dub",                 // 156
	"EBM",                 // 157
	"Eclectic",            // 158
	"Electro",             // 159
	"Electroclash",        // 160
	"Emo",                 // 161
	"Experimental",        // 162
	"Garage",              // 163
	"Global",              // 164
	"IDM",                 // 165
	"Illbient",            // 166
	"Industro-Goth",       // 167
	"Jam Band",            // 168
	"Krautrock",           // 169
	"Leftfield",           // 170
	"Lounge",              // 171
	"Math Rock",           // 172
	"New Romantic",        // 173
	"Nu-Breakz",           // 174
	"Post-Punk",           // 175
	"Post-Rock",           // 176
	"Psytrance",           // 177
	"Shoegaze",            // 178
	"Space Rock",          // 179
	"Trop Rock",           // 180
	"World Music",         // 181
	"Neoclassical",        // 182
	"Audiobook",           // 183
	"Audio Theatre",       // 184
	"Neue Deutsche Welle", // 185
	"Podcast",             // 186
	"Indie Rock",          // 187
	"G-Funk",              // 188
	"Dubstep",             // 189
	"Garage Rock",         // 190
	"Psybient",            // 191
}

// byExact and byFold index allowed: byExact for an exact lookup, byFold, keyed
// by the lower-cased name, to find the right spelling of a name in the wrong
// case.
var (
	byExact = make(map[string]struct{}, len(allowed))
	byFold  = make(map[string]string, len(allowed))
)

func init() {
	for _, g := range allowed {
		byExact[g] = struct{}{}
		byFold[strings.ToLower(g)] = g
	}
}

// Valid reports whether name is an allowed genre, spelled exactly as in the
// list. It does not trim or fold anything: " Rock" and "rock" are not allowed.
func Valid(name string) bool {
	_, ok := byExact[name]
	return ok
}

// Canonical returns the allowed genre that name is a case-insensitive match
// for, spelled as it should be, and whether there is one. It is for suggesting
// the fix for a genre that is not allowed ("rock" should be "Rock"), and not
// for accepting one: only Valid decides what is allowed. It does not look past
// case, so "Folk-Rock" is not a spelling of "Folk/Rock".
func Canonical(name string) (string, bool) {
	g, ok := byFold[strings.ToLower(name)]
	return g, ok
}
