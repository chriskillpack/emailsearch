package column

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-mmap/mmap"
)

// Index file format structures
type serializedMatch struct {
	FilenameIndex uint32
	NumOffsets    uint32
	// Followed by NumOffsets of uint32
}

type serializedIndexHeader struct {
	Version    uint32
	NumEntries uint64 // Number of words in the index
	CorpusSize uint32 // Number of documents the index was built from

	// Followed by NumEntries of serializedWord
	//Entry      []serializedWord
}

type serializedWordOffsetHeader struct {
	Version    uint32
	NumEntries uint32
}

type serializedWordIndexOffset struct {
	WordIndex uint32 // Index into the word string table
	Offset    int64  // Binary offset into the index file
}

type catalogContentsOffset struct {
	Offset uint32 // Offset of the compressed content in the catalog
	Length uint32 // Length of the uncompressed content
}

type Index struct {
	filenames      []string
	words          []string
	offsets        []serializedWordIndexOffset
	contentOffsets []catalogContentsOffset
	CorpusSize     int

	indexRdr   *mmap.File
	catalogRdr *mmap.File
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
	if idx.indexRdr, err = mmap.Open(filepath.Join(indexdir, CorpusIndex)); err != nil {
		return nil, err
	}
	// Read in the index header
	var header serializedIndexHeader
	binary.Read(idx.indexRdr, binary.BigEndian, &header)
	idx.CorpusSize = int(header.CorpusSize)

	// Memory map the catalog in
	if idx.catalogRdr, err = mmap.Open(filepath.Join(indexdir, CorpusCatalog)); err != nil {
		return nil, err
	}
	// Read in the catalog header
	if err := idx.loadCatalog(idx.catalogRdr); err != nil {
		return nil, err
	}

	return idx, nil
}

func (idx *Index) Finish() {
	if idx.indexRdr != nil {
		idx.indexRdr.Close()
	}
	if idx.catalogRdr != nil {
		idx.catalogRdr.Close()
	}
}

type QueryWordMatch struct {
	Word   string
	Offset int
}

type QueryResults struct {
	Filename    string
	WordMatches []QueryWordMatch

	FilenameIndex int
}

// instead of grouping find results by file, should we group by word?
// how do we prefer if file A has all 3 query words, vs B which has 2?
func (idx *Index) QueryIndex(querywords []string) ([]QueryResults, error) {
	resultsmap := make(map[int][]QueryWordMatch)

	for _, query := range querywords {
		// Lookup the word in the word strings table
		wordIdx := -1
		for i, word := range idx.words {
			if strings.EqualFold(word, query) {
				wordIdx = i
				break
			}
		}
		if wordIdx == -1 {
			// If the word isn't found move onto the next one
			continue
		}

		// Look up the offset into the search index
		var offset int64
		for _, off := range idx.offsets {
			if int(off.WordIndex) == wordIdx {
				offset = int64(off.Offset)
				break
			}
		}

		// Not possible to have a valid offset of 0 because these are file offsets and there is a header
		if offset == 0 {
			// Word not found in the offsets table, this is an error, ignore for now
			return nil, nil
		}

		if _, err := idx.indexRdr.Seek(offset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek into index failed - %w", err)
		}

		numMatches, err := binary.ReadUvarint(idx.indexRdr)
		if err != nil {
			return nil, fmt.Errorf("failed to read index - %w", err)
		}

		// Read out the matches in files
		for range numMatches {
			fidx, _ := binary.ReadUvarint(idx.indexRdr)
			numoff, _ := binary.ReadUvarint(idx.indexRdr)

			// Read out the offsets for each file
			matchOffsets := make([]uint32, numoff)
			for j := range numoff {
				off, err := binary.ReadUvarint(idx.indexRdr)
				if err != nil {
					return nil, fmt.Errorf("error reading from index: %w", err)
				}
				matchOffsets[j] = uint32(off)

				resultsmap[int(fidx)] = append(resultsmap[int(fidx)], QueryWordMatch{query, int(matchOffsets[j])})
			}
		}
	}

	for _, wordmatches := range resultsmap {
		// Sort the words in each entry by increasing offset
		slices.SortFunc[[]QueryWordMatch](wordmatches, func(a, b QueryWordMatch) int {
			if a.Offset < b.Offset {
				return -1
			} else if a.Offset > b.Offset {
				return 1
			}

			return 0
		})
	}

	// Sort the results in order of decreasing matches
	// In cases when the number of matches is the same for now sort the filename
	// lexicographically. TODO - a better scoring criteria would consider how many
	// of the query words are in each file and secondly how close together they are
	results := make([]QueryResults, 0, len(resultsmap))
	for fidx, wordmatches := range resultsmap {
		results = append(results, QueryResults{idx.filenames[fidx], wordmatches, fidx})
	}
	slices.SortFunc(results, func(a, b QueryResults) int {
		la := len(a.WordMatches)
		lb := len(b.WordMatches)

		if la < lb {
			return 1
		} else if la > lb {
			return -1
		}

		// Same number of matches, tie-breaker: filenames lexicographically
		return strings.Compare(a.Filename, b.Filename)
	})

	return results, nil
}

func (idx *Index) CatalogContent(filenameIdx int) (content []byte, filename string, ok bool) {
	if filenameIdx < 0 || filenameIdx >= len(idx.filenames) {
		return
	}

	entry := &idx.contentOffsets[filenameIdx]
	if _, err := idx.catalogRdr.Seek(int64(entry.Offset), io.SeekStart); err != nil {
		return
	}

	contents := make([]byte, entry.Length)
	var (
		gzr *gzip.Reader
		err error
	)
	if gzr, err = gzip.NewReader(idx.catalogRdr); err != nil {
		return
	}

	if _, err = io.ReadFull(gzr, contents); err != nil {
		return
	}

	return contents, idx.filenames[filenameIdx], true
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

func (idx *Index) loadCatalog(r io.Reader) error {
	var ni uint32
	if err := binary.Read(r, binary.BigEndian, &ni); err != nil {
		return err
	}

	idx.contentOffsets = make([]catalogContentsOffset, ni)
	if err := binary.Read(r, binary.BigEndian, idx.contentOffsets); err != nil {
		return err
	}
	return nil
}
