package util

import (
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"regexp"
	"unicode"
)

func Sanitize(str string) string {
	return unrecognized(whitespace(translit(str)))
}

func translit(str string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ := transform.String(t, str)
	return s
}

func whitespace(str string) string {
	// consolidate whitespace
	re := regexp.MustCompile("(\\s+)")
	return re.ReplaceAllString(str, " ")
}

func unrecognized(str string) string {
	re := regexp.MustCompile("([^0-9A-Za-z,& \\-\\(\\)\\[\\]\\.])")
	return re.ReplaceAllString(str, "")
}
