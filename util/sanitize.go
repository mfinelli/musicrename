package util

import "unicode"
import "golang.org/x/text/transform"
import "golang.org/x/text/unicode/norm"
import "golang.org/x/text/runes"

func Sanitize(str string) string {
	return translit(str)
}

func translit(str string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ := transform.String(t, str)
	return s
}
