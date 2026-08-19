package services

import (
	"bbs-go/internal/models/constants"
	"bbs-go/internal/models/req"
	"bbs-go/internal/pkg/locales"
	"bbs-go/internal/pkg/search"
	"errors"
	"log/slog"
	"math"
	"strings"

	"bbs-go/internal/cache"
	"bbs-go/internal/repositories"

	"bbs-go/internal/pkg/params"

	"github.com/mlogclub/simple/common/dates"
	"github.com/mlogclub/simple/common/jsons"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"gorm.io/gorm"

	"bbs-go/internal/models"
)

var ArticleService = newArticleService()

func newArticleService() *articleService {
	return &articleService{listCache: &articleListMicroCache{}, tagsCache: newEntityTagsCache()}
}

type articleService struct {
	listCache *articleListMicroCache
	tagsCache *entityTagsCache
}

const articleListSelectColumns = `
	id, user_id, title, summary, content_type, cover, status, source_url,
	view_count, comment_count, like_count, create_time
`

func (s *articleService) InvalidateListCache() {
	if s.listCache != nil {
		s.listCache.invalidate()
	}
}

func (s *articleService) Get(id int64) *models.Article {
	return repositories.ArticleRepository.Get(sqls.DB(), id)
}

func (s *articleService) Take(where ...interface{}) *models.Article {
	return repositories.ArticleRepository.Take(sqls.DB(), where...)
}

func (s *articleService) Find(cnd *sqls.Cnd) []models.Article {
	return repositories.ArticleRepository.Find(sqls.DB(), cnd)
}

func (s *articleService) FindOne(cnd *sqls.Cnd) *models.Article {
	return repositories.ArticleRepository.FindOne(sqls.DB(), cnd)
}

func (s *articleService) FindPageByParams(params *params.QueryParams) (list []models.Article, paging *sqls.Paging) {
	return repositories.ArticleRepository.FindPageByParams(sqls.DB(), params)
}

func (s *articleService) FindPageByCnd(cnd *sqls.Cnd) (list []models.Article, paging *sqls.Paging) {
	return repositories.ArticleRepository.FindPageByCnd(sqls.DB(), cnd)
}

func (s *articleService) Update(t *models.Article) error {
	err := repositories.ArticleRepository.Update(sqls.DB(), t)
	if err == nil {
		s.InvalidateListCache()
	}
	return err
}

func (s *articleService) Updates(id int64, columns map[string]interface{}) error {
	err := repositories.ArticleRepository.Updates(sqls.DB(), id, columns)
	if err == nil {
		s.InvalidateListCache()
	}
	return err
}

func (s *articleService) UpdateColumn(id int64, name string, value interface{}) error {
	err := repositories.ArticleRepository.UpdateColumn(sqls.DB(), id, name, value)
	if err == nil {
		s.InvalidateListCache()
	}
	return err
}

func (s *articleService) Delete(id int64) error {
	deleted := false
	err := sqls.WithTransaction(func(ctx *sqls.TxContext) error {
		result := ctx.Tx.Model(&models.Article{}).
			Where("id = ? AND status <> ?", id, constants.StatusDeleted).
			UpdateColumn("status", constants.StatusDeleted)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		deleted = true
		return ctx.Tx.Model(&models.ArticleTag{}).Where("article_id = ?", id).
			UpdateColumn("status", constants.StatusDeleted).Error
	})
	if err != nil {
		return err
	}
	if !deleted {
		return nil
	}
	s.InvalidateListCache()
	cache.ArticleTagCache.Invalidate(id)
	if s.tagsCache != nil {
		s.tagsCache.invalidate(id)
	}
	if err := search.DeleteArticleIndex(id); err != nil {
		slog.Error("queue article index delete failed", slog.Int64("articleId", id), slog.Any("err", err))
	}
	return nil
}

// 根据文章编号批量获取文章
func (s *articleService) GetArticleInIds(articleIds []int64) []models.Article {
	if len(articleIds) == 0 {
		return nil
	}
	var articles []models.Article
	_ = sqls.DB().Model(&models.Article{}).Select(articleListSelectColumns).
		Where("id IN ? AND status = ?", articleIds, constants.StatusOk).Find(&articles).Error
	articleMap := make(map[int64]models.Article, len(articles))
	for _, article := range articles {
		articleMap[article.Id] = article
	}
	ordered := make([]models.Article, 0, len(articles))
	for _, articleId := range articleIds {
		if article, exists := articleMap[articleId]; exists {
			ordered = append(ordered, article)
		}
	}
	return ordered
}

// 获取文章对应的标签
func (s *articleService) GetArticleTags(articleId int64) []models.Tag {
	tagIds := cache.ArticleTagCache.Get(articleId)
	return cache.TagCache.GetList(tagIds)
}

// GetArticleTagsMap loads tags for an article page in two batched lookups,
// avoiding one article-tag query per row during list rendering.
func (s *articleService) GetArticleTagsMap(articleIds []int64) map[int64][]models.Tag {
	result := make(map[int64][]models.Tag, len(articleIds))
	if len(articleIds) == 0 {
		return result
	}

	uniqueArticleIds := make([]int64, 0, len(articleIds))
	seenArticleIds := make(map[int64]struct{}, len(articleIds))
	for _, articleId := range articleIds {
		if articleId <= 0 {
			continue
		}
		if _, exists := seenArticleIds[articleId]; exists {
			continue
		}
		seenArticleIds[articleId] = struct{}{}
		uniqueArticleIds = append(uniqueArticleIds, articleId)
	}
	if len(uniqueArticleIds) == 0 {
		return result
	}

	missing := uniqueArticleIds
	if s.tagsCache != nil {
		var cached map[int64][]models.Tag
		cached, missing = s.tagsCache.getMany(uniqueArticleIds)
		for id, tags := range cached {
			result[id] = tags
		}
	}
	if len(missing) == 0 {
		return result
	}

	loaded := make(map[int64][]models.Tag, len(missing))
	for _, id := range missing {
		loaded[id] = nil
	}
	articleTags := repositories.ArticleTagRepository.FindByArticleIds(sqls.DB(), missing, constants.StatusOk)
	if len(articleTags) > 0 {
		tagIds := make([]int64, 0, len(articleTags))
		seenTagIds := make(map[int64]struct{}, len(articleTags))
		for _, articleTag := range articleTags {
			if _, exists := seenTagIds[articleTag.TagId]; exists {
				continue
			}
			seenTagIds[articleTag.TagId] = struct{}{}
			tagIds = append(tagIds, articleTag.TagId)
		}

		tags := repositories.TagRepository.GetTagInIds(sqls.DB(), tagIds)
		tagsById := make(map[int64]models.Tag, len(tags))
		for _, tag := range tags {
			tagsById[tag.Id] = tag
		}
		for _, articleTag := range articleTags {
			if tag, exists := tagsById[articleTag.TagId]; exists {
				loaded[articleTag.ArticleId] = append(loaded[articleTag.ArticleId], tag)
			}
		}
	}
	if s.tagsCache != nil {
		s.tagsCache.putMany(missing, loaded)
	}
	for id, tags := range loaded {
		result[id] = cloneTags(tags)
	}
	return result
}

// 文章列表
func (s *articleService) GetArticles(cursor int64) (articles []models.Article, nextCursor int64, hasMore bool) {
	if cursor == 0 && s.listCache != nil {
		if cachedArticles, cachedCursor, cachedHasMore, ok := s.listCache.get(); ok {
			return cachedArticles, cachedCursor, cachedHasMore
		}
		s.listCache.loadMu.Lock()
		defer s.listCache.loadMu.Unlock()
		if cachedArticles, cachedCursor, cachedHasMore, ok := s.listCache.get(); ok {
			return cachedArticles, cachedCursor, cachedHasMore
		}
	}
	limit := 20
	query := sqls.DB().Model(&models.Article{}).Select(articleListSelectColumns).
		Where("status = ?", constants.StatusOk)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	_ = query.Order("id DESC").Limit(limit).Find(&articles).Error
	if len(articles) > 0 {
		nextCursor = articles[len(articles)-1].Id
		hasMore = len(articles) >= limit
	} else {
		nextCursor = cursor
	}
	if cursor == 0 && s.listCache != nil {
		s.listCache.put(articles, nextCursor, hasMore)
	}
	return
}

// 标签文章列表
func (s *articleService) GetTagArticles(tagId int64, cursor int64) (articles []models.Article, nextCursor int64, hasMore bool) {
	limit := 20
	cnd := sqls.NewCnd().Eq("tag_id", tagId).Eq("status", constants.StatusOk).Desc("id").Limit(limit)
	if cursor > 0 {
		cnd.Lt("id", cursor)
	}
	nextCursor = cursor
	articleTags := repositories.ArticleTagRepository.Find(sqls.DB(), cnd)
	if len(articleTags) > 0 {
		var articleIds []int64
		for _, articleTag := range articleTags {
			articleIds = append(articleIds, articleTag.ArticleId)
			nextCursor = articleTag.Id
		}
		for _, article := range s.GetArticleInIds(articleIds) {
			if article.Status == constants.StatusOk {
				articles = append(articles, article)
			}
		}
	}
	hasMore = len(articleTags) >= limit
	return
}

// 发布文章
func (s *articleService) Publish(userId int64, form req.CreateArticleReq) (article *models.Article, err error) {
	modules := SysConfigService.GetModules()
	if !modules.Article {
		return nil, errors.New(locales.Get("article.disabled"))
	}

	form.Title = strings.TrimSpace(form.Title)
	form.Summary = strings.TrimSpace(form.Summary)
	form.Content = strings.TrimSpace(form.Content)

	if strs.IsBlank(form.Title) {
		return nil, errors.New(locales.Get("article.title_required"))
	}
	if strs.IsBlank(form.Content) {
		return nil, errors.New(locales.Get("article.content_required"))
	}

	// 获取后台配置 否是开启发表文章审核
	status := constants.StatusOk
	if SysConfigService.IsArticlePending() {
		status = constants.StatusReview
	}

	if strs.IsBlank(form.Summary) {
		form.Summary = BuildArticleSummary(form.ContentType, form.Content)
	}

	article = &models.Article{
		UserId:      userId,
		Title:       form.Title,
		Summary:     form.Summary,
		Content:     form.Content,
		ContentType: form.ContentType,
		Status:      status,
		SourceUrl:   form.SourceUrl,
		CreateTime:  dates.NowTimestamp(),
		UpdateTime:  dates.NowTimestamp(),
	}

	if form.Cover != nil {
		article.Cover = jsons.ToJsonStr(form.Cover)
	}

	err = sqls.DB().Transaction(func(tx *gorm.DB) error {
		var (
			tagIds []int64
			err    error
		)
		if tagIds, err = repositories.TagRepository.GetOrCreates(tx, form.Tags); err != nil {
			return err
		}
		if err = repositories.ArticleRepository.Create(tx, article); err != nil {
			return err
		}
		return repositories.ArticleTagRepository.AddArticleTags(tx, article.Id, tagIds)
	})
	if err == nil {
		s.InvalidateListCache()
		search.UpdateArticleIndex(article)
	}

	return
}

// 修改文章
func (s *articleService) Edit(articleId int64, tags []string, title, content string, cover *req.ImageDTO) error {
	if len(title) == 0 {
		return errors.New(locales.Get("article.title_required"))
	}
	if len(content) == 0 {
		return errors.New(locales.Get("article.content_required"))
	}

	article := s.Get(articleId)
	if article == nil {
		return errors.New(locales.Get("article.not_found"))
	}

	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"title":   title,
			"summary": BuildArticleSummary(article.ContentType, content),
			"content": content,
		}
		if cover != nil {
			updates["cover"] = jsons.ToJsonStr(cover)
		} else {
			updates["cover"] = ""
		}
		err := repositories.ArticleRepository.Updates(tx, articleId, updates)
		if err != nil {
			return err
		}
		tagIds, err := repositories.TagRepository.GetOrCreates(tx, tags)
		if err != nil {
			return err
		}
		if err := repositories.ArticleTagRepository.DeleteArticleTags(tx, articleId); err != nil {
			return err
		}
		return repositories.ArticleTagRepository.AddArticleTags(tx, articleId, tagIds)
	})
	cache.ArticleTagCache.Invalidate(articleId)
	if s.tagsCache != nil {
		s.tagsCache.invalidate(articleId)
	}
	if err == nil {
		s.InvalidateListCache()
		search.UpdateArticleIndex(s.Get(articleId))
	}
	return err
}

func (s *articleService) PutTags(articleId int64, tags []string) error {
	err := sqls.DB().Transaction(func(tx *gorm.DB) error {
		tagIds, err := repositories.TagRepository.GetOrCreates(tx, tags)
		if err != nil {
			return err
		}
		if err := repositories.ArticleTagRepository.DeleteArticleTags(tx, articleId); err != nil {
			return err
		}
		return repositories.ArticleTagRepository.AddArticleTags(tx, articleId, tagIds)
	})
	if err != nil {
		return err
	}
	cache.ArticleTagCache.Invalidate(articleId)
	if s.tagsCache != nil {
		s.tagsCache.invalidate(articleId)
	}
	s.InvalidateListCache()
	search.UpdateArticleIndex(s.Get(articleId))
	return nil
}

// 倒序扫描
func (s *articleService) ScanDesc(callback func(articles []models.Article)) {
	var cursor int64 = math.MaxInt64
	for {
		logrus.Info("scan articles desc, cursor:" + cast.ToString(cursor))
		list := repositories.ArticleRepository.Find(sqls.DB(), sqls.NewCnd().
			Lt("id", cursor).Desc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

func (s *articleService) ScanByUser(userId int64, callback func(articles []models.Article)) {
	var cursor int64 = 0
	for {
		list := repositories.ArticleRepository.Find(sqls.DB(), sqls.NewCnd().
			Eq("user_id", userId).Gt("id", cursor).Asc("id").Limit(1000))
		if len(list) == 0 {
			break
		}
		cursor = list[len(list)-1].Id
		callback(list)
	}
}

// 浏览数+1
func (s *articleService) IncrViewCount(articleId int64) {
	ViewCountService.IncrArticle(articleId)
}

func (s *articleService) GetUserArticles(userId, cursor int64) (articles []models.Article, nextCursor int64, hasMore bool) {
	limit := 20
	query := sqls.DB().Model(&models.Article{}).Select(articleListSelectColumns).
		Where("status = ?", constants.StatusOk)
	if userId > 0 {
		query = query.Where("user_id = ?", userId)
	}
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	_ = query.Order("id DESC").Limit(limit).Find(&articles).Error
	if len(articles) > 0 {
		nextCursor = articles[len(articles)-1].Id
		hasMore = len(articles) >= limit
	} else {
		nextCursor = cursor
	}
	return
}
