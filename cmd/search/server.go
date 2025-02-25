//go:generate tailwindcss --minify --input build/in.css --output static/tailwind.css

package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chriskillpack/emailsearch"
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
	hs     *http.Server
	logger *log.Logger

	Index *emailsearch.Index
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

func NewServer(idx *emailsearch.Index, port string) *Server {
	srv := &Server{Index: idx, logger: log.Default()}
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
	mux.Handle("GET /search", s.logRequest(s.serveSearch()))
	mux.Handle("GET /prefix", s.queryPrefix())
	mux.Handle("GET /email/{email}", s.logRequest(s.retrieveEmail()))
	mux.Handle("GET /", s.logRequest(s.serveRoot()))

	return mux
}

func (s *Server) serveSearch() http.HandlerFunc {
	type SearchResult struct {
		Result      emailsearch.QueryResults
		PathSegment string
	}

	return func(w http.ResponseWriter, req *http.Request) {
		if s.Index == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Cache-Control", "no-store, no-cache")

		qvals := req.URL.Query()
		query, ok := qvals["q"]
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		start := time.Now()
		queryparts := strings.Split(query[0], " ")
		queryresults, err := s.Index.QueryIndex(queryparts)
		duration := time.Since(start)
		s.logger.Printf("serveSearch query=%v", queryparts)
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

		data := struct {
			Query        string
			NumResults   int
			NumMatches   int
			ResponseTime string
			Results      []SearchResult
			NDocuments   int
		}{query[0], len(queryresults), totMatches, duration.String(), searchResults, s.Index.CorpusSize}
		if err := resultsPartialTmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error rendering template %s\n", err)
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
			s.logger.Printf("Failed Base64 decode")
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
		s.logger.Printf("retrieveEmail %q", filename)

		hc := highlightContent(content, highlights.Highlights)
		data := struct {
			Contents template.HTML
			Filename string
		}{template.HTML(string(hc)), filename}
		if err := emailTmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error rendering template %s\n", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}
}

func (s *Server) queryPrefix() http.HandlerFunc {
	type queryResults struct {
		Matches []string `json:"matches"`
	}

	return func(w http.ResponseWriter, req *http.Request) {
		var res queryResults

		qvals := req.URL.Query()
		query, ok := qvals["q"]

		enc := json.NewEncoder(w)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if ok && len(query) >= 1 && len(query[0]) >= 3 {
			res.Matches = s.Index.Prefix(query[0], 15)
		}
		if err := enc.Encode(&res); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
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

// Request logging middleware
func (s *Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()

		lrw := newLoggingResponseWriter(w)
		next.ServeHTTP(lrw, req)

		duration := time.Since(start)

		s.logger.Printf("method=%s path=%s status=%d duration=%s",
			req.Method,
			req.URL.EscapedPath(),
			lrw.statusCode,
			duration)
	})
}

// loggingResponseWriter wraps an http.ResponseWriter to capture the set
// statusCode. This is necessary because the status code is unexported and
// there is no read method.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, 0}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Encode the search result and all match locations into []byte
func generateEmailURL(result emailsearch.QueryResults) []byte {
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
