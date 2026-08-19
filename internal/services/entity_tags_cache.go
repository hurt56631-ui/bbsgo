package services

import (
	"sync"
	"time"

	"bbs-go/internal/models"
)

const (
	entityTagsCacheTTL        = 30 * time.Second
	entityTagsCacheMaxEntries = 30000
)

type entityTagsCacheEntry struct {
	tags      []models.Tag
	expiresAt time.Time
}

type entityTagsCache struct {
	mu      sync.RWMutex
	entries map[int64]entityTagsCacheEntry
}

func newEntityTagsCache() *entityTagsCache {
	return &entityTagsCache{entries: make(map[int64]entityTagsCacheEntry)}
}

func cloneTags(tags []models.Tag) []models.Tag {
	if len(tags) == 0 {
		return nil
	}
	return append([]models.Tag(nil), tags...)
}

func (c *entityTagsCache) getMany(ids []int64) (map[int64][]models.Tag, []int64) {
	result := make(map[int64][]models.Tag, len(ids))
	missing := make([]int64, 0, len(ids))
	now := time.Now()
	c.mu.RLock()
	for _, id := range ids {
		entry, ok := c.entries[id]
		if ok && now.Before(entry.expiresAt) {
			result[id] = cloneTags(entry.tags)
			continue
		}
		missing = append(missing, id)
	}
	c.mu.RUnlock()
	return result, missing
}

func (c *entityTagsCache) putMany(ids []int64, values map[int64][]models.Tag) {
	if len(ids) == 0 {
		return
	}
	expiresAt := time.Now().Add(entityTagsCacheTTL)
	c.mu.Lock()
	if len(c.entries)+len(ids) > entityTagsCacheMaxEntries {
		c.entries = make(map[int64]entityTagsCacheEntry, entityTagsCacheMaxEntries)
	}
	for _, id := range ids {
		c.entries[id] = entityTagsCacheEntry{tags: cloneTags(values[id]), expiresAt: expiresAt}
	}
	c.mu.Unlock()
}

func (c *entityTagsCache) invalidate(id int64) {
	if id <= 0 {
		return
	}
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}
