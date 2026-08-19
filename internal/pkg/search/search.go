package search

import (
	"bbs-go/internal/cache"
	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	"bbs-go/internal/pkg/config"
	"bbs-go/internal/repositories"
	"errors"
	"html"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	bleve "github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/mitchellh/mapstructure"
	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"
	"github.com/spf13/cast"
)

const (
	indexPendingMaxSize = 20000
	indexDrainBatchSize = 512
	indexRetryDelay     = time.Second
	maxSearchPageSize   = 50
	maxSearchOffset     = 1000
)

type indexJobType uint8

const (
	indexJobTopic indexJobType = iota + 1
	indexJobArticle
	indexJobUser
	indexJobDeleteTopic
	indexJobDeleteArticle
	indexJobDeleteUser
)

type indexJob struct {
	kind    indexJobType
	id      int64
	topic   *models.Topic
	article *models.Article
	user    *models.User
}

var (
	index        bleve.Index
	indexStateMu sync.RWMutex
	indexWriteMu sync.Mutex

	indexSignal    chan struct{}
	indexPendingMu sync.Mutex
	indexPending   map[string]indexJob
	indexWG        sync.WaitGroup
)

func Init() {
	indexStateMu.Lock()
	defer indexStateMu.Unlock()
	if index != nil {
		return
	}

	indexPath := config.Instance.Search.IndexPath
	opened, err := bleve.Open(indexPath)
	if err != nil {
		if err == bleve.ErrorIndexPathDoesNotExist {
			opened = newIndex(indexPath)
		} else {
			slog.Error("open search index failed", slog.Any("err", err))
			return
		}
	}
	if opened == nil {
		slog.Error("search index unavailable")
		return
	}
	index = opened
	indexPendingMu.Lock()
	indexPending = make(map[string]indexJob)
	indexPendingMu.Unlock()
	indexSignal = make(chan struct{}, 1)
	indexWG.Add(1)
	go runIndexWriter(indexSignal)
}

func currentIndex() bleve.Index {
	indexStateMu.RLock()
	defer indexStateMu.RUnlock()
	return index
}

func indexJobKey(job indexJob) string {
	var prefix byte
	switch job.kind {
	case indexJobTopic, indexJobDeleteTopic:
		prefix = 't'
	case indexJobArticle, indexJobDeleteArticle:
		prefix = 'a'
	case indexJobUser, indexJobDeleteUser:
		prefix = 'u'
	default:
		prefix = 'x'
	}
	return string(prefix) + ":" + strconv.FormatInt(job.id, 10)
}

// enqueueIndexJob coalesces repeated updates for the same entity and never
// blocks an API request on Bleve disk I/O. If the bounded pending set is full,
// the write is rejected and can be repaired by the admin reindex operation.
func enqueueIndexJob(job indexJob) error {
	indexStateMu.RLock()
	defer indexStateMu.RUnlock()
	if index == nil || indexSignal == nil {
		return errors.New("search index unavailable")
	}

	key := indexJobKey(job)
	indexPendingMu.Lock()
	if _, exists := indexPending[key]; !exists && len(indexPending) >= indexPendingMaxSize {
		indexPendingMu.Unlock()
		return errors.New("search index pending queue full")
	}
	// The newest operation wins. A delete therefore replaces an older update,
	// and a later recreate/update replaces an older delete.
	indexPending[key] = job
	indexPendingMu.Unlock()

	select {
	case indexSignal <- struct{}{}:
	default:
	}
	return nil
}

func runIndexWriter(signals <-chan struct{}) {
	defer indexWG.Done()
	for range signals {
		drainIndexJobs()
	}
	// Close may race with one final enqueue that already passed the state lock.
	// Drain the pending map once more before the index is closed.
	drainIndexJobs()
}

func drainIndexJobs() {
	for {
		jobs := takePendingIndexJobs(indexDrainBatchSize)
		if len(jobs) == 0 {
			return
		}
		if err := executeIndexJobs(jobs); err != nil {
			slog.Error("search index batch update failed", slog.Int("count", len(jobs)), slog.Any("err", err))
			requeueIndexJobs(jobs)
			time.AfterFunc(indexRetryDelay, signalIndexWriter)
			return
		}
	}
}

// requeueIndexJobs restores failed writes without overwriting a newer update
// that may have arrived while the batch was being committed.
func requeueIndexJobs(jobs []indexJob) {
	indexPendingMu.Lock()
	defer indexPendingMu.Unlock()
	if indexPending == nil {
		return
	}
	for _, job := range jobs {
		key := indexJobKey(job)
		if _, exists := indexPending[key]; exists {
			continue
		}
		if len(indexPending) >= indexPendingMaxSize {
			slog.Error("search index retry queue full", slog.String("key", key))
			continue
		}
		indexPending[key] = job
	}
}

func signalIndexWriter() {
	indexStateMu.RLock()
	defer indexStateMu.RUnlock()
	if index == nil || indexSignal == nil {
		return
	}
	select {
	case indexSignal <- struct{}{}:
	default:
	}
}

func takePendingIndexJobs(limit int) []indexJob {
	indexPendingMu.Lock()
	defer indexPendingMu.Unlock()
	if len(indexPending) == 0 {
		return nil
	}
	jobs := make([]indexJob, 0, min(limit, len(indexPending)))
	for key, job := range indexPending {
		jobs = append(jobs, job)
		delete(indexPending, key)
		if len(jobs) >= limit {
			break
		}
	}
	return jobs
}

func executeIndexJobs(jobs []indexJob) error {
	if len(jobs) == 0 {
		return nil
	}
	var (
		topics       []models.Topic
		articles     []models.Article
		users        []models.User
		deleteTopics []int64
		deleteArts   []int64
		deleteUsers  []int64
	)
	for _, job := range jobs {
		switch job.kind {
		case indexJobTopic:
			if job.topic != nil {
				topics = append(topics, *job.topic)
			}
		case indexJobArticle:
			if job.article != nil {
				articles = append(articles, *job.article)
			}
		case indexJobUser:
			if job.user != nil {
				users = append(users, *job.user)
			}
		case indexJobDeleteTopic:
			deleteTopics = append(deleteTopics, job.id)
		case indexJobDeleteArticle:
			deleteArts = append(deleteArts, job.id)
		case indexJobDeleteUser:
			deleteUsers = append(deleteUsers, job.id)
		}
	}

	var errs []error
	if err := IndexTopicBatch(topics); err != nil {
		errs = append(errs, err)
	}
	if err := IndexArticleBatch(articles); err != nil {
		errs = append(errs, err)
	}
	if err := IndexUserBatch(users); err != nil {
		errs = append(errs, err)
	}
	if len(deleteTopics)+len(deleteArts)+len(deleteUsers) > 0 {
		if err := deleteIndexBatch(deleteTopics, deleteArts, deleteUsers); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func deleteIndexBatch(topicIDs, articleIDs, userIDs []int64) error {
	idx := currentIndex()
	if idx == nil {
		return errors.New("search index unavailable")
	}
	batch := idx.NewBatch()
	for _, id := range topicIDs {
		batch.Delete(searchDocID(EntityTypeTopic, id))
		batch.Delete(cast.ToString(id)) // remove legacy topic-only document IDs
	}
	for _, id := range articleIDs {
		batch.Delete(searchDocID(EntityTypeArticle, id))
	}
	for _, id := range userIDs {
		batch.Delete(searchDocID(EntityTypeUser, id))
	}
	return commitBatch(idx, batch)
}

// Close drains pending writes before closing the local Bleve index.
func Close() error {
	indexStateMu.Lock()
	signals := indexSignal
	indexSignal = nil
	if signals != nil {
		close(signals)
	}
	indexStateMu.Unlock()

	indexWG.Wait()
	indexWriteMu.Lock()
	defer indexWriteMu.Unlock()

	indexStateMu.Lock()
	idx := index
	index = nil
	indexStateMu.Unlock()
	if idx == nil {
		return nil
	}
	return idx.Close()
}

func NewTopicDoc(topic *models.Topic) *TopicDocument {
	if topic == nil {
		return nil
	}
	return newTopicDocument(topic, cache.UserCache.Get(topic.UserId), getTopicTags(topic.Id))
}

func NewArticleDoc(article *models.Article) *ArticleDocument {
	if article == nil {
		return nil
	}
	return newArticleDocument(article, cache.UserCache.Get(article.UserId), getArticleTags(article.Id))
}

func NewUserDoc(user *models.User) *UserDocument {
	if user == nil {
		return nil
	}
	return &UserDocument{
		Type:         EntityTypeUser,
		Id:           user.Id,
		Username:     html.EscapeString(user.Username.String),
		Nickname:     html.EscapeString(user.Nickname),
		Avatar:       user.Avatar,
		Description:  html.EscapeString(user.Description),
		Status:       user.Status,
		TopicCount:   user.TopicCount,
		CommentCount: user.CommentCount,
		FansCount:    user.FansCount,
		FollowCount:  user.FollowCount,
		Score:        user.Score,
		Exp:          user.Exp,
		Level:        user.Level,
		CreateTime:   user.CreateTime,
	}
}

func getTopicTags(topicId int64) []models.Tag {
	topicTags := repositories.TopicTagRepository.Find(sqls.DB(), sqls.NewCnd().Where("topic_id = ?", topicId))

	var tagIds []int64
	for _, topicTag := range topicTags {
		tagIds = append(tagIds, topicTag.TagId)
	}
	return cache.TagCache.GetList(tagIds)
}

func getArticleTags(articleId int64) []models.Tag {
	tagIds := cache.ArticleTagCache.Get(articleId)
	return cache.TagCache.GetList(tagIds)
}

func UpdateTopicIndexAsync(topic *models.Topic) {
	UpdateTopicIndex(topic)
}

// UpdateTopicIndex queues an ordered write. The model is copied so callers may
// safely reuse or mutate their instance after this function returns.
func UpdateTopicIndex(topic *models.Topic) {
	if topic == nil {
		return
	}
	copy := *topic
	if copy.Status != constants.StatusOk {
		if err := DeleteTopicIndex(copy.Id); err != nil {
			slog.Error("queue topic search index delete failed", slog.Int64("id", copy.Id), slog.Any("err", err))
		}
		return
	}
	if err := enqueueIndexJob(indexJob{kind: indexJobTopic, id: copy.Id, topic: &copy}); err != nil {
		slog.Error("queue topic search index failed", slog.Int64("id", copy.Id), slog.Any("err", err))
	}
}

func DeleteTopicIndex(id int64) error {
	return enqueueIndexJob(indexJob{kind: indexJobDeleteTopic, id: id})
}

func UpdateArticleIndex(article *models.Article) {
	if article == nil {
		return
	}
	copy := *article
	if copy.Status != constants.StatusOk {
		if err := DeleteArticleIndex(copy.Id); err != nil {
			slog.Error("queue article search index delete failed", slog.Int64("id", copy.Id), slog.Any("err", err))
		}
		return
	}
	if err := enqueueIndexJob(indexJob{kind: indexJobArticle, id: copy.Id, article: &copy}); err != nil {
		slog.Error("queue article search index failed", slog.Int64("id", copy.Id), slog.Any("err", err))
	}
}

func DeleteArticleIndex(id int64) error {
	return enqueueIndexJob(indexJob{kind: indexJobDeleteArticle, id: id})
}

func UpdateUserIndex(user *models.User) {
	if user == nil {
		return
	}
	copy := *user
	if copy.Status != constants.StatusOk {
		if err := DeleteUserIndex(copy.Id); err != nil {
			slog.Error("queue user search index delete failed", slog.Int64("id", copy.Id), slog.Any("err", err))
		}
		return
	}
	if err := enqueueIndexJob(indexJob{kind: indexJobUser, id: copy.Id, user: &copy}); err != nil {
		slog.Error("queue user search index failed", slog.Int64("id", copy.Id), slog.Any("err", err))
	}
}

func DeleteUserIndex(id int64) error {
	return enqueueIndexJob(indexJob{kind: indexJobDeleteUser, id: id})
}

func normalizeSearchPaging(page, limit int) (*sqls.Paging, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxSearchPageSize {
		limit = maxSearchPageSize
	}
	paging := &sqls.Paging{Page: page, Limit: limit}
	if paging.Offset() > maxSearchOffset {
		return nil, errors.New("search pagination is too deep")
	}
	return paging, nil
}

// 分页查询
func SearchTopic(keyword string, categoryId int64, categoryIds []int64, timeRange, page, limit int) (docs []TopicDocument, paging *sqls.Paging, err error) {
	idx := currentIndex()
	if idx == nil {
		err = errors.New("search index unavailable")
		return
	}

	paging, err = normalizeSearchPaging(page, limit)
	if err != nil {
		return
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(bleve.NewMatchAllQuery())
	query.AddMust(typeQuery(EntityTypeTopic))
	query.AddMust(exactNumericQuery("status", int64(constants.StatusOk)))

	if strs.IsNotBlank(keyword) {
		query.AddMust(keywordQuery(keyword, []string{"title", "content", "tags", "nickname"}))
	}

	if categoryId != 0 {
		if categoryId == -1 { // 推荐
			boolFieldQuery := bleve.NewBoolFieldQuery(true)
			boolFieldQuery.SetField("recommend")
			query.AddMust(boolFieldQuery)
		} else {
			categoryQuery := buildCategoryQuery(categoryId, categoryIds)
			if categoryQuery != nil {
				query.AddMust(categoryQuery)
			}
		}
	}
	if timeRange != 0 {
		var beginTime int64
		switch timeRange {
		case 1: // 一天内
			beginTime = dates.Timestamp(time.Now().Add(-24 * time.Hour))
		case 2: // 一周内
			beginTime = dates.Timestamp(time.Now().Add(-7 * 24 * time.Hour))
		case 3: // 一月内
			beginTime = dates.Timestamp(time.Now().AddDate(0, -1, 0))
		case 4: // 一年内
			beginTime = dates.Timestamp(time.Now().AddDate(-1, 0, 0))
		}

		min := float64(beginTime)
		max := float64(math.MaxInt64)
		createTimeQuery := bleve.NewNumericRangeQuery(&min, &max)
		createTimeQuery.SetField("createTime")
		query.AddMust(createTimeQuery)
	}

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.From = paging.Offset()
	searchRequest.Size = paging.Limit
	searchRequest.Fields = []string{"*"}
	searchRequest.Highlight = bleve.NewHighlightWithStyle("html")
	searchRequest.Highlight.AddField("title")
	searchRequest.Highlight.AddField("content")

	result, err := idx.Search(searchRequest)
	if err != nil {
		slog.Error("搜索失败:", slog.Any("err", err))
		return docs, paging, err
	}

	for _, hit := range result.Hits {
		storedDoc := hitFields(hit.Fields, hit.Fragments)
		normalizeTags(storedDoc)
		var doc TopicDocument
		if err := mapstructure.Decode(storedDoc, &doc); err != nil {
			slog.Error(err.Error())
		}
		docs = append(docs, doc)
	}

	return
}

func SearchArticle(keyword string, timeRange, page, limit int) (docs []ArticleDocument, paging *sqls.Paging, err error) {
	idx := currentIndex()
	if idx == nil {
		err = errors.New("search index unavailable")
		return
	}

	paging, err = normalizeSearchPaging(page, limit)
	if err != nil {
		return
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(bleve.NewMatchAllQuery())
	query.AddMust(typeQuery(EntityTypeArticle))
	query.AddMust(exactNumericQuery("status", int64(constants.StatusOk)))
	if strs.IsNotBlank(keyword) {
		query.AddMust(keywordQuery(keyword, []string{"title", "summary", "content", "tags", "nickname"}))
	}
	addTimeRangeQuery(query, timeRange)

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.From = paging.Offset()
	searchRequest.Size = paging.Limit
	searchRequest.Fields = []string{"*"}
	searchRequest.Highlight = bleve.NewHighlightWithStyle("html")
	searchRequest.Highlight.AddField("title")
	searchRequest.Highlight.AddField("summary")
	searchRequest.Highlight.AddField("content")

	result, err := idx.Search(searchRequest)
	if err != nil {
		slog.Error("搜索失败:", slog.Any("err", err))
		return
	}
	for _, hit := range result.Hits {
		storedDoc := hitFields(hit.Fields, hit.Fragments)
		normalizeTags(storedDoc)
		var doc ArticleDocument
		if err := mapstructure.Decode(storedDoc, &doc); err != nil {
			slog.Error(err.Error())
		}
		docs = append(docs, doc)
	}
	return
}

func SearchUser(keyword string, page, limit int) (docs []UserDocument, paging *sqls.Paging, err error) {
	idx := currentIndex()
	if idx == nil {
		err = errors.New("search index unavailable")
		return
	}

	paging, err = normalizeSearchPaging(page, limit)
	if err != nil {
		return
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(bleve.NewMatchAllQuery())
	query.AddMust(typeQuery(EntityTypeUser))
	query.AddMust(exactNumericQuery("status", int64(constants.StatusOk)))
	if strs.IsNotBlank(keyword) {
		query.AddMust(keywordQuery(keyword, []string{"username", "nickname", "description"}))
	}

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.From = paging.Offset()
	searchRequest.Size = paging.Limit
	searchRequest.Fields = []string{"*"}
	searchRequest.Highlight = bleve.NewHighlightWithStyle("html")
	searchRequest.Highlight.AddField("nickname")
	searchRequest.Highlight.AddField("username")
	searchRequest.Highlight.AddField("description")

	result, err := idx.Search(searchRequest)
	if err != nil {
		slog.Error("搜索失败:", slog.Any("err", err))
		return
	}
	for _, hit := range result.Hits {
		storedDoc := hitFields(hit.Fields, hit.Fragments)
		var doc UserDocument
		if err := mapstructure.Decode(storedDoc, &doc); err != nil {
			slog.Error(err.Error())
		}
		docs = append(docs, doc)
	}
	return
}

func SearchAll(keyword string, limit int) (AllResult, error) {
	if limit <= 0 {
		limit = 5
	}

	var (
		topics   []TopicDocument
		articles []ArticleDocument
		users    []UserDocument
		wg       sync.WaitGroup
		errCh    = make(chan error, 3)
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		var err error
		topics, _, err = SearchTopic(keyword, 0, nil, 0, 1, limit)
		if err != nil {
			errCh <- err
			return
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		articles, _, err = SearchArticle(keyword, 0, 1, limit)
		if err != nil {
			errCh <- err
			return
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		users, _, err = SearchUser(keyword, 1, limit)
		if err != nil {
			errCh <- err
			return
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return AllResult{}, err
		}
	}
	return AllResult{Topics: topics, Articles: articles, Users: users}, nil
}

func buildCategoryQuery(categoryId int64, categoryIds []int64) blevequery.Query {
	if len(categoryIds) == 0 {
		return buildExactCategoryQuery(categoryId)
	}
	if len(categoryIds) == 1 {
		return buildExactCategoryQuery(categoryIds[0])
	}
	queries := make([]blevequery.Query, 0, len(categoryIds))
	for _, id := range categoryIds {
		queries = append(queries, buildExactCategoryQuery(id))
	}
	return bleve.NewDisjunctionQuery(queries...)
}

func buildExactCategoryQuery(categoryId int64) blevequery.Query {
	return exactNumericQuery("categoryId", categoryId)
}

func exactNumericQuery(field string, value int64) blevequery.Query {
	f := float64(value)
	b := true
	query := bleve.NewNumericRangeInclusiveQuery(&f, &f, &b, &b)
	query.SetField(field)
	return query
}

func typeQuery(entityType string) blevequery.Query {
	q := bleve.NewTermQuery(entityType)
	q.SetField("type")
	return q
}

func keywordQuery(keyword string, fields []string) blevequery.Query {
	queries := make([]blevequery.Query, 0, len(fields))
	for _, field := range fields {
		q := bleve.NewMatchQuery(keyword)
		q.SetField(field)
		queries = append(queries, q)
	}
	return bleve.NewDisjunctionQuery(queries...)
}

func addTimeRangeQuery(query *blevequery.BooleanQuery, timeRange int) {
	if timeRange == 0 {
		return
	}
	var beginTime int64
	if timeRange == 1 {
		beginTime = dates.Timestamp(time.Now().Add(-24 * time.Hour))
	} else if timeRange == 2 {
		beginTime = dates.Timestamp(time.Now().Add(-7 * 24 * time.Hour))
	} else if timeRange == 3 {
		beginTime = dates.Timestamp(time.Now().AddDate(0, -1, 0))
	} else if timeRange == 4 {
		beginTime = dates.Timestamp(time.Now().AddDate(-1, 0, 0))
	}
	if beginTime == 0 {
		return
	}
	min := float64(beginTime)
	max := float64(math.MaxInt64)
	createTimeQuery := bleve.NewNumericRangeQuery(&min, &max)
	createTimeQuery.SetField("createTime")
	query.AddMust(createTimeQuery)
}

func hitFields(fields map[string]interface{}, fragments map[string][]string) map[string]interface{} {
	storedDoc := make(map[string]interface{})
	for key, field := range fields {
		storedDoc[key] = field
	}
	for field, values := range fragments {
		if len(values) > 0 {
			storedDoc[field] = values[0]
		}
	}
	return storedDoc
}

func normalizeTags(storedDoc map[string]interface{}) {
	tagField, ok := storedDoc["tags"]
	if !ok {
		return
	}
	switch v := tagField.(type) {
	case string:
		storedDoc["tags"] = []string{v}
	case []interface{}:
		var tags []string
		for _, tag := range v {
			if name, ok := tag.(string); ok {
				tags = append(tags, name)
			}
		}
		storedDoc["tags"] = tags
	}
}
