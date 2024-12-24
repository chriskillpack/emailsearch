package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"

	"github.com/chriskillpack/column"
)

var (
	flagInputPath = flag.String("emails", "/Users/chris/enron_emails/maildir", "directory of emails")
	flagOutDir    = flag.String("out", "./out", "directory to place generated files")
	flagThreads   = flag.Int("threads", 10, "threads to use")
	flagMaxFiles  = flag.Uint("maxfiles", 0, "maximum number of files to inject, 0 to disable limit")

	verboseOutput bool
)

func verbose(format string, a ...any) {
	if verboseOutput {
		fmt.Printf(format, a...)
	}
}

// Walk a path of the filesystem and return the set of files in that path
// The names of the files are relative to the walk path, so Walk("/home/chris")
// will return ["foo/cat.txt"] for /home/chris/foo/cat.txt
func walk(path string) ([]string, int64, error) {
	files := []string{}

	var maxSize int64
	err := filepath.WalkDir(path, func(wpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		finfo, err := d.Info()
		if err != nil {
			return err
		}
		maxSize = max(maxSize, finfo.Size())

		relpath, err := filepath.Rel(path, wpath)
		if err != nil {
			return err
		}

		files = append(files, relpath)
		return nil // Continue walking
	})

	return files, maxSize, err
}

func main() {
	flag.BoolVar(&verboseOutput, "v", false, "Verbose output")
	flag.BoolVar(&verboseOutput, "verbose", false, "Verbose output")
	flag.Parse()

	if *flagThreads <= 0 || *flagThreads > 100 {
		log.Fatal("Threads needs to be between 1 and 100")
	}
	verbose("Running with %d threads\n", *flagThreads)

	index := column.IndexBuilder{
		Verbose:   verboseOutput,
		NThreads:  *flagThreads,
		InputPath: *flagInputPath,
	}
	index.Init()

	files, maxSize, err := walk(*flagInputPath)
	if err != nil {
		log.Fatal(err)
	}
	verbose("Found %d files\n", len(files))

	maxFiles := uint(len(files))
	if *flagMaxFiles > 0 {
		maxFiles = min(*flagMaxFiles, maxFiles)
		verbose("Only injesting first %d files\n", maxFiles)
	}

	fmt.Println("Injesting files")
	index.InjestFiles(files[0:maxFiles], maxSize)

	fmt.Println("Serializing corpus")
	if err := index.Serialize(*flagOutDir); err != nil {
		log.Fatal(err)
	}
}
