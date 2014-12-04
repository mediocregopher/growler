package main

import (
	"code.google.com/p/go.net/html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mediocregopher/growler/config"
	"github.com/mediocregopher/growler/stats"
	"github.com/mediocregopher/growler/tracker"
)

var parseSearch = map[string]string{
	"a":   "href",
	"img": "src",
}

var rootURL *url.URL
var rootPath string
var wg sync.WaitGroup

func init() {
	log.Printf("Setting GOMAXPRROCS to %d", config.NumProcs)
	runtime.GOMAXPROCS(config.NumProcs)

	var err error
	rootReq, err := http.NewRequest("GET", config.Src, nil)
	if err != nil {
		log.Fatalf("parsing rootReq: %s", err)
	}
	rootURL = rootReq.URL
	rootPath = path.Clean(rootURL.Path)
}

func extractLinks(body io.Reader) ([]string, error) {
	t := html.NewTokenizer(body)
	ret := make([]string, 0, 16)

tokenLoop:
	for {
		tt := t.Next()
		switch tt {

		case html.ErrorToken:
			if err := t.Err(); err == io.EOF {
				break tokenLoop
			} else {
				return nil, err
			}

		case html.StartTagToken:
			tagNameB, hasAttr := t.TagName()
			if !hasAttr {
				continue tokenLoop
			}
			searchAttr, ok := parseSearch[string(tagNameB)]
			if !ok {
				continue tokenLoop
			}
			for {
				attr, val, moreAttr := t.TagAttr()
				if string(attr) == searchAttr {
					ret = append(ret, string(val))
				} else if !moreAttr {
					continue tokenLoop
				}
			}
		}
	}

	return ret, nil
}

func lastChar(s string) byte {
	return s[len(s)-1]
}

func getFilePath(u *url.URL) (string, error) {
	relPath, err := filepath.Rel(rootPath, u.Path)
	if err != nil {
		return "", err
	}

	filePath := path.Join(config.Dst, relPath)
	filePathLast := lastChar(filePath)
	if lastChar(u.Path) == '/' && filePathLast != '/' {
		filePath += "/"
		filePathLast = '/'
	}

	if filePathLast == '/' {
		filePath += "index.html"
	}
	return filePath, nil
}

type downloader struct {
	i int
}

func (d *downloader) Println(args ...interface{}) {
	fargs := make([]interface{}, 0, len(args) + 2)
	fargs = append(fargs, d.i, " - ")
	fargs = append(fargs, args...)
	log.Print(fargs...)
}

func (d *downloader) Printf(format string, args ...interface{}) {
	fargs := make([]interface{}, 0, len(args) + 1)
	fargs = append(fargs, d.i)
	fargs = append(fargs, args...)
	log.Printf("%d - " + format, fargs...)
}

func (d *downloader) Fatal(args ...interface{}) {
	fargs := make([]interface{}, 0, len(args) + 2)
	fargs = append(fargs, d.i, "-")
	fargs = append(fargs, args...)
	log.Fatal(fargs...)
}

func (d *downloader) Fatalf(format string, args ...interface{}) {
	fargs := make([]interface{}, 0, len(args) + 1)
	fargs = append(fargs, d.i)
	fargs = append(fargs, args...)
	log.Fatalf("%d - " + format, fargs...)
}

// Does a GET to retrieve the file from disk and returns the io.ReadCloser for
// the body. Also returns whether or not the body should be written to disk
// (always true).
func (d *downloader) getPage(
	client *http.Client, page *url.URL,
) (
	*http.Response, io.ReadCloser, string, bool, error,
) {
	d.Printf("GET %s", page)
	r, err := client.Get(page.String())
	defer stats.IncrGet(d.i)
	if err != nil {
		return nil, nil, "", true, err
	}
	filePath, err := getFilePath(r.Request.URL)
	if err != nil {
		return nil, nil, "", true, err
	}
	return r, r.Body, filePath, true, nil
}

// Does the processing necessary to know if a file should be actually downloaded
// or not. May include a HEAD call to the server if the file already exists in
// some form on disk. Returns:
// * The http.Response from the last HEAD or GET performed
// * an io.ReadCloser which must be closed once read from
// * the filePath to use
// * whether the contents of the io.ReadCloser should be written to the file for
//   the page.
func (d *downloader) maybeGetPage(
	client *http.Client, page *url.URL,
) (
	*http.Response, io.ReadCloser, string, bool, error,
) {

	var filestat os.FileInfo
	var filePath string
	var err error

	filePath, err = getFilePath(page)
	if err != nil {
		return nil, nil, "", false, err
	}

	if config.ForceDownload {
		return d.getPage(client, page)
	}

	filestat, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return d.getPage(client, page)
		} else {
			return nil, nil, "", false, err
		}
	}

	d.Printf("HEAD %s", page)
	defer stats.IncrHead(d.i)
	r, err := client.Head(page.String())
	if err != nil {
		return nil, nil, "", false, err
	}
	drainAndClose(r.Body) // HEAD response has no body, but we Close anyway

	// We recompute the filePath and filestat in case the HEAD got redirected
	// and we're actually looking at a different file now
	filePath, err = getFilePath(page)
	if err != nil {
		return nil, nil, "", false, err
	}

	filestat, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return d.getPage(client, page)
		} else {
			return nil, nil, "", false, err
		}
	}

	lmStr := r.Header.Get("Last-Modified")
	// We don't want to make any assumptions if we don't know the last modified
	// time
	if lmStr == "" {
		return d.getPage(client, page)
	}

	lm, err := time.Parse(time.RFC1123, lmStr)
	if err != nil {
		d.Printf("error parsing last modified (%s); %s", lmStr, err)
		return d.getPage(client, page)
	}

	if lm.After(filestat.ModTime()) {
		return d.getPage(client, page)
	}

	if r.ContentLength != filestat.Size() {
		return d.getPage(client, page)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, "", false, err
	}
	return r, f, filePath, false, nil
}

func drainAndClose(b io.ReadCloser) {
	io.Copy(ioutil.Discard, b)
	b.Close()
}

func (d *downloader) processPage(
	client *http.Client, page *url.URL,
) (
	retURLs []*url.URL,
) {
	retURLs = make([]*url.URL, 0, 0)
	page.Path = path.Clean(page.Path)

	if !tracker.CanFetch(page.Path) {
		return
	}

	d.Printf("processesing %s", page)
	defer stats.IncrTotal(d.i)

	r, body, filePath, store, err := d.maybeGetPage(client, page)
	if err != nil {
		d.Printf("getPage: %s err: %s", page, err)
		return
	}
	defer drainAndClose(body)

	// page might have changed inside of maybeGetPage due to redirects
	page = r.Request.URL

	var bodyReader io.Reader
	if store {
		if _, ok := config.ExcludeFiles[path.Base(filePath)]; ok {
			d.Printf("skipping %s because exclude", filePath)
			bodyReader = body
		} else {
			d.Printf("storing as %s", filePath)
			fileDir := path.Dir(filePath)
			if err := os.MkdirAll(fileDir, 0755); err != nil {
				d.Fatalf("MkdirAll(%s) err: %s", fileDir, err)
				return
			}

			f, err := os.Create(filePath)
			if err != nil {
				d.Fatalf("Create(%s) err: %s", filePath, err)
			}
			defer f.Close()
			bodyReader = io.TeeReader(body, f)
		}
	} else {
		bodyReader = body
	}

	// At this point reading from bodyReader will also write to the
	// corresponding file for this page on the filesystem, if necessary. In the
	// next part we only extractLinks from a page which is html, otherwise we
	// io.Copy into an ioutil.Discard to "read" the whole page, so that we write
	// to the page's file if necessary.

	if strings.Index(r.Header.Get("Content-Type"), "text/html") != 0 {
		if _, err := io.Copy(ioutil.Discard, bodyReader); err != nil {
			d.Printf("Copy page: %s err: %s", page, err)
		}
		return
	}

	links, err := extractLinks(bodyReader)
	if err != nil {
		d.Printf("extractLinks page: %s err: %s", page, err)
		return
	}

	retURLs = make([]*url.URL, 0, len(links))
	for _, link := range links {
		linkURL, err := url.Parse(link)
		if err != nil {
			d.Printf("url.Parse link: %s page: %s err: %s", link, page, err)
			return
		}

		absURL := page.ResolveReference(linkURL)
		if strings.HasPrefix(path.Clean(absURL.Path), rootPath) {
			retURLs = append(retURLs, absURL)
		}
	}

	return retURLs
}

func (d *downloader) crawl() {
	client := new(http.Client)
	client.Transport = &http.Transport{
		MaxIdleConnsPerHost: 100,
	}

	for i := 0; i < 3; {
		page := tracker.FreeLink()
		if page == nil {
			i++
			d.Printf("downloader waiting (%d)", i)
			time.Sleep(30 * time.Second)
			continue
		}
		i = 0

		retURLs := d.processPage(client, page)
		if len(retURLs) == 0 {
			continue
		}
		tracker.AddFreeLinks(retURLs)
	}

	d.Println("downloader routine done")
	wg.Done()
}

func main() {
	log.SetOutput(os.Stdout)

	srcURL, err := url.ParseRequestURI(config.Src)
	if err != nil {
		log.Fatalf("parsing src: %s", err)
	}

	tracker.AddFreeLinks([]*url.URL{srcURL})

	log.Printf("Spawning %d downloaders", config.NumDownloaders)
	for i := 0; i < config.NumDownloaders; i++ {
		d := downloader{i}
		go d.crawl()
		wg.Add(1)
	}

	wg.Wait()
}
