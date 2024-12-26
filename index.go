package column

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

type match struct {
	FilenameStringIndex int
	Offsets             []int
}

// fileIndex tracks the positions of words in a specific file
type fileIndex map[string][]int

// wordIndex is the global index for all the files in the corpus
// As such it tracks more information than LocalIndex does.
type wordIndex map[string][]match

// Index file format structures
type serializedMatch struct {
	FilenameIndex int32
	NumOffsets    int32
	// Followed by NumOffsets of int32
}

type serializedWord struct {
	WordLen    int32
	Word       string
	NumMatches int32
	// Followed by NumMatches of serializedMatch
}

type serializedIndexHeader struct {
	Version    int32
	NumEntries int64
	// Followed by NumEntries of serializedWord
	//Entry      []serializedWord
}

type serializedWordIndexOffset struct {
	WordIndex int32 // Index into the word string table
	Offset    int64 // Binary offset into the index file
}

type Index struct {
	filenames []string
	words     []string
}

func LoadIndexFromDisk(indexdir string) (*Index, error) {
	idx := &Index{}
	var err error
	// if idx.filenames, err = loadStringTable(filepath.Join(indexdir, FilenamesStringTable)); err != nil {
	// 	return nil, err
	// }
	if idx.words, err = loadStringTable(filepath.Join(indexdir, WordsStringTable)); err != nil {
		return nil, err
	}
	return nil, nil
}

func loadStringTable(filename string) ([]string, error) {
	f, err := os.Open(filename)
	defer f.Close()
	if err != nil {
		return nil, err
	}

	hdr := serializedStringSetHeader{}
	if err = binary.Read(f, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}
	fmt.Printf("nstrings %d\n", hdr.NStrings)

	if hdr.Version != 1 {
		return nil, fmt.Errorf("invalid file version %d", hdr.Version)
	}

	strings := make([]string, hdr.NStrings)
	scratch := make([]byte, hdr.MaxLen)
	for i := range hdr.NStrings {
		var slen int16
		binary.Read(f, binary.BigEndian, &slen)
		binary.Read(f, binary.BigEndian, scratch[0:slen])
		strings[i] = string(scratch[0:slen])

		fmt.Printf("%d: %d %q\n", i, slen, strings[i])
	}

	return strings, nil
}
