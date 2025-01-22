package column

import (
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

func TestTrieSerialize(t *testing.T) {
	trie := NewTrie()
	trie.InsertWord("apple")
	trie.InsertWord("ape")

	strie, err := trie.Serialize()
	if err != nil {
		t.Errorf("Error serializing trie - %s", err)
	}
	trie2, err := DeserializeTrie(strie)
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
