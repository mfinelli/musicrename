package models_test

import "testing"

import "github.com/mfinelli/musicrename/models"

func TestCueString(t *testing.T) {
	tests := []struct {
		cue models.Cue
		exp string
	}{
		{models.Cue{Name: "test album", Format: "cue"}, "test album.cue"},
	}

	for _, test := range tests {
		if test.cue.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.cue.String())
		}
	}
}

func TestCueFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		c  models.Cue
		exp string
	}{
		{models.Cue{Album: &artist.Albums[0], RealPath: "test album.cue", Name: "test album", Format: "cue"}, "/tmp/test/[2000] test album/test album.cue"},
	}

	for _, test := range tests {
		if test.c.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.c.FullPath())
		}
	}
}
