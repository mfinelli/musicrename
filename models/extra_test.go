package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestExtraString(t *testing.T) {
	tests := []struct {
		e   models.Extra
		exp string
	}{
		{models.Extra{Name: "test", Format: "png"}, "test.png"},
	}

	for _, test := range tests {
		if test.e.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.e.String())
		}
	}
}

func TestExtraFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)
	extradir, _ := models.ParseExtraDir("test")
	album.AddExtraDir(&extradir)

	tests := []struct {
		e   models.Extra
		exp string
	}{
		{models.Extra{ExtraDir: &extradir, RealPath: "test.png", Name: "test", Format: "png"}, "/tmp/test/[2000] test album/test/test.png"},
	}

	for _, test := range tests {
		if test.e.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.e.FullPath())
		}
	}
}

func TestParseExtra(t *testing.T) {
	tests := []struct {
		input string
		extra models.Extra
	}{
		{"test.png", models.Extra{RealPath: "test.png", Name: "test", Format: "png"}},
	}

	for _, test := range tests {
		e, _ := models.ParseExtra(test.input)
		if !reflect.DeepEqual(e, test.extra) {
			t.Errorf("Expected %v but got %v", test.extra, e)
		}
	}
}
