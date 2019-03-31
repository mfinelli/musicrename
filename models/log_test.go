package models_test

import "reflect"
import "testing"

import "github.com/mfinelli/musicrename/models"

func TestLogString(t *testing.T) {
	tests := []struct {
		log models.Log
		exp string
	}{
		{models.Log{Name: "test album"}, "test album.log"},
	}

	for _, test := range tests {
		if test.log.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.log.String())
		}
	}
}

func TestLogFullPath(t *testing.T) {
	artist := models.Artist{RootDir: "/tmp", RealPath: "test", Name: "test"}
	album, _ := models.ParseAlbum("[2000] test album")
	artist.AddAlbum(&album)

	tests := []struct {
		l   models.Log
		exp string
	}{
		{models.Log{Album: &artist.Albums[0], RealPath: "test album.log", Name: "test album"}, "/tmp/test/[2000] test album/test album.log"},
	}

	for _, test := range tests {
		if test.l.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.l.FullPath())
		}
	}
}

func TestParseLog(t *testing.T) {
	tests := []struct {
		input string
		log   models.Log
	}{
		{"test album.log", models.Log{RealPath: "test album.log", Name: "test album"}},
	}

	for _, test := range tests {
		e, _ := models.ParseLog(test.input)
		if !reflect.DeepEqual(e, test.log) {
			t.Errorf("Expected %v but got %v", test.log, e)
		}
	}
}
