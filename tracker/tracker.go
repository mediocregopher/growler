package tracker

import (
	"container/list"
	"net/url"
	"sync"
)

var tracker = map[string]struct{}{}
var trackerLock = sync.Mutex{}

func CanFetch(path string) bool {
	trackerLock.Lock()
	defer trackerLock.Unlock()

	if _, ok := tracker[path]; ok {
		return false
	}

	tracker[path] = struct{}{}
	return true
}

var freeLinks = list.New()
var freeLinksLock = sync.Mutex{}

func FreeLink() *url.URL {
	freeLinksLock.Lock()
	defer freeLinksLock.Unlock()

	f := freeLinks.Front()
	if f == nil {
		return nil
	}

	return freeLinks.Remove(f).(*url.URL)
}

func AddFreeLinks(u []*url.URL) {
	freeLinksLock.Lock()
	defer freeLinksLock.Unlock()

	for i := range u {
		freeLinks.PushFront(u[i])
	}
}
