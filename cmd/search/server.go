package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chriskillpack/column"
)

var (
	//go:embed tmpl/*.html
	tmplFS embed.FS

	//go:embed static
	staticFS embed.FS

	indexTmpl          *template.Template
	resultsPartialTmpl *template.Template
	emailTmpl          *template.Template
)

type Server struct {
	hs *http.Server

	Index *column.Index
}

type matchHighlight struct {
	Offset, Length int // units are characters
}

type emailMatch struct {
	Highlights []matchHighlight

	FilenameIndex int
}

func init() {
	indexTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/index.html"))
	resultsPartialTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/_results.html"))
	emailTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/email.html"))
}

func NewServer(idx *column.Index, port string) *Server {
	srv := &Server{Index: idx}
	srv.hs = &http.Server{
		Addr:    net.JoinHostPort("0.0.0.0", port),
		Handler: srv.serveHandler(),
	}
	return srv
}

func (s *Server) Start() error {
	return s.hs.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.hs.Shutdown(ctx)
}

func (s *Server) serveHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.Handle("GET /search", s.serveSearch())
	mux.Handle("GET /email/{email}", s.retrieveEmail())
	mux.Handle("GET /", s.serveRoot())

	return mux
}

func (s *Server) serveSearch() http.HandlerFunc {
	type SearchResult struct {
		Result      column.QueryResults
		PathSegment string
	}

	return func(w http.ResponseWriter, req *http.Request) {
		if s.Index == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Cache-Control", "no-store, no-cache")

		qvals := req.URL.Query()
		query, ok := qvals["query"]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		start := time.Now()
		queryparts := strings.Split(query[0], " ")
		queryresults, err := s.Index.QueryIndex(queryparts)
		end := time.Now()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Compute total number of matches
		var totMatches int
		for i := range queryresults {
			totMatches += len(queryresults[i].WordMatches)
		}

		searchResults := make([]SearchResult, min(len(queryresults), 10))
		for i := range searchResults {
			searchResults[i].Result = queryresults[i]
			searchResults[i].PathSegment = base64.URLEncoding.EncodeToString(generateEmailURL(queryresults[i]))
		}

		w.WriteHeader(http.StatusOK)

		duration := end.Sub(start)
		data := struct {
			Query        string
			NumResults   int
			NumMatches   int
			ResponseTime string
			Results      []SearchResult
			NDocuments   int
		}{query[0], len(queryresults), totMatches, duration.String(), searchResults, s.Index.CorpusSize}
		if err := resultsPartialTmpl.Execute(w, data); err != nil {
			log.Printf("Error rendering template %s\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}

// We need a URL format that will contain everything we need
// File Index varuint32
// Number of matches uint16
// Match offsets [numMatch]varuint{}
func (s *Server) retrieveEmail() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		emailP := req.PathValue("email")
		if len(emailP) == 0 {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Nothing to see here"))
			return
		}

		urlData, err := base64.URLEncoding.DecodeString(emailP)
		if err != nil {
			log.Printf("Failed Base64 decode")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		highlights, err := decodeEmailURL(urlData)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		content, filename, ok := s.Index.CatalogContent(highlights.FilenameIndex)
		if !ok {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		hc := highlightContent(content, highlights.Highlights)
		data := struct {
			Contents template.HTML
			Filename string
		}{template.HTML(string(hc)), filename}
		if err := emailTmpl.Execute(w, data); err != nil {
			log.Printf("Error rendering template %s\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}

func (s *Server) serveRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		escQuery := req.URL.Query().Get("q")
		query, _ := url.QueryUnescape(escQuery)

		data := struct {
			Query string
		}{query}
		indexTmpl.Execute(w, data)
	}
}

// Encode the search result and all match locations into []byte
func generateEmailURL(result column.QueryResults) []byte {
	blob := make([]byte, 0, 256)
	blob = binary.AppendUvarint(blob, uint64(result.FilenameIndex))
	blob = binary.AppendUvarint(blob, uint64(len(result.WordMatches)))
	for _, match := range result.WordMatches {
		blob = binary.AppendUvarint(blob, uint64(match.Offset))
		blob = binary.AppendUvarint(blob, uint64(len(match.Word)))
	}

	return blob
}

const (
	openMarkTag  = "<mark>"
	closeMarkTag = "</mark>"
)

func highlightContent(content []byte, highlights []matchHighlight) []byte {
	if len(highlights) == 0 {
		return content
	}

	totalSize := len(content) + (len(openMarkTag)+len(closeMarkTag))*len(highlights)

	var buf bytes.Buffer
	buf.Grow(totalSize)

	lastPos := 0
	for _, h := range highlights {
		buf.Write(content[lastPos:h.Offset])
		buf.WriteString(openMarkTag)
		buf.Write(content[h.Offset : h.Offset+h.Length])
		buf.WriteString(closeMarkTag)

		lastPos = h.Offset + h.Length
	}
	buf.Write(content[lastPos:])

	return buf.Bytes()
}

func decodeEmailURL(data []byte) (emailMatch, error) {
	ret := emailMatch{}

	buf := bytes.NewBuffer(data)

	filenameIdx, err := readVarint(buf)
	if err != nil {
		return ret, fmt.Errorf("reading filename index - %w", err)
	}
	if filenameIdx < 0 {
		return ret, fmt.Errorf("invalid filename index - %w", err)
	}
	ret.FilenameIndex = filenameIdx

	numMatches, err := readVarint(buf)
	if err != nil {
		return ret, err
	}
	if numMatches < 0 {
		return ret, fmt.Errorf("invalid number of matches: %d", numMatches)
	}

	ret.Highlights = make([]matchHighlight, numMatches)
	for i := range numMatches {
		offset, err := readVarint(buf)
		if err != nil {
			return ret, err
		}
		ret.Highlights[i].Offset = offset

		length, err := readVarint(buf)
		if err != nil {
			return ret, err
		}
		ret.Highlights[i].Length = length
	}

	return ret, nil
}

func readVarint(buf *bytes.Buffer) (int, error) {
	i64, err := binary.ReadUvarint(buf)
	return int(i64), err
}
