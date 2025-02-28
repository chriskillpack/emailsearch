# Email Search

[Check it out](https://emailsearch.fly.dev/).

This project started as a time limited take home project for an engineering role I applied for. I passed the take home but the project never left my head and I continued to work on it: and thus this.

The only tested email corpus supported is the [Enron email archive](https://www.cs.cmu.edu/~enron/enron_mail_20150507.tar.gz).

# Indexing emails

The search engine requires a search index, the production of which is the responsibility of `cmd/indexer`. This program walks a corpus of mailbox messages (RFC 5322 and 6532), extracting words from email bodies (headers are ignored for now) and outputting a directory of data files which comprise the search index.

The main data file is called the index. This data structure maps every word to every file it occurs in and it's locations within those files. The location is an offset from the beginning of the message body. As emails typically contain many words, and those words are likely to appear in multiple emails, this can make the index pretty large. As a space saving technique the search index stores each filename only once, in a filename stringset, and instead stores the (confusingly named) file index in the index.

## Running the indexer

```
$ go run ./cmd/indexer --emails /path/to/enron_emails --out email_index
⠹ Enumerating files        (517401/-) [9s]
Injesting files 1/2      100% |████████████████████████████████████████|
Injesting files 2/2      100% |████████████████████████████████████████|
Serializing filenames    100% |████████████████████████████████████████|
Serializing words        100% |████████████████████████████████████████|
Serializing index        100% |████████████████████████████████████████|
Serializing word offsets 100% |████████████████████████████████████████|
Serializing catalog      100% |████████████████████████████████████████|
Serializing prefix tree  100% |████████████████████████████████████████|
Success. Took 3m29.460381125s to run.
```

There are some command line flags to control the indexer

```
$ go run ./cmd/indexer help
  -emails string
        directory of emails
  -maxfiles int
        maximum number of files to inject, -1 to disable limit (default -1)
  -out string
        directory to place generated files (default "./out")
  -threads int
        threads to use (default 10)
  -v    Verbose output
  -verbose
        Verbose output
```

### Index datastructure example

TODO: Move into a technical document.

Injesting the email file `example.email` with the body `presentation sent`. The indexer adds `example.email` to the filename set, and receives the index 0 in return. It will then create two entries in index, one for each word in the email body, `"presentation"` and `"sent"`. The index will look similar to this:

```
"presentation" -> [{filename_index: 0, offsets: [0]}]
"sent" -> [{filename_index: 0, offsets: [13]}]
```

That reads as *the word "presentation" can be found at message offset 0 in filename_index 0 (`example.email`). The word "sent" can be found at message offset 13 in filename_index 0*.

Now the indexer injests a second email `scandal.email` with the body `fraud presentation here`. Again the indexer inserts the filename and this time receives the index 1. It adds two new entries to the index for `"fraud"` and `"here"` and extends the existing entry for `"presentation"`. This is what the index will look like now:

```
"presentation" -> [{filename_index: 0, offsets: [0]}, {filename_index: 1, offsets: [6]}]
"fraud" -> [{filename_index: 1, offsets: [0]}]
"sent" -> [{filename_index: 0, offsets: [13]}]
"here" -> [{filename_index: 1, offsets: [19]}]
```

We will focus only on the extended entry for `"presentation"`. Now this entry reads *"presentation" is found in two files: file index 0 (`example.email`) at offset 0, and also in file index 1 (`scandal.email`) at offset 6*.

Strictly speaking the indexer doesn't need to track filenames beyond indexing, but the set is saved out to disk for the search engine to use for presentation purposes.

Similar to filenames the indexer does not store words literally in the index, instead representing each word encountered with a unique word index. If we assume the following word index mapping `word_index 0 = "presentation", word_index 1 = "sent", word_index 2 = "fraud" and word_index 3 = "here"` then the index is actually stored like this

```
word_index 0 -> [{filename_index: 0, offsets: [0]}, {filename_index: 1, offsets: [6]}]
word_index 2 -> [{filename_index: 1, offsets: [0]}]
word_index 1 -> [{filename_index: 0, offsets: [13]}]
word_index 3 -> [{filename_index: 1, offsets: [19]}]
```

Notice that the index is not sorted by word_index order, this is not something that the search engine requires. This avoids shared coordination of index allocation which makes parallelizing index generation easier.

Unlike filenames, which can be discarded, the word -> word index mapping is required by the search engine. This is how it will map words in the query into indices in the map.

TODO - rewrite this section. ~The generated corpus is quite large (on the order of 1G) and loading this into device memory may not be possible. To allow for efficient searches we create another file `word.offsets` which stores two int32 entries for each word in the corpus. The first word is the word index in the words.sid file and the second if the byte offset into the corpus file. This way only words.sid, filenames.sid and word.offsets have to be loaded into memory (a total of 102Mb).

The corpus file can be memory mapped and accessed to read out the match information.~

# Search interface

Start the web server

```
$ go run ./cmd/search --indexdir=email_index
Loaded filename strings table: 517401 entries (36.0 MB)
Loaded words strings table: 595111 entries (10.1 MB)
Loaded word offsets table: 595111 entries (22.7 MB)
Loaded prefix tree: 3182208 nodes (595 MB)
2025/02/27 13:55:58 Ready, took 618.383834ms to load index

```

The server listens on `0.0.0.0:8080` though the port can be changed via the `PORT` environment variable.

## Search algorithm

The indexer takes the input email direction and generates the following in the output directory:
```
dir/
  corpus.index - The generated search index
  corpus.cat - Compressed catalog of the indexed corpus content
  filenames.sid - The string table of email filenames
  words.sid - The string table of words in the corpus
  word.offsets - The offsets of each word into corpus.index
  query.trie - The words in the index stored in a prefix tree
```

The search algorithm would take each word and look it up in `words.sid` to retrieve `word_index`. `word.offsets` would be indexed by word_index to retrieve the match offset into the corpus file. Seek to that location in the file (or memory address if using memory mapping) and then read in the match information. This will give you a list of files (technically filename indices) and offsets within the email body where that word occurs. Use the filename indices with `filenames.sid` to retrieve the names of the files. With this information each email can be loaded and shown to the user.

# Deployment

The website is hosted on [Fly](https://fly.io). To deploy you will need `flyctl` installed, [instructions](https://fly.io/docs/flyctl/install/).

**Important** The email index is read-only and the easiest way to ship it to Fly is in the docker image. We use a layered docker image with a `data` layer to achieve this. The data layer copies the email index from the directory `email_index`. Any changes to this directory and you will have a large [~800Mbs] upload ahead of you. As a result I try and avoid changes to this directory, or batch up multiple changes into a single deploy.

To deploy:

```
$ fly deploy
```

# TODO

* Add support for [Okapi BM25](https://en.wikipedia.org/wiki/Okapi_BM25) result ranking. This is a popular ranking mechanism that should be within scope of this system.
* Explore replacing the Trie data structure with a Radix tree. The current Trie has a lot of nodes and uses a lot of memory.
* Go 1.23 introduced string interning. Use that to reduces index generation working memory size. Currently max RSS usage on full maildir is 6370Mb.

# Performance improvements

* Parallelization of email injestion greatly improved performance. This also includes the binary string set file format as well:
go run ./cmd/indexer --out parallel_out --threads 50  143.34s user 49.86s system 278% cpu 1:09.31 total

For comparison, previous single threaded performance (with text stringset file format)
go run ./cmd/indexer --out out  168.98s user 311.53s system 82% cpu 9:39.31 total

With parallelization the output is no longer deterministic because we have no guarantees over insertion order into the main index. TODO - order insertion.
