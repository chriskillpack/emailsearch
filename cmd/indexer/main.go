package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/chriskillpack/column"
	"github.com/schollz/progressbar/v3"
)

var (
	flagInputPath = flag.String("emails", "/Users/chris/enron_emails/maildir", "directory of emails")
	flagOutDir    = flag.String("out", "./out", "directory to place generated files")
	flagThreads   = flag.Int("threads", 10, "threads to use")
	flagMaxFiles  = flag.Int("maxfiles", -1, "maximum number of files to inject, -1 to disable limit")

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
func walk(path string, n int) ([]string, int64, error) {
	files := []string{}

	bar := progressbar.NewOptions(
		n,
		progressbar.OptionSetDescription("Enumerating files"),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionThrottle(50*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
	)

	var maxSize int64
	err := filepath.WalkDir(path, func(wpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		bar.Add(1)
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

		// If a limit was set and the limit has been exceeded stop walking
		if n >= 0 && len(files) >= n {
			return fs.SkipAll
		}

		return nil // Continue walking
	})

	bar.Finish()

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

	progressChan := make(chan column.InjestUpdate)
	index.ProgressCh = progressChan

	files, maxSize, err := walk(*flagInputPath, *flagMaxFiles)
	if err != nil {
		log.Fatal(err)
	}

	bar := progressbar.NewOptions(
		len(files),
		progressbar.OptionSetDescription("Injesting files 1/2"),
		progressbar.OptionThrottle(50*time.Millisecond),
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
	)
	go func() {
		fn := sync.OnceFunc(func() {
			bar.Reset()
			bar.Describe("Injesting files 2/2")
		})

		for p := range progressChan {
			bar.Add(1)

			if p.Phase == 2 {
				fn()
			}
		}

		bar.Finish()
	}()

	index.InjestFiles(files, maxSize)

	if err := index.Serialize(*flagOutDir); err != nil {
		log.Fatal(err)
	}
}
