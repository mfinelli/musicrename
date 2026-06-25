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
	"unicode/utf8"

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

	// regexNonStandardSpace matches any whitespace character that is not a
	// regular ASCII space (e.g. tabs, newlines, carriage returns). Applied
	// before regexStrip to convert these to spaces, preserving word
	// boundaries that would otherwise be lost (e.g. "Dark\tSide" becomes
	// "dark side" rather than "darkside").
	regexNonStandardSpace = regexp.MustCompile(`[^\S ]+`)

	// regexSpaces identifies sequences of one or more space characters.
	// After regexStrip, only ASCII spaces can remain, so \s is not needed.
	regexSpaces = regexp.MustCompile(` +`)

	// trans is the package-level transliterator instance, shared across all
	// CleanString calls to avoid repeated allocation.
	trans = transliterator.NewTransliterator(nil)
)

// manualOverrides contains hardcoded replacements that bypass the standard
// pipeline. Format: [Original String] -> [Context Type] -> [Replacement
// String]
var manualOverrides = map[string]map[OverrideType]string{
	"AC/DC": {
		// Use Unicode fraction slash (U+2044) to preserve visual separation
		// without using the filesystem path-separator slash.
		ArtistOverride: "ac⁄dc",
	},
	"P!nk": {
		ArtistOverride: "pink",
	},
}

// CleanString transforms an input string through the sanitization pipeline:
//  1. Manual Overrides: If a match for the given kind exists, it returns the
//     replacement immediately, skipping all subsequent steps.
//  2. Transliteration: Converts Unicode characters to their ASCII equivalents
//     (e.g., 'é' -> 'e').
//  3. Lowercasing: Converts all characters to lowercase.
//  4. Non-standard Whitespace: Converts tabs, newlines, and other non-space
//     whitespace to regular spaces, preserving word boundaries.
//  5. Regex Stripping: Removes all characters except a-z, 0-9, and spaces.
//  6. Whitespace Normalization: Collapses multiple spaces into one and trims
//     leading/trailing whitespace.
//
// The caller is responsible for checking whether the returned string is empty,
// which can happen when all characters are stripped (e.g., input "!!!"). An
// empty result should be treated as a warning condition.
func CleanString(input string, kind OverrideType) string {
	if typeMap, ok := manualOverrides[input]; ok {
		if replacement, exists := typeMap[kind]; exists {
			return replacement
		}
	}

	input = trans.Transliterate(input, "en")
	input = strings.ToLower(input)
	input = regexNonStandardSpace.ReplaceAllString(input, " ")
	input = regexStrip.ReplaceAllString(input, "")
	input = regexSpaces.ReplaceAllString(input, " ")
	input = strings.TrimSpace(input)

	return input
}

// Truncate cuts a string down to the specified character (rune) limit. If the
// string length is already within the limit, it is returned unchanged.
// Operates on runes rather than bytes to correctly handle multi-byte
// characters such as those produced by manual overrides.
// A limit of zero or less returns an empty string.
func Truncate(name string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(name)
	if len(runes) <= limit {
		return name
	}
	return string(runes[:limit])
}

// TruncateWithOffset calculates a dynamic truncation limit based on the length
// of a directory name and subtracts an additional character to account for the
// path separator (/). This ensures the full relative path length (e.g.,
// "artwork/filename.flac") stays within maxLimit characters in sums.md5.
//
// maxLimit: The total allowable length for the full relative path segment.
// dirName: The name of the parent directory (e.g., "artwork", "scans").
func TruncateWithOffset(name string, dirName string, maxLimit int) string {
	limit := maxLimit - len(dirName) - 1 // -1 for the path separator
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

	firstChar, _ := utf8.DecodeRuneInString(artist)
	if !unicode.IsLetter(firstChar) {
		return "0/" + artist, nil
	}

	return string(firstChar) + "/" + artist, nil
}
