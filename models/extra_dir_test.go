package models_test

import "testing"

import "github.com/mfinelli/musicrename/models"

func TestExtraDirString(t *testing.T) {
	tests := []struct {
		ed  models.ExtraDir
		exp string
	}{
		{models.ExtraDir{Name: "test"}, "test"},
	}

	for _, test := range tests {
		if test.ed.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.ed.String())
		}
	}
}

func TestExtraDirFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		ed  models.ExtraDir
		exp string
	}{
		{models.ExtraDir{Album: &artist.Albums[0], RealPath: "test", Name: "test"}, "/tmp/test/[2000] test album/test"},
	}

	for _, test := range tests {
		if test.ed.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.ed.FullPath())
		}
	}
}
