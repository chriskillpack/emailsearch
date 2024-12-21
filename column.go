package column

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"net/mail"
	"os"
	"path/filepath"
	"text/scanner"
)

type Match struct {
	FilenameStringIndex int
	Offsets             []int
}

// LocalIndex tracks the positions of words in a specific file
type LocalIndex map[string][]int

// WordIndex is the global index for all the files in the corpus
// As such it tracks more information than LocalIndex does.
type WordIndex map[string][]Match

type Corpus struct {
	filenames *StringSet
	words     *StringSet
	wordIndex WordIndex
}

// Corpus file format structures
type BinaryMatch struct {
	FilenameIndex int32
	NumOffsets    int32
	// Followed by NumOffsets of int32
}

type BinaryWord struct {
	WordLen    int32
	Word       string
	NumMatches int32
	// Followed by NumMatches of BinaryMatch
}

type BinaryCorpus struct {
	Version    int32
	NumEntries int64
	// Followed by NumEntries of BinaryWord
	//Entry      []BinaryWord
}

type BinaryWordCorpusOffset struct {
	WordIndex int32 // Index into the word string table
	Offset    int64 // Binary offset into the corpus file
}

func NewCorpus() *Corpus {
	c := &Corpus{
		filenames: NewStringSet(),
		words:     NewStringSet(),
		wordIndex: make(WordIndex),
	}
	return c
}

func ComputeIndex(filename string, data []byte) (LocalIndex, error) {
	rdr := bytes.NewReader(data)
	m, err := mail.ReadMessage(rdr)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(m.Body)
	if err != nil {
		return nil, err
	}

	index := make(LocalIndex)

	// TODO - offsets are in email body not from start of file
	var s scanner.Scanner
	s.Init(bytes.NewReader(body))
	s.Error = func(_ *scanner.Scanner, _ string) {} // Do not report messages to stderr

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		txt := s.TokenText()
		if _, ok := index[txt]; !ok {
			index[txt] = []int{s.Position.Offset}
		} else {
			index[txt] = append(index[txt], s.Position.Offset)
		}
	}

	return index, nil
	// fmt.Printf("%v\n", index)
	/*
		var s scanner.Scanner
		s.Init(bytes.NewReader(data))
		s.Filename = filename

		for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
			txt := s.TokenText()
			fmt.Printf("token: %s\n", txt)
		}
	*/

	/*
		lines := []string{}
		scanner := bufio.NewScanner(bytes.NewReader(data))
		body := false
		for scanner.Scan() {
			// Assume for now each file is an email with headers, a blank line and then the body
			// For now let's skip all the headers
			l := strings.Trim(scanner.Text(), " \t")
			if !body && l != "" {
				continue // Assume header because we haven't reached a blank line
			}
			if l == "" {
				body = true // In the body now, but also we don't want blank lines from the body
				continue
			}
			lines = append(lines, l)
		}

		// Scan over each word in the line tokenizing it
		for _, line := range lines {
			tokens := strings.Split(line, " \t")

			// For each token add it into the index
		}
	*/
}

// Seralize the corpus files to an output directory. The directory will be created if it
// does not exist.
func (c *Corpus) Serialize(dir string) error {
	if err := createOutDir(dir); err != nil {
		return err
	}

	// Filename stringset
	if err := serializeStringSet(c.filenames, filepath.Join(dir, "filenames.sid")); err != nil {
		return err
	}
	fmt.Println("Serialized filename stringset")

	// Word stringset (redundant)
	if err := serializeStringSet(c.words, filepath.Join(dir, "words.sid")); err != nil {
		return err
	}
	fmt.Println("Serialized word stringset")

	f, err := os.Create(filepath.Join(dir, "corpus.index"))
	if err != nil {
		return err
	}
	defer f.Close()

	wordCorpusOffsets := make([]BinaryWordCorpusOffset, len(c.wordIndex))
	wcocounter := 0

	out := &bytes.Buffer{}

	bc := BinaryCorpus{Version: 1, NumEntries: int64(len(c.wordIndex))}
	binary.Write(out, binary.BigEndian, bc)
	out.WriteTo(f)

	bw := BinaryWord{}
	bm := BinaryMatch{}
	for word, matches := range c.wordIndex {
		out.Reset()

		widx, _ := c.words.Index(word)
		wordCorpusOffsets[wcocounter].WordIndex = int32(widx)
		foff, _ := f.Seek(0, io.SeekCurrent)
		wordCorpusOffsets[wcocounter].Offset = foff
		wcocounter++

		bw.NumMatches = int32(len(matches))
		bw.WordLen = int32(len(word))
		bw.Word = word
		binary.Write(out, binary.BigEndian, bw)

		for i := range matches {
			bm.FilenameIndex = int32(matches[i].FilenameStringIndex)
			bm.NumOffsets = int32(len(matches[i].Offsets))
			binary.Write(out, binary.BigEndian, bm)

			offsets := make([]int32, len(matches[i].Offsets))
			for j, off := range matches[i].Offsets {
				offsets[j] = int32(off)
			}
			binary.Write(out, binary.BigEndian, offsets)
		}

		out.WriteTo(f)
	}
	f.Close()
	fmt.Println("Serialized index")

	f, err = os.Create(filepath.Join(dir, "word.offsets"))
	if err != nil {
		return err
	}
	binary.Write(f, binary.BigEndian, wordCorpusOffsets)
	f.Close()
	fmt.Println("Serialized word offsets")

	// The initial text based approach below was scrapped because it is too slow
	/*
			// Bespoke text format for the corpus
			// {VERSION NUMBER} {NUM ENTRIES}
			// For each entry
			// "{WORD}" {NUM FILES WITH THAT WORD}
			// fidx {INDEX OF FILENAME IN FILENAMES TABLE}
			// {NUM OFFSETS}: {OFFSET 1} {OFFSET 2} ... {OFFSET N}
			fmt.Fprintf(f, "1 %d\n", len(c.wordIndex))
			for word, matches := range c.wordIndex {
				fmt.Fprintf(f, "%q %d\n", word, len(matches))

				for i := range matches {
					fmt.Fprintf(f, "fidx %d\n", matches[i].FilenameStringIndex)
					fmt.Fprintf(f, "%d: ", len(matches[i].Offsets))
					for _, offset := range matches[i].Offsets {
						fmt.Fprintf(f, "%d ", offset)
					}
					fmt.Fprintf(f, "\n")
				}
			}
			fmt.Fprintf(f, "\n")
		f.Close()
	*/

	return nil
}

type FileStringTableHeader struct {
	NumStrings int32

	// Followed by each string one after the other
	// Each string is of the form byte length (int16) and then the bytes of the string
	// Strings are stored as UTF-8
}

// Persists the stringset ss to filepath
// The format is binary
func serializeStringSet(ss *StringSet, filepath string) error {
	filenames := ss.Flatten()
	out := &bytes.Buffer{}

	if err := binary.Write(out, binary.BigEndian, int32(len(filenames))); err != nil {
		return err
	}

	for _, filename := range filenames {
		binary.Write(out, binary.BigEndian, int16(len(filename)))
		out.WriteString(filename)
	}

	f, err := os.Create(filepath)
	if err != nil {
		return err
	}
	out.WriteTo(f)
	f.Close()

	return nil
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

func (c *Corpus) MergeInLocalIndex(localIndex LocalIndex, filename string) {
	fidx := c.filenames.Insert(filename)

	for word, offsets := range localIndex {
		c.words.Insert(word)

		if _, ok := c.wordIndex[word]; !ok {
			// If the word is not in the corpus, add the word to the index
			c.wordIndex[word] = []Match{{fidx, offsets}}
		} else {
			c.wordIndex[word] = append(c.wordIndex[word], Match{fidx, offsets})
		}
	}
}

// Walk a path of the filesystem and return the set of files in that path
// The names of the files are relative to the walk path, so Walk("/home/chris")
// will return ["foo/cat.txt"] for /home/chris/foo/cat.txt
func Walk(path string) ([]string, int64, error) {
	files := []string{}

	var maxSize int64
	err := filepath.WalkDir(path, func(wpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		finfo, err := d.Info()
		if err != nil {
			return err
		}
		maxSize = max(maxSize, finfo.Size())

		relpath, err := filepath.Rel(path, wpath)
		if err != nil {
			return err
		}

		files = append(files, relpath)
		return nil // Continue walking
	})

	return files, maxSize, err
}
