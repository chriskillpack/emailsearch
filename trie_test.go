package emailsearch

import (
	"bytes"
	"slices"
	"sort"
	"testing"
)

func TestTrieInsert(t *testing.T) {
	trie := NewTrie()

	cases := []struct {
		Name string
		Word string
	}{
		{"blank line", ""},
		{"hello", "a word"},
		{"heel", "another word"},
		{"hello", "a duplicate word"},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			trie.InsertWord(tc.Word)
			if !trie.Has(tc.Word) {
				t.Errorf("Expected %q to be found after insertion", tc.Word)
			}
		})
	}
}

func TestTrieHas(t *testing.T) {
	trie := NewTrie()
	words := []string{"hello", "help", "world", "work"}
	for _, word := range words {
		trie.InsertWord(word)
	}

	cases := []struct {
		Name     string
		Word     string
		Expected bool
	}{
		{"existing word", "hello", true},
		{"existing word", "help", true},
		{"prefix", "hel", false},
		{"empty string", "", false},
		{"existing word", "world", true},
		{"non-existent word with existing prefix", "worlds", false},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := trie.Has(tc.Word); got != tc.Expected {
				t.Errorf("unexpected")
			}
		})
	}
}

func TestTrieWithPrefix(t *testing.T) {
	trie := NewTrie()
	words := []string{"apple", "app", "apricot", "banana", "append", "application"}
	for _, word := range words {
		trie.InsertWord(word)
	}

	cases := []struct {
		Name     string
		Prefix   string
		Expected []string
	}{
		{"Common prefix", "app", []string{"apple", "app", "append", "application"}},
		{"Single match", "apr", []string{"apricot"}},
		{"Blank prefix", "", []string{"apple", "app", "apricot", "banana", "append", "application"}},
		{"No matches", "x", []string{}},
		{"Single word", "banana", []string{"banana"}},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := trie.WithPrefix(tc.Prefix)
			sort.Strings(got)
			sort.Strings(tc.Expected)

			if !slices.Equal[[]string](got, tc.Expected) {
				t.Errorf("WithPrefix returned %v; expected %v", got, tc.Expected)
			}
		})
	}

}

func TestTrieSerialize(t *testing.T) {
	trie := NewTrie()
	trie.InsertWord("apple")
	trie.InsertWord("ape")

	strie, err := trie.Serialize()
	if err != nil {
		t.Errorf("Error serializing trie - %s", err)
	}

	trie2, err := DeserializeTrie(bytes.NewReader(strie))
	if err != nil {
		t.Errorf("Error deserializing trie - %s", err)
	}

	if want, got := true, trie2.Has("apple"); want != got {
		t.Errorf("Expected to find \"apple\" but did not")
	}
	if want, got := true, trie2.Has("ape"); want != got {
		t.Errorf("Expected to find \"ape\" but did not")
	}
	if want, got := false, trie2.Has("a"); want != got {
		t.Errorf("Expected to not find \"a\" but did")
	}
}
