package services

import (
	"bbs-go/internal/models"
	"bbs-go/internal/pkg/search"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/sqls"
)

const (
	searchReindexBatchSize = 500
	searchReindexPause     = 10 * time.Millisecond
)

var SearchReindexService = newSearchReindexService()

type SearchReindexStatus struct {
	Running          bool   `json:"running"`
	Processed        int64  `json:"processed"`
	Total            int64  `json:"total"`
	TopicProcessed   int64  `json:"topicProcessed"`
	TopicTotal       int64  `json:"topicTotal"`
	ArticleProcessed int64  `json:"articleProcessed"`
	ArticleTotal     int64  `json:"articleTotal"`
	UserProcessed    int64  `json:"userProcessed"`
	UserTotal        int64  `json:"userTotal"`
	StartedAt        int64  `json:"startedAt"`
	FinishedAt       int64  `json:"finishedAt"`
	Error            string `json:"error"`
}

type searchReindexService struct {
	mu     sync.Mutex
	status SearchReindexStatus
}

func newSearchReindexService() *searchReindexService {
	return &searchReindexService{}
}

func (s *searchReindexService) Status() SearchReindexStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *searchReindexService) Start() (SearchReindexStatus, bool) {
	s.mu.Lock()
	if s.status.Running {
		status := s.status
		s.mu.Unlock()
		return status, false
	}

	s.status = SearchReindexStatus{
		Running:   true,
		StartedAt: dates.NowTimestamp(),
	}
	status := s.status
	s.mu.Unlock()

	go s.run()
	return status, true
}

func (s *searchReindexService) run() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("search reindex failed", slog.Any("err", r))
			s.finishWithError("search reindex failed")
		}
	}()

	// Count and scan every row. Public rows are indexed, while review/deleted
	// rows are explicitly removed by Index*Batch. This also repairs a stale
	// public document when a previous asynchronous delete was lost.
	topicTotal := TopicService.Count(sqls.NewCnd())
	var articleTotal int64
	sqls.DB().Model(&models.Article{}).Count(&articleTotal)
	var userTotal int64
	sqls.DB().Model(&models.User{}).Count(&userTotal)
	s.setTotals(topicTotal, articleTotal, userTotal)

	if err := s.reindexTopics(); err != nil {
		s.finishWithError(err.Error())
		return
	}
	if err := s.reindexArticles(); err != nil {
		s.finishWithError(err.Error())
		return
	}
	if err := s.reindexUsers(); err != nil {
		s.finishWithError(err.Error())
		return
	}
	s.finishWithError("")
}

func (s *searchReindexService) reindexTopics() error {
	cursor := int64(math.MaxInt64)
	for {
		var topics []models.Topic
		err := sqls.DB().Model(&models.Topic{}).
			Select("id", "category_id", "user_id", "title", "content", "content_type", "status", "recommend", "create_time").
			Where("id < ?", cursor).
			Order("id DESC").Limit(searchReindexBatchSize).Find(&topics).Error
		if err != nil {
			return err
		}
		if len(topics) == 0 {
			return nil
		}
		if err := search.IndexTopicBatch(topics); err != nil {
			return err
		}
		s.addTopicProcessed(int64(len(topics)))
		cursor = topics[len(topics)-1].Id
		time.Sleep(searchReindexPause)
	}
}

func (s *searchReindexService) reindexArticles() error {
	cursor := int64(math.MaxInt64)
	for {
		var articles []models.Article
		err := sqls.DB().Model(&models.Article{}).
			Select("id", "user_id", "title", "summary", "content", "content_type", "status", "create_time").
			Where("id < ?", cursor).
			Order("id DESC").Limit(searchReindexBatchSize).Find(&articles).Error
		if err != nil {
			return err
		}
		if len(articles) == 0 {
			return nil
		}
		if err := search.IndexArticleBatch(articles); err != nil {
			return err
		}
		s.addArticleProcessed(int64(len(articles)))
		cursor = articles[len(articles)-1].Id
		time.Sleep(searchReindexPause)
	}
}

func (s *searchReindexService) reindexUsers() error {
	var cursor int64
	for {
		var users []models.User
		err := sqls.DB().Model(&models.User{}).
			Select("id", "username", "nickname", "avatar", "description", "status", "topic_count", "comment_count", "fans_count", "follow_count", "score", "exp", "level", "create_time").
			Where("id > ?", cursor).
			Order("id ASC").Limit(searchReindexBatchSize).Find(&users).Error
		if err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}
		if err := search.IndexUserBatch(users); err != nil {
			return err
		}
		s.addUserProcessed(int64(len(users)))
		cursor = users[len(users)-1].Id
		time.Sleep(searchReindexPause)
	}
}

func (s *searchReindexService) setTotals(topicTotal, articleTotal, userTotal int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.TopicTotal = topicTotal
	s.status.ArticleTotal = articleTotal
	s.status.UserTotal = userTotal
	s.status.Total = topicTotal + articleTotal + userTotal
}

func (s *searchReindexService) addTopicProcessed(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Processed += count
	s.status.TopicProcessed += count
}

func (s *searchReindexService) addArticleProcessed(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Processed += count
	s.status.ArticleProcessed += count
}

func (s *searchReindexService) addUserProcessed(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Processed += count
	s.status.UserProcessed += count
}

func (s *searchReindexService) finishWithError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Running = false
	s.status.FinishedAt = dates.NowTimestamp()
	s.status.Error = message
}
