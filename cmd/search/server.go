package main

import (
	"context"
	"embed"
	"net"
	"net/http"
	"strings"
	"text/template"

	"github.com/chriskillpack/column"
)

var (
	//go:embed tmpl/*.html
	tmplFS embed.FS

	//go:embed static
	staticFS embed.FS

	indexTmpl          *template.Template
	resultsPartialTmpl *template.Template
)

type Server struct {
	hs *http.Server

	Index *column.Index
}

func init() {
	indexTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/index.html"))
	resultsPartialTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/_results.html"))
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
	mux.Handle("GET /", s.serveRoot())

	return mux
}

func (s *Server) serveSearch() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if s.Index == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		qvals := req.URL.Query()
		if query, ok := qvals["query"]; ok {
			queryparts := strings.Split(query[0], " ")
			s.Index.FindWord(queryparts[0])
		}

		w.Header().Set("Cache-Control", "no-store, no-cache")
		w.WriteHeader(http.StatusOK)
		resultsPartialTmpl.Execute(w, nil)
	}
}

func (s *Server) serveRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		indexTmpl.Execute(w, nil)
	}
}
