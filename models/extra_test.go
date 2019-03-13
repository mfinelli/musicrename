package models_test

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
