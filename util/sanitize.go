package util

import "regexp"
import "strings"
import "unicode"
import "golang.org/x/text/transform"
import "golang.org/x/text/unicode/norm"
import "golang.org/x/text/runes"

func Sanitize(str string, max int) string {
	return lastChar(unrecognized(whitespace(colons(maxlen(translit(str), max)))))
}

func translit(str string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ := transform.String(t, str)
	return s
}

func maxlen(str string, max int) string {
	if len(str) > max {
		return str[0:max]
	}

	return str
}

func lastChar(str string) string {
	notAllowed := []rune{'(', '.', '-', '[', ',', '&', ' '}
	runes := []rune(str)

	for contains(runes[len(runes)-1], notAllowed) {
		runes = runes[:len(runes)-1]
	}

	return strings.TrimSpace(string(runes))
}

func contains(needle rune, haystack []rune) bool {
	for _, hay := range haystack {
		if needle == hay {
			return true
		}
	}
	return false
}

func colons(str string) string {
	re := regexp.MustCompile("(-\\s+)")
	return re.ReplaceAllString(str, " ")
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
