package services

import (
	"hash/fnv"
	"sync"
	"time"

	"bbs-go/internal/models"
)

const (
	publicListCacheTTL        = 3 * time.Second
	publicListCacheMaxEntries = 256
)

type topicPageCacheEntry struct {
	topics     []models.Topic
	nextCursor int64
	hasMore    bool
	expiresAt  time.Time
}

type topicListMicroCache struct {
	mu        sync.Mutex
	loadLocks [32]sync.Mutex
	entries   map[string]topicPageCacheEntry
}

func newTopicListMicroCache() *topicListMicroCache {
	return &topicListMicroCache{entries: make(map[string]topicPageCacheEntry)}
}

func (c *topicListMicroCache) loadLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &c.loadLocks[h.Sum32()%uint32(len(c.loadLocks))]
}

func (c *topicListMicroCache) get(key string) ([]models.Topic, int64, bool, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, 0, false, false
	}
	if now.After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, 0, false, false
	}
	return append([]models.Topic(nil), entry.topics...), entry.nextCursor, entry.hasMore, true
}

func (c *topicListMicroCache) put(key string, topics []models.Topic, nextCursor int64, hasMore bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= publicListCacheMaxEntries {
		// The cache is intentionally tiny and short-lived. Clearing it is cheaper
		// than maintaining another LRU on the hottest list path.
		c.entries = make(map[string]topicPageCacheEntry)
	}
	c.entries[key] = topicPageCacheEntry{
		topics:     append([]models.Topic(nil), topics...),
		nextCursor: nextCursor,
		hasMore:    hasMore,
		expiresAt:  time.Now().Add(publicListCacheTTL),
	}
}

func (c *topicListMicroCache) invalidate() {
	c.mu.Lock()
	c.entries = make(map[string]topicPageCacheEntry)
	c.mu.Unlock()
}

type articlePageCacheEntry struct {
	articles   []models.Article
	nextCursor int64
	hasMore    bool
	expiresAt  time.Time
}

type articleListMicroCache struct {
	mu     sync.Mutex
	loadMu sync.Mutex
	entry  articlePageCacheEntry
	valid  bool
}

func (c *articleListMicroCache) get() ([]models.Article, int64, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.valid || time.Now().After(c.entry.expiresAt) {
		c.valid = false
		return nil, 0, false, false
	}
	return append([]models.Article(nil), c.entry.articles...), c.entry.nextCursor, c.entry.hasMore, true
}

func (c *articleListMicroCache) put(articles []models.Article, nextCursor int64, hasMore bool) {
	c.mu.Lock()
	c.entry = articlePageCacheEntry{
		articles:   append([]models.Article(nil), articles...),
		nextCursor: nextCursor,
		hasMore:    hasMore,
		expiresAt:  time.Now().Add(publicListCacheTTL),
	}
	c.valid = true
	c.mu.Unlock()
}

func (c *articleListMicroCache) invalidate() {
	c.mu.Lock()
	c.valid = false
	c.entry = articlePageCacheEntry{}
	c.mu.Unlock()
}
