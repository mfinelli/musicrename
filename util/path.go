package util

import (
	"fmt"
	"strings"
)

func PrefixFromArtistAlbum(artist, year, album string) string {
	artistDir := Sanitize(artist)
	folder := artist[0:1]
	albumDir := Sanitize(album)

	return strings.Replace(strings.ToLower(fmt.Sprintf("%s/%s/[%s]%s", folder, artistDir, year, albumDir)), " ", "-", -1)
}
