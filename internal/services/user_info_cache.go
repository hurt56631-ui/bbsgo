package services

import (
	"sync"
	"time"

	"bbs-go/internal/models"
)

const (
	userInfoCacheTTL        = 30 * time.Second
	userInfoCacheMaxEntries = 20000
)

type userInfoCacheEntry struct {
	user      models.User
	expiresAt time.Time
}

type userInfoCache struct {
	mu      sync.RWMutex
	entries map[int64]userInfoCacheEntry
}

func newUserInfoCache() *userInfoCache {
	return &userInfoCache{entries: make(map[int64]userInfoCacheEntry)}
}

func (c *userInfoCache) getMany(ids []int64) (map[int64]models.User, []int64) {
	result := make(map[int64]models.User, len(ids))
	missing := make([]int64, 0, len(ids))
	now := time.Now()

	c.mu.RLock()
	for _, id := range ids {
		entry, ok := c.entries[id]
		if ok && now.Before(entry.expiresAt) {
			result[id] = entry.user
			continue
		}
		missing = append(missing, id)
	}
	c.mu.RUnlock()
	return result, missing
}

func (c *userInfoCache) putMany(users []models.User) {
	if len(users) == 0 {
		return
	}
	expiresAt := time.Now().Add(userInfoCacheTTL)
	c.mu.Lock()
	if len(c.entries)+len(users) > userInfoCacheMaxEntries {
		// Short TTL plus a full reset is cheaper than maintaining an LRU on every
		// list request, and keeps the memory ceiling deterministic.
		c.entries = make(map[int64]userInfoCacheEntry, userInfoCacheMaxEntries)
	}
	for _, user := range users {
		c.entries[user.Id] = userInfoCacheEntry{user: user, expiresAt: expiresAt}
	}
	c.mu.Unlock()
}

func (c *userInfoCache) invalidate(id int64) {
	if id <= 0 {
		return
	}
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}
