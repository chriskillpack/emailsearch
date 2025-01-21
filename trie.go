package column

import (
	"bytes"
	"encoding/binary"
)

type TrieNode struct {
	Children map[rune]*TrieNode
	IsWord   bool
}

type Trie struct {
	Root *TrieNode
}

// Returns the root of a new word
func NewTrie() *Trie {
	return &Trie{
		Root: &TrieNode{
			Children: make(map[rune]*TrieNode),
			IsWord:   false,
		},
	}
}

func DeserializeTrie(data []byte) *Trie {
	buf := bytes.NewReader(data)

	return &Trie{
		Root: deserializeNode(buf, 0),
	}
}

func (t *Trie) InsertWord(w string) {
	current := t.Root

	for _, ch := range w {
		if _, exists := current.Children[ch]; !exists {
			current.Children[ch] = &TrieNode{
				Children: make(map[rune]*TrieNode),
				IsWord:   false,
			}
		}
		current = current.Children[ch]
	}
	current.IsWord = true
}

func (t *Trie) Has(w string) bool {
	current := t.Root

	for _, ch := range w {
		if _, has := current.Children[ch]; !has {
			return false
		}
		current = current.Children[ch]
	}

	return current.IsWord
}

func (t *Trie) Serialize() []byte {
	return t.serializeNode(t.Root)
}

func (t *Trie) serializeNode(node *TrieNode) []byte {
	buf := &bytes.Buffer{}
	// 0x00: u8 1 if this node is the end of a word, 0 otherwise
	// 0x01: u16 number of children
	// ...
	binary.Write(buf, binary.BigEndian, node.IsWord)
	nc := uint16(len(node.Children))
	binary.Write(buf, binary.BigEndian, nc)

	for ch, nd := range node.Children {
		buf.WriteRune(ch)
		buf.Write(t.serializeNode(nd))
	}

	return buf.Bytes()
}

func deserializeNode(br *bytes.Reader, level int) *TrieNode {
	node := &TrieNode{
		Children: make(map[rune]*TrieNode),
	}
	binary.Read(br, binary.BigEndian, &node.IsWord)

	var nc uint16
	binary.Read(br, binary.BigEndian, &nc)
	for range nc {
		r, _, _ := br.ReadRune()
		node.Children[r] = deserializeNode(br, level+1)
	}

	return node
}
