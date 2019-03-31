package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestPlaylistString(t *testing.T) {
	tests := []struct {
		p   models.Playlist
		exp string
	}{
		{models.Playlist{Name: "test album", Format: "m3u"}, "test album.m3u"},
	}

	for _, test := range tests {
		if test.p.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.p.String())
		}
	}
}

func TestPlaylistFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		p   models.Playlist
		exp string
	}{
		{models.Playlist{Album: &artist.Albums[0], RealPath: "test album.m3u", Name: "test album", Format: "m3u"}, "/tmp/test/[2000] test album/test album.m3u"},
	}

	for _, test := range tests {
		if test.p.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.p.FullPath())
		}
	}
}

func TestParsePlaylist(t *testing.T) {
	tests := []struct {
		input string
		p     models.Playlist
	}{
		{"test album.m3u", models.Playlist{RealPath: "test album.m3u", Name: "test album", Format: "m3u"}},
	}

	for _, test := range tests {
		e, _ := models.ParsePlaylist(test.input)
		if !reflect.DeepEqual(e, test.p) {
			t.Errorf("Expected %v but got %v", test.p, e)
		}
	}
}
