package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/chriskillpack/column"
)

var (
	flagIndexDir = flag.String("indexdir", "out/", "Directory that holds the search index")
	flagQuery    = flag.String("query", "", "query index, print results, quit")
)

func main() {
	flag.Parse()

	idx, err := column.LoadIndexFromDisk(*flagIndexDir)
	if err != nil {
		log.Fatal(err)
	}

	if *flagQuery != "" {
		idx.QueryIndex([]string{*flagQuery})
		idx.Finish()
		os.Exit(0)
	}

	// Start webserver
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	srv := NewServer(idx, "8080")

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %s", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error at server shutdown: %s", err)
		}
	}()
	wg.Wait()
}
