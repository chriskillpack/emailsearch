# Email Search

This project started as a time limited take home project for an engineering role I applied for. I passed the take home but the project never left my head and I continued to work on it. And thus this.

The only tested email corpus supported is the [Enron email archive](https://www.cs.cmu.edu/~enron/enron_mail_20150507.tar.gz).

# Indexing emails

The search engine requires a search index, which is the job of `cmd/indexer`. This program walks a corpus of mailbox messages (RFC 5322 and 6532), injesting the bodies of emails and producing a series of data files which is writes to an output directory.

The main data file is called the index. This data structure maps every word seen to every file it occurs in and it's location within those files. The location is an offset from the beginning of the message body. As emails typically contain many words, and those words are likely to appear in multiple emails, this can make the index pretty large. As a space saving technique the search index stores each filename only once, in a filename stringset, and instead stores the (confusingly named) file index in the index.

### An example

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

# Searching

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

# TODO

* Go 1.23 introduced string interning. Use that to reduces index generation working memory size. Currently max RSS usage on full maildir is 6370Mb.

# Performance improvements

* Parallelization of email injestion greatly improved performance. This also includes the binary string set file format as well:
go run ./cmd/column --out parallel_out --threads 50  143.34s user 49.86s system 278% cpu 1:09.31 total

For comparison, previous single threaded performance (with text stringset file format)
go run ./cmd/column --out out  168.98s user 311.53s system 82% cpu 9:39.31 total

With parallelization the output is no longer deterministic because we have no guarantees over insertion order into the main index. TODO - order insertion.