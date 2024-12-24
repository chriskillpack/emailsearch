package column

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
