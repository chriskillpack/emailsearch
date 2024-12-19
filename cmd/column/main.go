package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/chriskillpack/column"
)

var flagEmail = flag.String("emails", "/Users/chris/enron_emails/maildir", "directory of emails")
var flagOutDir = flag.String("out", "./out", "output directory")

func main() {
	flag.Parse()

	files, maxSize, err := column.Walk(*flagEmail)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Found %d files\n", len(files))

	work := make([]byte, maxSize+10)
	corpus := column.NewCorpus()

	// Compute an index for each file
	for i, f := range files {
		if i == 0 || ((i % 10000) == 0) || i == len(files)-1 {
			fmt.Printf("%d -> %s\n", i, f)
		}

		fd, err := os.Open(f)
		if err != nil {
			fmt.Printf("Skipping file %s - %s", f, err)
			continue
		}

		n, err := fd.Read(work)
		fd.Close()
		if err != nil {
			fmt.Printf("Error reading from %s - %s. Skipping.", f, err)
			continue
		}

		localIndex, err := column.ComputeIndex(f, work[0:n])
		if err != nil {
			fmt.Printf("Error generating index - %s. Skipping.", err)
		}

		// Merge the local index into the main index
		corpus.MergeInLocalIndex(localIndex, f)
	}

	fmt.Println("Serializing corpus")
	if err := corpus.Serialize(*flagOutDir); err != nil {
		log.Fatal(err)
	}
}
