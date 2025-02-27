package emailsearch

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-mmap/mmap"
)

// Index file format structures
const indexMagic uint32 = 'I'<<24 | 'N'<<16 | 'D'<<8 | 'X'

type serializedIndexHeader struct {
	Magic      uint32
	Version    uint32
	NumEntries uint64 // Number of words in the index
	CorpusSize uint32 // Number of documents the index was built from

	// Followed by NumEntries of serializedWord
	//Entry      []serializedWord
}

const wordOffsetMagic uint32 = 'W'<<24 | 'R'<<16 | 'D'<<8 | 'O'

type serializedWordOffsetHeader struct {
	Magic      uint32
	Version    uint32
	NumEntries uint32
}

type serializedWordIndexOffset struct {
	WordIndex uint32 // Index into the word string table
	Offset    int64  // Binary offset into the index file
}

const catalogMagic uint32 = 'C'<<24 | 'T'<<16 | 'L'<<8 | 'G'

type serializedCatalogHeader struct {
	Magic      uint32
	Version    uint32
	NumEntries uint32
}

type catalogContentEntry struct {
	Offset uint32 // Offset of the compressed content in the catalog
	Length uint32 // Length of the uncompressed content
}

// Index represents a search index and corpus that can be queried.
type Index struct {
	filenames      []string
	words          []string
	offsets        []serializedWordIndexOffset
	contentEntry   []catalogContentEntry
	wordsToOffsets map[string]int64
	prefixTree     *Trie
	CorpusSize     int

	indexRdr   *mmap.File // The search index is memory mapped
	catalogRdr *mmap.File // The compressed catalog is memory mapped
}

// LoadIndexFromDisk reads in data files generated by the indexer and wires
// everything up in memory.
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

	idx.buildWordOffsetsMap()

	idx.prefixTree, err = loadPrefixTree(filepath.Join(indexdir, QueryPrefixTree))
	if err != nil {
		return nil, err
	}
	fmt.Println("Loaded prefix tree")

	// Memory map the index in
	if idx.indexRdr, err = mmap.Open(filepath.Join(indexdir, CorpusIndex)); err != nil {
		return nil, err
	}
	// Read in the index header
	var header serializedIndexHeader
	if err = binary.Read(idx.indexRdr, binary.BigEndian, &header); err != nil {
		return nil, err
	}
	if header.Magic != indexMagic || header.Version != 1 {
		return nil, fmt.Errorf("unsupported index version number %d", header.Version)
	}
	idx.CorpusSize = int(header.CorpusSize)

	// Memory map the catalog in
	if idx.catalogRdr, err = mmap.Open(filepath.Join(indexdir, CorpusCatalog)); err != nil {
		return nil, err
	}
	// Read in the catalog header
	if err := idx.loadCatalogHeader(idx.catalogRdr); err != nil {
		return nil, err
	}

	return idx, nil
}

// Finish closes out file memory mappings. It does free up allocated memory.
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
	qwres := make([]map[int][]QueryWordMatch, len(querywords))
	for i := range len(querywords) {
		qwres[i] = make(map[int][]QueryWordMatch)
	}

	for qi, query := range querywords {
		lquery := strings.ToLower(query)

		// Skip stop words
		if isStopWord(lquery) {
			continue
		}

		offset, exists := idx.wordsToOffsets[lquery]
		if !exists {
			break
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

				qwres[qi][int(fidx)] = append(qwres[qi][int(fidx)], QueryWordMatch{query, int(matchOffsets[j])})
			}
		}
	}

	// Intersect all the query result maps which implements keyword1 AND keyword2 AND ...
	searchresults := intersectWordResults(qwres)

	// Sort the combined results so that matches are in increasing order
	for _, wordmatches := range searchresults {
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
	results := make([]QueryResults, 0, len(searchresults))
	for fidx, wordmatches := range searchresults {
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

// intersectWordResults combines the search results for the individual query words
// together into a final result set. Currently this is done by computing the
// intersection the separate results.
func intersectWordResults(results []map[int][]QueryWordMatch) map[int][]QueryWordMatch {
	if len(results) == 0 {
		return nil
	}

	final := make(map[int][]QueryWordMatch)
	firstMap := results[0]

	for k, v := range firstMap {
		found := true
		temp := slices.Clone(v) // do not modify firstMap

		for _, m := range results[1:] {
			if _, ok := m[k]; ok {
				temp = append(temp, m[k]...)
			} else {
				found = false
				break
			}
		}
		if found {
			final[k] = temp
		}
	}

	return final
}

// CatalogContent returns the content and filename of an indexed file.
func (idx *Index) CatalogContent(filenameIdx int) (content []byte, filename string, ok bool) {
	if filenameIdx < 0 || filenameIdx >= len(idx.filenames) {
		return
	}

	entry := &idx.contentEntry[filenameIdx]
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

// Prefix returns a slice of strings of words in the index that have prefix
// as their own prefix.
//
// The count determines the number of matching words to return:
//   - n > 0: at most n matches
//   - n == 0: the result in nil (no matches).
//   - n < 0: all matches
func (idx *Index) Prefix(prefix string, n int) []string {
	if idx.prefixTree == nil || n == 0 {
		return nil
	}

	matches := idx.prefixTree.WithPrefix(strings.ToLower(prefix))

	// Filter out stop words
	matches = filterFunc(matches, func(s string) bool { return !isStopWord(s) })

	if n < 0 {
		return matches
	}

	return matches[:min(len(matches), n)]
}

// buildWordOffsetsMap combines the words stringset and the word index offset
// table into a single map from word to index offset.
func (idx *Index) buildWordOffsetsMap() {
	idx.wordsToOffsets = make(map[string]int64)

	// Walk the offsets table
	for _, wo := range idx.offsets {
		word := idx.words[wo.WordIndex]
		idx.wordsToOffsets[word] = wo.Offset
	}
}

// filterFunc returns a new []string with only the elements of x for which f(x)
// returns true.
func filterFunc(x []string, f func(string) bool) []string {
	out := make([]string, 0, len(x))

	for _, a := range x {
		if f(a) {
			out = append(out, a)
		}
	}

	return out
}

// loadStringTable loads a serialized string table from disk and returns it
// as []string. The order of entries in []string matches that in the file.
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

	if hdr.Magic != stringSetMagic || hdr.Version != 1 {
		return nil, fmt.Errorf("unsupported string set version number %d", hdr.Version)
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
	if hdr.Magic != wordOffsetMagic || hdr.Version != 1 {
		return nil, fmt.Errorf("unsupported offsets version number %d", hdr.Version)
	}

	offsets := make([]serializedWordIndexOffset, hdr.NumEntries)
	if err := binary.Read(buf, binary.BigEndian, offsets); err != nil {
		return nil, err
	}

	return offsets, nil
}

// loadCatalogHeader reads in the compressed content catalog header which
// stores the offsets and uncompressed lengths of all injested content.
func (idx *Index) loadCatalogHeader(r io.Reader) error {
	var hdr serializedCatalogHeader
	if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
		return err
	}
	if hdr.Magic != catalogMagic || hdr.Version != 1 {
		return fmt.Errorf("unsupported catalog version number %d", hdr.Version)
	}

	idx.contentEntry = make([]catalogContentEntry, hdr.NumEntries)
	if err := binary.Read(r, binary.BigEndian, idx.contentEntry); err != nil {
		return err
	}
	return nil
}

// loadPrefixTree loads a serialized trie data structure into memory and returns
// the Trie instance.
func loadPrefixTree(filename string) (*Trie, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	trie, err := DeserializeTrie(data)
	if err != nil {
		return nil, err
	}

	return trie, err
}
