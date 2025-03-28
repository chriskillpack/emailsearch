package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/chriskillpack/emailsearch"
)

var (
	flagIndexDir = flag.String("indexdir", "out/", "Directory that holds the search index")
	flagQuery    = flag.String("query", "", "query index, print results, quit")
)

func main() {
	flag.Parse()

	start := time.Now()
	idx, err := emailsearch.LoadIndexFromDisk(*flagIndexDir, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	duration := time.Since(start)
	log.Printf("Ready, took %s to load index", duration.String())

	if *flagQuery != "" {
		results, err := idx.QueryIndex([]string{*flagQuery})
		if err != nil {
			log.Fatal(err)
		}
		// TODO: prettier printing of results
		fmt.Printf("%+v\n", results)

		idx.Finish()
		os.Exit(0)
	}

	// Start webserver
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := NewServer(idx, port)

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
