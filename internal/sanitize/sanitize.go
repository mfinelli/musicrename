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

// Package sanitize provides tools for normalizing music library metadata. It
// transforms inconsistent strings (Artist, Album, Title) into a sanitized,
// ASCII-only format suitable for cross-platform filesystem paths.
package sanitize

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/alexsergivan/transliterator"
)

// OverrideType defines the context of the string being sanitized. This allows
// different sanitization results for the same input depending on whether it
// is used as an Artist name, Album name, or Track title.
type OverrideType int

const (
	// ArtistOverride is used for artist name sanitization.
	ArtistOverride OverrideType = iota
	// AlbumOverride is used for album name sanitization.
	AlbumOverride
	// TrackOverride is used for track title sanitization.
	TrackOverride
)

var (
	// regexStrip removes any character that is not a lowercase letter,
	// number, or space.
	regexStrip = regexp.MustCompile(`[^a-z0-9 ]+`)
	// regexWhitespace identifies sequences of one or more whitespace
	// characters.
	regexWhitespace = regexp.MustCompile(`\s+`)
)

// manualOverrides contains hardcoded replacements that bypass the standard
// pipeline. Format: [Original String] -> [Context Type] -> [Replacement
// String]
var manualOverrides = map[string]map[OverrideType]string{
	"AC/DC": {
		ArtistOverride: "ac⁄dc",
	},
	"P!nk": {
		ArtistOverride: "pink",
	},
}

// CleanString transforms an input string through the sanitization pipeline:
//  1. Manual Overrides: If a match for the given kind exists, it returns the
//     replacement immediately.
//  2. Transliteration: Converts Unicode characters to their ASCII equivalents
//     (e.g., 'é' -> 'e').
//  3. Lowercasing: Converts all characters to lowercase.
//  4. Regex Stripping: Removes all characters except a-z, 0-9, and spaces.
//  5. Whitespace Normalization: Collapses multiple spaces into one and trims
//     leading/trailing whitespace.
func CleanString(input string, kind OverrideType) string {
	if typeMap, ok := manualOverrides[input]; ok {
		if replacement, exists := typeMap[kind]; exists {
			return replacement
		}
	}

	t := transliterator.NewTransliterator(nil)
	input = t.Transliterate(input, "en")
	input = strings.ToLower(input)
	input = regexStrip.ReplaceAllString(input, "")
	input = regexWhitespace.ReplaceAllString(input, " ")
	input = strings.TrimSpace(input)

	return input
}

// Truncate cuts a string down to the specified limit. If the string length is
// already within the limit, it is returned unchanged.
func Truncate(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	return name[:limit]
}

// TruncateWithOffset calculates a dynamic truncation limit based on the length
// of a directory name. This is used to ensure that the full relative path
// length stays within a specific maximum (e.g., for MD5 sum file
// compatibility).
//
// maxLimit: The total allowable length for the segment.
// dirName: The name of the parent directory to subtract from the limit.
func TruncateWithOffset(name string, dirName string, maxLimit int) string {
	limit := maxLimit - len(dirName)
	if limit < 0 {
		limit = 0
	}
	return Truncate(name, limit)
}

// GetFirstLetterPath creates a nested directory structure based on the first
// letter of the artist's name (e.g., "beyonce" -> "b/beyonce").
//
// If the artist name starts with a non-letter character (number or symbol),
// it defaults to the "0/" directory.
//
// Returns an error if the provided artist string is empty.
func GetFirstLetterPath(artist string) (string, error) {
	if len(artist) == 0 {
		return "", errors.New("artist name cannot be empty")
	}

	firstChar := rune(artist[0])
	if !unicode.IsLetter(firstChar) {
		return "0/" + artist, nil
	}

	return string(firstChar) + "/" + artist, nil
}
