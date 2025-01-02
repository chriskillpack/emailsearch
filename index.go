package column

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/exp/mmap"
)

type match struct {
	FilenameStringIndex int
	Offsets             []int
}

// Index file format structures
type serializedMatch struct {
	FilenameIndex uint32
	NumOffsets    uint32
	// Followed by NumOffsets of uint32
}

type serializedWord struct {
	NumMatches uint32
	// Followed by NumMatches of serializedMatch
}

type serializedIndexHeader struct {
	Version    uint32
	NumEntries uint64
	// Followed by NumEntries of serializedWord
	//Entry      []serializedWord
}

type serializedWordOffsetHeader struct {
	Version    uint32
	NumEntries uint32
}

type serializedWordIndexOffset struct {
	WordIndex int32 // Index into the word string table
	Offset    int64 // Binary offset into the index file
}

type Index struct {
	filenames []string
	words     []string
	offsets   []serializedWordIndexOffset

	indexRdr *mmap.ReaderAt
}

func LoadIndexFromDisk(indexdir string) (*Index, error) {
	idx := &Index{}
	var err error

	if idx.filenames, err = loadStringTable(filepath.Join(indexdir, FilenamesStringTable)); err != nil {
		return nil, err
	}
	fmt.Printf("Loaded filename strings table: %d entries\n", len(idx.filenames))

	if idx.words, err = loadStringTable(filepath.Join(indexdir, WordsStringTable)); err != nil {
		return nil, err
	}
	fmt.Printf("Loaded words strings table: %d entries\n", len(idx.words))

	idx.offsets, err = loadOffsetsTable(filepath.Join(indexdir, IndexWordOffsets))
	if err != nil {
		return nil, err
	}
	fmt.Printf("Loaded word offsets table: %d entries\n", len(idx.offsets))

	if len(idx.offsets) != len(idx.words) {
		return nil, fmt.Errorf("data mismatch")
	}

	// Memory map the index in
	idx.indexRdr, err = mmap.Open(filepath.Join(indexdir, CorpusIndex))
	if err != nil {
		return nil, err
	}

	return idx, nil
}

func (idx *Index) Finish() {
	if idx.indexRdr != nil {
		idx.indexRdr.Close()
	}
}

func (idx *Index) FindWord(query string) {
	// Lookup the word in the word strings table
	wordIdx := -1
	for i, word := range idx.words {
		if strings.EqualFold(word, query) {
			wordIdx = i
			break
		}
	}
	if wordIdx == -1 {
		return
	}
	fmt.Printf("Word %q found at index %d\n", query, wordIdx)

	// Look up the offset into the search index
	var offset int64
	for _, off := range idx.offsets {
		if int(off.WordIndex) == wordIdx {
			offset = int64(off.Offset)
			break
		}
	}
	if offset == 0 {
		return
	}
	fmt.Printf("Index offset %d for %d\n", offset, wordIdx)

	var match serializedWord
	scratch := make([]byte, 1000)
	n, err := idx.indexRdr.ReadAt(scratch[0:4], int64(offset))
	if n != 4 || err != nil {
		return
	}
	match.NumMatches = binary.BigEndian.Uint32(scratch)
	fmt.Printf("Match info: %+v\n", match)
	offset += 4

	// Read out the matches in files
	for i := range match.NumMatches {
		if n, err := idx.indexRdr.ReadAt(scratch[0:8], offset); n != 8 || err != nil {
			return
		}
		offset += 8

		var sm serializedMatch
		sm.FilenameIndex = binary.BigEndian.Uint32(scratch[0:4])
		sm.NumOffsets = binary.BigEndian.Uint32(scratch[4:8])
		fmt.Printf(" match %d: %+v\n", i, sm)
		fmt.Printf("  file %s\n", idx.filenames[sm.FilenameIndex])

		// Read out the offsets for each file
		matchOffsets := make([]uint32, sm.NumOffsets)
		for j := range sm.NumOffsets {
			idx.indexRdr.ReadAt(scratch[0:4], offset)
			offset += 4
			matchOffsets[j] = binary.BigEndian.Uint32(scratch[0:4])
		}
		fmt.Printf("   offsets: %v\n", matchOffsets)
	}
}

func loadStringTable(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(data)

	hdr := serializedStringSetHeader{}
	if err = binary.Read(buf, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}

	if hdr.Version != 1 {
		return nil, fmt.Errorf("invalid file version %d", hdr.Version)
	}

	strings := make([]string, hdr.NStrings)
	scratch := make([]byte, hdr.MaxLen)

	for i := range hdr.NStrings {
		slen, err := binary.ReadUvarint(buf)
		if err != nil {
			return nil, err
		}

		buf.Read(scratch[0:slen])
		strings[i] = string(scratch[0:slen])
	}

	return strings, nil
}

func loadOffsetsTable(filename string) ([]serializedWordIndexOffset, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(data)
	hdr := serializedWordOffsetHeader{}
	if err := binary.Read(buf, binary.BigEndian, &hdr); err != nil {
		return nil, err
	}

	offsets := make([]serializedWordIndexOffset, hdr.NumEntries)
	if err := binary.Read(buf, binary.BigEndian, offsets); err != nil {
		return nil, err
	}

	return offsets, nil
}
