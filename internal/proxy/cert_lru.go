package proxy

import (
	"container/list"
	"crypto/tls"
	"sync"
)

type certLRU struct {
	capacity int
	list     *list.List
	entries  map[string]*list.Element
	mu       sync.Mutex
}

type certLRUEntry struct {
	host string
	cert *tls.Certificate
}

func newCertLRU(capacity int) *certLRU {
	return &certLRU{
		capacity: capacity,
		list:     list.New(),
		entries:  make(map[string]*list.Element),
	}
}

func (c *certLRU) get(host string) (*tls.Certificate, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[host]
	if !ok {
		return nil, false
	}
	c.list.MoveToFront(elem)
	return elem.Value.(*certLRUEntry).cert, true
}

func (c *certLRU) put(host string, cert *tls.Certificate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[host]; ok {
		elem.Value.(*certLRUEntry).cert = cert
		c.list.MoveToFront(elem)
		return
	}
	if c.list.Len() >= c.capacity {
		tail := c.list.Back()
		if tail != nil {
			c.list.Remove(tail)
			delete(c.entries, tail.Value.(*certLRUEntry).host)
		}
	}
	elem := c.list.PushFront(&certLRUEntry{host: host, cert: cert})
	c.entries[host] = elem
}
