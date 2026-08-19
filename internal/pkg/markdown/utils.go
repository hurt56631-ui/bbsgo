package markdown

import (
	"container/list"
	"crypto/sha256"
	"sync"

	"bbs-go/internal/pkg/html"

	"github.com/88250/lute"
	"github.com/mlogclub/simple/common/strs"
)

const (
	maxRenderCacheEntries = 512
	maxRenderCacheBytes   = 16 * 1024 * 1024
	maxRenderCacheItem    = 512 * 1024
)

type renderCacheEntry struct {
	key   [sha256.Size]byte
	html  string
	bytes int
}

type boundedRenderCache struct {
	mu         sync.Mutex
	items      map[[sha256.Size]byte]*list.Element
	order      *list.List
	totalBytes int
}

var (
	enginePool = sync.Pool{New: func() interface{} { return newEngine() }}
	htmlCache  = newBoundedRenderCache()
)

func newEngine() *lute.Lute {
	engine := lute.New(func(engine *lute.Lute) {
		engine.SetSanitize(true)
		engine.SetGFMTaskListItem(true)
	})
	return engine
}

func newBoundedRenderCache() *boundedRenderCache {
	return &boundedRenderCache{
		items: make(map[[sha256.Size]byte]*list.Element),
		order: list.New(),
	}
}

func (c *boundedRenderCache) get(key [sha256.Size]byte) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, exists := c.items[key]
	if !exists {
		return "", false
	}
	c.order.MoveToFront(element)
	return element.Value.(*renderCacheEntry).html, true
}

func (c *boundedRenderCache) put(key [sha256.Size]byte, rendered string) {
	entryBytes := len(rendered) + sha256.Size
	if entryBytes <= 0 || entryBytes > maxRenderCacheItem {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		entry := element.Value.(*renderCacheEntry)
		c.totalBytes -= entry.bytes
		entry.html = rendered
		entry.bytes = entryBytes
		c.totalBytes += entryBytes
		c.order.MoveToFront(element)
	} else {
		entry := &renderCacheEntry{key: key, html: rendered, bytes: entryBytes}
		element := c.order.PushFront(entry)
		c.items[key] = element
		c.totalBytes += entryBytes
	}

	for c.order.Len() > maxRenderCacheEntries || c.totalBytes > maxRenderCacheBytes {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*renderCacheEntry)
		delete(c.items, entry.key)
		c.totalBytes -= entry.bytes
		c.order.Remove(oldest)
	}
}

func ToHTML(markdownStr string) string {
	if strs.IsBlank(markdownStr) {
		return ""
	}

	key := sha256.Sum256([]byte(markdownStr))
	if rendered, exists := htmlCache.get(key); exists {
		return rendered
	}

	engine := enginePool.Get().(*lute.Lute)
	rendered := engine.MarkdownStr("", markdownStr)
	enginePool.Put(engine)
	htmlCache.put(key, rendered)
	return rendered
}

func GetSummary(markdownStr string, summaryLen int) string {
	htmlStr := ToHTML(markdownStr)
	return html.GetSummary(htmlStr, summaryLen)
}
