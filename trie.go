package emailsearch

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"maps"
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

type ReadRuneReader interface {
	io.Reader
	io.RuneReader
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

func DeserializeTrie(rdr ReadRuneReader) (*Trie, error) {
	var hdr serializedTrieHeader
	if err := binary.Read(rdr, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}

	// Only version 1 is supported
	if hdr.Magic != trieMagic || hdr.Version != 1 {
		return nil, fmt.Errorf("unsupported version number %d", hdr.Version)
	}

	return &Trie{
		Root: deserializeNode(rdr, 0),
		N:    int(hdr.NumNodes),
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

// Serialize the trie into an io.Writer
func (t *Trie) Serialize(w io.Writer) error {
	if int(uint32(t.N)) != t.N {
		panic("Number of trie nodes exceeds file format limits")
	}

	buf := bufio.NewWriter(w)

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
	if err := binary.Write(buf, binary.BigEndian, &hdr); err != nil {
		return err
	}

	if err := t.serializeNode(t.Root, buf); err != nil {
		return err
	}

	return buf.Flush()
}

func (t *Trie) serializeNode(node *TrieNode, buf *bufio.Writer) error {
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
	switch node.IsWord {
	case true:
		buf.WriteByte(1)
	case false:
		buf.WriteByte(0)
	}

	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(len(node.Children)))
	buf.Write(p[:])

	for _, r := range slices.Sorted(maps.Keys(node.Children)) {
		buf.WriteRune(r)
		if err := t.serializeNode(node.Children[r], buf); err != nil {
			return err
		}
	}

	return nil
}

func deserializeNode(rdr ReadRuneReader, level int) *TrieNode {
	node := &TrieNode{}
	binary.Read(rdr, binary.BigEndian, &node.IsWord)

	var nc uint16
	binary.Read(rdr, binary.BigEndian, &nc)
	node.Children = make(map[rune]*TrieNode, nc)

	for range nc {
		r, _, _ := rdr.ReadRune()
		node.Children[r] = deserializeNode(rdr, level+1)
	}

	return node
}
