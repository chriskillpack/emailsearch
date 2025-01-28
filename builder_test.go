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

func TestIsStopWord(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected bool
	}{
		// Test exact matches from the stop words list
		{"common word 'the'", "the", true},
		{"common word 'be'", "be", true},
		{"common word 'to'", "to", true},
		{"common word 'of'", "of", true},
		{"common word 'and'", "and", true},

		// Test different cases
		{"uppercase word", "THE", true},
		{"mixed case word", "ThE", true},
		{"mixed case 'AnD'", "AnD", true},

		// Test non-stop words
		{"uncommon word", "elephant", false},
		{"empty string", "", false},
		{"number", "123", false},
		{"special characters", "!@#", false},
		{"longer word", "something", false},

		// Test edge cases
		{"single letter non-stop word", "x", false},
		{"single letter stop word 'i'", "i", true},
		{"whitespace", " ", false},
		{"stop word with spaces", " the ", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := isStopWord(tc.input)
			if result != tc.expected {
				t.Errorf("isStopWord(%q) = %v; want %v", tc.input, result, tc.expected)
			}
		})
	}
}
