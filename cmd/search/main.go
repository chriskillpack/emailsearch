package main

import (
	"flag"
	"log"

	"github.com/chriskillpack/column"
)

var flagIndexDir = flag.String("indexdir", "out/", "Directory that holds the search index")

func main() {
	flag.Parse()

	idx, err := column.LoadIndexFromDisk(*flagIndexDir)
	if err != nil {
		log.Fatal(err)
	}

	query := "forecast"
	if len(flag.Args()) > 0 {
		query = flag.Arg(0)
	}
	idx.FindWord(query)

	idx.Finish()
}
