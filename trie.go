package emailsearch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
)

const trieMagic uint32 = 'T'<<24 | 'R'<<16 | 'I'<<8 | 'E'

type TrieNode struct {
	Children map[rune]*TrieNode
	IsWord   bool
}

type Trie struct {
	Root *TrieNode
	N    int // Number of nodes in Trie
}

type serializedTrieHeader struct {
	Magic    uint32
	Version  uint32
	NumNodes uint32
}

// Returns the root of a new word
func NewTrie() *Trie {
	return &Trie{
		Root: &TrieNode{
			Children: make(map[rune]*TrieNode),
			IsWord:   false,
		},
		N: 1,
	}
}

func DeserializeTrie(data []byte) (*Trie, error) {
	buf := bytes.NewReader(data)

	var hdr serializedTrieHeader
	if err := binary.Read(buf, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}

	// Only version 1 is supported
	if hdr.Magic != trieMagic || hdr.Version != 1 {
		return nil, fmt.Errorf("unsupported version number %d", hdr.Version)
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
			t.N += 1
		}
		current = current.Children[ch]
	}
	current.IsWord = true
}

func (t *Trie) Has(w string) bool {
	node := t.findNode(w)
	return node != nil && node.IsWord
}

func (t *Trie) WithPrefix(prefix string) []string {
	node := t.findNode(prefix)
	if node == nil {
		return []string{}
	}

	results := []string{}
	t.collectWords(node, prefix, &results)
	return results
}

func (t *Trie) findNode(w string) *TrieNode {
	current := t.Root

	for _, ch := range w {
		if _, has := current.Children[ch]; !has {
			return nil
		}
		current = current.Children[ch]
	}

	return current
}

func (t *Trie) collectWords(node *TrieNode, prefix string, results *[]string) {
	if node.IsWord {
		// We found a word on the descent, add it now
		*results = append(*results, prefix)
	}

	// Descend through all the children
	for ch, child := range node.Children {
		t.collectWords(child, prefix+string(ch), results)
	}
}

// Important: Serialize() is not guaranteed to be generate the same binary
// layout for a given trie. This is because Go iterates over map keys in random
// order.
func (t *Trie) Serialize() ([]byte, error) {
	if int(uint32(t.N)) != t.N {
		panic("Number of trie nodes exceeds file format limits")
	}

	buf := &bytes.Buffer{}

	// Trie file format (Big Endian)
	// 0x00: u32 Magic Number 'TRIE'
	// 0x04: u32 Version Number, currently 1
	// 0x08: Number of serialized nodes
	// 0x0C: Tree structure (see serializeNode)
	hdr := serializedTrieHeader{
		Magic:    trieMagic,
		Version:  1,
		NumNodes: uint32(t.N),
	}
	binary.Write(buf, binary.BigEndian, &hdr)

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

	keys := make([]rune, 0, len(node.Children))
	for r := range node.Children {
		keys = append(keys, r)
	}
	slices.Sort(keys)

	for _, r := range keys {
		buf.WriteRune(r)
		buf.Write(t.serializeNode(node.Children[r]))
	}

	return buf.Bytes()
}

func deserializeNode(br *bytes.Reader, level int) *TrieNode {
	node := &TrieNode{}
	binary.Read(br, binary.BigEndian, &node.IsWord)

	var nc uint16
	binary.Read(br, binary.BigEndian, &nc)
	node.Children = make(map[rune]*TrieNode, nc)

	for range nc {
		r, _, _ := br.ReadRune()
		node.Children[r] = deserializeNode(br, level+1)
	}

	return node
}
