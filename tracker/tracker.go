package tracker

import (
	"sync"
	"net/url"
)

var tracker = map[string]struct{}{}
var lock = sync.Mutex{}

func CanFetch(path string) bool {
	lock.Lock()
	defer lock.Unlock()

	if _, ok := tracker[path]; ok {
		return false
	}

	tracker[path] = struct{}{}
	return true
}

var freeLinks = make(chan *url.URL, 1024)

func FreeLinks() <-chan *url.URL {
	return freeLinks
}

func AddFreeLinks(u []*url.URL) {
	for i := range u {
		select {
		case freeLinks <- u[i]:
		default:
			return
		}
	}
}

