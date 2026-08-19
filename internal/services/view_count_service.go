package services

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mlogclub/simple/sqls"
	"gorm.io/gorm"
)

const (
	viewCountBatchSize  = 200
	viewCountShardCount = 32
)

var ViewCountService = newViewCountService()

type viewCountShard struct {
	mu            sync.Mutex
	topicCounts   map[int64]int64
	articleCounts map[int64]int64
}

type viewCountService struct {
	flushMu       sync.Mutex
	bufferEnabled atomic.Bool
	shards        [viewCountShardCount]viewCountShard
}

func newViewCountService() *viewCountService {
	s := &viewCountService{}
	for i := range s.shards {
		s.shards[i].topicCounts = make(map[int64]int64)
		s.shards[i].articleCounts = make(map[int64]int64)
	}
	return s
}

// EnableBuffering enables periodic aggregation. Before the scheduler starts,
// increments continue to write directly so development and tests do not lose counts.
func (s *viewCountService) EnableBuffering() {
	s.bufferEnabled.Store(true)
}

func (s *viewCountService) IncrTopic(topicId int64) {
	if topicId <= 0 {
		return
	}
	if s.addBuffered(topicId, true) {
		return
	}
	_ = sqls.DB().Exec("update t_topic set view_count = view_count + 1 where id = ?", topicId).Error
}

func (s *viewCountService) IncrArticle(articleId int64) {
	if articleId <= 0 {
		return
	}
	if s.addBuffered(articleId, false) {
		return
	}
	_ = sqls.DB().Exec("update t_article set view_count = view_count + 1 where id = ?", articleId).Error
}

func (s *viewCountService) addBuffered(id int64, topic bool) bool {
	if !s.bufferEnabled.Load() {
		return false
	}
	shard := &s.shards[uint64(id)%viewCountShardCount]
	shard.mu.Lock()
	if topic {
		shard.topicCounts[id]++
	} else {
		shard.articleCounts[id]++
	}
	shard.mu.Unlock()
	return true
}

// Flush writes one batched update per group of IDs instead of one UPDATE for
// every page view. On any database error the snapshot is merged back so the
// next run can retry it.
func (s *viewCountService) Flush() error {
	// Serialize scheduled and shutdown flushes. During shutdown this also waits
	// for any in-flight cron flush before taking the final snapshot.
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	topicCounts, articleCounts := s.takeSnapshot()
	if len(topicCounts) == 0 && len(articleCounts) == 0 {
		return nil
	}

	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		if err := flushViewCounts(tx, "t_topic", topicCounts); err != nil {
			return err
		}
		return flushViewCounts(tx, "t_article", articleCounts)
	})
	if err != nil {
		s.restoreSnapshot(topicCounts, articleCounts)
	}
	return err
}

func (s *viewCountService) takeSnapshot() (map[int64]int64, map[int64]int64) {
	topicCounts := make(map[int64]int64)
	articleCounts := make(map[int64]int64)
	for i := range s.shards {
		shard := &s.shards[i]
		shard.mu.Lock()
		for id, count := range shard.topicCounts {
			topicCounts[id] += count
		}
		for id, count := range shard.articleCounts {
			articleCounts[id] += count
		}
		shard.topicCounts = make(map[int64]int64)
		shard.articleCounts = make(map[int64]int64)
		shard.mu.Unlock()
	}
	return topicCounts, articleCounts
}

func (s *viewCountService) restoreSnapshot(topicCounts, articleCounts map[int64]int64) {
	for id, count := range topicCounts {
		shard := &s.shards[uint64(id)%viewCountShardCount]
		shard.mu.Lock()
		shard.topicCounts[id] += count
		shard.mu.Unlock()
	}
	for id, count := range articleCounts {
		shard := &s.shards[uint64(id)%viewCountShardCount]
		shard.mu.Lock()
		shard.articleCounts[id] += count
		shard.mu.Unlock()
	}
}

func flushViewCounts(tx *gorm.DB, table string, counts map[int64]int64) error {
	if len(counts) == 0 {
		return nil
	}
	if table != "t_topic" && table != "t_article" {
		return fmt.Errorf("unsupported view count table: %s", table)
	}

	ids := make([]int64, 0, len(counts))
	for id, count := range counts {
		if id > 0 && count > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for start := 0; start < len(ids); start += viewCountBatchSize {
		end := start + viewCountBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		if err := flushViewCountBatch(tx, table, batch, counts); err != nil {
			return err
		}
	}
	return nil
}

func flushViewCountBatch(tx *gorm.DB, table string, ids []int64, counts map[int64]int64) error {
	if len(ids) == 0 {
		return nil
	}

	var query strings.Builder
	query.WriteString("UPDATE ")
	query.WriteString(table)
	query.WriteString(" SET view_count = view_count + CASE id ")

	args := make([]interface{}, 0, len(ids)*3)
	for _, id := range ids {
		query.WriteString("WHEN ? THEN ? ")
		args = append(args, id, counts[id])
	}
	query.WriteString("ELSE 0 END WHERE id IN (")
	for i, id := range ids {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteByte('?')
		args = append(args, id)
	}
	query.WriteByte(')')

	return tx.Exec(query.String(), args...).Error
}
