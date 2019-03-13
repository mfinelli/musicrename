package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestArtistString(t *testing.T) {
	tests := []struct {
		a   models.Artist
		exp string
	}{
		{models.Artist{RootDir: "/tmp", Name: "test"}, "test"},
	}

	for _, test := range tests {
		if test.a.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.a.String())
		}
	}
}

func TestArtistFullPath(t *testing.T) {
	tests := []struct {
		a   models.Artist
		exp string
	}{
		{models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}, "/tmp/test"},
		{models.Artist{RootDir: "/tmp/", RealPath: "test", Name: "test"}, "/tmp/test"},
	}

	for _, test := range tests {
		if test.a.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.a.FullPath())
		}
	}
}

func TestArtistAddAlbum(t *testing.T) {
	tostr := func(arr []models.Album) []string {
		ret := make([]string, len(arr))
		for i, v := range arr {
			ret[i] = v.String()
		}
		return ret
	}

	album1 := models.Album{Year: 2000, Name: "test0"}
	album2 := models.Album{Year: 2001, Name: "test1"}

	tests := []struct {
		a   models.Artist
		add []models.Album
		exp []models.Album
	}{
		{models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test", Albums: []models.Album{album1}}, []models.Album{album2}, []models.Album{album1, album2}},
	}

	for _, test := range tests {
		for _, album := range test.add {
			test.a.AddAlbum(&album)
		}

		if !reflect.DeepEqual(tostr(test.a.Albums), tostr(test.exp)) {
			t.Errorf("Expected %v but got %v", test.exp, test.a.Albums)
		}
	}
}

func TestParseArtist(t *testing.T) {
	rootDir := "/tmp"
	tests := []struct {
		input  string
		artist models.Artist
	}{
		{"test", models.Artist{RootDir: rootDir, RealPath: "test", Name: "test"}},
	}

	for _, test := range tests {
		a := models.ParseArtist(rootDir, test.input)
		if !reflect.DeepEqual(a, test.artist) {
			t.Errorf("Expected %v but got %v", test.artist, a)
		}
	}
}
