package column

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"maps"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const (
	FilenamesStringTable = "filenames.sid"
	WordsStringTable     = "words.sid"
	CorpusIndex          = "corpus.index"
	IndexWordOffsets     = "word.offsets"
	CorpusCatalog        = "corpus.cat"
	QueryPrefixTree      = "query.trie"
)

// RE to split on spaces and include ' in the word
// TODO: try a regex that doesn't include trailing punctation such as ,?! and :
// `[^\s]+(?:'[^\s]+)*(?<![.,:;!?"])`
var emailWordsRe = regexp.MustCompile(`[^\s]+(?:'[^\s]+)*`)

type IndexBuilder struct {
	Verbose             bool
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

const (
	SerializePhase_FilenameSet = 1
	SerializePhase_WordsSet    = 2
	SerializePhase_Index       = 3
	SerializePhase_Catalog     = 4
	SerializePhase_Trie        = 5

	SerializeEvent_BeginPhase    = 0
	SerializeEvent_EndPhase      = 1
	SerializeEvent_ProgressPhase = 2
)

// SerializeUpdate holds information about a progress change in the Serialize
// method.
type SerializeUpdate struct {
	Event int // See SerializeEvent_* constants
	Phase int // See SerializePhase_* constants
	N     int // Number of items
}

func (i *IndexBuilder) Init() {
	i.initOnce.Do(func() {
		i.filenames = NewStringSet()
		i.words = NewStringSet()
		i.wordIndex = make(wordIndex)
	})
}

func (ib *IndexBuilder) verbose(format string, a ...any) {
	if ib.Verbose {
		fmt.Printf(format, a...)
	}
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
		for i, file := range filenames {
			if i == 0 || ((i % 10000) == 0) || i == len(filenames)-1 {
				ib.verbose("%d -> %s\n", i, file)
			}

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

func (idx *IndexBuilder) computeFileIndex(content []byte) fileIndex {
	// Find all the words in the email body
	// It doesn't handle lines that end with =XX where XX is a number
	index := make(fileIndex)
	for _, word := range emailWordsRe.FindAllIndex(content, -1) {
		txt := string(content[word[0]:word[1]])

		if _, ok := index[txt]; !ok {
			index[txt] = []int{word[0]}
		} else {
			index[txt] = append(index[txt], word[0])
		}
	}

	return index
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
	fmt.Println("Serializing filename stringset")
	if err := ib.filenames.Serialize(filepath.Join(dir, FilenamesStringTable)); err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}

	// Word stringset (phase 2)
	fmt.Println("Serializing word stringset")
	if err := ib.words.Serialize(filepath.Join(dir, WordsStringTable)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
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
	fmt.Println("Serializing prefix tree")
	if err := ib.buildAndWritePrefixTree(filepath.Join(dir, QueryPrefixTree)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}

	if ib.SerializeProgressCh != nil {
		close(ib.SerializeProgressCh)
	}

	return nil
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
		matches := ib.wordIndex[word]

		widx, _ := ib.words.Index(word)
		wordCorpusOffsets[widx].WordIndex = uint32(widx)
		foff, err := f.Seek(0, io.SeekCurrent) // TODO - replace with something else
		if err != nil {
			return err
		}
		wordCorpusOffsets[widx].Offset = foff

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

	fmt.Println("Serializing word offsets")
	if err := writeIndexOffsetsFile(wordCorpusOffsets, offsetsFname); err != nil {
		return err
	}

	return nil
}

func (ib *IndexBuilder) writeCatalog(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// File format of the catalog
	// 0x00: u32 Number of catalog entries (N) in offset table
	// 0x04: u32 File offset to compressed content of file index 0
	// 0x08: u32 Length of uncompressed content of file index 0
	// 0x0C: u32 File offset to compressed content of file index 1
	// 0x10: u32 Length of uncompressed content of file index 1
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
	binary.Write(f, binary.BigEndian, uint32(len(ib.injested)))
	offsets := make([]uint32, len(ib.injested)*2)
	binary.Write(f, binary.BigEndian, offsets) // write out as a placeholder
	blah, _ := f.Seek(0, io.SeekCurrent)
	foffset := uint32(blah)

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_BeginPhase,
		Phase: SerializePhase_Catalog,
		N:     len(ib.injested),
	})

	for _, injested := range ib.injested {
		if injested.Err != nil {
			continue
		}

		if int(uint32(injested.Len)) != injested.Len {
			panic("content length overflow")
		}

		fidx, _ := ib.filenames.Index(injested.Filename)
		offsets[fidx*2+0] = foffset
		offsets[fidx*2+1] = uint32(injested.Len)

		if _, err := f.Write(injested.Compressed); err != nil {
			return err
		}

		// Overflow check on the offset
		if foffset+uint32(len(injested.Compressed)) < foffset {
			panic("offset overflow")
		}
		foffset += uint32(len(injested.Compressed))

		ib.serializeUpdate(SerializeUpdate{
			Event: SerializeEvent_ProgressPhase,
			Phase: SerializePhase_Catalog,
			N:     1,
		})
	}

	// Go back and write out the completed offsets table
	if _, err = f.Seek(4, io.SeekStart); err != nil {
		return err
	}

	ib.serializeUpdate(SerializeUpdate{
		Event: SerializeEvent_EndPhase,
		Phase: SerializePhase_Catalog,
	})

	return binary.Write(f, binary.BigEndian, offsets)
}

func (ib *IndexBuilder) buildAndWritePrefixTree(filename string) error {
	trie := NewTrie()

	words, _ := ib.words.Flatten()
	for _, word := range words {
		trie.InsertWord(word)
	}

	data, err := trie.Serialize()
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0666)
}

func writeIndexOffsetsFile(wordCorpusOffsets []serializedWordIndexOffset, filename string) error {
	if int(uint32(len(wordCorpusOffsets))) != len(wordCorpusOffsets) {
		panic("number of documents exceeds file format limits")
	}

	buf := &bytes.Buffer{}

	hdr := serializedWordOffsetHeader{
		Version:    1,
		NumEntries: uint32(len(wordCorpusOffsets)),
	}
	if err := binary.Write(buf, binary.BigEndian, &hdr); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.BigEndian, wordCorpusOffsets); err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	_, err = buf.WriteTo(f)
	f.Close()

	return err
}

// Reads everything from the Reader r into data starting from the front. It
// assumes that data is big enough. Returns the number of bytes read and the
// error. If an EOF is encountered the function assumes success and returns
// a nil error in that case.
func readAllInto(data []byte, r io.Reader) (int, error) {
	off := 0
	for {
		n, err := r.Read(data[off:cap(data)])
		off += n
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return off, err
		}
	}
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
