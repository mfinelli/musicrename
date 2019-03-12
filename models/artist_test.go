package models_test

import "testing"
import "github.com/mfinelli/musicrename/models"

func TestArtistFullPath(t *testing.T) {
	tests := []struct {
		a   models.Artist
		exp string
	}{
		{models.Artist{RootDir: "/tmp", Name: "test"}, "/tmp/test"},
		{models.Artist{RootDir: "/tmp/", Name: "test"}, "/tmp/test"},
	}

	for _, test := range tests {
		if test.a.FullPath() != test.exp {
			t.Errorf("Expected %s but got %s", test.exp, test.a.FullPath())
		}
	}
}

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
