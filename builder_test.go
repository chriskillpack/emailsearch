package column

import (
	"slices"
	"testing"
)

func TestSplitText(t *testing.T) {
	cases := []struct {
		Name     string
		Input    string
		Expected []string
	}{
		{"Blank", "", []string{}},
		{"One word", "hello", []string{"hello"}},
		{"Two words", "hello world", []string{"hello", "world"}},
		{"Apostrophe", "Mark's house", []string{"Mark", "s", "house"}},
		{"Puncutation madness", "Dave's sleep).Calamity: sister's", []string{"Dave", "s", "sleep", "Calamity", "sister", "s"}},
		{"Leading whitespace", " hello", []string{"hello"}},
		{"Leading punctuation", ",,,world", []string{"world"}},
		{"Trailing punctuation", "information!!!", []string{"information"}},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var words []string
			for s := range splitText(tc.Input) {
				words = append(words, tc.Input[s.start:s.end])
			}

			if slices.Compare[[]string](words, tc.Expected) != 0 {
				t.Errorf("Expected %v, got %v", tc.Expected, words)
			}
		})
	}
}
