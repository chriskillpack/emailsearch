# Email search index

I was not able to complete the problem in time. I only got as far as generating the search index
and persisting it to disk.

The basic approach was to build a search index that mapped a word to every file (email) and it's location in the email body. I made the decision to discard the headers to get something working but with more time I would have included them.

# Code quality

I have left some commented out code in the file. These are there to show some of the dead ends I went down working on this problem.

# Pre-processor

cmd/column walks the input set of files and parses each in turn as email. It discards the headers and then tokenizes the body of each email. For each word (token) it finds, it inserts into a map along with the file and it's byte offset in the email body.

As a file is very likely to appear multiple times in the index, I maintained a table of the unique set of filenames and the corpus stores an index into that set.

### An example

The file `"example.email"` has the body `"presentation sent"`. This will create two entries in the index, `"presentation"` and `"sent"`. The entries will look like this

```
"presentation" -> [{filename_index: 0, offsets: [0]}]
"sent" -> [{filename_index: 0, offsets: [13]}]
```

If a second email `scandal.email` with the body `"fraud presentation here"` is injested into the index new entries will be added and the resulting structures will look like this

```
"presentation" -> [{filename_index: 0, offsets: [0]}, {filename_index: 1, offsets: [6]}]
"fraud" -> [{filename_index: 1, offsets: [0]}]
"sent" -> [{filename_index: 0, offsets: [13]}]
"here" -> [{filename_index: 1, offsets: [19]}]
```

The unique set of filenames and words is written out to disk as `filenames.sid` and `words.sid`. These are a newline based text format using the simple schema

```
{INDEX}: {STRING}
...
{INDEX}: {STRING}
```

This schema was chosen for readability and ease of serializing and parsing.

The generated corpus is quite large (on the order of 1G) and loading this into device memory may not be possible. To allow for efficient searches we create another file "word.offsets" which stores two int32 entries for each word in the corpus. The first word is the word index in the words.sid file and the second if the byte offset into the corpus file. This way only words.sid, filenames.sid and word.offsets have to be loaded into memory (a total of 102Mb)

The corpus file can be memory mapped and accessed to read out the match information.

# Searching

On device disk we would store all the original emails plus the preprocessed files:
```
  corpus.index - The generated search index
  filenames.sid - The string table of email filenames
  words.sid - The string table of words in the corpus.
  word.offsets - The offsets of each word into corpus.index
```

The search algorithm would take each word and look it up in `words.sid` to retrieve `word_index`. `word.offsets` would be indexed by word_index to retrieve the match offset into the corpus file. Seek to that location in the file (or memory address if using memory mapping) and then read in the match information. This will give you a list of files (technically filename indices) and offsets within the email body where that word occurs. Use the filename indices with `filenames.sid` to retrieve the names of the files. With this information each email can be loaded and shown to the user.

# Extensions

* I would build a prefix tree (trie) for all the words in the corpus. This would allow multiple searches to be done, e.g. a search of the trie for "app" would also return "apple", "application", "appliance", "apply", etc. These multiple words could then be used as search terms.

* Refine the email tokenizer. It has problems such as dealing with quotation, and I did not spend any time fixing the issues.

* Add email headers to the corpus as part of the search time.

# TODO

* Use varint encoding for string lengths in serialized string sets
* Go 1.23 introduced string interning. Use that to reduces index generation working memory size (currently > 18Gbs).

# Performance improvements

* Parallelization of email injestion greatly improved performance. This also includes the binary string set file format as well:
go run ./cmd/column --out parallel_out --threads 50  143.34s user 49.86s system 278% cpu 1:09.31 total

For comparison, previous single threaded performance (with text stringset file format)
go run ./cmd/column --out out  168.98s user 311.53s system 82% cpu 9:39.31 total

With parallelization the output is no longer deterministic because we have no guarantees over insertion order into the main index. TODO - order insertion.