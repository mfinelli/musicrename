package models_test

import "reflect"
import "testing"
import "github.com/mfinelli/musicrename/models"

func TestAlbumString(t *testing.T) {
	tests := []struct {
		a   models.Album
		exp string
	}{
		{models.Album{Year: 2000, Name: "test"}, "[2000] test"},
	}

	for _, test := range tests {
		if test.a.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.a.String())
		}
	}
}

func TestAlbumFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", Name: "test", RealPath: "test"}

	tests := []struct {
		a   models.Album
		exp string
	}{
		{models.Album{Artist: &artist, Year: 2000, RealPath: "[2000] test", Name: "test"}, "/tmp/test/[2000] test"},
	}

	for _, test := range tests {
		if test.a.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.a.FullPath())
		}
	}
}

func TestAlbumAddSong(t *testing.T) {
	tostr := func(arr []models.Song) []string {
		ret := make([]string, len(arr))
		for i, v := range arr {
			ret[i] = v.String()
		}
		return ret
	}

	song1 := models.Song{Track: 1, Name: "test1", Format: "flac"}
	song2 := models.Song{Track: 2, Name: "test2", Format: "flac"}

	tests := []struct {
		a   models.Album
		add []models.Song
		exp []models.Song
	}{
		{models.Album{Year: 2000, Name: "test", Songs: []models.Song{song1}}, []models.Song{song2}, []models.Song{song1, song2}},
	}

	for _, test := range tests {
		for _, song := range test.add {
			test.a.AddSong(&song)
		}

		if !reflect.DeepEqual(tostr(test.a.Songs), tostr(test.exp)) {
			t.Errorf("Expected %v but got %v", test.exp, test.a.Songs)
		}
	}
}

func TestParseAlbum(t *testing.T) {
	tests := []struct {
		input string
		album models.Album
	}{
		{"[2000] test album", models.Album{Year: 2000, Name: "test album", RealPath: "[2000] test album"}},
		{"notanalbum", models.Album{}},
	}

	for _, test := range tests {
		a, _ := models.ParseAlbum(test.input)
		if !reflect.DeepEqual(a, test.album) {
			t.Errorf("Expected %v but got %v", test.album, a)
		}
	}
}
