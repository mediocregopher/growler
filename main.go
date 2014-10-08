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
	"strings"
	"time"
	
	"github.com/mediocregopher/growler/config"
)

var incoming = make(chan []*url.URL)
var outgoing = make(chan *url.URL, config.NumDownloaders)

var parseSearch = map[string]string{
	"a":   "href",
	"img": "src",
}

var rootURL *url.URL
var rootPath string

func init() {
	go filter()
	for i := 0; i < config.NumDownloaders; i++ {
		go crawl()
	}

	var err error
	rootReq, err := http.NewRequest("GET", config.Src, nil)
	if err != nil {
		log.Fatalf("parsing rootReq: %s", err)
	}
	rootURL = rootReq.URL
	rootPath = path.Clean(rootURL.Path)
}

func cleanURL(u *url.URL) {
	p := u.Path
	u.Path = path.Clean(p)
}

func filter() {
	m := map[string]struct{}{}
	for pages := range incoming {
		for _, page := range pages {
			cleanURL(page)
			pageStr := page.String()
			if _, ok := m[pageStr]; !ok {
				m[pageStr] = struct{}{}
				outgoing <- page
			}
		}
	}
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

// Does a GET to retrieve the file from disk and returns the io.ReadCloser for
// the body. Also returns whether or not the body should be written to disk
// (always true).
func getPage(
	client *http.Client, page *url.URL,
) (
	*http.Response, io.ReadCloser, string, bool, error,
) {
	log.Printf("GET %s", page)
	r, err := client.Get(page.String())
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
func maybeGetPage(
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
		return getPage(client, page)
	}

	filestat, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return getPage(client, page)
		} else {
			return nil, nil, "", false, err
		}
	}

	log.Printf("HEAD %s", page)
	r, err := client.Head(page.String())
	if err != nil {
		return nil, nil, "", false, err
	}
	r.Body.Close() // HEAD response has no body, but we Close anyway

	// We recompute the filePath and filestat in case the HEAD got redirected
	// and we're actually looking at a different file now
	filePath, err = getFilePath(page)
	if err != nil {
		return nil, nil, "", false, err
	}

	filestat, err = os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return getPage(client, page)
		} else {
			return nil, nil, "", false, err
		}
	}

	lmStr := r.Header.Get("Last-Modified")
	// We don't want to make any assumptions if we don't know the last modified
	// time
	if lmStr == "" {
		return getPage(client, page)
	}

	lm, err := time.Parse(time.RFC1123, lmStr)
	if err != nil {
		log.Printf("error parsing last modified (%s); %s", lmStr, err)
		return getPage(client, page)
	}

	if lm.After(filestat.ModTime()) {
		return getPage(client, page)
	}
	
	if r.ContentLength != filestat.Size() {
		return getPage(client, page)
	}

	f, err := os.Open(filePath)	
	if err != nil {
		return nil, nil, "", false, err
	}
	return r, f, filePath, false, nil
}

func processPage(client *http.Client, page *url.URL) {
	log.Printf("processPage on %s", page)

	r, body, filePath, store, err := maybeGetPage(client, page)
	if err != nil {
		log.Printf("getPage: %s err: %s", page, err)
		return
	}
	defer body.Close()

	// page might have changed inside of maybeGetPage due to redirects
	page = r.Request.URL

	var bodyReader io.Reader
	if store {
		log.Printf("storing as %s", filePath)
		fileDir := path.Dir(filePath)
		if err := os.MkdirAll(fileDir, 0755); err != nil {
			log.Fatalf("MkdirAll(%s) err: %s", fileDir, err)
			return
		}

		f, err := os.Create(filePath)
		if err != nil {
			log.Fatalf("Create(%s) err: %s", filePath, err)
		}
		defer f.Close()
		bodyReader = io.TeeReader(body, f)
	} else {
		bodyReader = body
	}

	// At this point reading from bodyReader will also write to the
	// corresponding file for this page on the filesystem, if necessary. In the
	// next part we only extractLinks from a page which is html, otherwise we
	// io.Copy into an ioutil.Discard to "read" the whole page, so that we write
	// to the page's file if necessary.

	if r.Header.Get("Content-Type") != "text/html" {
		if _, err := io.Copy(ioutil.Discard, bodyReader); err != nil {
			log.Printf("Copy page: %s err: %s", page, err)
		}
		return
	}

	links, err := extractLinks(bodyReader)	
	if err != nil {
		log.Printf("extractLinks page: %s err: %s", page, err)
		return
	}

	absURLs := make([]*url.URL, 0, len(links))
	for _, link := range links {
		linkURL, err := url.Parse(link)
		if err != nil {
			log.Printf("url.Parse link: %s page: %s err: %s", link, page, err)
			return
		}

		absURL := page.ResolveReference(linkURL)
		if strings.HasPrefix(path.Clean(absURL.Path), rootPath) {
			absURLs = append(absURLs, absURL)
		}
	}

	if len(absURLs) > 0 {
		go func() {
			select {
			case incoming <- absURLs:
			case <-time.After(1 * time.Second):
			}
		}()
	}
}

func crawl() {
	client := new(http.Client)
	for page := range outgoing {
		processPage(client, page)
	}
}

func main() {
	srcURL, err := url.ParseRequestURI(config.Src)
	if err != nil {
		log.Fatalf("parsing src: %s", err)
	}
	incoming <- []*url.URL{srcURL}
	select {}
}
