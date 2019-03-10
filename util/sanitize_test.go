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
		{"waytoomanycharacters", "waytoomany"},
		{"remove -", "remove"},
		{"a - [", "a"},
		{"a- b", "a b"},
		{"a- b- c", "a b c"},
		{"a   c", "a c"},
		{"a#d", "ad"},
	}

	for _, test := range tests {
		str := util.Sanitize(test.input, 10)
		if str != test.output {
			t.Errorf("expected %s but got %s", test.output, str)
		}
	}
}
