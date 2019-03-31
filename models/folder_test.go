package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestFolderString(t *testing.T) {
	tests := []struct {
		f   models.Folder
		exp string
	}{
		{models.Folder{}, "folder.jpg"},
	}

	for _, test := range tests {
		if test.f.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.f.String())
		}
	}
}

func TestFolderFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		f   models.Folder
		exp string
	}{
		{models.Folder{Album: &artist.Albums[0], RealPath: "folder.jpg"}, "/tmp/test/[2000] test album/folder.jpg"},
	}

	for _, test := range tests {
		if test.f.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.f.FullPath())
		}
	}
}

func TestParseFolder(t *testing.T) {
	tests := []struct {
		input string
		f     models.Folder
	}{
		{"folder.jpg", models.Folder{RealPath: "folder.jpg"}},
	}

	for _, test := range tests {
		e, _ := models.ParseFolder(test.input)
		if !reflect.DeepEqual(e, test.f) {
			t.Errorf("Expected %v but got %v", test.f, e)
		}
	}
}
