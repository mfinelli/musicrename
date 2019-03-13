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
