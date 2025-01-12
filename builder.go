package column

import (
	"bytes"
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
)

// RE to split on spaces and include ' in the word
// TODO: try a regex that doesn't include trailing punctation such as ,?! and :
// `[^\s]+(?:'[^\s]+)*(?<![.,:;!?"])`
var emailWordsRe = regexp.MustCompile(`[^\s]+(?:'[^\s]+)*`)

type IndexBuilder struct {
	Verbose   bool
	NThreads  int
	InputPath string

	filenames *StringSet
	words     *StringSet
	wordIndex wordIndex
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
	Filename string
	Index    fileIndex
	Err      error
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
	inCh := make(chan string, ib.NThreads)
	outCh := make(chan injestedFile)

	var wg sync.WaitGroup
	wg.Add(ib.NThreads)

	// Spin up the worker "threads" (goroutines)
	for _ = range ib.NThreads {
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
				filep := filepath.Join(ib.InputPath, work)

				outData := injestedFile{Filename: work}
				var n int
				f, err := os.Open(filep)
				if err != nil {
					outData.Err = err
					goto respond
				}

				n, err = f.Read(scratch)
				f.Close()
				if err != nil {
					outData.Err = err
					goto respond
				}

				outData.Index, outData.Err = ib.computeFileIndex(scratch[0:n])
			respond:
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
	injested := make([]injestedFile, 0, len(filenames))
	for result := range outCh {
		injested = append(injested, result)
	}
	slices.SortFunc(injested, func(a, b injestedFile) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	// This is all single threaded for now
	for _, result := range injested {
		if result.Err == nil {
			// Merge the file index into the main index
			ib.MergeInFileIndex(result.Index, result.Filename)
			ib.nDocs++
		} else {
			fmt.Printf("Encountered error processing %s\n", result.Filename)
		}
	}

	return nil
}

func (idx *IndexBuilder) computeFileIndex(filedata []byte) (fileIndex, error) {
	rdr := bytes.NewReader(filedata)
	m, err := mail.ReadMessage(rdr)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(m.Body)
	if err != nil {
		return nil, err
	}

	// Find all the words in the email body
	// It doesn't handle lines that end with =XX where XX is a number
	index := make(fileIndex)
	for _, word := range emailWordsRe.FindAllIndex(body, -1) {
		txt := string(body[word[0]:word[1]])

		if _, ok := index[txt]; !ok {
			index[txt] = []int{word[0]}
		} else {
			index[txt] = append(index[txt], word[0])
		}
	}

	return index, nil
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

	// Filename stringset
	if err := ib.filenames.Serialize(filepath.Join(dir, FilenamesStringTable)); err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}
	fmt.Println("Serialized filename stringset")

	// Word stringset
	if err := ib.words.Serialize(filepath.Join(dir, WordsStringTable)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}
	fmt.Println("Serialized word stringset")

	if err := ib.writeIndexAndOffsets(filepath.Join(dir, CorpusIndex), filepath.Join(dir, IndexWordOffsets)); err != nil {
		return fmt.Errorf("failed to serialize: %w", err)
	}
	fmt.Println("Serialized index")

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
		CorpusSize: uint32(ib.nDocs),
	}
	binary.Write(out, binary.BigEndian, bc)
	out.WriteTo(f)

	sortedWords := slices.Sorted(maps.Keys(ib.wordIndex))

	scratch := make([]byte, binary.MaxVarintLen64*2)
	for _, word := range sortedWords {
		matches := ib.wordIndex[word]

		widx, _ := ib.words.Index(word)
		wordCorpusOffsets[widx].WordIndex = int32(widx)
		foff, _ := f.Seek(0, io.SeekCurrent) // TODO - replace with something else
		wordCorpusOffsets[widx].Offset = foff

		n := binary.PutUvarint(scratch, uint64(len(matches)))
		out.Write(scratch[:n])

		for i := range matches {
			// FilenameIndex
			n = binary.PutUvarint(scratch, uint64(matches[i].FilenameStringIndex))
			// NumOffsets
			n += binary.PutUvarint(scratch[n:], uint64(len(matches[i].Offsets)))
			out.Write(scratch[:n])

			for _, off := range matches[i].Offsets {
				n = binary.PutUvarint(scratch, uint64(off))
				out.Write(scratch[:n])
			}
		}

		out.WriteTo(f)
	}
	f.Close()

	if err := writeIndexOffsetsFile(wordCorpusOffsets, offsetsFname); err != nil {
		return err
	}
	fmt.Println("Serialized word offsets")

	return nil
}

func writeIndexOffsetsFile(wordCorpusOffsets []serializedWordIndexOffset, filename string) error {
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

func createOutDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}
