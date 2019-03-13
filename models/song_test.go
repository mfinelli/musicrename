package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestSongString(t *testing.T) {
	tests := []struct {
		s   models.Song
		exp string
	}{
		{models.Song{Disc: 0, Track: 1, Name: "test", Format: "flac"}, "01 test.flac"},
		{models.Song{Disc: 1, Track: 2, Name: "disc", Format: "flac"}, "1-02 disc.flac"},
	}

	for _, test := range tests {
		if test.s.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.s.String())
		}
	}
}

func TestSongFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		song models.Song
		exp  string
	}{
		{models.Song{Album: &artist.Albums[0], RealPath: "01 test song.flac", Disc: 0, Track: 1, Name: "test song", Format: "flac"}, "/tmp/test/[2000] test album/01 test song.flac"},
	}

	for _, test := range tests {
		if test.song.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.song.FullPath())
		}
	}
}

func TestParseSong(t *testing.T) {
	tests := []struct {
		input string
		song  models.Song
	}{
		{"1-01 test song.flac", models.Song{RealPath: "1-01 test song.flac", Disc: 1, Track: 1, Name: "test song", Format: "flac"}},
		{"02 test song.flac", models.Song{RealPath: "02 test song.flac", Disc: 0, Track: 2, Name: "test song", Format: "flac"}},
		{"notasong", models.Song{}},
	}

	for _, test := range tests {
		s, _ := models.ParseSong(test.input)
		if !reflect.DeepEqual(s, test.song) {
			t.Errorf("Expected %v but got %v", test.song, s)
		}
	}
}
