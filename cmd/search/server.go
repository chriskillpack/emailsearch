package main

import (
	"context"
	"embed"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
)

type Server struct {
	hs *http.Server

	Index      *column.Index
	EmailsRoot string
}

func init() {
	funcMap := template.FuncMap{
		"pathescape": func(value string) string {
			return url.PathEscape(value)
		},
	}
	indexTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/index.html"))
	resultsPartialTmpl = template.Must(template.New("_results.html").Funcs(funcMap).ParseFS(tmplFS, "tmpl/_results.html"))
}

func NewServer(idx *column.Index, emailDir string, port string) *Server {
	srv := &Server{Index: idx, EmailsRoot: emailDir}
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
	return func(w http.ResponseWriter, req *http.Request) {
		if s.Index == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		qvals := req.URL.Query()
		query, ok := qvals["query"]
		if !ok {
			w.Header().Set("Cache-Control", "no-store, no-cache")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		start := time.Now()
		queryparts := strings.Split(query[0], " ")
		queryresults, err := s.Index.QueryIndex(queryparts)
		end := time.Now()
		if err != nil {
			w.Header().Set("Cache-Control", "no-store, no-cache")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Cache-Control", "no-store, no-cache")
		w.WriteHeader(http.StatusOK)

		duration := end.Sub(start)
		data := struct {
			Query        string
			NumResults   int
			ResponseTime string
			Results      []column.QueryResults
		}{query[0], len(queryresults), duration.String(), queryresults[:min(len(queryresults), 10)]}
		if err := resultsPartialTmpl.Execute(w, data); err != nil {
			log.Printf("Error! %s\n", err)
		}
	}
}

func (s *Server) retrieveEmail() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		escEmail := req.PathValue("email")
		if len(escEmail) == 0 || len(s.EmailsRoot) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		email, err := url.PathUnescape(escEmail)
		if err != nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		emailpath := filepath.Join(s.EmailsRoot, email)
		f, err := os.Open(emailpath)
		if err != nil {
			log.Printf("Error opening email %q - %s\n", emailpath, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		emailbody, err := io.ReadAll(f)
		if err != nil {
			log.Printf("Error reading email %q - %s\n", emailpath, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(emailbody)

		defer f.Close()
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
