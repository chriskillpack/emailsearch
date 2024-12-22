package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/chriskillpack/column"
)

var (
	flagInputPath = flag.String("emails", "/Users/chris/enron_emails/maildir", "directory of emails")
	flagOutDir    = flag.String("out", "./out", "directory to place generated files")
	flagThreads   = flag.Int("threads", 10, "threads to use")
	flagMaxFiles  = flag.Uint("maxfiles", 0, "maximum number of files to inject, 0 to disable limit")

	verboseOutput bool
)

// Holds the output of one of the injestion workers
type InjestedFile struct {
	Filename string
	Index    column.LocalIndex
	Err      error
}

func verbose(format string, a ...any) {
	if verboseOutput {
		fmt.Printf(format, a...)
	}
}

func main() {
	flag.BoolVar(&verboseOutput, "v", false, "Verbose output")
	flag.BoolVar(&verboseOutput, "verbose", false, "Verbose output")
	flag.Parse()

	if *flagThreads <= 0 || *flagThreads > 100 {
		log.Fatal("Threads needs to be between 1 and 100")
	}
	verbose("Running with %d threads\n", *flagThreads)

	files, maxSize, err := column.Walk(*flagInputPath)
	if err != nil {
		log.Fatal(err)
	}
	verbose("Found %d files\n", len(files))

	maxFiles := uint(len(files))
	if *flagMaxFiles > 0 {
		maxFiles = min(*flagMaxFiles, maxFiles)
		verbose("Only injesting first %d files\n", maxFiles)
	}

	nThreads := *flagThreads

	inCh := make(chan string, *flagThreads)
	outCh := make(chan InjestedFile)

	var wg sync.WaitGroup
	wg.Add(nThreads)

	// Spin up the worker "threads" (goroutines)
	for _ = range nThreads {
		// Each worker gets it's own scratch buffer to load file data into. This
		// is an attempt to reduce churn in the GC. The scratch buffer is sized
		// to the maximum file size to avoid reallocating the buffer.
		scratch := make([]byte, maxSize)

		go func(scratch []byte) {
			defer wg.Done()

			// Each worker pulls a filename of an email from the input channel,
			// builds a LocalIndex of the email body and then sends result
			// through the output channel.
			for work := range inCh {
				filep := filepath.Join(*flagInputPath, work)

				outData := InjestedFile{Filename: work}
				var n int
				f, err := os.Open(filep)
				if err != nil {
					outData.Err = err
					goto respond
				}

				n, err = f.Read(scratch)
				f.Close()
				if err != nil {
					outData.Err = err
					goto respond
				}

				outData.Index, outData.Err = column.ComputeIndex(work, scratch[0:n])
			respond:
				outCh <- outData
			}
		}(scratch)
	}

	// Spin up a goroutine to insert the filenames
	go func() {
		for i, file := range files[0:maxFiles] {
			if i == 0 || ((i % 10000) == 0) || i == len(files[0:maxFiles])-1 {
				verbose("%d -> %s\n", i, file)
			}

			inCh <- file
		}
		close(inCh)
	}()

	// Spin up a goroutine to wait for the worker and insertion goroutine to
	// be complete and then close the output channel to indicate that there
	// are no more results.
	go func() {
		wg.Wait()
		close(outCh)
	}()

	// Retrieve the injested results and sort for a deterministic building of
	// the main index.
	injested := make([]InjestedFile, 0, maxFiles)
	for result := range outCh {
		injested = append(injested, result)
	}
	slices.SortFunc(injested, func(a, b InjestedFile) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	// This is all single threaded for now
	corpus := column.NewCorpus()
	for _, result := range injested {
		if result.Err == nil {
			// Merge the local index into the main index
			corpus.MergeInLocalIndex(result.Index, result.Filename)
		} else {
			fmt.Printf("Encountered error processing %s\n", result.Filename)
		}
	}

	fmt.Println("Serializing corpus")
	if err := corpus.Serialize(*flagOutDir); err != nil {
		log.Fatal(err)
	}
}
