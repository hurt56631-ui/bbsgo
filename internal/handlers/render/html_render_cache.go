package render

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"

	"bbs-go/internal/models/resp"
)

const (
	htmlRenderCacheMaxEntries = 512
	htmlRenderCacheMaxBytes   = 32 << 20
	htmlRenderCacheMaxInput   = 512 << 10
	htmlRenderCacheTTL        = 5 * time.Minute
)

type htmlRenderCacheKey [32]byte

type htmlRenderCacheValue struct {
	key       htmlRenderCacheKey
	html      string
	toc       []resp.TopicTocItem
	size      int
	expiresAt time.Time
}

type htmlRenderLRU struct {
	mu        sync.Mutex
	loadLocks [32]sync.Mutex
	items     map[htmlRenderCacheKey]*list.Element
	lru       *list.List
	bytes     int
	maxSize   int
}

var renderedHTMLCache = newHTMLRenderLRU(htmlRenderCacheMaxBytes)

func newHTMLRenderLRU(maxSize int) *htmlRenderLRU {
	return &htmlRenderLRU{
		items:   make(map[htmlRenderCacheKey]*list.Element),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

func makeHTMLRenderCacheKey(content string, buildToc, redirectEnabled bool) htmlRenderCacheKey {
	h := sha256.New()
	var flags [2]byte
	if buildToc {
		flags[0] = 1
	}
	if redirectEnabled {
		flags[1] = 1
	}
	_, _ = h.Write(flags[:])
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(content)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(content))
	var key htmlRenderCacheKey
	copy(key[:], h.Sum(nil))
	return key
}

func (c *htmlRenderLRU) loadLock(key htmlRenderCacheKey) *sync.Mutex {
	index := binary.LittleEndian.Uint32(key[:4]) % uint32(len(c.loadLocks))
	return &c.loadLocks[index]
}

func (c *htmlRenderLRU) get(key htmlRenderCacheKey) (string, []resp.TopicTocItem, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return "", nil, false
	}
	value := element.Value.(*htmlRenderCacheValue)
	if now.After(value.expiresAt) {
		c.removeElement(element)
		return "", nil, false
	}
	c.lru.MoveToFront(element)
	return value.html, append([]resp.TopicTocItem(nil), value.toc...), true
}

func (c *htmlRenderLRU) put(key htmlRenderCacheKey, html string, toc []resp.TopicTocItem) {
	size := len(html) + len(toc)*64
	if size <= 0 || size > c.maxSize/2 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.items[key]; ok {
		c.removeElement(existing)
	}
	value := &htmlRenderCacheValue{
		key:       key,
		html:      html,
		toc:       append([]resp.TopicTocItem(nil), toc...),
		size:      size,
		expiresAt: time.Now().Add(htmlRenderCacheTTL),
	}
	element := c.lru.PushFront(value)
	c.items[key] = element
	c.bytes += size

	for c.lru.Len() > htmlRenderCacheMaxEntries || c.bytes > c.maxSize {
		c.removeElement(c.lru.Back())
	}
}

func (c *htmlRenderLRU) removeElement(element *list.Element) {
	if element == nil {
		return
	}
	value := element.Value.(*htmlRenderCacheValue)
	delete(c.items, value.key)
	c.bytes -= value.size
	c.lru.Remove(element)
}
