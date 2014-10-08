# growler

A simple, multi-threaded webcrawler and mirror-er written in go.

## Building

After retrieving dependencies with either
[goat](https://github.com/mediocregopher/goat) or:

```
go get ./...
```

You can build a binary with:

```
go build
```

## Usage

```
./growler -src http://example.com/directory -dst my-directory
```

Will copy all files and directories inside of `http://example.com/directory`
into `my-directory`. For example `http://example.com/directory/images/image.jpg`
would be stored as `my-directory/images/image.jpg`.

Growler will not download a file if it sees that the file is the same size on
the server and hasn't been modified since the file was downloaded last. You can
turn this off with `-force-download`

You can set the `-num-downloaders` option to increase or decrease the number of
go-routines downloading at any given moment.

Files can be excluded by their name by using `-exclude-file`. This option can be
set multiple times.
