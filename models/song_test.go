package models_test

import "testing"
import "github.com/mfinelli/musicrename/models"

func TestSongString(t *testing.T) {
	tests := []struct {
		s   models.Song
		exp string
	}{
		{models.Song{Disc: 0, Track: 1, Name: "test", Format: "flac"}, "01 test.flac"},
		{models.Song{Disc: 1, Track: 2, Name: "disc", Format: "flac"}, "1-02 disc.flac"},
	}

	for _, test := range tests {
		if test.s.String() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.s.String())
		}
	}
}
