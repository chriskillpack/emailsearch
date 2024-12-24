package column

import (
	"bytes"
	"encoding/binary"
	"os"
)

type serializedStringSetHeader struct {
	NumStrings int32

	// Followed by each string one after the other
	// Each string is of the form byte length (int16) and then the bytes of the string
	// Strings are stored as UTF-8
}

type StringSet struct {
	strings map[string]int
	index   int
}

func NewStringSet() *StringSet {
	ss := &StringSet{strings: make(map[string]int)}

	return ss
}

// Insert a string into the string set and return it's index
func (ss *StringSet) Insert(s string) int {
	if idx, ok := ss.strings[s]; ok {
		return idx
	}
	idx := ss.index
	ss.strings[s] = idx
	ss.index = ss.index + 1
	return idx
}

// Return the index of a string in the set. Returns false if the word is not
// in the set.
func (ss *StringSet) Index(s string) (int, bool) {
	idx, ok := ss.strings[s]
	return idx, ok
}

// Flattens the set and returns it as an array of strings in insertion order
func (ss *StringSet) Flatten() []string {
	sa := make([]string, len(ss.strings))
	for str, index := range ss.strings {
		sa[index] = str
	}

	return sa
}

// Persists the stringset to filepath. The format is binary.
func (ss *StringSet) Serialize(filepath string) error {
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
