package util_test

import "testing"

import "github.com/mfinelli/musicrename/util"

func TestSanitize(t *testing.T) {
	tests := []struct {
		input  string
		output string
	}{
		{"test", "test"},
		{"tèst", "test"},
	}

	for _, test := range tests {
		str := util.Sanitize(test.input)
		if str != test.output {
			t.Errorf("expected %s but got %s", test.output, str)
		}
	}
}
