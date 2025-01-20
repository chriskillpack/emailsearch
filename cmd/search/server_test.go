package main

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestHighlightContent(t *testing.T) {
	cases := []struct {
		Name       string
		Input      string
		Highlights []matchHighlight
		Expected   string
	}{
		{"One highlight", "Hello world", []matchHighlight{{6, 5}}, "Hello <mark>world</mark>"},
		{"Two highlights", "Hello world under world", []matchHighlight{{6, 5}, {18, 5}}, "Hello <mark>world</mark> under <mark>world</mark>"},
		{"Midword", "Helloworld", []matchHighlight{{5, 5}}, "Hello<mark>world</mark>"},
		{"After last", "Hello world this is a fine day", []matchHighlight{{6, 5}}, "Hello <mark>world</mark> this is a fine day"},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			want, got := tc.Expected, highlightContent([]byte(tc.Input), tc.Highlights)
			if string(got) != want {
				t.Errorf("Expected %q, got %q", want, string(got))
			}
		})
	}
}

func createTestData(filenameIdx int, highlights []matchHighlight) []byte {
	buf := make([]byte, 0, 64)

	buf = binary.AppendUvarint(buf, uint64(filenameIdx))
	buf = binary.AppendUvarint(buf, uint64(len(highlights)))
	for _, h := range highlights {
		buf = binary.AppendUvarint(buf, uint64(h.Offset))
		buf = binary.AppendUvarint(buf, uint64(h.Length))
	}

	return buf
}

func TestDecodeEmailURL(t *testing.T) {
	cases := []struct {
		Name        string
		Input       []byte
		Expected    emailMatch
		WantErr     bool
		ErrContains string
	}{
		{
			Name:        "empty input",
			Input:       []byte{},
			WantErr:     true,
			ErrContains: "reading filename index",
		},
		{
			Name:        "invalid filename index",
			Input:       createTestData(-10, nil),
			WantErr:     true,
			ErrContains: "invalid filename index",
		},
		{
			Name:  "zero highlights",
			Input: createTestData(1, nil),
			Expected: emailMatch{
				FilenameIndex: 1,
			},
		},
		{
			Name:  "single highlight",
			Input: createTestData(1, []matchHighlight{{Offset: 10, Length: 5}}),
			Expected: emailMatch{
				FilenameIndex: 1,
				Highlights:    []matchHighlight{{Offset: 10, Length: 5}},
			},
		},
		{
			Name:  "multiple highlights",
			Input: createTestData(1, []matchHighlight{{Offset: 10, Length: 7}, {Offset: 20, Length: 6}}),
			Expected: emailMatch{
				FilenameIndex: 1,
				Highlights:    []matchHighlight{{Offset: 10, Length: 7}, {Offset: 20, Length: 6}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := decodeEmailURL(tc.Input)

			if tc.WantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tc.ErrContains != "" && !strings.Contains(err.Error(), tc.ErrContains) {
					t.Errorf("wanted error to contain %s, got %s", tc.ErrContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if got.FilenameIndex != tc.Expected.FilenameIndex {
				t.Errorf("FilenameIndex = %v, want %v", got.FilenameIndex, tc.Expected.FilenameIndex)
			}

			if len(got.Highlights) != len(tc.Expected.Highlights) {
				t.Errorf("len(Highlights) = %v, want %v", len(got.Highlights), len(tc.Expected.Highlights))
				return
			}
		})
	}
}
