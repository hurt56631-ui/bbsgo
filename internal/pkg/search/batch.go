package search

import (
	"errors"
	"html"

	"bbs-go/internal/models"
	"bbs-go/internal/models/constants"
	html2 "bbs-go/internal/pkg/html"
	"bbs-go/internal/pkg/markdown"
	"bbs-go/internal/pkg/text"
	"bbs-go/internal/repositories"

	bleve "github.com/blevesearch/bleve/v2"
	"github.com/mlogclub/simple/common/strs"
	"github.com/mlogclub/simple/sqls"
	"github.com/spf13/cast"
)

func newTopicDocument(topic *models.Topic, user *models.User, tags []models.Tag) *TopicDocument {
	if topic == nil {
		return nil
	}
	doc := &TopicDocument{
		Type:       EntityTypeTopic,
		Id:         topic.Id,
		CategoryId: topic.CategoryId,
		UserId:     topic.UserId,
		Title:      html.EscapeString(topic.Title),
		Status:     topic.Status,
		Recommend:  topic.Recommend,
		CreateTime: topic.CreateTime,
	}

	content := topic.Content
	if topic.ContentType == constants.ContentTypeMarkdown {
		content = markdown.ToHTML(content)
	}
	doc.Content = html.EscapeString(html2.GetHtmlText(content))
	if user != nil {
		doc.Nickname = html.EscapeString(user.Nickname)
	}
	for _, tag := range tags {
		doc.Tags = append(doc.Tags, tag.Name)
	}
	return doc
}

func newArticleDocument(article *models.Article, user *models.User, tags []models.Tag) *ArticleDocument {
	if article == nil {
		return nil
	}
	doc := &ArticleDocument{
		Type:       EntityTypeArticle,
		Id:         article.Id,
		UserId:     article.UserId,
		Title:      html.EscapeString(article.Title),
		Summary:    html.EscapeString(article.Summary),
		Status:     article.Status,
		CreateTime: article.CreateTime,
	}

	content := article.Content
	if article.ContentType == constants.ContentTypeMarkdown {
		content = markdown.ToHTML(content)
	}
	content = html.EscapeString(html2.GetHtmlText(content))
	doc.Content = content
	if strs.IsBlank(doc.Summary) {
		doc.Summary = text.GetSummary(content, constants.SummaryLen)
	}
	if user != nil {
		doc.Nickname = html.EscapeString(user.Nickname)
	}
	for _, tag := range tags {
		doc.Tags = append(doc.Tags, tag.Name)
	}
	return doc
}

func loadUsersByIDs(ids []int64) map[int64]models.User {
	unique := uniquePositiveIDs(ids)
	users := repositories.UserRepository.FindInfoByIds(sqls.DB(), unique)
	result := make(map[int64]models.User, len(users))
	for _, user := range users {
		result[user.Id] = user
	}
	return result
}

func loadTopicTagsByIDs(topicIDs []int64) map[int64][]models.Tag {
	result := make(map[int64][]models.Tag, len(topicIDs))
	relations := repositories.TopicTagRepository.FindByTopicIds(sqls.DB(), uniquePositiveIDs(topicIDs), constants.StatusOk)
	if len(relations) == 0 {
		return result
	}
	tagIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		tagIDs = append(tagIDs, relation.TagId)
	}
	tags := repositories.TagRepository.GetTagInIds(sqls.DB(), uniquePositiveIDs(tagIDs))
	tagsByID := make(map[int64]models.Tag, len(tags))
	for _, tag := range tags {
		tagsByID[tag.Id] = tag
	}
	for _, relation := range relations {
		if tag, ok := tagsByID[relation.TagId]; ok {
			result[relation.TopicId] = append(result[relation.TopicId], tag)
		}
	}
	return result
}

func loadArticleTagsByIDs(articleIDs []int64) map[int64][]models.Tag {
	result := make(map[int64][]models.Tag, len(articleIDs))
	relations := repositories.ArticleTagRepository.FindByArticleIds(sqls.DB(), uniquePositiveIDs(articleIDs), constants.StatusOk)
	if len(relations) == 0 {
		return result
	}
	tagIDs := make([]int64, 0, len(relations))
	for _, relation := range relations {
		tagIDs = append(tagIDs, relation.TagId)
	}
	tags := repositories.TagRepository.GetTagInIds(sqls.DB(), uniquePositiveIDs(tagIDs))
	tagsByID := make(map[int64]models.Tag, len(tags))
	for _, tag := range tags {
		tagsByID[tag.Id] = tag
	}
	for _, relation := range relations {
		if tag, ok := tagsByID[relation.TagId]; ok {
			result[relation.ArticleId] = append(result[relation.ArticleId], tag)
		}
	}
	return result
}

func uniquePositiveIDs(ids []int64) []int64 {
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// IndexTopicBatch builds all author/tag relations in fixed query counts and
// commits one Bleve batch instead of one disk write per document.
func IndexTopicBatch(topics []models.Topic) error {
	if len(topics) == 0 {
		return nil
	}
	idx := currentIndex()
	if idx == nil {
		return errors.New("search index unavailable")
	}
	userIDs := make([]int64, 0, len(topics))
	topicIDs := make([]int64, 0, len(topics))
	for _, topic := range topics {
		if topic.Status != constants.StatusOk {
			continue
		}
		userIDs = append(userIDs, topic.UserId)
		topicIDs = append(topicIDs, topic.Id)
	}
	users := loadUsersByIDs(userIDs)
	tags := loadTopicTagsByIDs(topicIDs)
	batch := idx.NewBatch()
	for i := range topics {
		topic := &topics[i]
		if topic.Status != constants.StatusOk {
			batch.Delete(searchDocID(EntityTypeTopic, topic.Id))
			batch.Delete(cast.ToString(topic.Id))
			continue
		}
		doc := newTopicDocument(topic, userPtr(users, topic.UserId), tags[topic.Id])
		if doc != nil {
			if err := batch.Index(searchDocID(EntityTypeTopic, topic.Id), doc); err != nil {
				return err
			}
		}
	}
	return commitBatch(idx, batch)
}

func IndexArticleBatch(articles []models.Article) error {
	if len(articles) == 0 {
		return nil
	}
	idx := currentIndex()
	if idx == nil {
		return errors.New("search index unavailable")
	}
	userIDs := make([]int64, 0, len(articles))
	articleIDs := make([]int64, 0, len(articles))
	for _, article := range articles {
		if article.Status != constants.StatusOk {
			continue
		}
		userIDs = append(userIDs, article.UserId)
		articleIDs = append(articleIDs, article.Id)
	}
	users := loadUsersByIDs(userIDs)
	tags := loadArticleTagsByIDs(articleIDs)
	batch := idx.NewBatch()
	for i := range articles {
		article := &articles[i]
		if article.Status != constants.StatusOk {
			batch.Delete(searchDocID(EntityTypeArticle, article.Id))
			continue
		}
		doc := newArticleDocument(article, userPtr(users, article.UserId), tags[article.Id])
		if doc != nil {
			if err := batch.Index(searchDocID(EntityTypeArticle, article.Id), doc); err != nil {
				return err
			}
		}
	}
	return commitBatch(idx, batch)
}

func IndexUserBatch(users []models.User) error {
	if len(users) == 0 {
		return nil
	}
	idx := currentIndex()
	if idx == nil {
		return errors.New("search index unavailable")
	}
	batch := idx.NewBatch()
	for i := range users {
		if users[i].Status != constants.StatusOk {
			batch.Delete(searchDocID(EntityTypeUser, users[i].Id))
			continue
		}
		doc := NewUserDoc(&users[i])
		if doc != nil {
			if err := batch.Index(searchDocID(EntityTypeUser, users[i].Id), doc); err != nil {
				return err
			}
		}
	}
	return commitBatch(idx, batch)
}

func userPtr(users map[int64]models.User, id int64) *models.User {
	user, ok := users[id]
	if !ok {
		return nil
	}
	copy := user
	return &copy
}

func commitBatch(idx bleve.Index, batch *bleve.Batch) error {
	if batch == nil {
		return nil
	}
	indexWriteMu.Lock()
	defer indexWriteMu.Unlock()
	return idx.Batch(batch)
}
