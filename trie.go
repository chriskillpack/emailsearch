package column

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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

func DeserializeTrie(data []byte) (*Trie, error) {
	buf := bytes.NewReader(data)

	// Only version 1 is supported
	var version uint32
	binary.Read(buf, binary.BigEndian, &version)
	if version != 1 {
		return nil, fmt.Errorf("unsupported version number %d", version)
	}

	return &Trie{
		Root: deserializeNode(buf, 0),
	}, nil
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

// Important: Serialize() is not guaranteed to be generate the same binary
// layout for a given trie. This is because Go iterates over map keys in random
// order.
func (t *Trie) Serialize() ([]byte, error) {
	buf := &bytes.Buffer{}

	// Trie file format (Big Endian)
	// 0x00: u32 Version Number, currently 1
	// 0x04: Tree structure (see serializeNode)
	binary.Write(buf, binary.BigEndian, uint32(1))
	st := t.serializeNode(t.Root)
	n, err := buf.Write(st)
	if n < len(st) || err != nil {
		if n < len(st) {
			return nil, io.ErrShortWrite
		}
		return nil, err
	}

	return buf.Bytes(), nil
}

func (t *Trie) serializeNode(node *TrieNode) []byte {
	buf := &bytes.Buffer{}

	// Trie node file format
	// All offsets below are relative to the start of the root node in the file.
	// runes, Go's type for a character in a string, are utf-8 encoded.
	// 0x00               : u8    1 if this node is the end of a word, 0 otherwise
	// 0x01               : u16   number of children
	// 0x03               : rune  for child 0
	// 0x03+rune0         : sub-tree under child 0
	// 0x03+rune0+subtree0: child 1, rune (utf-8 encoded)
	// ...
	// ...                : child N-1, rune (utf-8 encoded)
	// ...                : subtree under child N-1
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
