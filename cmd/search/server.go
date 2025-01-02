package main

import (
	"context"
	"embed"
	"net"
	"net/http"
	"text/template"
)

var (
	//go:embed tmpl/*.html
	tmplFS embed.FS

	indexTmpl *template.Template
)

type Server struct {
	hs *http.Server
}

func init() {
	indexTmpl = template.Must(template.ParseFS(tmplFS, "tmpl/index.html"))
}

func NewServer(port string) *Server {
	srv := &Server{}
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
	mux.Handle("GET /", s.serveRoot())

	return mux
}

func (s *Server) serveRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		indexTmpl.Execute(w, nil)
	}
}
