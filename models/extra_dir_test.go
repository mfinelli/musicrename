package models_test

import "reflect"
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

func TestExtraDirAddExtra(t *testing.T) {
	tostr := func(arr []models.Extra) []string {
		ret := make([]string, len(arr))
		for i, v := range arr {
			ret[i] = v.String()
		}
		return ret
	}

	extra1 := models.Extra{Name: "test1", Format: "png"}
	extra2 := models.Extra{Name: "test2", Format: "png"}

	tests := []struct {
		ed  models.ExtraDir
		add []models.Extra
		exp []models.Extra
	}{
		{models.ExtraDir{Name: "test", Extras: []models.Extra{extra1}}, []models.Extra{extra2}, []models.Extra{extra1, extra2}},
	}

	for _, test := range tests {
		for _, e := range test.add {
			test.ed.AddExtra(&e)
		}

		if !reflect.DeepEqual(tostr(test.ed.Extras), tostr(test.exp)) {
			t.Errorf("Expected %v but got %v", test.exp, test.ed.Extras)
		}
	}
}

func TestParseExtraDir(t *testing.T) {
	tests := []struct {
		input string
		exp   models.ExtraDir
	}{
		{"test", models.ExtraDir{RealPath: "test", Name: "test"}},
	}

	for _, test := range tests {
		ed, _ := models.ParseExtraDir(test.input)
		if !reflect.DeepEqual(ed, test.exp) {
			t.Errorf("Expected %v but got %v", test.exp, ed)
		}
	}
}
