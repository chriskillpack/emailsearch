package emailsearch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
)

var errTooBigToSave = errors.New("the capacity of the stringset exceeds disk format")

const stringSetMagic uint32 = 'S'<<24 | 'T'<<16 | 'R'<<8 | 'S'

type serializedStringSetHeader struct {
	Magic    uint32
	Version  uint32 // currently 1
	NStrings uint32
	MaxLen   uint16

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
func (ss *StringSet) Flatten() ([]string, int) {
	maxlen := 0
	sa := make([]string, len(ss.strings))
	for str, index := range ss.strings {
		sa[index] = str
		maxlen = max(maxlen, len(str))
	}

	return sa, maxlen
}

// Persists the stringset to filepath. The format is binary.
func (ss *StringSet) Serialize(outpath string) error {
	strings, maxlen := ss.Flatten()

	if len(strings) > math.MaxUint32 || maxlen >= math.MaxUint16 {
		return errTooBigToSave
	}

	out := &bytes.Buffer{}
	out.Grow(20 * len(strings)) // Assume avg string length is 20 bytes

	hdr := serializedStringSetHeader{
		Magic:    stringSetMagic,
		Version:  1,
		NStrings: uint32(len(strings)),
		MaxLen:   uint16(maxlen),
	}
	if err := binary.Write(out, binary.BigEndian, &hdr); err != nil {
		return err
	}

	scratch := [binary.MaxVarintLen16]byte{}
	for _, str := range strings {
		// Write out length as a varint
		n := binary.PutUvarint(scratch[:], uint64(len(str)))
		out.Write(scratch[0:n])
		// The WriteString only writes out the contents of the string, there is
		// no preceding fields or trailing zero byte.
		out.WriteString(str)
	}

	return os.WriteFile(outpath, out.Bytes(), 0666)
}
