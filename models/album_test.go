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

func TestAlbumAddExtraDir(t *testing.T) {
	tostr := func(arr []models.ExtraDir) []string {
		ret := make([]string, len(arr))
		for i, v := range arr {
			ret[i] = v.String()
		}
		return ret
	}

	dir1 := models.ExtraDir{Name: "test1"}
	dir2 := models.ExtraDir{Name: "test2"}

	tests := []struct {
		a   models.Album
		add []models.ExtraDir
		exp []models.ExtraDir
	}{
		{models.Album{Year: 2000, Name: "test", ExtraDirs: []models.ExtraDir{dir1}}, []models.ExtraDir{dir2}, []models.ExtraDir{dir1, dir2}},
	}

	for _, test := range tests {
		for _, dir := range test.add {
			test.a.AddExtraDir(&dir)
		}

		if !reflect.DeepEqual(tostr(test.a.ExtraDirs), tostr(test.exp)) {
			t.Errorf("Expected %v but got %v", test.exp, test.a.ExtraDirs)
		}
	}
}

func TestAlbumAddCue(t *testing.T) {
	cue := models.Cue{Name: "test album", Format: "cue"}

	tests := []struct {
		a   models.Album
		add models.Cue
	}{
		{models.Album{Year: 2000, Name: "test album"}, cue},
	}

	for _, test := range tests {
		test.a.AddCue(&test.add)

		if test.a.Cue != &test.add {
			t.Errorf("Expected %v but got %v", test.add, test.a.Cue)
		}
	}
}

func TestAlbumAddPlaylist(t *testing.T) {
	playlist := models.Playlist{Name: "test album", Format: "m3u"}

	tests := []struct {
		a   models.Album
		add models.Playlist
	}{
		{models.Album{Year: 2000, Name: "test album"}, playlist},
	}

	for _, test := range tests {
		test.a.AddPlaylist(&test.add)

		if test.a.Playlist != &test.add {
			t.Errorf("Expected %v but got %v", test.add, test.a.Playlist)
		}
	}
}

func TestAlbumAddLog(t *testing.T) {
	tostr := func(arr []models.Log) []string {
		ret := make([]string, len(arr))
		for i, v := range arr {
			ret[i] = v.String()
		}
		return ret
	}

	log1 := models.Log{Name: "disc1"}
	log2 := models.Log{Name: "disc2"}

	tests := []struct {
		a   models.Album
		add []models.Log
		exp []models.Log
	}{
		{models.Album{Year: 2000, Name: "test", Logs: []models.Log{log1}}, []models.Log{log2}, []models.Log{log1, log2}},
	}

	for _, test := range tests {
		for _, log := range test.add {
			test.a.AddLog(&log)
		}

		if !reflect.DeepEqual(tostr(test.a.Logs), tostr(test.exp)) {
			t.Errorf("Expected %v but got %v", test.exp, test.a.Logs)
		}
	}
}

func TestAlbumAddFolder(t *testing.T) {
	folder := models.Folder{}

	tests := []struct {
		a   models.Album
		add models.Folder
	}{
		{models.Album{Year: 2000, Name: "test album"}, folder},
	}

	for _, test := range tests {
		test.a.AddFolder(&test.add)

		if test.a.Folder != &test.add {
			t.Errorf("Expected %v but got %v", test.add, test.a.Folder)
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
