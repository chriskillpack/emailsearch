package emailsearch

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"maps"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unsafe"
)

const (
	FilenamesStringTable = "filenames.sid"
	WordsStringTable     = "words.sid"
	CorpusIndex          = "corpus.index"
	IndexWordOffsets     = "word.offsets"
	CorpusCatalog        = "corpus.cat"
	QueryPrefixTree      = "query.trie"
)

type IndexBuilder struct {
	NThreads            int
	InputPath           string
	InjestProgressCh    chan<- InjestUpdate
	SerializeProgressCh chan<- SerializeUpdate

	filenames *StringSet
	words     *StringSet
	wordIndex wordIndex
	injested  []injestedFile
	nDocs     int // Number of documents successfully processed and merged into index

	initOnce sync.Once
}

// fileIndex tracks the positions of words in a specific file
type fileIndex map[string][]int

type match struct {
	FilenameStringIndex int
	Offsets             []int
}

// wordIndex is the global index for all the files in the corpus
// As such it tracks more information than LocalIndex does.
type wordIndex map[string][]match

// Holds the output of one of the injestion workers
type injestedFile struct {
	Filename   string
	Index      fileIndex
	Len        int    // length of the indexed content in the file
	Compressed []byte // gzip compressed copy of filedata that was injested
	Err        error  // error during processing
}

type InjestUpdate struct {
	Filename string
	Success  bool
	Phase    int
}

type SerializePhase int

const (
	SerializePhase_FilenameSet SerializePhase = iota + 1
	SerializePhase_WordsSet
	SerializePhase_Index
	SerializePhase_Catalog
	SerializePhase_WordOffsets
	SerializePhase_PrefixTree
)

const (
	SerializeEvent_BeginPhase = iota
	SerializeEvent_EndPhase
	SerializeEvent_ProgressPhase
)

// SerializeUpdate holds information about a progress change in the Serialize
// method.
type SerializeUpdate struct {
	Event int            // See SerializeEvent_* constants
	Phase SerializePhase // See SerializePhase_* constants
	N     int            // Number of items
}

func (i *IndexBuilder) Init() {
	i.initOnce.Do(func() {
		i.filenames = NewStringSet()
		i.words = NewStringSet()
		i.wordIndex = make(wordIndex)
	})
}

func (ib *IndexBuilder) InjestFiles(filenames []string, maxSize int64) error {
	// 32-bit overflow check
	if int(uint32(len(filenames))) != len(filenames) {
		panic("number of files exceeds file format limits")
	}

	inCh := make(chan string, ib.NThreads)
	outCh := make(chan injestedFile)

	var wg sync.WaitGroup
	wg.Add(ib.NThreads)

	// Spin up the worker "threads" (goroutines)
	for range ib.NThreads {
		// Each worker gets it's own scratch buffer to load file data into. This
		// is an attempt to reduce churn in the GC. The scratch buffer is sized
		// to the maximum file size to avoid reallocating the buffer.
		scratch := make([]byte, maxSize)

		go func(scratch []byte) {
			defer wg.Done()

			// Each worker pulls a filename of an email from the input channel,
			// builds a LocalIndex of the email body and then sends result
			// through the output channel.
			for work := range inCh {
				outData := injestedFile{Filename: work}

				f, err := os.Open(filepath.Join(ib.InputPath, work))
				if err != nil {
					outData.Err = err
					outCh <- outData
					continue
				}

				if m, err := mail.ReadMessage(f); err == nil {
					compbody := &bytes.Buffer{}
					gzw := gzip.NewWriter(compbody)
					n, err := readAllInto(scratch, io.TeeReader(m.Body, gzw))
					if err == nil {
						outData.Index = ib.computeFileIndex(scratch[:n])
						gzw.Close()
						outData.Compressed = compbody.Bytes()
						outData.Len = int(n)
					}
				}
				f.Close()
				outData.Err = err
				outCh <- outData
			}
		}(scratch)
	}

	// Spin up a goroutine to insert the filenames
	go func() {
		for _, file := range filenames {
			inCh <- file
		}
		close(inCh)
	}()

	// Spin up a goroutine to wait for the worker and insertion goroutine to
	// be complete and then close the output channel to indicate that there
	// are no more results.
	go func() {
		wg.Wait()
		close(outCh)
	}()

	// Retrieve the injested results and sort for a deterministic building of
	// the main index.
	ib.injested = make([]injestedFile, 0, len(filenames))
	for result := range outCh {
		ib.injested = append(ib.injested, result)

		success := result.Err == nil
		ib.injestUpdate(InjestUpdate{result.Filename, success, 1})
	}
	slices.SortFunc(ib.injested, func(a, b injestedFile) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	// This is all single threaded for now
	for _, result := range ib.injested {
		if result.Err != nil {
			fmt.Printf("Encountered error processing %s\n", result.Filename)
			continue
		}

		// Merge the file index into the main index
		ib.MergeInFileIndex(result.Index, result.Filename)
		ib.nDocs++

		ib.injestUpdate(InjestUpdate{result.Filename, true, 2})
	}
	if ib.InjestProgressCh != nil {
		close(ib.InjestProgressCh)
	}

	return nil
}

// TODO: It doesn't handle lines that end with =XX where XX is a number
func (idx *IndexBuilder) computeFileIndex(content []byte) fileIndex {
	// Find all the words in the email body
	index := make(fileIndex)

	s := string(content) // TODO: investigate memory / perf hit of this
	for span := range splitText(s) {
		word := s[span.start:span.end]
		txt := strings.ToLower(word)

		// Ignore short words
		if len(word) < 3 {
			continue
		}

		// Ignore stop words
		if isStopWord(word) {
			continue
		}

		if _, ok := index[txt]; !ok {
			index[txt] = []int{span.start}
		} else {
			index[txt] = append(index[txt], span.start)
		}
	}

	return index
}

type wordSpan struct {
	start, end int
}

func splitText(text string) iter.Seq[wordSpan] {
	return func(yield func(wordSpan) bool) {
		var start int = -1

		for i, r := range text {
			if (unicode.IsLetter(r) || unicode.IsDigit(r)) && start == -1 {
				start = i
			} else if !(unicode.IsLetter(r) && !unicode.IsDigit(r)) && start != -1 {
				if (!yield(wordSpan{start, i})) {
					return
				}

				start = -1
			}
		}

		if start != -1 {
			yield(wordSpan{start, len(text)})
		}
	}
}

var (
	iswOnce sync.Once
	swMap   map[string]struct{}
)

func isStopWord(s string) bool {
	// Top 20 taken from https://en.wikipedia.org/wiki/Most_common_words_in_English
	var stopWords = []string{
		"the", "be", "to", "of", "and",
		"a", "in", "that", "have", "i",
		"it", "for", "not", "on", "with",
		"he", "as", "you", "do", "at",
	}
	iswOnce.Do(func() {
		swMap = make(map[string]struct{})
		for _, s := range stopWords {
			swMap[s] = struct{}{}
		}
	})

	_, exists := swMap[strings.ToLower(s)]
	return exists
}

func (c *IndexBuilder) MergeInFileIndex(fileIndex fileIndex, filename string) {
	fidx := c.filenames.Insert(filename)

	sortedWords := slices.Sorted(maps.Keys(fileIndex))
	for _, word := range sortedWords {
		offsets := fileIndex[word]
		c.words.Insert(word)

		if _, ok := c.wordIndex[word]; !ok {
			// If the word is not in the corpus, add the word to the index
			c.wordIndex[word] = []match{{fidx, offsets}}
		} else {
			c.wordIndex[word] = append(c.wordIndex[word], match{fidx, offsets})
		}
	}
}

// Seralize the index files to an output directory. The directory will be created if it
// does not exist.
func (ib *IndexBuilder) Serialize(dir string) error {
	if err := createOutDir(dir); err != nil {
		return err
	}

	// Filename stringset (phase 1)
	if err := ib.serializeStringSet(ib.filenames, filepath.Join(dir, FilenamesStringTable), SerializePhase_FilenameSet); err != nil {
		return fmt.Errorf("failed to serialize filename string set: %w", err)
	}

	// Word stringset (phase 2)
	if err := ib.serializeStringSet(ib.words, filepath.Join(dir, WordsStringTable), SerializePhase_WordsSet); err != nil {
		return fmt.Errorf("failed to serialize word string set: %w", err)
	}

	// Index and offsets file (phase 3)
	if err := ib.writeIndexAndOffsets(filepath.Join(dir, CorpusIndex), filepath.Join(dir, IndexWordOffsets)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}

	// Compressed corpus catalog (phase 4)
	if err := ib.writeCatalog(filepath.Join(dir, CorpusCatalog)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}

	// Build and serialize the prefix tree (phase 5)
	if err := ib.buildAndWritePrefixTree(filepath.Join(dir, QueryPrefixTree)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}

	if ib.SerializeProgressCh != nil {
		close(ib.SerializeProgressCh)
	}

	return nil
}

func (ib *IndexBuilder) serializeStringSet(set *StringSet, filepath string, phase SerializePhase) error {
	update := SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: phase,
		N:     1,
	}
	ib.serializeUpdate(update)

	err := set.Serialize(filepath)

	update.Event = SerializeEvent_EndPhase
	ib.serializeUpdate(update)

	return err
}

func (ib *IndexBuilder) writeIndexAndOffsets(indexFname, offsetsFname string) error {
	f, err := os.Create(indexFname)
	if err != nil {
		return err
	}
	defer f.Close()

	wordCorpusOffsets := make([]serializedWordIndexOffset, len(ib.wordIndex))

	out := &bytes.Buffer{}

	bc := serializedIndexHeader{
		Magic:      indexMagic,
		Version:    1,
		NumEntries: uint64(len(ib.wordIndex)),
		CorpusSize: uint32(ib.nDocs), // guaranteed value won't overflow uint32
	}
	binary.Write(out, binary.BigEndian, bc)
	out.WriteTo(f)

	sortedWords := slices.Sorted(maps.Keys(ib.wordIndex))

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: SerializePhase_Index,
		N:     len(sortedWords),
	})

	scratch := make([]byte, binary.MaxVarintLen64*2)
	for _, word := range sortedWords {
		widx, _ := ib.words.Index(word)
		wordCorpusOffsets[widx].WordIndex = uint32(widx)
		foff, err := f.Seek(0, io.SeekCurrent) // TODO - replace with something else
		if err != nil {
			return err
		}
		wordCorpusOffsets[widx].Offset = foff

		matches := ib.wordIndex[word]
		n := binary.PutUvarint(scratch, uint64(len(matches)))
		if _, err := out.Write(scratch[:n]); err != nil {
			return err
		}

		for i := range matches {
			// FilenameIndex
			n = binary.PutUvarint(scratch, uint64(matches[i].FilenameStringIndex))
			// NumOffsets
			n += binary.PutUvarint(scratch[n:], uint64(len(matches[i].Offsets)))
			if _, err := out.Write(scratch[:n]); err != nil {
				return err
			}

			for _, off := range matches[i].Offsets {
				n = binary.PutUvarint(scratch, uint64(off))
				if _, err := out.Write(scratch[:n]); err != nil {
					return err
				}
			}
		}

		out.WriteTo(f)

		ib.serializeUpdate(SerializeUpdate{
			Event: SerializeEvent_ProgressPhase,
			Phase: SerializePhase_Index,
			N:     1,
		})
	}
	f.Close()

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_EndPhase,
		Phase: SerializePhase_Index,
	})

	if err := ib.writeIndexOffsetsFile(wordCorpusOffsets, offsetsFname); err != nil {
		return err
	}

	return nil
}

func (ib *IndexBuilder) writeCatalog(filename string) error {
	if int(uint32(len(ib.injested))) != len(ib.injested) {
		panic("number of catalog items exceeds file format limits")
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	wr := bufio.NewWriter(f)

	// File format of the catalog
	// 0x00: u32 Magic number 'CTLG'
	// 0x04: u32 Version number
	// 0x08: u32 Number of catalog entries (N) in offset table
	// 0x0C: u32 File offset to compressed content of file index 0
	// 0x10: u32 Length of uncompressed content of file index 0
	// 0x14: u32 File offset to compressed content of file index 1
	// 0x18: u32 Length of uncompressed content of file index 1
	// ....:
	// ....: u32 File offset to compressed content of file index N-1
	// ....: u32 Length of uncompressed content of file index N-1
	// ....: Compressed content of file index 0
	// ....:
	// ....: Compressed content of file index N-1
	// EOF
	// If an offset and length are 0 it means that there is no stored content
	// for the corresponding file. This can happen because there was an error
	// indexing the files content.
	hdr := serializedCatalogHeader{
		Magic:      catalogMagic,
		Version:    1,
		NumEntries: uint32(len(ib.injested)),
	}
	if err := binary.Write(wr, binary.BigEndian, &hdr); err != nil {
		return err
	}
	hdrSize := int(unsafe.Sizeof(hdr))

	// Compute the offsets for the compressed content
	offlens := make([]uint32, len(ib.injested)*2)
	// offset holds the byte offset into the file of the initial byte of the
	// first injested file.
	offset := hdrSize + len(ib.injested)*2*4 // *4 for byte size of a uint32

	// Walk the injested content to fill out the offlens table
	for _, injested := range ib.injested {
		if injested.Err != nil {
			continue
		}

		if int(uint32(injested.Len)) != injested.Len {
			panic("content length overflow")
		}

		fidx, _ := ib.filenames.Index(injested.Filename)
		offlens[fidx*2+0] = uint32(offset)
		offlens[fidx*2+1] = uint32(injested.Len)

		// Check that advancing offset by data length does not overflow uint32
		if uint32(offset+len(injested.Compressed)) < uint32(offset) {
			panic("offset overflow")
		}
		offset += len(injested.Compressed)
	}

	// Write out the offlens table
	if err := binary.Write(wr, binary.BigEndian, offlens); err != nil {
		return err
	}

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: SerializePhase_Catalog,
		N:     len(ib.injested),
	})

	// Now walk the injested files again, this time writing out their content
	for _, injested := range ib.injested {
		_, err := wr.Write(injested.Compressed)
		if err != nil {
			return err
		}

		ib.serializeUpdate(SerializeUpdate{
			Event: SerializeEvent_ProgressPhase,
			Phase: SerializePhase_Catalog,
			N:     1,
		})
	}

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_EndPhase,
		Phase: SerializePhase_Catalog,
	})

	return wr.Flush()
}

func (ib *IndexBuilder) buildAndWritePrefixTree(filename string) error {
	update := SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: SerializePhase_PrefixTree,
		N:     1,
	}
	ib.serializeUpdate(update)

	trie := NewTrie()

	words, _ := ib.words.Flatten()
	for _, word := range words {
		trie.InsertWord(word)
	}

	// Write out the prefix tree
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	err = trie.Serialize(f)
	if err != nil {
		return err
	}

	update.Event = SerializeEvent_EndPhase
	ib.serializeUpdate(update)

	return err
}

func (ib *IndexBuilder) injestUpdate(u InjestUpdate) {
	if ib.InjestProgressCh != nil {
		ib.InjestProgressCh <- u
	}
}

func (ib *IndexBuilder) serializeUpdate(u SerializeUpdate) {
	if ib.SerializeProgressCh != nil {
		ib.SerializeProgressCh <- u
	}
}

func (ib *IndexBuilder) writeIndexOffsetsFile(wordCorpusOffsets []serializedWordIndexOffset, filename string) error {
	if int(uint32(len(wordCorpusOffsets))) != len(wordCorpusOffsets) {
		panic("number of documents exceeds file format limits")
	}

	update := SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: SerializePhase_WordOffsets,
		N:     1,
	}
	ib.serializeUpdate(update)

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	wr := bufio.NewWriter(f)

	// File format of the index offsets file
	// 0x00: u32 Magic number 'WRDO'
	// 0x04: u32 Version number (currently 1)
	// 0x08: u32 Number of entries in the table
	// 0x0C: u32 Index of word 0 in the words stringset
	// 0x10: s64 Byte offset in the index for word 0 matches
	// 0x18: u32 Index of word 1 in the words stringset
	// 0x1C: s64 Byte offset in the index for word 1 matches
	// ....:
	// ....: u32 Index of word N-1 in the words stringset
	// ....: s64 Byte offset in the index for word N-1 matches
	hdr := serializedWordOffsetHeader{
		Magic:      wordOffsetMagic,
		Version:    1,
		NumEntries: uint32(len(wordCorpusOffsets)),
	}
	if err := binary.Write(wr, binary.BigEndian, &hdr); err != nil {
		return err
	}
	if err := binary.Write(wr, binary.BigEndian, wordCorpusOffsets); err != nil {
		return err
	}

	update.Event = SerializeEvent_EndPhase
	ib.serializeUpdate(update)

	return wr.Flush()
}

// Reads everything from the Reader r into data starting from the front. It
// assumes that data is big enough. Returns the number of bytes read and the
// error. If an EOF is encountered the function assumes success and returns
// a nil error in that case.
func readAllInto(data []byte, r io.Reader) (int, error) {
	n, err := io.ReadFull(r, data)
	if err != nil {
		// The input buffer is sized large enough for the biggest file, so for
		// every other file we expected io.ErrUnexpectedEOF, and that's okay
		// it means we read to the end of the file.
		if err == io.ErrUnexpectedEOF {
			err = nil
		}
		return n, err
	}
	return n, nil
}

func createOutDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}
